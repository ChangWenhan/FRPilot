package qos

import (
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"frpmon/internal/config"
	"frpmon/internal/frpsapi"
)

const (
	PollInterval = 15 * time.Second
	IFBDev       = "ifb0"
	// 手动模式 root 类速率：仅兜底结构用（未填写机器不限速）
	manualRootMbps = 100000.0
	// classid 从 2 开始（1 是 root 类）
	firstClassID = 2
)

// Runner 执行系统命令（tc/ip），测试可注入。
type Runner interface {
	Run(args ...string) error
	Output(args ...string) (string, error)
}

type execRunner struct{}

func (execRunner) Run(args ...string) error {
	out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %v: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (execRunner) Output(args ...string) (string, error) {
	out, err := exec.Command(args[0], args[1:]...).Output()
	if err != nil {
		return "", fmt.Errorf("%s: %v", args[0], err)
	}
	return string(out), nil
}

type classState struct {
	id   int     // classid 子网（1:0x%x），1 是 root
	rate float64 // bps
	port int     // 该机器的远程端口
}

type MachineSample struct {
	Name    string
	Port    int
	Online  bool
	RateIn  float64 // B/s
	RateOut float64 // B/s
	Active  bool
}

// Status 提供给 Web 层展示。
type Status struct {
	Mode          string             `json:"mode"`
	Interface     string             `json:"interface"`
	BudgetOutMbps float64            `json:"budgetOutMbps"`
	BudgetInMbps  float64            `json:"budgetInMbps"`
	Active        []string           `json:"active"`
	QuotaOutKbps  map[string]float64 `json:"quotaOutKbps"`
	QuotaInKbps   map[string]float64 `json:"quotaInKbps"`
	LastError     string             `json:"lastError"`
	UpdatedAt     time.Time          `json:"updatedAt"`
}

// Service 带宽均衡：基于 frps 服务器 tc 整形，按机器（代理端口）限速。
type Service struct {
	cfg    *config.Manager
	runner Runner
	iface  string // 检测到的整形接口

	mu          sync.Mutex
	errMu       sync.Mutex
	prevTotals  map[string][2]int64 // name → (in, out) 累计字节
	active      map[string]bool
	lastActive  map[string]time.Time
	appliedOut  map[string]*classState
	appliedIn   map[string]*classState
	nextID      int
	ids         map[string]int
	lastErr     error
	lastApply   time.Time
	stop        chan struct{}
}

func New(cfg *config.Manager) *Service {
	return &Service{
		cfg:        cfg,
		runner:     execRunner{},
		prevTotals: map[string][2]int64{},
		active:     map[string]bool{},
		lastActive: map[string]time.Time{},
		appliedOut: map[string]*classState{},
		appliedIn:  map[string]*classState{},
		nextID:     firstClassID,
		ids:        map[string]int{},
	}
}

// SetRunner 测试注入。
func (s *Service) SetRunner(r Runner) { s.runner = r }

// Start 启动控制循环。
func (s *Service) Start() {
	if s.stop != nil {
		return
	}
	s.stop = make(chan struct{})
	go func() {
		s.reconcile()
		t := time.NewTicker(PollInterval)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-t.C:
				s.reconcile()
			}
		}
	}()
}

// Stop 停止并清理全部整形规则。
func (s *Service) Stop() {
	if s.stop == nil {
		return
	}
	close(s.stop)
	s.stop = nil
	s.cleanupAll()
}

// Status 返回当前状态快照（供 Web 层）。
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{
		Mode:         s.cfg.Get().Qos.Mode,
		Interface:    s.iface,
		QuotaOutKbps: map[string]float64{},
		QuotaInKbps:  map[string]float64{},
		UpdatedAt:    s.lastApply,
	}
	for n, c := range s.appliedOut {
		st.QuotaOutKbps[n] = c.rate / 1000
	}
	for n, c := range s.appliedIn {
		st.QuotaInKbps[n] = c.rate / 1000
	}
	for n := range s.active {
		st.Active = append(st.Active, n)
	}
	sort.Strings(st.Active)
	s.errMu.Lock()
	if s.lastErr != nil {
		st.LastError = s.lastErr.Error()
	}
	s.errMu.Unlock()
	c := s.cfg.Get().Qos
	st.BudgetOutMbps = c.BudgetOutMbps
	st.BudgetInMbps = c.BudgetInMbps
	return st
}

// ---- 控制循环 ----

