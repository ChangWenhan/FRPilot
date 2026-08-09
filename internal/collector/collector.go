package collector

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"frpmon/internal/config"
	"frpmon/internal/sshx"
	"frpmon/internal/store"
)

// 采集频率：快速指标 30s，慢速（安全/定时任务/端口）5min
const (
	FastInterval = 30 * time.Second
	SlowInterval = 5 * time.Minute
	SSHTimeout   = 15 * time.Second
)

// Collector 每台「监控中」机器一个独立采集循环；模块级容错。
type Collector struct {
	db    *store.DB
	cfg   *config.Manager
	mu    sync.Mutex
	loops map[int64]struct{}
	// 上一帧 /proc/stat（CPU 差分）
	prevStat map[int64]string
	prevNet  map[int64]map[string][2]int64 // 网卡速率差分
	stopCh   chan struct{}
}

func New(db *store.DB, cfg *config.Manager) *Collector {
	return &Collector{
		db:       db,
		cfg:      cfg,
		loops:    map[int64]struct{}{},
		prevStat: map[int64]string{},
		prevNet:  map[int64]map[string][2]int64{},
		stopCh:   make(chan struct{}),
	}
}

// Start 启动：为所有已启用监控的机器建立采集循环。
func (c *Collector) Start() {
	machines, err := c.db.ListMachines()
	if err != nil {
		log.Printf("collector: 加载机器失败: %v", err)
		return
	}
	for _, m := range machines {
		if m.Enabled {
			c.startLoop(m)
		}
	}
	log.Printf("collector: 已启动 %d 个采集循环", c.countLoops())
}

func (c *Collector) Stop() { close(c.stopCh) }

func (c *Collector) countLoops() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.loops)
}

// SyncMachine 机器启用/停用时同步采集循环。
func (c *Collector) SyncMachine(m *store.Machine) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if m.Enabled {
		if _, ok := c.loops[m.ID]; !ok {
			c.loops[m.ID] = struct{}{}
			go c.loop(m)
			log.Printf("collector: 开始采集 %s", m.Name)
		}
	} else {
		delete(c.loops, m.ID)
		log.Printf("collector: 停止采集 %s", m.Name)
	}
}

func (c *Collector) startLoop(m *store.Machine) {
	c.mu.Lock()
	c.loops[m.ID] = struct{}{}
	c.mu.Unlock()
	go c.loop(m)
}

func (c *Collector) IsCollecting(machineID int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.loops[machineID]
	return ok
}

// CollectNow 立即采集一次（手动触发），不依赖是否启用监控。
func (c *Collector) CollectNow(m *store.Machine) error {
	_, err := c.collect(m, true)
	return err
}

// machineHost 隧道入口：frps 服务器 IP + 隧道端口（部署在 frps 本机时也走该地址）。
func (c *Collector) machineHost(m *store.Machine) (string, int) {
	host := c.cfg.Get().Frps.SSHHost
	if host == "" {
		host = "127.0.0.1"
	}
	return host, m.TunnelPort
}

func (c *Collector) loop(m *store.Machine) {
	fast := time.NewTicker(FastInterval)
	defer fast.Stop()
	slow := time.NewTicker(SlowInterval)
	defer slow.Stop()
	lastErr := ""
	c.collect(m, false)
	for {
		select {
		case <-c.stopCh:
			return
		case <-fast.C:
			if !c.IsCollecting(m.ID) {
				return // 已停用：自我退出
			}
			if ok, err := c.collect(m, false); !ok && err != nil {
				if msg := err.Error(); msg != lastErr {
					log.Printf("collector: %s 采集失败: %v", m.Name, err)
					lastErr = msg
				}
			} else {
				lastErr = ""
			}
		case <-slow.C:
			if !c.IsCollecting(m.ID) {
				return
			}
			c.collect(m, true)
		}
	}
}

