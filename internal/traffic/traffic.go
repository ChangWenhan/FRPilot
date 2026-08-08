package traffic

import (
	"math"
	"sync"
	"time"

	"frpmon/internal/config"
	"frpmon/internal/frpsapi"
	"frpmon/internal/store"
)

// 流量轮询频率与异常检测参数
const (
	PollInterval     = 30 * time.Second
	AnomalyThreshold = 5.0 // 当前速率 > 历史均值 N 倍 → 异常突增
)

// Service 流量监控：轮询 dashboard API，计算每 proxy 速率，做带宽流向分析与异常检测。
type Service struct {
	db  *store.DB
	cfg *config.Manager
	mu  sync.Mutex

	prevTotals map[string][2]int64 // proxy → (in, out) 上一轮累计值
	lastSample map[string]*ProxyFlow
	anomaly    map[string]bool
}

type ProxyFlow struct {
	Proxy    string  `json:"proxy"`
	MachineID int64  `json:"machineId"`
	InBytes  int64   `json:"inBytes"`
	OutBytes int64   `json:"outBytes"`
	RateIn   float64 `json:"rateIn"`  // B/s
	RateOut  float64 `json:"rateOut"` // B/s
	PctIn    float64 `json:"pctIn"`   // 占总入流量百分比
	PctOut   float64 `json:"pctOut"`
	Anomaly  bool    `json:"anomaly"` // 流量突增异常标记
}

type Snapshot struct {
	TS         time.Time     `json:"ts"`
	TotalIn    int64         `json:"totalIn"`
	TotalOut   int64         `json:"totalOut"`
	RateInSum  float64       `json:"rateInSum"`
	RateOutSum float64       `json:"rateOutSum"`
	Flows      []*ProxyFlow  `json:"flows"`
	Top        *ProxyFlow    `json:"top"` // 带宽主要流向
	Anomalies  []*ProxyFlow  `json:"anomalies"`
	Online     int           `json:"onlineProxies"`
	ServerVer  string        `json:"serverVersion"`
}

func New(db *store.DB, cfg *config.Manager) *Service {
	return &Service{
		db:         db,
		cfg:        cfg,
		prevTotals: map[string][2]int64{},
		lastSample: map[string]*ProxyFlow{},
		anomaly:    map[string]bool{},
	}
}

// Start 启动轮询循环。
func (s *Service) Start() {
	go func() {
		ticker := time.NewTicker(PollInterval)
		defer ticker.Stop()
		// 清理历史（与采集器共用保留天数）
		cleanup := time.NewTicker(6 * time.Hour)
		defer cleanup.Stop()
		s.poll()
		for {
			select {
			case <-ticker.C:
				s.poll()
			case <-cleanup.C:
				_ = s.db.CleanupTraffic()
			}
		}
	}()
}

// proxyData 测试注入用（与 frpsapi.Proxy 字段对齐）。
type proxyData struct {
	Name   string
	In     int64
	Out    int64
	Status string
}

// pollWithProxies 用给定 proxy 数据执行一轮采样（测试与内部复用）。
func (s *Service) pollWithProxies(proxies []proxyData) {
	now := time.Now()
	elapsed := PollInterval.Seconds()
	var pts []*store.TrafficPoint
	var flows []*ProxyFlow
	s.mu.Lock()
	for _, p := range proxies {
		if p.Status != "online" {
			continue
		}
		prev, hasPrev := s.prevTotals[p.Name]
		rateIn, rateOut := float64(0), float64(0)
		if hasPrev {
			rateIn = float64(p.In-prev[0]) / elapsed
			rateOut = float64(p.Out-prev[1]) / elapsed
			if rateIn < 0 || rateOut < 0 {
				// frps 重启导致计数器归零：重置基线
				rateIn, rateOut = 0, 0
			}
		}
		s.prevTotals[p.Name] = [2]int64{p.In, p.Out}
		machineID := s.machineIDByName(p.Name)
		f := &ProxyFlow{
			Proxy: p.Name, MachineID: machineID,
			InBytes: p.In, OutBytes: p.Out,
			RateIn: rateIn, RateOut: rateOut,
		}
		s.lastSample[p.Name] = f
		flows = append(flows, f)
		pts = append(pts, &store.TrafficPoint{
			TS: now, Proxy: p.Name, MachineID: machineID,
			InBytes: p.In, OutBytes: p.Out,
			RateIn: rateIn, RateOut: rateOut,
		})
	}
	s.mu.Unlock()
	if len(pts) > 0 {
		_ = s.db.SaveTraffic(pts)
	}
	s.detectAnomalies(flows)
}

