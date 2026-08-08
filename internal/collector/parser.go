package collector

import (
	"strconv"
	"strings"
)

// ---- 纯解析函数：输入为远端命令输出文本，输出结构化数据 ----
// 每个解析器都做容错：格式异常时返回零值 + 标记，不 panic。

type SysInfo struct {
	Hostname  string   `json:"hostname"`
	UptimeSec int64    `json:"uptimeSec"`
	OS        string   `json:"os"`
	Kernel    string   `json:"kernel"`
	CPUCores  int      `json:"cpuCores"`
	Load1     float64  `json:"load1"`
	Load5     float64  `json:"load5"`
	Load15    float64  `json:"load15"`
	MemTotal  int64    `json:"memTotalMB"`
	MemFree   int64    `json:"memFreeMB"`
	MemAvail  int64    `json:"memAvailMB"`
	Disk      []Disk   `json:"disk"`
	NetDev    []NetDev `json:"netDev"`
	CPUStat   string   `json:"cpuStatRaw"` // /proc/stat 第一行，用于差分算使用率
	CPU       float64  `json:"cpuPct"`     // 差分计算后的使用率（-1=暂无）
	NetInRate float64  `json:"netInRateBps"`
	NetOutRate float64 `json:"netOutRateBps"`
}

type Disk struct {
	Mount string  `json:"mount"`
	SizeGB float64 `json:"sizeGB"`
	UsedGB float64 `json:"usedGB"`
	UsePct float64 `json:"usePct"`
}

type NetDev struct {
	Name string `json:"name"`
	In   int64  `json:"inBytes"`
	Out  int64  `json:"outBytes"`
}

func ParseSysInfo(text string) *SysInfo {
	s := &SysInfo{Disk: []Disk{}, NetDev: []NetDev{}}
	section := ""
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "===HOSTNAME==="):
			section = "hostname"
		case strings.HasPrefix(line, "===UPTIME==="):
			section = "uptime"
		case strings.HasPrefix(line, "===OS==="):
			section = "os"
		case strings.HasPrefix(line, "===KERNEL==="):
			section = "kernel"
		case strings.HasPrefix(line, "===CPUCORES==="):
			section = "cores"
		case strings.HasPrefix(line, "===LOAD==="):
			section = "load"
		case strings.HasPrefix(line, "===MEM==="):
			section = "mem"
		case strings.HasPrefix(line, "===STAT==="):
			section = "stat"
		case strings.HasPrefix(line, "===DISK==="):
			section = "disk"
		case strings.HasPrefix(line, "===NET==="):
			section = "net"
		case line == "":
			continue
		default:
			switch section {
			case "hostname":
				s.Hostname = line
			case "uptime":
				s.UptimeSec = parseUptime(line)
			case "os":
				s.OS = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
			case "kernel":
				s.Kernel = line
			case "cores":
				s.CPUCores, _ = strconv.Atoi(line)
			case "load":
				f := strings.Fields(line)
				if len(f) >= 3 {
					s.Load1, _ = strconv.ParseFloat(f[0], 64)
					s.Load5, _ = strconv.ParseFloat(f[1], 64)
					s.Load15, _ = strconv.ParseFloat(f[2], 64)
				}
			case "mem":
				parseMeminfo(line, s)
			case "stat":
				if strings.HasPrefix(line, "cpu ") {
					s.CPUStat = line
				}
			case "disk":
				if d := parseDfLine(line); d != nil {
					s.Disk = append(s.Disk, *d)
				}
			case "net":
				if n := parseNetDevLine(line); n != nil {
					s.NetDev = append(s.NetDev, *n)
				}
			}
		}
	}
	return s
}

func parseUptime(line string) int64 {
	// 格式: 12345.67 或 "up 3 days,  4:05,  3 users, load average: ..."
	if i := strings.Index(line, "up "); i >= 0 {
		rest := strings.TrimSpace(line[i+3:])
		parts := strings.Fields(rest)
		if len(parts) >= 2 && strings.Contains(parts[1], "day") {
			days, _ := strconv.ParseFloat(parts[0], 64)
			// 剩余部分找 "H:MM," 形式的运行时间
			for _, p := range parts[2:] {
				p = strings.TrimSuffix(p, ",")
				if strings.Contains(p, ":") {
					return int64(days*86400) + parseHMSHours(p)
				}
			}
			return int64(days * 86400)
		}
		if len(parts) >= 1 {
			if secs, err := strconv.ParseFloat(parts[0], 64); err == nil {
				return int64(secs)
			}
			if secs := parseHMS(strings.TrimSuffix(parts[0], ",")); secs > 0 {
				return secs
			}
		}
	}
	// 纯秒格式（/proc/uptime 兜底）
	f := strings.Fields(line)
	if len(f) > 0 {
		if secs, err := strconv.ParseFloat(f[0], 64); err == nil {
			return int64(secs)
		}
	}
	return 0
}

