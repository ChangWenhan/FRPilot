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
	ScanModeQuick   = "quick"    // ClamAV 快速：常用目录
	ScanModeFull    = "full"     // ClamAV 全盘：/
	ScanModeRootkit = "rootkit"  // rkhunter + chkrootkit
)

var scanModeLabels = map[string]string{
	ScanModeQuick:   "ClamAV 快速扫描",
	ScanModeFull:    "ClamAV 全盘扫描",
	ScanModeRootkit: "Rootkit 检查",
}

var clamInfectedRe = regexp.MustCompile(`^(.+?):\s*(FOUND|ERROR)(\.\d+)?\s*$`)

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
	_ = tm.db.Log(opID, opName, "scan_start", fmt.Sprintf("机器 %d 台", len(machines)), "模式: "+scanModeLabels[mode])

	go func() {
		for _, m := range machines {
			res := tm.scanMachine(m, mode)
			tm.mu.Lock()
			task.Results = append(task.Results, res)
			tm.mu.Unlock()
		}
		tm.mu.Lock()
		task.Status = "done"
		tm.mu.Unlock()
		_ = tm.db.Log(opID, opName, "scan_done", fmt.Sprintf("机器 %d 台", len(machines)), "模式: "+scanModeLabels[mode])
	}()
	return task, nil
}

// scanMachine 对单台机器执行扫描，返回结构化结果。
func (tm *TaskManager) scanMachine(m *store.Machine, mode string) *TaskResult {
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
	conn, err := sshx.Dial(host, m.TunnelPort, m.SSHUser, pass)
	if err != nil {
		res.Status = "failed"
		res.Output = fmt.Sprintf("SSH 连接失败: %v", err)
		return res
	}
	defer conn.Close()

	cmd, timeout := scanCommand(mode)
	out, errOut, err := conn.RunSudo(cmd, sudoPass, timeout)
	res.Duration = time.Since(start).Round(10 * time.Millisecond).String()
	combined := out
	if errOut != "" {
		combined += "\n" + errOut
	}
	res.Output = summarizeScan(mode, combined)
	if err != nil {
		res.Status = "failed"
		res.Output = fmt.Sprintf("扫描被中断（可能超时）: %v\n%s", err, res.Output)
		return res
	}
	res.Status = "ok"
	return res
}

// scanCommand 构造远端扫描命令与超时。
func scanCommand(mode string) (string, time.Duration) {
	switch mode {
	case ScanModeQuick:
		return `command -v clamscan >/dev/null 2>&1 && clamscan -r -i --no-banner --max-filesize=128M --max-scansize=512M /etc /home /tmp /opt /usr/local /root /var/www 2>&1 || echo "__NO_CLAMAV__"`, 30 * time.Minute
	case ScanModeFull:
		return `command -v clamscan >/dev/null 2>&1 && clamscan -r -i --no-banner --exclude-dir=/proc --exclude-dir=/sys --exclude-dir=/dev --exclude-dir=/run / 2>&1 || echo "__NO_CLAMAV__"`, 4 * time.Hour
	default: // rootkit
		return `out=""; if command -v rkhunter >/dev/null 2>&1; then out="$out
[RKHUNTER]
$(rkhunter --check --skip-keypress --nocolors --no-version-check 2>&1)"; fi; if command -v chkrootkit >/dev/null 2>&1; then out="$out
[CHKROOTKIT]
$(chkrootkit 2>&1)"; fi; [ -n "$out" ] && echo "$out" || echo "__NO_SCANNER__"`, 20 * time.Minute
	}
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
	default: // rootkit
		if strings.Contains(raw, "__NO_SCANNER__") {
			return "未检测到 rkhunter / chkrootkit"
		}
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
