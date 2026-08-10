package actions

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"frpmon/internal/sshx"
	"frpmon/internal/store"
)

// 病毒扫描模式
const (
	ScanModeQuick   = "quick"   // ClamAV 快速：常用目录
	ScanModeFull    = "full"    // ClamAV 全盘：/
	ScanModeRootkit = "rootkit" // rkhunter + chkrootkit
	ScanModeUpdate  = "update"  // freshclam 更新 ClamAV 病毒库
)

var scanModeLabels = map[string]string{
	ScanModeQuick:   "ClamAV 快速扫描",
	ScanModeFull:    "ClamAV 全盘扫描",
	ScanModeRootkit: "Rootkit 检查",
	ScanModeUpdate:  "更新病毒库",
}

var clamInfectedRe = regexp.MustCompile(`^(.+?):\s*(FOUND|ERROR)(\.\d+)?\s*$`)

const (
	quickPerDirTimeout  = 8 * time.Minute  // 快速模式：单目录超时
	fullPerDirTimeout   = 30 * time.Minute // 全盘模式：单目录超时
	rootkitPhaseTimeout = 10 * time.Minute
	updateTimeout       = 10 * time.Minute // 病毒库更新超时（freshclam 可能下载大文件）
)

// scanPhase 单台机器内的一个扫描阶段（一个 SSH 命令）。
type scanPhase struct {
	label   string
	cmd     string
	timeout time.Duration
	// marker 该阶段工具缺失时的输出标记；命中后跳过后续阶段。
	marker string
}

// guardedCmd 包裹远端命令：仅当工具存在时才执行扫描，缺失时输出 marker。
// 命令一律以 0 退出，避免工具自身的非零退出码（clamscan: 1=发现病毒/2=扫描出错，
// rkhunter: 1=发现警告/2=出错）被误判为"工具缺失"或"命令中断/超时"。
func guardedCmd(check, run, marker string) string {
	return fmt.Sprintf("if %s >/dev/null 2>&1; then %s 2>&1; else echo '%s'; fi; exit 0", check, run, marker)
}

// scanPhases 按模式构造扫描阶段序列（逐目录执行，便于实时进度）。
func scanPhases(mode string) []scanPhase {
	switch mode {
	case ScanModeQuick:
		dirs := []string{"/etc", "/home", "/tmp", "/opt", "/usr/local", "/root", "/var/www"}
		return dirPhases(dirs, quickPerDirTimeout)
	case ScanModeFull:
		dirs := []string{"/bin", "/boot", "/etc", "/home", "/lib", "/lib64", "/media", "/mnt",
			"/opt", "/root", "/sbin", "/srv", "/tmp", "/usr", "/var"}
		return dirPhases(dirs, fullPerDirTimeout)
	default:
		return []scanPhase{
			{
				label:   "rkhunter 检查",
				cmd:     guardedCmd(`command -v rkhunter`, `rkhunter --check --skip-keypress --nocolors --no-version-check`, "__NO_RKHUNTER__"),
				timeout: rootkitPhaseTimeout,
				marker:  "__NO_RKHUNTER__",
			},
			{
				label:   "chkrootkit 检查",
				cmd:     guardedCmd(`command -v chkrootkit`, `chkrootkit`, "__NO_CHKROOTKIT__"),
				timeout: rootkitPhaseTimeout,
				marker:  "__NO_CHKROOTKIT__",
			},
		}
	case ScanModeUpdate:
		return []scanPhase{
			{
				// 用独立日志文件避免与 freshclam 守护进程争抢 /var/log/clamav/freshclam.log 的锁
				label:   "更新 ClamAV 病毒库",
				cmd:     guardedCmd(`command -v freshclam`, `freshclam --stdout -l /tmp/frpm-freshclam.log; rm -f /tmp/frpm-freshclam.log`, "__NO_FRESHCLAM__"),
				timeout: updateTimeout,
				marker:  "__NO_FRESHCLAM__",
			},
		}
	}
}

func dirPhases(dirs []string, perDirTimeout time.Duration) []scanPhase {
	phases := make([]scanPhase, 0, len(dirs))
	for i, d := range dirs {
		// 注意：不要加 --no-banner，ClamAV >= 1.5 已移除该选项（未知选项导致退出码 2）。
		phases = append(phases, scanPhase{
			label:   fmt.Sprintf("扫描 %s（%d/%d）", d, i+1, len(dirs)),
			cmd:     guardedCmd(`command -v clamscan`, fmt.Sprintf(`clamscan -r -i --max-filesize=128M --max-scansize=512M %s`, d), "__NO_CLAMAV__"),
			timeout: perDirTimeout,
			marker:  "__NO_CLAMAV__",
		})
	}
	return phases
}