// parseHMSHours 解析 uptime 中的运行时间：H:MM 或 H:MM:SS（按 3600/60 进制）。
func parseHMSHours(s string) int64 {
	parts := strings.Split(s, ":")
	total := int64(0)
	mult := []int64{3600, 60, 1}
	for i, p := range parts {
		if i >= len(mult) {
			break
		}
		n, _ := strconv.ParseInt(p, 10, 64)
		total += n * mult[i]
	}
	return total
}

func parseHMS(s string) int64 {
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		n, _ := strconv.ParseInt(s, 10, 64)
		return n
	}
	total := int64(0)
	for i, p := range parts {
		n, _ := strconv.ParseInt(p, 10, 64)
		mult := int64(1)
		for j := i; j < len(parts)-1; j++ {
			mult *= 60
		}
		total += n * mult
	}
	return total
}

func parseMeminfo(line string, s *SysInfo) {
	f := strings.Fields(line)
	if len(f) < 2 {
		return
	}
	key := strings.TrimSuffix(f[0], ":")
	val, _ := strconv.ParseInt(f[1], 10, 64)
	// free -m 输出 kB
	val /= 1024
	switch key {
	case "MemTotal":
		s.MemTotal = val
	case "MemFree":
		s.MemFree = val
	case "MemAvailable":
		s.MemAvail = val
	}
}

func parseDfLine(line string) *Disk {
	f := strings.Fields(line)
	if len(f) < 6 {
		return nil
	}
	mount := f[5]
	// 过滤伪文件系统（efivars 等不在真实磁盘上的挂载）
	if strings.HasPrefix(mount, "/sys") || strings.HasPrefix(mount, "/proc") || strings.HasPrefix(mount, "/dev/") {
		return nil
	}
	usePct, _ := strconv.ParseFloat(strings.TrimSuffix(f[4], "%"), 64)
	kb, _ := strconv.ParseFloat(f[1], 64)
	used, _ := strconv.ParseFloat(f[2], 64)
	if kb <= 0 {
		return nil
	}
	return &Disk{
		Mount:  mount,
		SizeGB: round1(kb / 1024 / 1024),
		UsedGB: round1(used / 1024 / 1024),
		UsePct: usePct,
	}
}

func parseNetDevLine(line string) *NetDev {
	line = strings.TrimSpace(line)
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return nil
	}
	name := strings.TrimSpace(line[:idx])
	rest := strings.Fields(line[idx+1:])
	if len(rest) < 8 {
		return nil
	}
	in, _ := strconv.ParseInt(rest[0], 10, 64)
	out, _ := strconv.ParseInt(rest[7], 10, 64)
	if name == "lo" {
		return nil
	}
	return &NetDev{Name: name, In: in, Out: out}
}

// CPUUsage 用两次 /proc/stat 采样计算 CPU 使用率（0-100）。
func CPUUsage(prev, cur string) float64 {
	pv := parseCPULine(prev)
	cv := parseCPULine(cur)
	if pv == nil || cv == nil {
		return -1
	}
	idleD := cv[3] - pv[3]
	totalD := (cv[0] + cv[1] + cv[2] + cv[3]) - (pv[0] + pv[1] + pv[2] + pv[3])
	if totalD <= 0 {
		return -1
	}
	used := float64(totalD-idleD) / float64(totalD) * 100
	return round1(used)
}

func parseCPULine(line string) []float64 {
	f := strings.Fields(line)
	if len(f) < 5 {
		return nil
	}
	out := make([]float64, 0, 4)
	for _, x := range f[1:5] {
		v, _ := strconv.ParseFloat(x, 64)
		out = append(out, v)
	}
	return out
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}