// collect 执行一轮采集。slow=true 时额外采集安全/定时任务/端口。
// 每次从 DB 重新加载机器（凭据可能被更新、监控可能被停用）。
// 返回是否成功（SSH 可达）。
func (c *Collector) collect(m *store.Machine, slow bool) (bool, error) {
	// 重新加载：拿到最新凭据与启用状态
	fresh, err := c.db.GetMachineByID(m.ID)
	if err != nil {
		return false, err
	}
	if !fresh.Enabled {
		c.SyncMachine(fresh) // 停用则移除自身循环
		return false, fmt.Errorf("机器已停用监控")
	}
	m = fresh
	host, port := c.machineHost(m)
	pass, err := c.db.DecryptSecret(m.SSHPassEnc)
	if err != nil {
		return false, err
	}
	// sudo 密码：仅当 SSH 用户非 root 且配置了 sudo 密码时提升权限执行。
	sudoPass := ""
	if m.SSHUser != "root" && m.SudoPassEnc != "" {
		sudoPass, err = c.db.DecryptSecret(m.SudoPassEnc)
		if err != nil {
			return false, err
		}
	}
	conn, err := sshx.Dial(host, port, m.SSHUser, pass)
	if err != nil {
		c.db.TouchMachine(m.ID, true, false)
		return false, fmt.Errorf("SSH 连接 %s(%s:%d) 失败: %w", m.Name, host, port, err)
	}
	defer conn.Close()

	// 快速指标
	fastOut, _, err := conn.RunSudo(fastScript(), sudoPass, SSHTimeout)
	if err != nil {
		c.db.TouchMachine(m.ID, true, false)
		return false, fmt.Errorf("采集快速指标失败: %w", err)
	}
	sys := ParseSysInfo(fastOut)
	sys.CPU = c.cpuPct(m.ID, sys.CPUStat)
	sys.NetInRate, sys.NetOutRate = c.netRates(m.ID, sys.NetDev)

	// 慢速模块：独立容错，失败不影响快照
	var gpu *GPUInfo
	var security []*SecurityItem
	var cron []*CronEntry
	var ports []*PortEntry
	if slow {
		gpu = c.tryGPU(conn, sudoPass)
		security = c.trySecurity(conn, sudoPass)
		cron = c.tryCron(conn, sudoPass)
		ports = c.tryPorts(conn, sudoPass)
	} else if snap, err := c.db.GetSnapshot(m.ID); err == nil {
		// 保留上一轮慢速数据
		var prev struct {
			GPU      *GPUInfo         `json:"gpu"`
			Security []*SecurityItem  `json:"security"`
			Cron     []*CronEntry     `json:"cron"`
			Ports    []*PortEntry     `json:"ports"`
		}
		if json.Unmarshal([]byte(snap.Data), &prev) == nil {
			gpu, security, cron, ports = prev.GPU, prev.Security, prev.Cron, prev.Ports
		}
	}
	if gpu == nil {
		gpu = &GPUInfo{Present: false}
	}

	// 隧道连通性（frps 侧隧道端口）
	tunnelUp := c.tunnelCheck(host, port)

	snap := map[string]any{
		"ts":       time.Now(),
		"hostname": sys.Hostname,
		"sys":      sys,
		"gpu":      gpu,
		"security": security,
		"cron":     cron,
		"ports":    ports,
		"tunnelUp": tunnelUp,
		"sshOk":    true,
	}
	b, _ := json.Marshal(snap)
	_ = c.db.SaveSnapshot(m.ID, string(b))

	// 历史指标
	disk := topDiskPct(sys.Disk)
	_ = c.db.SaveMetricFor(m.ID, &store.MetricPoint{
		TS:      time.Now(),
		CPU:     sys.CPU,
		Mem:     memPct(sys),
		Disk:    disk,
		GPUUtil: gpu.Util,
		GPUMem:  float64(gpu.MemUsed),
		GPUTemp: gpu.Temp,
		NetIn:   sys.NetInRate,
		NetOut:  sys.NetOutRate,
		Conns:   len(ports),
	})
	c.db.TouchMachine(m.ID, true, true)
	return true, nil
}

func (c *Collector) cpuPct(id int64, cur string) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	prev := c.prevStat[id]
	c.prevStat[id] = cur
	if prev == "" || cur == "" {
		return -1
	}
	return CPUUsage(prev, cur)
}

func (c *Collector) netRates(id int64, devs []NetDev) (float64, float64) {
	cur := map[string][2]int64{}
	var in, out int64
	for _, d := range devs {
		cur[d.Name] = [2]int64{d.In, d.Out}
		in += d.In
		out += d.Out
	}
	c.mu.Lock()
	prev := c.prevNet[id]
	c.prevNet[id] = cur
	c.mu.Unlock()
	if prev == nil {
		return 0, 0
	}
	var pin, pout int64
	for _, d := range devs {
		if p, ok := prev[d.Name]; ok {
			pin += p[0]
			pout += p[1]
		}
	}
	elapsed := FastInterval.Seconds()
	rateIn := float64(in-pin) / elapsed
	rateOut := float64(out-pout) / elapsed
	if rateIn < 0 {
		rateIn = 0
	}
	if rateOut < 0 {
		rateOut = 0
	}
	return rateIn, rateOut
}

