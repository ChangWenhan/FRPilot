package actions

import (
	"fmt"
	"sync"
	"time"

	"frpmon/internal/config"
	"frpmon/internal/sshx"
	"frpmon/internal/store"
)

// CleanupItem 清理命令项（白名单概念：只能执行预设命令）。
type CleanupItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Desc        string `json:"desc"`
	Risk        string `json:"risk"` // low | mid | high
	RequiresRoot bool  `json:"requiresRoot"`
	Command     string `json:"command"`
	PreviewCmd  string `json:"previewCmd"` // dry-run 预估命令
	Timeout     int    `json:"timeoutSec"`
}

// DefaultCleanupItems 内置清理命令集。
func DefaultCleanupItems() []CleanupItem {
	return []CleanupItem{
		{
			ID: "page_cache", Name: "释放内存页缓存", Risk: "low", RequiresRoot: true,
			Desc:        "sync 并清空 page cache（不影响文件内容）",
			Command:     "sync; echo 3 > /proc/sys/vm/drop_caches; sync; free -m | head -2",
			PreviewCmd:  "free -m | head -2",
			Timeout:     300, // sync 刷盘在大内存/繁忙机器上可能超过 30s，超时会以 SIGHUP(129) 杀死命令
		},
		{
			ID: "apt_cache", Name: "清理 APT 缓存", Risk: "low", RequiresRoot: true,
			Desc:        "apt-get clean 清除下载的 .deb 包缓存",
			Command:     "du -sh /var/cache/apt/archives 2>/dev/null; apt-get clean 2>&1; echo '--- 清理后 ---'; du -sh /var/cache/apt/archives 2>/dev/null",
			PreviewCmd:  "du -sh /var/cache/apt/archives 2>/dev/null",
			Timeout:     120,
		},
		{
			ID: "journal", Name: "压缩系统日志", Risk: "mid", RequiresRoot: true,
			Desc:        "journalctl vacuum 至 100M",
			Command:     "journalctl --disk-usage 2>/dev/null; journalctl --vacuum-size=100M 2>&1 | tail -3",
			PreviewCmd:  "journalctl --disk-usage 2>/dev/null",
			Timeout:     120,
		},
		{
			ID: "user_cache", Name: "清理用户缓存", Risk: "mid", RequiresRoot: false,
			Desc:        "删除各登录用户 ~/.cache 内容（pip/npm/临时缓存）",
			Command:     "du -sh /home/*/.cache 2>/dev/null; rm -rf /home/*/.cache/* 2>/dev/null; echo done",
			PreviewCmd:  "du -sh /home/*/.cache 2>/dev/null",
			Timeout:     120,
		},
		{
			ID: "tmp_files", Name: "清理 /tmp 临时文件", Risk: "high", RequiresRoot: true,
			Desc:        "删除 /tmp 下的临时文件（正在使用的文件不受影响）",
			Command:     "du -sh /tmp 2>/dev/null; find /tmp -type f -atime +1 -delete 2>/dev/null; find /tmp -type d -empty -delete 2>/dev/null; echo done",
			PreviewCmd:  "du -sh /tmp 2>/dev/null",
			Timeout:     120,
		},
	}
}

// Task 一次清理执行任务。
type Task struct {
	ID         int64         `json:"id"`
	Type       string        `json:"type"` // cleanup
	Status     string        `json:"status"`
	CreatedAt  time.Time     `json:"createdAt"`
	OperatorID int64         `json:"operatorId"`
	Operator   string        `json:"operator"`
	Results    []*TaskResult `json:"results"`
	Err        string        `json:"err,omitempty"`
}

type TaskResult struct {
	MachineID int64  `json:"machineId"`
	Machine   string `json:"machine"`
	ItemID    string `json:"itemId"`
	ItemName  string `json:"itemName"`
	Status    string `json:"status"` // ok | failed | skipped
	Output    string `json:"output"`
	Duration  string `json:"duration"`
}

// TaskManager 内存任务管理器（结果保留最近 N 个；审计日志持久化留痕）。
type TaskManager struct {
	db  *store.DB
	cfg *config.Manager
	mu  sync.Mutex
	tasks  map[int64]*Task
	nextID int64
}

func NewTaskManager(db *store.DB, cfg *config.Manager) *TaskManager {
	return &TaskManager{db: db, cfg: cfg, tasks: map[int64]*Task{}, nextID: 1}
}