func (s *Service) reconcile() {
	cfg := s.cfg.Get().Qos
	if s.iface == "" {
		if cfg.Interface != "" {
			s.iface = cfg.Interface
		} else {
			s.iface = detectDefaultIface(s.runner)
		}
	}
	if s.iface == "" {
		s.setErr(fmt.Errorf("无法检测整形接口（请手动指定接口）"))
		s.cleanupAll()
		return
	}

	if cfg.Mode == "off" {
		s.cleanupAll()
		s.setErr(nil)
		return
	}

	samples, err := s.sample()
	if err != nil {
		// frps dashboard 不可达：无法维护按端口规则，清理避免残留
		s.setErr(err)
		s.cleanupAll()
		return
	}

	s.updateActive(samples, cfg.ActiveKBps*1024, time.Duration(cfg.HysteresisSec)*time.Second)

	s.mu.Lock()
	desOut, rootOut, enableOut := s.desired(samples, true, cfg)
	desIn, rootIn, enableIn := s.desired(samples, false, cfg)
	s.lastErr = nil
	if enableOut {
		s.applyDirection(s.iface, "out", rootOut, desOut)
	} else {
		s.clearDirection(s.iface, "out")
	}
	if enableIn {
		s.applyDirection(s.iface, "in", rootIn, desIn)
	} else {
		s.clearDirection(s.iface, "in")
	}
	s.mu.Unlock()
}

// desired 计算期望 class 集合与 root 速率。out=true 表示出站方向。
func (s *Service) desired(samples []MachineSample, out bool, cfg config.QosConfig) (map[string]*classState, float64, bool) {
	res := map[string]*classState{}
	online := map[string]*MachineSample{}
	for i := range samples {
		if samples[i].Online {
			online[samples[i].Name] = &samples[i]
		}
	}
	if cfg.Mode == "manual" {
		var root float64 = manualRootMbps * 1e6
		enabled := false
		for _, it := range cfg.Manual {
			m, ok := online[it.Name]
			if !ok {
				continue
			}
			v := it.OutMbps
			if !out {
				v = it.InMbps
			}
			if v > 0 {
				res[it.Name] = &classState{id: s.classID(it.Name), rate: v * 1e6, port: m.Port}
				enabled = true
			}
		}
		return res, root, enabled
	}
	// auto
	budget := cfg.BudgetOutMbps
	if !out {
		budget = cfg.BudgetInMbps
	}
	if budget <= 0 {
		return res, 0, false
	}
	n := 0
	for _, m := range samples {
		if m.Online && m.Active {
			n++
		}
	}
	if n == 0 {
		return res, 0, false // 全部空闲：不限速
	}
	share := budget * 1e6 / float64(n)
	for _, m := range samples {
		if m.Online && m.Active {
			res[m.Name] = &classState{id: s.classID(m.Name), rate: share, port: m.Port}
		}
	}
	return res, budget * 1e6, true
}

// classID 为机器分配稳定的 classid 子网（两方向共用，设备不同无冲突）。
func (s *Service) classID(name string) int {
	if id, ok := s.ids[name]; ok {
		return id
	}
	id := s.nextID
	s.nextID++
	s.ids[name] = id
	return id
}

// ---- 采样 ----

func (s *Service) sample() ([]MachineSample, error) {
	f := s.cfg.Get().Frps
	if f.DashboardURL == "" {
		return nil, fmt.Errorf("frps dashboard 未配置")
	}
	client := frpsapi.NewClient(f.DashboardURL, f.DashboardUser, f.DashboardPass, f.Token)
	proxies, err := client.Proxies()
	if err != nil {
		return nil, err
	}
	elapsed := PollInterval.Seconds()
	var out []MachineSample
	for _, p := range proxies {
		if p.Conf.RemotePort <= 0 {
			continue // http/https/stcp 等无固定端口的代理不参与
		}
		m := MachineSample{Name: p.Name, Port: p.Conf.RemotePort, Online: p.Status == "online"}
		if prev, ok := s.prevTotals[p.Name]; ok && m.Online {
			rIn := float64(p.TodayTrafficIn-prev[0]) / elapsed
			rOut := float64(p.TodayTrafficOut-prev[1]) / elapsed
			if rIn < 0 || rOut < 0 {
				rIn, rOut = 0, 0 // frps 重启计数器清零：重置基线
			}
			m.RateIn, m.RateOut = rIn, rOut
		}
		s.prevTotals[p.Name] = [2]int64{p.TodayTrafficIn, p.TodayTrafficOut}
		out = append(out, m)
	}
	return out, nil
}

