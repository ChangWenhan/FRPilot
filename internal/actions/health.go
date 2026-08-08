package actions

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"frpmon/internal/config"
	"frpmon/internal/store"
)

// HealthItem 一条体检项结果。
type HealthItem struct {
	Category string `json:"category"`
	Status   string `json:"status"` // pass | warn | fail
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Score    int    `json:"score"` // 该项扣分（fail 10 / warn 3）
}

// HealthReport 完整体检报告。
type HealthReport struct {
	TS      time.Time     `json:"ts"`
	Machine string        `json:"machine"`
	Score   int           `json:"score"`
	Overall string        `json:"overall"` // pass(>=90) | warn(>=70) | fail
	Items   []*HealthItem `json:"items"`
}

// RunHealth 基于最新快照执行体检（无需额外 SSH）。
// 无快照时返回错误（提示先启用监控）。
func RunHealth(db *store.DB, cfg *config.Manager, m *store.Machine) (*HealthReport, error) {
	snap, err := db.GetSnapshot(m.ID)
	if err != nil {
		return nil, fmt.Errorf("该机器暂无采集快照（请先启用监控或点击「立即采集」）: %w", err)
	}
	var data struct {
		Sys struct {
			CPU      float64 `json:"cpuPct"`
			Load1    float64 `json:"load1"`
			Load5    float64 `json:"load5"`
			Load15   float64 `json:"load15"`
			CPUCores int     `json:"cpuCores"`
			MemTotal int64   `json:"memTotalMB"`
			MemAvail int64   `json:"memAvailMB"`
			Disk     []struct {
				Mount  string  `json:"mount"`
				UsePct float64 `json:"usePct"`
			} `json:"disk"`
		} `json:"sys"`
		GPU struct {
			Present  bool    `json:"present"`
			Temp     float64 `json:"tempC"`
			MemUsed  int64   `json:"memUsedMiB"`
			MemTotal int64   `json:"memTotalMiB"`
		} `json:"gpu"`
		Security []struct {
			Name      string `json:"name"`
			Installed bool   `json:"installed"`
			Active    string `json:"active"`
			Version   string `json:"version"`
		} `json:"security"`
		TunnelUp bool      `json:"tunnelUp"`
		TS       time.Time `json:"ts"`
	}
	if err := json.Unmarshal([]byte(snap.Data), &data); err != nil {
		return nil, fmt.Errorf("快照数据解析失败: %w", err)
	}

	th := cfg.Get().Health
	rep := &HealthReport{TS: time.Now(), Machine: m.Name, Items: []*HealthItem{}}
	add := func(category, status, title, detail string, score int) {
		rep.Items = append(rep.Items, &HealthItem{Category: category, Status: status, Title: title, Detail: detail, Score: score})
	}

	// 数据新鲜度
	ageMin := time.Since(snap.TS).Minutes()
	if ageMin > float64(th.SnapshotMaxAge) {
		add("system", "warn", "数据过期", fmt.Sprintf("最近采集于 %.0f 分钟前（阈值 %d 分钟）", ageMin, th.SnapshotMaxAge), 3)
	}

	// CPU
	if data.Sys.CPU >= 0 {
		switch {
		case data.Sys.CPU >= th.CPUFail:
			add("cpu", "fail", "CPU 使用率过高", fmt.Sprintf("%.1f%%（阈值 %v%%）", data.Sys.CPU, th.CPUFail), 10)
		case data.Sys.CPU >= th.CPUWarn:
			add("cpu", "warn", "CPU 使用率偏高", fmt.Sprintf("%.1f%%（阈值 %v%%）", data.Sys.CPU, th.CPUWarn), 3)
		default:
			add("cpu", "pass", "CPU 正常", fmt.Sprintf("%.1f%%", data.Sys.CPU), 0)
		}
	} else {
		add("cpu", "warn", "CPU 数据缺失", "无足够采样帧计算使用率", 3)
	}

	// 负载
	if data.Sys.CPUCores > 0 && data.Sys.Load1 >= 0 {
		ratio := data.Sys.Load1 / float64(data.Sys.CPUCores)
		switch {
		case ratio >= 2:
			add("cpu", "fail", "负载过高", fmt.Sprintf("1 分钟负载 %.2f（核数 %d，比值 %.1f）", data.Sys.Load1, data.Sys.CPUCores, ratio), 10)
		case ratio >= 1:
			add("cpu", "warn", "负载偏高", fmt.Sprintf("1 分钟负载 %.2f（核数 %d）", data.Sys.Load1, data.Sys.CPUCores), 3)
		default:
			add("cpu", "pass", "负载正常", fmt.Sprintf("1 分钟负载 %.2f", data.Sys.Load1), 0)
		}
	}

	// 内存
	if data.Sys.MemTotal > 0 && data.Sys.MemAvail >= 0 {
		used := float64(data.Sys.MemTotal-data.Sys.MemAvail) / float64(data.Sys.MemTotal) * 100
		switch {
		case used >= th.MemFail:
			add("mem", "fail", "内存使用率过高", fmt.Sprintf("%.1f%%（阈值 %v%%）", used, th.MemFail), 10)
		case used >= th.MemWarn:
			add("mem", "warn", "内存使用率偏高", fmt.Sprintf("%.1f%%（阈值 %v%%）", used, th.MemWarn), 3)
		default:
			add("mem", "pass", "内存正常", fmt.Sprintf("%.1f%%", used), 0)
		}
	}

	// 磁盘（每个挂载点）
	diskMax := -1.0
	for _, d := range data.Sys.Disk {
		if d.UsePct > diskMax {
			diskMax = d.UsePct
		}
		switch {
		case d.UsePct >= th.DiskFail:
			add("disk", "fail", "磁盘使用率过高", fmt.Sprintf("%s %.1f%%（阈值 %v%%）", d.Mount, d.UsePct, th.DiskFail), 10)
		case d.UsePct >= th.DiskWarn:
			add("disk", "warn", "磁盘使用率偏高", fmt.Sprintf("%s %.1f%%（阈值 %v%%）", d.Mount, d.UsePct, th.DiskWarn), 3)
		}
	}
	if diskMax >= 0 {
		add("disk", "pass", "磁盘检查完成", fmt.Sprintf("最大使用率 %.1f%%", diskMax), 0)
	}

	// GPU
	if data.GPU.Present {
		if data.GPU.Temp >= th.GPUTempFail {
			add("gpu", "fail", "GPU 温度过高", fmt.Sprintf("%.0f°C（阈值 %v°C）", data.GPU.Temp, th.GPUTempFail), 10)
		} else if data.GPU.Temp >= th.GPUTempWarn {
			add("gpu", "warn", "GPU 温度偏高", fmt.Sprintf("%.0f°C（阈值 %v°C）", data.GPU.Temp, th.GPUTempWarn), 3)
		} else {
			add("gpu", "pass", "GPU 温度正常", fmt.Sprintf("%.0f°C", data.GPU.Temp), 0)
		}
		if data.GPU.MemTotal > 0 {
			mu := float64(data.GPU.MemUsed) / float64(data.GPU.MemTotal) * 100
			if mu >= th.GPUMemFail {
				add("gpu", "fail", "GPU 显存不足", fmt.Sprintf("%.1f%%（%d/%d MiB）", mu, data.GPU.MemUsed, data.GPU.MemTotal), 10)
			} else if mu >= th.GPUMemWarn {
				add("gpu", "warn", "GPU 显存偏高", fmt.Sprintf("%.1f%%（%d/%d MiB）", mu, data.GPU.MemUsed, data.GPU.MemTotal), 3)
			}
		}
	} else {
		add("gpu", "pass", "无 GPU", "该机器未检测到 GPU 或不支持", 0)
	}

	// 安全软件
	for _, s := range data.Security {
		if !s.Installed {
			add("security", "warn", s.Name+" 未安装", "该机器未安装此安全软件（不视为错误）", 3)
			continue
		}
		if s.Active != "active" {
			add("security", "fail", s.Name+" 服务未运行", "服务状态: "+s.Active, 10)
			continue
		}
		if s.Name == "clamav-daemon" {
			days := clamDbAge(s.Version)
			if days > th.ClamDbMaxDays {
				add("security", "fail", "ClamAV 病毒库过期", fmt.Sprintf("病毒库距今 %d 天（阈值 %d 天）", days, th.ClamDbMaxDays), 10)
			} else {
				add("security", "pass", "ClamAV 正常", "服务运行中，病毒库新鲜", 0)
			}
		} else {
			add("security", "pass", s.Name+" 正常", "服务运行中", 0)
		}
	}
	if len(data.Security) == 0 {
		add("security", "warn", "安全软件数据缺失", "暂未采集（监控启用后 5 分钟内自动补齐）", 3)
	}

	// 隧道连通
	if !data.TunnelUp {
		add("network", "fail", "隧道不通", "frps 侧隧道端口无法连通", 10)
	} else {
		add("network", "pass", "隧道正常", "隧道端口连通", 0)
	}

	// 汇总评分
	score := 100
	for _, it := range rep.Items {
		score -= it.Score
	}
	if score < 0 {
		score = 0
	}
	rep.Score = score
	switch {
	case score >= 90:
		rep.Overall = "pass"
	case score >= 70:
		rep.Overall = "warn"
	default:
		rep.Overall = "fail"
	}
	return rep, nil
}