// StartCleanup 创建并后台执行清理任务。
// machineIDs: 目标机器；itemIDs: 清理项（空=全部）
func (tm *TaskManager) StartCleanup(machineIDs []int64, itemIDs []string, opID int64, opName string) (*Task, error) {
	items := tm.effectiveItems()
	if len(itemIDs) > 0 {
		var filtered []CleanupItem
		for _, it := range items {
			for _, id := range itemIDs {
				if it.ID == id {
					filtered = append(filtered, it)
				}
			}
		}
		items = filtered
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("没有可执行的清理项")
	}
	var machines []*store.Machine
	for _, id := range machineIDs {
		m, err := tm.db.GetMachineByID(id)
		if err != nil {
			return nil, fmt.Errorf("机器 %d 不存在: %w", id, err)
		}
		if m.SSHUser == "" {
			return nil, fmt.Errorf("机器 %s 未配置 SSH 凭据", m.Name)
		}
		machines = append(machines, m)
	}

	tm.mu.Lock()
	task := &Task{
		ID: tm.nextID, Type: "cleanup", Status: "running",
		CreatedAt: time.Now(), OperatorID: opID, Operator: opName,
	}
	tm.nextID++
	tm.tasks[task.ID] = task
	// 内存上限：最多保留最近 50 个任务，防止无界增长
	if len(tm.tasks) > 50 {
		var oldest int64
		for id := range tm.tasks {
			if oldest == 0 || id < oldest {
				oldest = id
			}
		}
		delete(tm.tasks, oldest)
	}
	tm.mu.Unlock()

	names := make([]string, 0, len(items))
	for _, it := range items {
		names = append(names, it.Name)
	}
	_ = tm.db.Log(opID, opName, "cleanup_start", fmt.Sprintf("机器 %d 台", len(machines)),
		fmt.Sprintf("项目: %s", join(names)))

	go func() {
		for _, m := range machines {
			res, err := tm.runOnMachine(m, items)
			if err != nil {
				tm.mu.Lock()
				task.Results = append(task.Results, &TaskResult{
					MachineID: m.ID, Machine: m.Name, Status: "failed",
					ItemName: "连接", Output: err.Error(),
				})
				tm.mu.Unlock()
				continue
			}
			tm.mu.Lock()
			task.Results = append(task.Results, res...)
			tm.mu.Unlock()
		}
		tm.mu.Lock()
		task.Status = "done"
		failN := 0
		for _, r := range task.Results {
			if r.Status != "ok" {
				failN++
			}
		}
		tm.mu.Unlock()
		_ = tm.db.Log(opID, opName, "cleanup_done", fmt.Sprintf("机器 %d 台", len(machines)),
			fmt.Sprintf("完成 %d 项，失败/跳过 %d 项", len(task.Results)-failN, failN))
	}()
	return task, nil
}

func (tm *TaskManager) runOnMachine(m *store.Machine, items []CleanupItem) ([]*TaskResult, error) {
	pass, err := tm.db.DecryptSecret(m.SSHPassEnc)
	if err != nil {
		return nil, err
	}
	sudoPass := ""
	if m.SSHUser != "root" && m.SudoPassEnc != "" {
		sudoPass, err = tm.db.DecryptSecret(m.SudoPassEnc)
		if err != nil {
			return nil, err
		}
	}
	host := tm.cfg.Get().Frps.SSHHost
	if host == "" {
		host = "127.0.0.1"
	}
	conn, err := sshx.Dial(host, m.TunnelPort, m.SSHUser, pass)
	if err != nil {
		return nil, fmt.Errorf("SSH 连接失败: %w", err)
	}
	defer conn.Close()

	var results []*TaskResult
	for _, it := range items {
		res := &TaskResult{MachineID: m.ID, Machine: m.Name, ItemID: it.ID, ItemName: it.Name}
		start := time.Now()
		// 非 root 用户执行需要 root 的项：配置了 sudo 密码则提升执行，否则跳过（不视为错误）
		if it.RequiresRoot && m.SSHUser != "root" {
			if sudoPass == "" {
				res.Status = "skipped"
				res.Output = "需要 root 权限，当前 SSH 用户: " + m.SSHUser + "（未配置 sudo 密码）"
			} else {
				out, errOut, err := conn.RunSudo(it.Command, sudoPass, time.Duration(it.Timeout)*time.Second)
				if err != nil {
					res.Status = "failed"
					res.Output = truncate(out+"\n"+errOut+"\n"+err.Error(), 2000)
				} else {
					res.Status = "ok"
					res.Output = truncate(out, 2000)
				}
			}
		} else {
			out, errOut, err := conn.Run(it.Command, time.Duration(it.Timeout)*time.Second)
			if err != nil {
				res.Status = "failed"
				res.Output = truncate(out + "\n" + errOut + "\n" + err.Error(), 2000)
			} else {
				res.Status = "ok"
				res.Output = truncate(out, 2000)
			}
		}
		res.Duration = time.Since(start).Round(10 * time.Millisecond).String()
		results = append(results, res)
	}
	return results, nil
}