// updateActive 活跃判定：速率超过阈值即活跃；低于阈值后在滞后窗口内保持活跃。
func (s *Service) updateActive(samples []MachineSample, threshold float64, hysteresis time.Duration) {
	now := time.Now()
	online := map[string]bool{}
	for _, m := range samples {
		online[m.Name] = m.Online
		if !m.Online {
			continue
		}
		if m.RateIn+m.RateOut > threshold {
			s.active[m.Name] = true
			s.lastActive[m.Name] = now
		} else if la, ok := s.lastActive[m.Name]; ok && now.Sub(la) <= hysteresis {
			s.active[m.Name] = true
		} else {
			delete(s.active, m.Name)
		}
	}
	for name := range s.active {
		if !online[name] {
			delete(s.active, name)
			delete(s.lastActive, name)
		}
	}
	for name := range s.lastActive {
		if !online[name] {
			delete(s.lastActive, name)
		}
	}
}

// ---- tc 应用 ----

// applyDirection 将期望 class 集合应用到指定方向。
// 幂等策略（鲁棒优先）：每轮清空旧 filter 后全量重建，
// class 用 replace 原地更新；删除的 class 在 filter 清空后 del（避免 busy）。
// dir: "out"（物理接口出口，匹配 sport）| "in"（ifb0 入口，匹配 dport）。
// 调用方需持有 mu。
func (s *Service) applyDirection(dev, dir string, rootRate float64, desired map[string]*classState) {
	applied := s.appliedOut
	shapingDev := dev
	if dir == "in" {
		applied = s.appliedIn
		shapingDev = IFBDev
	}
	cmds := s.buildBaseCommands(dev, dir, rootRate)
	// 1. 清空旧 filter（无 handle 无法精确删，全清后按期望重建）
	cmds = append(cmds, []string{"tc", "filter", "del", "dev", shapingDev, "parent", "1:", "protocol", "ip", "prio", "1", "u32"})
	// 2. class 全量 replace（新增/变更/不变均幂等）
	for _, want := range desired {
		cmds = append(cmds, []string{"tc", "class", "replace", "dev", shapingDev, "parent", "1:1", "classid", classIDStr(want.id), "htb", "rate", rateStr(want.rate), "ceil", rateStr(want.rate)})
	}
	// 3. 删除不再需要的 class（filter 已清空，不会 busy）
	for name := range applied {
		if _, ok := desired[name]; ok {
			continue
		}
		cmds = append(cmds, []string{"tc", "class", "del", "dev", shapingDev, "classid", classIDStr(applied[name].id)})
		delete(applied, name)
		s.releaseIfUnused(name)
	}
	// 4. filter 全量重建
	for name, want := range desired {
		cmds = append(cmds, s.filterAddCommand(shapingDev, dir, want)...)
		applied[name] = want
	}
	s.runCmds(cmds)
	if dir == "in" {
		s.appliedIn = applied
	} else {
		s.appliedOut = applied
	}
	s.lastApply = time.Now()
}

// clearDirection 清空某个方向的全部整形规则。调用方需持有 mu。
func (s *Service) clearDirection(iface, dir string) {
	applied := s.appliedOut
	var cmds [][]string
	if dir == "in" {
		applied = s.appliedIn
		for name := range applied {
			s.releaseIfUnused(name)
		}
		cmds = append(cmds,
			[]string{"tc", "filter", "del", "dev", iface, "parent", "ffff:", "protocol", "ip", "prio", "1", "u32"},
			[]string{"tc", "qdisc", "del", "dev", iface, "ingress"},
			[]string{"tc", "qdisc", "del", "dev", IFBDev, "root"},
		)
	} else {
		for name := range applied {
			s.releaseIfUnused(name)
		}
		cmds = append(cmds, []string{"tc", "qdisc", "del", "dev", iface, "root"})
	}
	clear(applied)
	s.runCmds(cmds)
	s.lastApply = time.Now()
}

// cleanupAll 清理两个方向的全部规则（Stop 与 off 模式）。
func (s *Service) cleanupAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearDirection(s.iface, "out")
	s.clearDirection(s.iface, "in")
	s.active = map[string]bool{}
	s.lastActive = map[string]time.Time{}
}

