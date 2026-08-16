package qos

import (
	"strings"
	"testing"
	"time"

	"frpmon/internal/config"
)

type fakeRunner struct {
	cmds [][]string
}

func (f *fakeRunner) Run(args ...string) error {
	f.cmds = append(f.cmds, args)
	return nil
}

func (f *fakeRunner) Output(args ...string) (string, error) {
	return "default via 172.22.159.253 dev eth0 proto dhcp metric 100\n", nil
}

func (f *fakeRunner) contains(parts ...string) bool {
	for _, c := range f.cmds {
		all := true
		for _, p := range parts {
			if !containsStr(c, p) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func newTestService() (*Service, *fakeRunner) {
	s := New(&config.Manager{})
	f := &fakeRunner{}
	s.SetRunner(f)
	return s, f
}

// ---- 活跃判定 ----

func TestUpdateActiveThresholdAndHysteresis(t *testing.T) {
	s, _ := newTestService()
	thr := 1.0 * 1024

	// 速率超阈值 → 活跃
	s.updateActive([]MachineSample{{Name: "a", Port: 6001, Online: true, RateIn: 2000}}, thr, 45*time.Second)
	if !s.active["a"] {
		t.Fatal("高流量机器应判活跃")
	}

	// 速率归零但仍在滞后窗口（模拟暂停）→ 保持活跃
	s.updateActive([]MachineSample{{Name: "a", Port: 6001, Online: true}}, thr, 45*time.Second)
	if !s.active["a"] {
		t.Fatal("滞后窗口内应保持活跃")
	}

	// 低速率不活跃机器不进入活跃集
	s.updateActive([]MachineSample{
		{Name: "a", Port: 6001, Online: true},
		{Name: "b", Port: 6002, Online: true, RateIn: 100},
	}, thr, 45*time.Second)
	if s.active["b"] {
		t.Fatal("低于阈值的机器不应活跃")
	}

	// 超出滞后窗口 → 清除
	s.lastActive["a"] = time.Now().Add(-46 * time.Second)
	s.updateActive([]MachineSample{{Name: "a", Port: 6001, Online: true}}, thr, 45*time.Second)
	if s.active["a"] {
		t.Fatal("超出滞后窗口应清除活跃")
	}
}

func TestUpdateActiveOfflineCleared(t *testing.T) {
	s, _ := newTestService()
	thr := 1.0 * 1024
	s.updateActive([]MachineSample{{Name: "a", Port: 6001, Online: true, RateIn: 5000}}, thr, 45*time.Second)
	if !s.active["a"] {
		t.Fatal("前置：a 应活跃")
	}
	// 机器掉线（不在列表中）
	s.updateActive([]MachineSample{}, thr, 45*time.Second)
	if s.active["a"] {
		t.Fatal("掉线机器应清除活跃")
	}
}

// ---- 配额计算 ----

func TestDesiredAutoFairShare(t *testing.T) {
	s, _ := newTestService()
	samples := []MachineSample{
		{Name: "a", Port: 6001, Online: true, Active: true},
		{Name: "b", Port: 6002, Online: true, Active: true},
		{Name: "c", Port: 6003, Online: true, Active: false},
	}
	cfg := config.QosConfig{Mode: "auto", BudgetOutMbps: 3, BudgetInMbps: 2, ActiveKBps: 1, HysteresisSec: 45}
	des, root, en := s.desired(samples, true, cfg)
	if !en {
		t.Fatal("出站方向应启用")
	}
	if root != 3e6 {
		t.Fatalf("root 应等于出站预算 3Mbps，得到 %v", root)
	}
	if len(des) != 2 {
		t.Fatalf("应只有 2 台活跃机器，得到 %d", len(des))
	}
	if des["a"].rate != 1.5e6 || des["b"].rate != 1.5e6 {
		t.Fatalf("两台活跃机器应各分 1.5Mbps，得到 a=%v b=%v", des["a"].rate, des["b"].rate)
	}
	if _, ok := des["c"]; ok {
		t.Fatal("不活跃机器不应有 class")
	}
	if des["a"].port != 6001 {
		t.Fatalf("端口映射错误: %v", des["a"].port)
	}
}

func TestDesiredAutoBudgetZeroDisabled(t *testing.T) {
	s, _ := newTestService()
	samples := []MachineSample{{Name: "a", Port: 6001, Online: true, Active: true}}
	cfg := config.QosConfig{Mode: "auto", BudgetOutMbps: 3, BudgetInMbps: 0}
	des, _, en := s.desired(samples, false, cfg)
	if en || len(des) != 0 {
		t.Fatal("预算为 0 的方向不应启用")
	}
}

func TestDesiredAutoNoActiveDisables(t *testing.T) {
	s, _ := newTestService()
	samples := []MachineSample{{Name: "a", Port: 6001, Online: true, Active: false}}
	cfg := config.QosConfig{Mode: "auto", BudgetOutMbps: 3}
	des, _, en := s.desired(samples, true, cfg)
	if en || len(des) != 0 {
		t.Fatal("全部空闲时不应限速")
	}
}

func TestDesiredManual(t *testing.T) {
	s, _ := newTestService()
	samples := []MachineSample{
		{Name: "a", Port: 6001, Online: true},
		{Name: "b", Port: 6002, Online: true},
		{Name: "offline", Port: 6003, Online: false},
	}
	cfg := config.QosConfig{Mode: "manual", Manual: []config.QosManualItem{
		{Name: "a", OutMbps: 0.5, InMbps: 0},
		{Name: "b", OutMbps: 1, InMbps: 2},
		{Name: "offline", OutMbps: 1, InMbps: 0},
	}}
	des, root, en := s.desired(samples, true, cfg)
	if !en {
		t.Fatal("手动模式有出站条目应启用")
	}
	if root != manualRootMbps*1e6 {
		t.Fatalf("手动模式 root 应不限速: %v", root)
	}
	if des["a"].rate != 0.5e6 || des["b"].rate != 1e6 {
		t.Fatalf("手动出站额度错误: %v %v", des["a"].rate, des["b"].rate)
	}
	if _, ok := des["offline"]; ok {
		t.Fatal("离线机器不应建 class")
	}
	desIn, _, enIn := s.desired(samples, false, cfg)
	if !enIn || desIn["b"].rate != 2e6 {
		t.Fatalf("手动入站额度错误: %v", desIn["b"].rate)
	}
	if _, ok := desIn["a"]; ok {
		t.Fatal("a 的入站额度为 0，不应建 class")
	}
}

// ---- tc 命令生成 ----

func TestApplyDirectionCreatesClasses(t *testing.T) {
	s, f := newTestService()
	s.mu.Lock()
	des := map[string]*classState{
		"a": {id: 2, rate: 1.5e6, port: 6001},
		"b": {id: 3, rate: 1.5e6, port: 6002},
	}
	s.applyDirection("eth0", "out", 3e6, des)
	s.mu.Unlock()

	if !f.contains("tc", "qdisc", "add", "dev", "eth0", "root") {
		t.Fatal("应创建出口 root qdisc")
	}
	if !f.contains("tc", "class", "replace", "dev", "eth0", "classid", "1:2", "htb", "rate", "1500kbit") {
		t.Fatal("机器 class 应存在（1.5Mbps=1500kbit）")
	}
	if !f.contains("tc", "filter", "add", "dev", "eth0", "match", "ip", "sport", "6001", "0xffff") {
		t.Fatal("出口 filter 应匹配 sport 6001 并带掩码")
	}
	if !f.contains("tc", "filter", "add", "dev", "eth0", "match", "ip", "sport", "6002", "0xffff") {
		t.Fatal("出口 filter 应匹配 sport 6002 并带掩码")
	}
}

func TestApplyDirectionIdempotentReapply(t *testing.T) {
	s, f := newTestService()
	s.mu.Lock()
	des := map[string]*classState{"a": {id: 2, rate: 1.5e6, port: 6001}}
	s.applyDirection("eth0", "out", 3e6, des)
	n1 := len(f.cmds)
	// 相同期望再次应用：class 全量 replace，无 class del
	des["a"] = &classState{id: 2, rate: 1.5e6, port: 6001}
	s.applyDirection("eth0", "out", 3e6, des)
	s.mu.Unlock()
	added := f.cmds[n1:]
	for _, c := range added {
		if containsStr(c, "class") && containsStr(c, "del") {
			t.Fatalf("相同期望不应删除 class: %v", c)
		}
	}
	if !f.contains("tc", "class", "replace", "dev", "eth0", "classid", "1:2", "htb", "rate", "1500kbit") {
		t.Fatal("class replace 应重复执行（幂等）")
	}
}

func TestApplyDirectionRemovesMachine(t *testing.T) {
	s, f := newTestService()
	s.mu.Lock()
	des := map[string]*classState{"a": {id: 2, rate: 1.5e6, port: 6001}}
	s.applyDirection("eth0", "out", 3e6, des)
	n1 := len(f.cmds)
	s.applyDirection("eth0", "out", 3e6, map[string]*classState{})
	s.mu.Unlock()
	if !f.contains("tc", "class", "del", "dev", "eth0", "classid", "1:2") {
		t.Fatal("应删除机器 class")
	}
	for _, c := range f.cmds[n1:] {
		if containsStr(c, "filter") && containsStr(c, "add") && containsStr(c, "6001") {
			t.Fatalf("机器移除后不应再添加其 filter: %v", c)
		}
	}
	if _, ok := s.ids["a"]; ok {
		t.Fatal("机器移除后 classid 应释放")
	}
}

func TestApplyDirectionIngressUsesIFB(t *testing.T) {
	s, f := newTestService()
	s.mu.Lock()
	des := map[string]*classState{"a": {id: 2, rate: 1e6, port: 6001}}
	s.applyDirection("eth0", "in", 3e6, des)
	s.mu.Unlock()
	if !f.contains("ip", "link", "add", IFBDev, "type", "ifb") {
		t.Fatal("入口方向应创建 ifb0")
	}
	if !f.contains("tc", "filter", "add", "dev", "eth0", "parent", "ffff:", "action", "mirred", "egress", "redirect", "dev", IFBDev) {
		t.Fatal("eth0 应挂 ingress 重定向到 ifb0")
	}
	if !f.contains("tc", "qdisc", "add", "dev", IFBDev, "root") {
		t.Fatal("ifb0 上应有 root qdisc")
	}
	if !f.contains("tc", "filter", "add", "dev", IFBDev, "match", "ip", "dport", "6001", "0xffff") {
		t.Fatal("入口 filter 应匹配 dport 6001")
	}
}

func TestClearAndCleanup(t *testing.T) {
	s, f := newTestService()
	s.mu.Lock()
	des := map[string]*classState{"a": {id: 2, rate: 1e6, port: 6001}}
	s.applyDirection("eth0", "out", 3e6, des)
	s.applyDirection("eth0", "in", 3e6, des)
	s.clearDirection("eth0", "in")
	s.clearDirection("eth0", "out")
	s.mu.Unlock()
	if !f.contains("tc", "qdisc", "del", "dev", "eth0", "ingress") {
		t.Fatal("入口清理应删除 ingress qdisc")
	}
	if !f.contains("tc", "qdisc", "del", "dev", IFBDev, "root") {
		t.Fatal("入口清理应删除 ifb0 root")
	}
	if !f.contains("tc", "qdisc", "del", "dev", "eth0", "root") {
		t.Fatal("出口清理应删除 root qdisc")
	}
	if len(s.appliedOut) != 0 || len(s.appliedIn) != 0 {
		t.Fatal("清理后 applied 应为空")
	}
}

// ---- 工具 ----

func TestRateStr(t *testing.T) {
	cases := map[float64]string{
		3e6:     "3000kbit",
		1.5e6:   "1500kbit",
		0.5e6:   "500kbit",
		500:     "1kbit", // 下限保护
		100e6:   "100000kbit",
	}
	for in, want := range cases {
		if got := rateStr(in); got != want {
			t.Errorf("rateStr(%v) = %s, want %s", in, got, want)
		}
	}
}

func TestDetectDefaultIface(t *testing.T) {
	f := &fakeRunner{}
	if got := detectDefaultIface(f); got != "eth0" {
		t.Fatalf("应检测到 eth0，得到 %q", got)
	}
}

func TestIsBenignError(t *testing.T) {
	benign := []string{
		"RTNETLINK answers: File exists",
		"RTNETLINK answers: No such file or directory",
		"Cannot find device \"ifb0\"",
		"No such device",
		"Error: Cannot delete qdisc with handle of zero.",
		"Error: Parent Qdisc doesn't exists.",
	}
	for _, m := range benign {
		if !isBenignError(&runErr{m}) {
			t.Errorf("应视为良性错误: %s", m)
		}
	}
	if isBenignError(&runErr{"Permission denied"}) {
		t.Error("权限错误不应视为良性")
	}
}

type runErr struct{ msg string }

func (e *runErr) Error() string { return e.msg }

// ---- 命令鲁棒性：未知错误透传 ----

func TestRunCmdsErrorSurfaced(t *testing.T) {
	s := New(&config.Manager{})
	f := &fakeRunner{}
	s.SetRunner(f)
	s.setErr(nil)
	s.runCmds([][]string{{"tc", "qdisc", "del", "dev", "eth0", "root"}})
	if s.lastErr != nil {
		t.Fatalf("fake runner 无错误，不应有 lastErr: %v", s.lastErr)
	}
	_ = strings.Join
}