// effectiveItems 内置 + 自定义清理项。
func (tm *TaskManager) effectiveItems() []CleanupItem {
	items := DefaultCleanupItems()
	for _, c := range tm.cfg.Get().CleanupCustom {
		risk := c.Risk
		if risk == "" {
			risk = "mid"
		}
		items = append(items, CleanupItem{
			ID: "custom_" + c.Name, Name: c.Name, Desc: c.Desc,
			Risk: risk, Command: c.Command, PreviewCmd: "", Timeout: 120,
		})
	}
	return items
}

// ListItems 返回全部可用清理项。
func (tm *TaskManager) ListItems() []CleanupItem { return tm.effectiveItems() }

// Preview 执行 dry-run：在目标机器上运行各项的预估命令。
func (tm *TaskManager) Preview(machineID int64, itemIDs []string) ([]*TaskResult, error) {
	m, err := tm.db.GetMachineByID(machineID)
	if err != nil {
		return nil, err
	}
	if m.SSHUser == "" {
		return nil, fmt.Errorf("机器 %s 未配置 SSH 凭据", m.Name)
	}
	pass, err := tm.db.DecryptSecret(m.SSHPassEnc)
	if err != nil {
		return nil, err
	}
	sudoPass := ""
	if m.SSHUser != "root" && m.SudoPassEnc != "" {
		sudoPass, err = tm.db.DecryptSecret(m.SudoPassEnc)
		if err != nil {
			return nil, err
		}
	}
	host := tm.cfg.Get().Frps.SSHHost
	if host == "" {
		host = "127.0.0.1"
	}
	conn, err := sshx.Dial(host, m.TunnelPort, m.SSHUser, pass)
	if err != nil {
		return nil, fmt.Errorf("SSH 连接失败: %w", err)
	}
	defer conn.Close()

	items := tm.effectiveItems()
	if len(itemIDs) > 0 {
		var filtered []CleanupItem
		for _, it := range items {
			for _, id := range itemIDs {
				if it.ID == id {
					filtered = append(filtered, it)
				}
			}
		}
		items = filtered
	}
	var results []*TaskResult
	for _, it := range items {
		res := &TaskResult{MachineID: m.ID, Machine: m.Name, ItemID: it.ID, ItemName: it.Name}
		if it.PreviewCmd == "" {
			res.Status = "skipped"
			res.Output = "无预览信息"
			results = append(results, res)
			continue
		}
		previewCmd := it.PreviewCmd
		if it.RequiresRoot && m.SSHUser != "root" {
			if sudoPass == "" {
				res.Status = "skipped"
				res.Output = "需要 root 权限，当前 SSH 用户: " + m.SSHUser + "（未配置 sudo 密码）"
				results = append(results, res)
				continue
			}
			out, _, err := conn.RunSudo(previewCmd, sudoPass, 30*time.Second)
			if err != nil {
				res.Status = "failed"
				res.Output = truncate(out+"\n"+err.Error(), 2000)
			} else {
				res.Status = "ok"
				res.Output = truncate(out, 2000)
			}
			results = append(results, res)
			continue
		}
		out, _, err := conn.Run(previewCmd, 30*time.Second)
		if err != nil {
			res.Status = "failed"
			res.Output = truncate(err.Error(), 500)
		} else {
			res.Status = "ok"
			res.Output = truncate(out, 1000)
		}
		results = append(results, res)
	}
	return results, nil
}

func (tm *TaskManager) ListTasks(limit int) []*Task {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	ids := make([]int64, 0, len(tm.tasks))
	for id := range tm.tasks {
		ids = append(ids, id)
	}
	// 按 id 倒序
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[j] > ids[i] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	out := make([]*Task, 0, limit)
	for _, id := range ids {
		if len(out) >= limit {
			break
		}
		out = append(out, tm.tasks[id])
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n...(截断)"
}

func join(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += "、"
		}
		out += s
	}
	return out
}
