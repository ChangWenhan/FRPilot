package collector

import (
	"strconv"
	"strings"
)

type GPUInfo struct {
	Present  bool    `json:"present"`
	Name     string  `json:"name"`
	Util     float64 `json:"util"`
	MemUsed  int64   `json:"memUsedMiB"`
	MemTotal int64   `json:"memTotalMiB"`
	Temp     float64 `json:"tempC"`
	Power    float64 `json:"powerW"`
}

// ParseGPU 解析 nvidia-smi 输出；NO_GPU 标记或空输出表示未安装/不支持 GPU。
func ParseGPU(text string) *GPUInfo {
	text = strings.TrimSpace(text)
	if text == "" || strings.Contains(text, "NO_GPU") || strings.Contains(text, "not found") || strings.Contains(text, "No devices") {
		return &GPUInfo{Present: false}
	}
	// 格式: NVIDIA GeForce RTX 4090, 23, 5018, 24564, 42, 156.50
	line := strings.SplitN(text, "\n", 2)[0]
	parts := strings.Split(line, ",")
	if len(parts) < 6 {
		return &GPUInfo{Present: false}
	}
	g := &GPUInfo{Present: true, Name: strings.TrimSpace(parts[0])}
	g.Util, _ = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	g.MemUsed, _ = strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
	g.MemTotal, _ = strconv.ParseInt(strings.TrimSpace(parts[3]), 10, 64)
	g.Temp, _ = strconv.ParseFloat(strings.TrimSpace(parts[4]), 64)
	g.Power, _ = strconv.ParseFloat(strings.TrimSpace(parts[5]), 64)
	return g
}

type SecurityItem struct {
	Name     string `json:"name"`
	Installed bool  `json:"installed"`
	Active   string `json:"active"`  // active | inactive | unknown
	Version  string `json:"version"`
	Detail   string `json:"detail"`
	Warn     string `json:"warn"` // 未安装/异常提示（不视为错误）
}

// ParseSecurity 解析安全软件批量探测输出。
// 输出协议（每行）：
//   ACTIVE <svc>|<name>        —— systemctl is-active 结果
//   VER <name> <text>           —— 版本/病毒库信息
//   COUNT <name> <number>       —— 计数器（如 fail2ban 封禁数）
//   EXTRA <name> <text>         —— 附加文本
//   MISSING <name>              —— 未安装
func ParseSecurity(text string) []*SecurityItem {
	items := map[string]*SecurityItem{}
	order := []string{}
	get := func(name string) *SecurityItem {
		if it, ok := items[name]; ok {
			return it
		}
		it := &SecurityItem{Name: name, Installed: true, Active: "unknown"}
		items[name] = it
		order = append(order, name)
		return it
	}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 2 {
			continue
		}
		verb := parts[0]
		switch verb {
		case "ACTIVE":
			kv := strings.SplitN(parts[1], "|", 2)
			if len(kv) < 2 {
				continue
			}
			it := get(kv[0])
			it.Active = kv[1]
		case "VER", "EXTRA":
			if len(parts) < 3 {
				continue
			}
			get(parts[1]).Version = parts[2]
		case "COUNT":
			if len(parts) < 3 {
				continue
			}
			get(parts[1]).Detail = parts[2]
		case "MISSING":
			it := get(parts[1])
			it.Installed = false
			it.Active = "uninstalled"
			it.Warn = "软件未安装"
		}
	}
	out := make([]*SecurityItem, 0, len(order))
	for _, name := range order {
		it := items[name]
		if !it.Installed {
			it.Version = ""
		}
		out = append(out, it)
	}
	return out
}

type CronEntry struct {
	Source string `json:"source"`
	User   string `json:"user"`
	Line   string `json:"line"`
	Command string `json:"command"`
}

// ParseCron 解析定时任务批量输出。协议：
//   SOURCE <name>        —— 当前来源（root crontab / etc / cron.d / timers / user:<u>）
//   CRON <user> <line>   —— 一条 crontab 条目
//   TIMER <line>         —— systemd timer 行
//   FILE <path>          —— cron.daily 等目录下的脚本
func ParseCron(text string) []*CronEntry {
	var out []*CronEntry
	source := ""
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 2 {
			continue
		}
		switch parts[0] {
		case "SOURCE":
			source = strings.TrimPrefix(parts[1], ">")
		case "CRON":
			if len(parts) < 3 {
				continue
			}
			u := parts[1]
			l := parts[2]
			if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "#") {
				continue
			}
			out = append(out, &CronEntry{Source: source, User: u, Line: l, Command: l})
		case "TIMER":
			if len(parts) < 2 {
				continue
			}
			out = append(out, &CronEntry{Source: "systemd-timer", User: "system", Line: parts[1], Command: parts[1]})
		case "FILE":
			if len(parts) < 2 {
				continue
			}
			out = append(out, &CronEntry{Source: "cron-script", User: "root", Line: parts[1], Command: parts[1]})
		}
	}
	return out
}

type PortEntry struct {
	Port    string `json:"port"`
	Process string `json:"process"`
}

// ParsePorts 解析 ss -tlnp 输出（每行: Proto Recv-Q Send-Q Local:Port Peer State Process）。
func ParsePorts(text string) []*PortEntry {
	var out []*PortEntry
	seen := map[string]bool{}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "State") || strings.HasPrefix(line, "Netid") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		local := f[3]
		port := local
		if idx := strings.LastIndex(local, ":"); idx >= 0 {
			port = local[idx+1:]
		}
		proc := ""
		for _, x := range f {
			if strings.HasPrefix(x, "users:") {
				if p := extractProcName(x); p != "" {
					proc = p
				}
			}
		}
		key := port + "|" + proc
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, &PortEntry{Port: port, Process: proc})
	}
	return out
}

func extractProcName(s string) string {
	// users:(("sshd",pid=123,fd=5))
	if i := strings.Index(s, "(\""); i >= 0 {
		rest := s[i+2:]
		if j := strings.Index(rest, "\""); j >= 0 {
			return rest[:j]
		}
	}
	return ""
}