// clamDbAge 从 ClamAV 版本串解析病毒库日期天数。
// 格式示例: ClamAV 1.5.3/28085/Fri Aug  7 14:24:10 2026
func clamDbAge(version string) int {
	parts := strings.Split(version, "/")
	if len(parts) < 3 {
		return -1
	}
	dateStr := strings.TrimSpace(parts[2])
	// "Fri Aug  7 14:24:10 2026" → [Fri, Aug, 7, 14:24:10, 2026]
	fields := strings.Fields(dateStr)
	if len(fields) < 4 {
		return -1
	}
	month := monthNum(fields[1])
	day, err := strconv.Atoi(fields[2])
	if err != nil {
		return -1
	}
	year, err := strconv.Atoi(fields[len(fields)-1])
	if err != nil {
		return -1
	}
	t := time.Date(year, month, day, 0, 0, 0, 0, time.Local)
	return int(time.Since(t).Hours() / 24)
}

func monthNum(s string) time.Month {
	months := map[string]time.Month{
		"Jan": time.January, "Feb": time.February, "Mar": time.March, "Apr": time.April,
		"May": time.May, "Jun": time.June, "Jul": time.July, "Aug": time.August,
		"Sep": time.September, "Oct": time.October, "Nov": time.November, "Dec": time.December,
	}
	return months[s]
}