func (s *Service) buildBaseCommands(dev, dir string, rootRate float64) [][]string {
	var cmds [][]string
	if dir == "in" {
		// 入口方向：物理接口 ingress → IFB 重定向，在 ifb0 上整形
		cmds = append(cmds,
			[]string{"ip", "link", "add", IFBDev, "type", "ifb"}, // 已存在会失败，可忽略
			[]string{"ip", "link", "set", "dev", IFBDev, "up"},
			[]string{"tc", "qdisc", "add", "dev", dev, "handle", "ffff:", "ingress"}, // 已存在会失败，可忽略
			[]string{"tc", "filter", "del", "dev", dev, "parent", "ffff:", "protocol", "ip", "prio", "1", "u32"},
			[]string{"tc", "filter", "add", "dev", dev, "parent", "ffff:", "protocol", "ip", "prio", "1", "u32", "match", "u32", "0", "0", "action", "mirred", "egress", "redirect", "dev", IFBDev},
			// 必须 del 再 add：系统默认 qdisc（handle 0）上直接 add 会静默不生效；
			// del 对不存在/默认 qdisc 的报错均为良性。
			[]string{"tc", "qdisc", "del", "dev", IFBDev, "root"},
			[]string{"tc", "qdisc", "add", "dev", IFBDev, "root", "handle", "1:", "htb", "default", "1"},
		)
		dev = IFBDev
	} else {
		cmds = append(cmds,
			[]string{"tc", "qdisc", "del", "dev", dev, "root"},
			[]string{"tc", "qdisc", "add", "dev", dev, "root", "handle", "1:", "htb", "default", "1"},
		)
	}
	cmds = append(cmds, []string{"tc", "class", "replace", "dev", dev, "parent", "1:", "classid", "1:1", "htb", "rate", rateStr(rootRate)})
	return cmds
}

func (s *Service) filterAddCommand(dev, dir string, c *classState) [][]string {
	key := "sport"
	if dir == "in" {
		key = "dport"
	}
	// match 必须带掩码 0xffff（无掩码的 "ip sport N" 是非法语法）
	return [][]string{{"tc", "filter", "add", "dev", dev, "parent", "1:", "protocol", "ip", "prio", "1", "u32", "match", "ip", key, fmt.Sprintf("%d", c.port), "0xffff", "flowid", classIDStr(c.id)}}
}

func (s *Service) runCmds(cmds [][]string) {
	for _, c := range cmds {
		if err := s.runner.Run(c...); err != nil {
			if !isBenignError(err) {
				s.setErr(err)
			}
		}
	}
}

// releaseIfUnused 只有两个方向都不再引用该机器时才释放 classid。
func (s *Service) releaseIfUnused(name string) {
	if _, ok := s.appliedOut[name]; ok {
		return
	}
	if _, ok := s.appliedIn[name]; ok {
		return
	}
	if id, ok := s.ids[name]; ok {
		delete(s.ids, name)
		if id < s.nextID {
			s.nextID = id
		}
	}
}

func (s *Service) setErr(err error) {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	if err == nil {
		s.lastErr = nil
		return
	}
	if s.lastErr == nil || s.lastErr.Error() != err.Error() {
		s.lastErr = err
		log.Printf("qos: %v", err)
	}
}

// ---- 工具 ----

func classIDStr(id int) string { return fmt.Sprintf("1:%x", id) }

func rateStr(bps float64) string {
	k := bps / 1000
	if k < 1 {
		k = 1
	}
	return fmt.Sprintf("%.0fkbit", k)
}

// isBenignError 判断可忽略的命令错误（设备/规则已存在、要删的规则不存在、
// 默认 qdisc 保护性拒绝等）。
func isBenignError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "File exists") ||
		strings.Contains(msg, "No such file or directory") ||
		strings.Contains(msg, "No such device") ||
		strings.Contains(msg, "Cannot find") ||
		strings.Contains(msg, "Cannot delete qdisc with handle of zero") ||
		strings.Contains(msg, "Parent Qdisc doesn't exists")
}

// detectDefaultIface 通过默认路由检测整形接口。
func detectDefaultIface(r Runner) string {
	out, err := r.Output("ip", "route", "show", "default")
	if err != nil {
		return ""
	}
	fs := strings.Fields(out)
	for i, f := range fs {
		if f == "dev" && i+1 < len(fs) {
			return fs[i+1]
		}
	}
	return ""
}