// poll 拉取一次 dashboard 数据并落库。
func (s *Service) poll() {
	client, err := NewClient(s.cfg)
	if err != nil {
		return
	}
	proxies, err := client.Proxies()
	if err != nil {
		return
	}
	datas := make([]proxyData, 0, len(proxies))
	for _, p := range proxies {
		datas = append(datas, proxyData{Name: p.Name, In: p.TodayTrafficIn, Out: p.TodayTrafficOut, Status: p.Status})
	}
	s.pollWithProxies(datas)
}

// machineIDByName 查询机器 id（缓存每次查库，机器数少可接受）。
func (s *Service) machineIDByName(name string) int64 {
	if m, err := s.db.GetMachineByName(name); err == nil {
		return m.ID
	}
	return 0
}

// detectAnomalies 异常检测：当前速率 vs 近 1 小时均值，超过阈值标记突增。
func (s *Service) detectAnomalies(flows []*ProxyFlow) {
	history, err := s.db.GetTrafficHistory("", 1)
	if err != nil || len(history) == 0 {
		return
	}
	avg := map[string][2]float64{} // proxy → 平均 in/out 速率
	cnt := map[string]int{}
	for _, h := range history {
		if h.RateIn < 0 || h.RateOut < 0 {
			continue
		}
		a := avg[h.Proxy]
		a[0] += h.RateIn
		a[1] += h.RateOut
		avg[h.Proxy] = a
		cnt[h.Proxy]++
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range flows {
		anomaly := false
		if n := cnt[f.Proxy]; n >= 3 {
			meanIn := avg[f.Proxy][0] / float64(n)
			meanOut := avg[f.Proxy][1] / float64(n)
			if (meanIn > 1e5 && f.RateIn > meanIn*AnomalyThreshold) ||
				(meanOut > 1e5 && f.RateOut > meanOut*AnomalyThreshold) {
				anomaly = true
			}
		}
		s.anomaly[f.Proxy] = anomaly
		f.Anomaly = anomaly
	}
}

// Latest 返回带宽流向快照（含占比与异常标记）。
func (s *Service) Latest() (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.lastSample) == 0 {
		return nil, nil
	}
	snap := &Snapshot{TS: time.Now(), Flows: []*ProxyFlow{}}
	var totalIn, totalOut, rateInSum, rateOutSum float64
	online := 0
	for _, f := range s.lastSample {
		totalIn += float64(f.InBytes)
		totalOut += float64(f.OutBytes)
		rateInSum += f.RateIn
		rateOutSum += f.RateOut
		if f.RateIn+f.RateOut > 0 || f.InBytes > 0 {
			online++
		}
		snap.Flows = append(snap.Flows, f)
	}
	for _, f := range snap.Flows {
		if totalIn > 0 {
			f.PctIn = round1(float64(f.InBytes) / totalIn * 100)
		}
		if totalOut > 0 {
			f.PctOut = round1(float64(f.OutBytes) / totalOut * 100)
		}
		if f.Anomaly {
			snap.Anomalies = append(snap.Anomalies, f)
		}
	}
	// 带宽主要流向：按 (rateIn+rateOut) 排序取第一
	best := snap.Flows[0]
	for _, f := range snap.Flows[1:] {
		if f.RateIn+f.RateOut > best.RateIn+best.RateOut {
			best = f
		}
	}
	snap.Top = best
	snap.TotalIn = int64(totalIn)
	snap.TotalOut = int64(totalOut)
	snap.RateInSum = round1(rateInSum)
	snap.RateOutSum = round1(rateOutSum)
	snap.Online = online
	return snap, nil
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// NewClient 构造 dashboard 客户端（复用配置）。
func NewClient(cfg *config.Manager) (*frpsapi.Client, error) {
	f := cfg.Get().Frps
	if f.DashboardURL == "" {
		return nil, errNotConfigured
	}
	return frpsapi.NewClient(f.DashboardURL, f.DashboardUser, f.DashboardPass, f.Token), nil
}

var errNotConfigured = &notConfiguredError{}

type notConfiguredError struct{}

func (e *notConfiguredError) Error() string { return "未配置 frps dashboard" }
