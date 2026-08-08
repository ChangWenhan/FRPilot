package traffic

import (
	"testing"
	"time"

	"frpmon/internal/config"
	"frpmon/internal/store"
)

func newTestSvc(t *testing.T) (*Service, *store.DB) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg, err := config.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(db, cfg), db
}

func TestRateDiff(t *testing.T) {
	svc, db := newTestSvc(t)
	// 模拟两次轮询：第一次建立基线，第二次计算速率
	svc.pollWithProxies([]proxyData{{Name: "ssh_a", In: 1000, Out: 500, Status: "online"}})
	pts1, _ := db.GetTrafficHistory("ssh_a", 24)
	if len(pts1) != 1 {
		t.Fatalf("首次采样应有 1 条, got %d", len(pts1))
	}
	if pts1[0].RateIn != 0 {
		t.Fatalf("首次采样速率应为 0, got %v", pts1[0].RateIn)
	}

	// 第二次：in 增加 3000（30s 间隔 → 100 B/s），out 增加 600
	svc.pollWithProxies([]proxyData{{Name: "ssh_a", In: 4000, Out: 1100, Status: "online"}})
	pts2, _ := db.GetTrafficHistory("ssh_a", 24)
	if len(pts2) != 2 {
		t.Fatalf("二次采样应有 2 条, got %d", len(pts2))
	}
	if pts2[1].RateIn != 100 || pts2[1].RateOut != 20 {
		t.Fatalf("速率差分错误: in=%v out=%v (期望 100/20)", pts2[1].RateIn, pts2[1].RateOut)
	}
}

func TestCounterResetHandling(t *testing.T) {
	svc, _ := newTestSvc(t)
	svc.pollWithProxies([]proxyData{{Name: "ssh_a", In: 1000, Out: 500, Status: "online"}})
	// frps 重启：计数器归零，不应出现负速率
	svc.pollWithProxies([]proxyData{{Name: "ssh_a", In: 50, Out: 30, Status: "online"}})
	snap, err := svc.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Flows[0].RateIn < 0 || snap.Flows[0].RateOut < 0 {
		t.Fatal("计数器归零不应产生负速率")
	}
}

func TestBandwidthTopAndPct(t *testing.T) {
	svc, db := newTestSvc(t)
	// 三台机器：A 流量最大
	svc.pollWithProxies([]proxyData{
		{Name: "ssh_a", In: 8000, Out: 2000, Status: "online"},
		{Name: "ssh_b", In: 1000, Out: 500, Status: "online"},
		{Name: "ssh_c", In: 1000, Out: 500, Status: "online"},
	})
	// 第二次轮询让速率非零（A 增速大）
	svc.pollWithProxies([]proxyData{
		{Name: "ssh_a", In: 8000 + 9000, Out: 2000 + 3000, Status: "online"},
		{Name: "ssh_b", In: 1000 + 100, Out: 500 + 50, Status: "online"},
		{Name: "ssh_c", In: 1000 + 100, Out: 500 + 50, Status: "online"},
	})
	snap, err := svc.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Top == nil || snap.Top.Proxy != "ssh_a" {
		t.Fatalf("带宽主要流向应为 ssh_a, got %+v", snap.Top)
	}
	// 占比：A 入流量 (8000+9000) / 总量 (17000+1100+1100) = 88.5%
	for _, f := range snap.Flows {
		if f.Proxy == "ssh_a" && f.PctIn != 88.5 {
			t.Fatalf("ssh_a 入流量占比应为 88.5%%, got %v", f.PctIn)
		}
	}
	if snap.TotalIn != 19200 {
		t.Fatalf("总入流量应为 19200, got %d", snap.TotalIn)
	}
	_ = db
}

func TestAnomalyDetection(t *testing.T) {
	svc, db := newTestSvc(t)
	// 1) 建立轮询基线（首帧速率 0）
	svc.pollWithProxies([]proxyData{{Name: "ssh_a", In: 1000, Out: 500, Status: "online"}})
	// 2) 模拟平稳期历史：速率 ~1MB/s（高于 1e5 均值门槛）
	now := time.Now()
	for i := 1; i <= 5; i++ {
		_ = db.SaveTraffic([]*store.TrafficPoint{{
			TS: now.Add(-time.Duration(i)*PollInterval), Proxy: "ssh_a",
			InBytes: 1000, OutBytes: 500, RateIn: 1e6, RateOut: 5e5,
		}})
	}
	// 3) 突增采样：速率 2e7（> 均值 1e6 的 5 倍阈值）
	svc.pollWithProxies([]proxyData{{Name: "ssh_a", In: 1000 + int64(2e7*PollInterval.Seconds()), Out: 500, Status: "online"}})
	snap, err := svc.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Anomalies) != 1 || snap.Anomalies[0].Proxy != "ssh_a" {
		t.Fatalf("应检测到 ssh_a 流量突增, got %+v", snap.Anomalies)
	}
}

func TestOfflineProxyExcluded(t *testing.T) {
	svc, _ := newTestSvc(t)
	svc.pollWithProxies([]proxyData{
		{Name: "ssh_a", In: 1000, Out: 500, Status: "online"},
		{Name: "ssh_b", In: 999, Out: 888, Status: "offline"},
	})
	snap, _ := svc.Latest()
	if len(snap.Flows) != 1 || snap.Flows[0].Proxy != "ssh_a" {
		t.Fatalf("离线 proxy 不应计入: %+v", snap.Flows)
	}
}
