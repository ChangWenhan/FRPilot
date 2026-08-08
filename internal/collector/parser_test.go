package collector

import (
	"testing"
)

func TestParseSysInfo(t *testing.T) {
	text := `===HOSTNAME===
node-a100
===UPTIME===
 12:34:56 up 3 days,  4:05,  3 users,  load average: 0.45, 0.30, 0.25
===OS===
PRETTY_NAME="Ubuntu 22.04.3 LTS"
===KERNEL===
5.15.0-91-generic
===CPUCORES===
64
===LOAD===
0.45 0.30 0.25 1/1234 5678
===MEM===
MemTotal:       524288 kB
MemFree:         10240 kB
MemAvailable:    262144 kB
===STAT===
cpu  100 200 300 400 500 0 0 0 0 0
===DISK===
Filesystem     1024-blocks      Used Available Capacity Mounted on
/dev/sda1        102400000   51200000  51200000      50% /
/dev/sdb1        204800000  184320000  20480000      90% /data
===NET===
enp1s0: 1000 0 0 0 0 0 0 2000 0 0 0 0 0 0 0 0
lo: 999 0 0 0 0 0 0 999 0 0 0 0 0 0 0 0
`
	s := ParseSysInfo(text)
	if s.Hostname != "node-a100" {
		t.Errorf("hostname = %q", s.Hostname)
	}
	if s.OS != "Ubuntu 22.04.3 LTS" {
		t.Errorf("os = %q", s.OS)
	}
	if s.Kernel != "5.15.0-91-generic" {
		t.Errorf("kernel = %q", s.Kernel)
	}
	if s.CPUCores != 64 {
		t.Errorf("cores = %d", s.CPUCores)
	}
	if s.Load1 != 0.45 || s.Load5 != 0.30 || s.Load15 != 0.25 {
		t.Errorf("load = %v %v %v", s.Load1, s.Load5, s.Load15)
	}
	if s.UptimeSec != 3*86400+4*3600+5*60 {
		t.Errorf("uptime = %d", s.UptimeSec)
	}
	if s.MemTotal != 512 || s.MemAvail != 256 {
		t.Errorf("mem = %d/%d", s.MemTotal, s.MemAvail)
	}
	if len(s.Disk) != 2 {
		t.Fatalf("disk 应解析 2 个, got %d", len(s.Disk))
	}
	if s.Disk[1].UsePct != 90 || s.Disk[1].Mount != "/data" {
		t.Errorf("disk[1] = %+v", s.Disk[1])
	}
	if len(s.NetDev) != 1 || s.NetDev[0].Name != "enp1s0" {
		t.Errorf("netdev = %+v (lo 应被过滤)", s.NetDev)
	}
	if s.NetDev[0].In != 1000 || s.NetDev[0].Out != 2000 {
		t.Errorf("net 计数 = %+v", s.NetDev[0])
	}
}

func TestCPUUsage(t *testing.T) {
	// 全忙：idle 不涨，其余翻倍 → 100%
	p := "cpu  100 200 300 400"
	c := "cpu  200 400 600 400"
	if u := CPUUsage(p, c); u != 100 {
		t.Errorf("全忙场景应 100%%, got %v", u)
	}
	// 半忙：total 增加 1200，idle 增加 600 → 50%
	c2 := "cpu  200 400 600 1000"
	if u := CPUUsage(p, c2); u != 50 {
		t.Errorf("半忙场景应 50%%, got %v", u)
	}
	if u := CPUUsage("", c2); u != -1 {
		t.Errorf("缺首帧应返回 -1, got %v", u)
	}
}

func TestParseGPU(t *testing.T) {
	text := `NVIDIA GeForce RTX 4090, 23, 5018, 24564, 42, 156.50`
	g := ParseGPU(text)
	if !g.Present || g.Name != "NVIDIA GeForce RTX 4090" || g.Util != 23 || g.MemUsed != 5018 || g.Temp != 42 {
		t.Errorf("gpu = %+v", g)
	}
	// 未安装场景
	if g2 := ParseGPU("NO_GPU"); g2.Present {
		t.Error("NO_GPU 应标记为不存在")
	}
	if g3 := ParseGPU(""); g3.Present {
		t.Error("空输出应标记为不存在")
	}
	if g4 := ParseGPU("nvidia-smi: command not found"); g4.Present {
		t.Error("命令不存在应标记为不存在")
	}
}

func TestParseSecurity(t *testing.T) {
	text := `ACTIVE clamav-daemon|active
ACTIVE clamav-freshclam|active
ACTIVE crowdsec|active
ACTIVE fail2ban|inactive
MISSING rkhunter
ACTIVE ufw|active
VER clamav-daemon ClamAV 1.5.3/27430/Fri Aug
COUNT fail2ban 0
COUNT crowdsec 3
EXTRA ufw Status: active
`
	items := ParseSecurity(text)
	if len(items) != 6 {
		t.Fatalf("应解析 6 项, got %d", len(items))
	}
	m := map[string]*SecurityItem{}
	for _, it := range items {
		m[it.Name] = it
	}
	if m["rkhunter"].Installed || m["rkhunter"].Warn == "" {
		t.Error("rkhunter 应标记未安装")
	}
	if m["fail2ban"].Active != "inactive" || m["fail2ban"].Detail != "0" {
		t.Errorf("fail2ban = %+v", m["fail2ban"])
	}
	if m["crowdsec"].Detail != "3" {
		t.Errorf("crowdsec 决策数 = %v", m["crowdsec"].Detail)
	}
	if m["clamav-daemon"].Version == "" {
		t.Error("clamav 版本应解析")
	}
}

func TestParseCron(t *testing.T) {
	text := `SOURCE >root
CRON root */5 * * * * /usr/bin/sync
SOURCE >/etc/cron.d/backup
CRON root 0 2 * * * /opt/backup.sh
SOURCE >user:cwh
CRON cwh 30 3 * * * /home/cwh/job.sh
TIMER fwupd-refresh.service (Sun 00:43:43 2026)
FILE /etc/cron.daily/logrotate
`
	entries := ParseCron(text)
	if len(entries) != 5 {
		t.Fatalf("应解析 5 条, got %d", len(entries))
	}
	if entries[0].Source != "root" || entries[0].Command == "" {
		t.Errorf("root 任务 = %+v", entries[0])
	}
	if entries[2].User != "cwh" {
		t.Errorf("用户任务 = %+v", entries[2])
	}
	if entries[3].Source != "systemd-timer" {
		t.Errorf("timer = %+v", entries[3])
	}
}

func TestParsePorts(t *testing.T) {
	text := `State Recv-Q Send-Q Local Address:Port Peer Address:Port Process
LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=123,fd=5))
LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=123,fd=6))
LISTEN 0 128 [::]:6005 [::]:* users:(("sshd",pid=123,fd=8))
LISTEN 0 100 127.0.0.1:8000 0.0.0.0:* users:(("node",pid=456,fd=20))
`
	ports := ParsePorts(text)
	if len(ports) != 3 {
		t.Fatalf("应去重后 3 个端口, got %d", len(ports))
	}
	if ports[0].Port != "22" || ports[0].Process != "sshd" {
		t.Errorf("ports[0] = %+v", ports[0])
	}
	if ports[2].Port != "8000" || ports[2].Process != "node" {
		t.Errorf("ports[2] = %+v", ports[2])
	}
}