func (c *Collector) tunnelCheck(host string, port int) bool {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ---- 慢速模块：各自容错 ----

func (c *Collector) tryGPU(conn *sshx.Conn, sudoPass string) *GPUInfo {
	out, _, _ := conn.RunSudo(`nvidia-smi --query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw --format=csv,noheader,nounits 2>/dev/null || echo NO_GPU`, sudoPass, SSHTimeout)
	return ParseGPU(out)
}

func (c *Collector) trySecurity(conn *sshx.Conn, sudoPass string) []*SecurityItem {
	script := `
for svc in clamav-daemon clamav-freshclam crowdsec fail2ban ufw; do
  if systemctl list-unit-files --type=service "$svc.service" 2>/dev/null | grep -q "$svc"; then
    st=$(systemctl is-active "$svc" 2>/dev/null); echo "ACTIVE $svc|$st"
  elif [ -x /usr/sbin/$svc ] || [ -x /usr/bin/$svc ] || command -v $svc >/dev/null 2>&1; then
    st=$(systemctl is-active "$svc" 2>/dev/null || echo unknown); echo "ACTIVE $svc|$st"
  else
    echo "MISSING $svc"
  fi
done
# rkhunter 是 cron 定时扫描型工具（非守护进程），按检查新鲜度判定
if [ -x /usr/bin/rkhunter ] || [ -x /usr/sbin/rkhunter ] || command -v rkhunter >/dev/null 2>&1; then
  echo "ACTIVE rkhunter|scheduled"
  if [ -f /etc/cron.daily/rkhunter ] || [ -f /etc/cron.weekly/rkhunter ] || [ -f /etc/cron.d/rkhunter ]; then
    echo "EXTRA rkhunter cron:配置存在"
  fi
  last=$(stat -c %Y /var/log/rkhunter.log 2>/dev/null || echo 0)
  if [ "$last" != "0" ] && [ -n "$last" ]; then
    days=$(( ( $(date +%s) - last ) / 86400 ))
    echo "EXTRA rkhunter 上次检查:${days}天前"
  else
    echo "EXTRA rkhunter 无检查日志"
  fi
else
  echo "MISSING rkhunter"
fi
cs=$(clamscan --version 2>/dev/null | head -1); [ -n "$cs" ] && echo "VER clamav-daemon $cs"
f2b=$(fail2ban-client status 2>/dev/null | grep -oP 'banned IP count:\s*\K\d+' | awk '{s+=$1} END {print s+0}'); [ -n "$f2b" ] && echo "COUNT fail2ban $f2b"
csc=$(cscli decisions list 2>/dev/null | tail -n +3 | wc -l); [ -n "$csc" ] && echo "COUNT crowdsec $csc"
ufw=$(ufw status 2>/dev/null | head -1); [ -n "$ufw" ] && echo "EXTRA ufw $ufw"
`
	out, _, _ := conn.RunSudo(script, sudoPass, SSHTimeout)
	return ParseSecurity(out)
}

func (c *Collector) tryCron(conn *sshx.Conn, sudoPass string) []*CronEntry {
	script := `
echo "SOURCE >root"
crontab -l 2>/dev/null | while IFS= read -r l; do [ -n "$l" ] && echo "CRON root $l"; done
echo "SOURCE >/etc/crontab"
while IFS= read -r l; do [ -n "$l" ] && [ -z "${l##*[![:space:]]*}" ] && echo "CRON root $l"; done < /etc/crontab 2>/dev/null
for f in /etc/cron.d/*; do [ -f "$f" ] && { echo "SOURCE >$f"; while IFS= read -r l; do [ -n "$l" ] && echo "CRON root $l"; done < "$f"; }; done
for d in /etc/cron.hourly /etc/cron.daily /etc/cron.weekly /etc/cron.monthly; do
  for f in "$d"/*; do [ -f "$f" ] && echo "FILE $f"; done
done
for u in $(awk -F: '$7 ~ /(bash|sh|zsh)$/ {print $1}' /etc/passwd); do
  ul=$(crontab -l -u "$u" 2>/dev/null)
  if [ -n "$ul" ]; then
    echo "SOURCE >user:$u"
    echo "$ul" | while IFS= read -r l; do [ -n "$l" ] && echo "CRON $u $l"; done
  fi
done
systemctl list-timers --all --no-pager 2>/dev/null | awk 'NR>1 && NF>=5 {print "TIMER "$NF" ("$1" "$2")"}'
`
	out, _, _ := conn.RunSudo(script, sudoPass, SSHTimeout)
	return ParseCron(out)
}

func (c *Collector) tryPorts(conn *sshx.Conn, sudoPass string) []*PortEntry {
	out, _, _ := conn.RunSudo(`ss -tlnp 2>/dev/null`, sudoPass, SSHTimeout)
	return ParsePorts(out)
}

// ---- 快照聚合辅助 ----

func memPct(s *SysInfo) float64 {
	if s.MemTotal <= 0 || s.MemAvail < 0 {
		return -1
	}
	used := s.MemTotal - s.MemAvail
	return round1(float64(used) / float64(s.MemTotal) * 100)
}

func topDiskPct(disks []Disk) float64 {
	max := float64(-1)
	for _, d := range disks {
		if d.UsePct > max {
			max = d.UsePct
		}
	}
	return max
}