// StartScan 创建并后台执行病毒扫描任务（复用清理任务的异步机制）。
// mode: quick（ClamAV 常用目录）| full（ClamAV 全盘）| rootkit（rkhunter/chkrootkit）
func (tm *TaskManager) StartScan(machineIDs []int64, mode string, opID int64, opName string) (*Task, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if _, ok := scanModeLabels[mode]; !ok {
		return nil, fmt.Errorf("未知扫描模式: %s（可用 quick / full / rootkit）", mode)
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
	if len(machines) == 0 {
		return nil, fmt.Errorf("请选择至少一台机器")
	}

	task := tm.newTask("scan", opID, opName)
	task.Progress = &TaskProgress{
		TotalMachines: len(machines),
		Current:       machines[0].Name,
		Phase:         "准备中",
	}
	_ = tm.db.Log(opID, opName, "scan_start", fmt.Sprintf("机器 %d 台", len(machines)), "模式: "+scanModeLabels[mode])

	go func() {
		for _, m := range machines {
			report := func(label string, machineFrac float64) {
				tm.mu.Lock()
				defer tm.mu.Unlock()
				p := task.Progress
				if p == nil {
					return
				}
				p.Current = m.Name
				p.Phase = label
				pct := (float64(p.DoneMachines) + machineFrac) / float64(p.TotalMachines) * 100
				if pct > 99 && p.DoneMachines < p.TotalMachines {
					pct = 99 // 最后一台完成前不满 100%
				}
				p.Pct = int(pct)
			}
			res := tm.scanMachine(m, mode, report)
			tm.mu.Lock()
			task.Results = append(task.Results, res)
			if task.Progress != nil {
				task.Progress.DoneMachines++
				task.Progress.Pct = int(float64(task.Progress.DoneMachines) / float64(task.Progress.TotalMachines) * 100)
			}
			tm.mu.Unlock()
		}
		tm.mu.Lock()
		task.Status = "done"
		tm.mu.Unlock()
		_ = tm.db.Log(opID, opName, "scan_done", fmt.Sprintf("机器 %d 台", len(machines)), "模式: "+scanModeLabels[mode])
	}()
	return task, nil
}

// scanMachine 对单台机器分阶段执行扫描；report 每完成一个阶段回调一次进度。
func (tm *TaskManager) scanMachine(m *store.Machine, mode string, report func(label string, machineFrac float64)) *TaskResult {
	start := time.Now()
	res := &TaskResult{MachineID: m.ID, Machine: m.Name, ItemID: mode, ItemName: scanModeLabels[mode]}

	pass, err := tm.db.DecryptSecret(m.SSHPassEnc)
	if err != nil {
		res.Status = "failed"
		res.Output = "解密 SSH 凭据失败: " + err.Error()
		return res
	}
	sudoPass := ""
	if m.SSHUser != "root" && m.SudoPassEnc != "" {
		sudoPass, err = tm.db.DecryptSecret(m.SudoPassEnc)
		if err != nil {
			res.Status = "failed"
			res.Output = "解密 sudo 密码失败: " + err.Error()
			return res
		}
	}
	host := tm.cfg.Get().Frps.SSHHost
	if host == "" {
		host = "127.0.0.1"
	}
	report("正在连接并检测扫描工具", 0)
	conn, err := sshx.Dial(host, m.TunnelPort, m.SSHUser, pass)
	if err != nil {
		res.Status = "failed"
		res.Output = fmt.Sprintf("SSH 连接失败: %v", err)
		return res
	}
	defer conn.Close()

	phases := scanPhases(mode)
	noTool := ""
	var raws []string
	for i, ph := range phases {
		report(ph.label, float64(i)/float64(len(phases)))
		out, errOut, err := conn.RunSudo(ph.cmd, sudoPass, ph.timeout)
		raw := out
		if errOut != "" {
			raw += "\n" + errOut
		}
		raws = append(raws, raw)
		if strings.Contains(raw, ph.marker) {
			noTool = ph.marker
			break
		}
		if err != nil {
			res.Status = "failed"
			res.Output = fmt.Sprintf("扫描被中断（可能超时）: %v\n%s", err, summarizeScan(mode, strings.Join(raws, "\n")))
			res.Duration = time.Since(start).Round(10 * time.Millisecond).String()
			return res
		}
	}
	res.Duration = time.Since(start).Round(10 * time.Millisecond).String()
	res.Output = summarizeScan(mode, strings.Join(raws, "\n"))
	if noTool != "" {
		res.Status = "skipped"
		res.Output = toolMissingNote(noTool)
		return res
	}
	res.Status = "ok"
	return res
}

func toolMissingNote(marker string) string {
	switch marker {
	case "__NO_CLAMAV__":
		return "未检测到 ClamAV（apt install clamav clamav-daemon 后重试）"
	case "__NO_RKHUNTER__":
		return "未检测到 rkhunter"
	case "__NO_CHKROOTKIT__":
		return "未检测到 chkrootkit"
	case "__NO_FRESHCLAM__":
		return "未检测到 freshclam（apt install clamav 后重试）"
	}
	return "未检测到对应扫描工具"
}

// summarizeScan 从原始输出中提炼摘要：威胁清单 + 统计。
func summarizeScan(mode, raw string) string {
	switch mode {
	case ScanModeQuick, ScanModeFull:
		if strings.Contains(raw, "__NO_CLAMAV__") {
			return "未检测到 ClamAV（apt install clamav clamav-daemon 后重试）"
		}
		var infected []string
		for _, line := range strings.Split(raw, "\n") {
			l := strings.TrimSpace(line)
			if m := clamInfectedRe.FindStringSubmatch(l); m != nil {
				infected = append(infected, l)
			}
		}
		var sb strings.Builder
		sb.WriteString("ClamAV 扫描完成\n")
		for _, key := range []string{"Known viruses:", "Engine version:", "Scanned directories:", "Scanned files:", "Infected files:", "Data scanned:", "Time:"} {
			for _, line := range strings.Split(raw, "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), key) {
					sb.WriteString(strings.TrimSpace(line) + "\n")
				}
			}
		}
		if len(infected) > 0 {
			sb.WriteString(fmt.Sprintf("\n发现 %d 个威胁:\n", len(infected)))
			for i, f := range infected {
				if i >= 50 {
					sb.WriteString(fmt.Sprintf("... 其余 %d 条已省略\n", len(infected)-i))
					break
				}
				sb.WriteString(f + "\n")
			}
		} else {
			sb.WriteString("\n未发现威胁\n")
		}
		return sb.String()
	case ScanModeUpdate:
		if strings.Contains(raw, "__NO_FRESHCLAM__") {
			return "未检测到 freshclam（apt install clamav 后重试）"
		}
		var sb strings.Builder
		sb.WriteString("ClamAV 病毒库更新\n")
		if strings.Contains(raw, "Failed to lock") || strings.Contains(raw, "Resource temporarily unavailable") {
			sb.WriteString("提示：freshclam 守护进程（clamav-freshclam 服务）正在更新病毒库，本次未能立即执行；守护进程默认每 2 小时自动更新一次，请稍后重试\n")
		}
		matched := false
		for _, line := range strings.Split(raw, "\n") {
			l := strings.TrimSpace(line)
			if l == "" {
				continue
			}
			for _, k := range []string{"Database updated", "already up-to-date", "OUTDATED",
				"Current database version", "Database version", "Downloading", "bytecode",
				"Checking availability", "Update process", "Last update"} {
				if strings.Contains(l, k) {
					sb.WriteString(l + "\n")
					matched = true
					break
				}
			}
		}
		if !matched {
			// 兜底：未解析到关键输出时展示原始输出尾部
			sb.WriteString("（未解析到关键输出，原始输出尾部如下）\n")
			lines := strings.Split(raw, "\n")
			start := len(lines) - 8
			if start < 0 {
				start = 0
			}
			for _, l := range lines[start:] {
				sb.WriteString(strings.TrimSpace(l) + "\n")
			}
		}
		return sb.String()
	default: // rootkit
		var warns []string
		for _, line := range strings.Split(raw, "\n") {
			l := strings.TrimSpace(line)
			if strings.Contains(l, "INFECTED") || strings.Contains(l, "[ Warning ]") || strings.HasPrefix(l, "Warning:") {
				warns = append(warns, l)
			}
		}
		var sb strings.Builder
		sb.WriteString("Rootkit 检查完成（rkhunter / chkrootkit）\n")
		if len(warns) > 0 {
			sb.WriteString(fmt.Sprintf("\n发现 %d 个警告/感染项:\n", len(warns)))
			for i, w := range warns {
				if i >= 50 {
					sb.WriteString(fmt.Sprintf("... 其余 %d 条已省略\n", len(warns)-i))
					break
				}
				sb.WriteString(w + "\n")
			}
		} else {
			sb.WriteString("\n未发现异常\n")
		}
		return sb.String()
	}
}
