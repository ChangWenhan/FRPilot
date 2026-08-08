package actions

import (
	"encoding/json"
	"testing"
	"time"

	"frpmon/internal/config"
	"frpmon/internal/store"
)

func newTestEnv(t *testing.T) (*store.DB, *config.Manager) {
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
	return db, cfg
}

// 构造快照数据写入 db
func seedSnapshot(t *testing.T, db *store.DB, machineID int64, data map[string]any) {
	t.Helper()
	b, _ := json.Marshal(data)
	if err := db.SaveSnapshot(machineID, string(b)); err != nil {
		t.Fatal(err)
	}
}

func healthySnapshot() map[string]any {
	return map[string]any{
		"ts": time.Now(),
		"sys": map[string]any{
			"cpuPct": 20.0, "load1": 0.5, "cpuCores": 16,
			"memTotalMB": 128000, "memAvailMB": 100000,
			"disk": []map[string]any{{"mount": "/", "usePct": 30.0}},
		},
		"gpu": map[string]any{"present": true, "tempC": 50.0, "memUsedMiB": 1000, "memTotalMiB": 24564},
		"security": []map[string]any{
			{"name": "clamav-daemon", "installed": true, "active": "active", "version": "ClamAV 1.5.3/28085/Fri Aug  7 14:24:10 2026"},
			{"name": "crowdsec", "installed": true, "active": "active", "version": ""},
		},
		"tunnelUp": true,
	}
}

func TestHealthPass(t *testing.T) {
	db, cfg := newTestEnv(t)
	m, _, _ := db.UpsertMachineFromDiscovery("ssh_ok", 6005)
	seedSnapshot(t, db, m.ID, healthySnapshot())
	rep, err := RunHealth(db, cfg, m)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Overall != "pass" || rep.Score < 90 {
		t.Fatalf("健康机器应 pass, got %s/%d", rep.Overall, rep.Score)
	}
}

func TestHealthFailHighLoad(t *testing.T) {
	db, cfg := newTestEnv(t)
	m, _, _ := db.UpsertMachineFromDiscovery("ssh_bad", 6005)
	snap := healthySnapshot()
	sys := snap["sys"].(map[string]any)
	sys["cpuPct"] = 95.0
	sys["load1"] = 40.0
	sys["cpuCores"] = 4
	seedSnapshot(t, db, m.ID, snap)
	rep, err := RunHealth(db, cfg, m)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Overall == "pass" {
		t.Fatalf("高负载机器不应 pass, got %s/%d", rep.Overall, rep.Score)
	}
	var hasCPU, hasLoad bool
	for _, it := range rep.Items {
		if it.Category == "cpu" && it.Status == "fail" {
			hasCPU = true
		}
		if it.Title == "负载过高" && it.Status == "fail" {
			hasLoad = true
		}
	}
	if !hasCPU || !hasLoad {
		t.Fatalf("应标记 CPU 与负载 fail: %+v", rep.Items)
	}
}

func TestHealthSecurityUninstalled(t *testing.T) {
	db, cfg := newTestEnv(t)
	m, _, _ := db.UpsertMachineFromDiscovery("ssh_missing", 6005)
	snap := healthySnapshot()
	snap["security"] = []map[string]any{
		{"name": "clamav-daemon", "installed": false, "active": "uninstalled", "version": ""},
		{"name": "fail2ban", "installed": true, "active": "inactive", "version": ""},
	}
	seedSnapshot(t, db, m.ID, snap)
	rep, err := RunHealth(db, cfg, m)
	if err != nil {
		t.Fatal(err)
	}
	var uninstWarn, inactFail bool
	for _, it := range rep.Items {
		if it.Title == "clamav-daemon 未安装" && it.Status == "warn" {
			uninstWarn = true
		}
		if it.Title == "fail2ban 服务未运行" && it.Status == "fail" {
			inactFail = true
		}
	}
	if !uninstWarn {
		t.Error("未安装应标记 warn（不报错）")
	}
	if !inactFail {
		t.Error("服务未运行应标记 fail")
	}
}

func TestHealthNoSnapshot(t *testing.T) {
	db, cfg := newTestEnv(t)
	m, _, _ := db.UpsertMachineFromDiscovery("ssh_none", 6005)
	if _, err := RunHealth(db, cfg, m); err == nil {
		t.Fatal("无快照时应返回错误")
	}
}

func TestHealthReportPersist(t *testing.T) {
	db, cfg := newTestEnv(t)
	m, _, _ := db.UpsertMachineFromDiscovery("ssh_ok", 6005)
	seedSnapshot(t, db, m.ID, healthySnapshot())
	rep, err := RunHealth(db, cfg, m)
	if err != nil {
		t.Fatal(err)
	}
	itemsJSON, _ := json.Marshal(rep.Items)
	_ = db.SaveHealthReport(&store.HealthReport{
		TS: rep.TS, MachineID: m.ID, Machine: rep.Machine,
		Score: rep.Score, Overall: rep.Overall, ItemsJSON: string(itemsJSON),
	})
	reports, err := db.ListHealthReports(m.ID, 10)
	if err != nil || len(reports) != 1 {
		t.Fatalf("报告应持久化 1 条, got %d err=%v", len(reports), err)
	}
	if reports[0].Score != rep.Score || reports[0].Overall != rep.Overall {
		t.Fatalf("报告字段不符: %+v", reports[0])
	}
}

// TestHealthRkhunterScheduled rkhunter 按 cron 定时扫描判定，不再按服务运行状态误报。
func TestHealthRkhunterScheduled(t *testing.T) {
	db, cfg := newTestEnv(t)
	m, _, _ := db.UpsertMachineFromDiscovery("ssh_rkh", 6005)

	// 场景1：定时扫描新鲜 → pass
	snap := healthySnapshot()
	snap["security"] = []map[string]any{
		{"name": "rkhunter", "installed": true, "active": "scheduled", "version": "上次检查:2天前"},
	}
	seedSnapshot(t, db, m.ID, snap)
	rep, err := RunHealth(db, cfg, m)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, it := range rep.Items {
		if it.Title == "rkhunter 正常" && it.Status == "pass" {
			found = true
		}
	}
	if !found {
		t.Fatalf("新鲜定时扫描应 pass: %+v", rep.Items)
	}

	// 场景2：检查过期 → fail
	snap["security"] = []map[string]any{
		{"name": "rkhunter", "installed": true, "active": "scheduled", "version": "上次检查:30天前"},
	}
	seedSnapshot(t, db, m.ID, snap)
	rep2, err := RunHealth(db, cfg, m)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, it := range rep2.Items {
		if it.Title == "rkhunter 检查过期" && it.Status == "fail" {
			found = true
		}
	}
	if !found {
		t.Fatalf("过期检查应 fail: %+v", rep2.Items)
	}

	// 场景3：无检查日志 → warn（不误报 fail）
	snap["security"] = []map[string]any{
		{"name": "rkhunter", "installed": true, "active": "scheduled", "version": "无检查日志"},
	}
	seedSnapshot(t, db, m.ID, snap)
	rep3, err := RunHealth(db, cfg, m)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, it := range rep3.Items {
		if it.Title == "rkhunter 无检查记录" && it.Status == "warn" {
			found = true
		}
	}
	if !found {
		t.Fatalf("无检查日志应 warn: %+v", rep3.Items)
	}
}

func TestClamDbAge(t *testing.T) {
	// 病毒库日期是今天 → 0 天
	today := time.Now()
	ver := "ClamAV 1.5.3/28085/" + today.Format("Mon Jan 2 15:04:05 2006")
	if d := clamDbAge(ver); d > 1 {
		t.Fatalf("今日病毒库应为 0-1 天, got %d (%s)", d, ver)
	}
	old := time.Now().AddDate(0, 0, -30)
	ver2 := "ClamAV 1.5.3/28085/" + old.Format("Mon Jan 2 15:04:05 2006")
	if d := clamDbAge(ver2); d < 29 {
		t.Fatalf("30 天前病毒库应约 30 天, got %d", d)
	}
	if d := clamDbAge("ClamAV unknown"); d != -1 {
		t.Fatalf("无法解析应返回 -1, got %d", d)
	}
}

func TestCleanupItems(t *testing.T) {
	db, cfg := newTestEnv(t)
	tm := NewTaskManager(db, cfg)
	items := tm.ListItems()
	if len(items) < 5 {
		t.Fatalf("内置清理项应 >= 5, got %d", len(items))
	}
	// 自定义项追加
	_ = cfg.Update(func(c *config.AppConfig) {
		c.CleanupCustom = []config.CustomCleanupItem{
			{Name: "清理 pip 缓存", Command: "pip cache purge 2>/dev/null; echo done", Risk: "low"},
		}
	})
	tm2 := NewTaskManager(db, cfg)
	if n := len(tm2.ListItems()); n != len(items)+1 {
		t.Fatalf("自定义项应追加, got %d want %d", n, len(items)+1)
	}
}

func TestStartCleanupValidation(t *testing.T) {
	db, cfg := newTestEnv(t)
	tm := NewTaskManager(db, cfg)
	// 未配置凭据的机器拒绝
	m, _, _ := db.UpsertMachineFromDiscovery("ssh_none", 6005)
	if _, err := tm.StartCleanup([]int64{m.ID}, nil, 1, "admin"); err == nil {
		t.Fatal("无凭据机器应拒绝清理")
	}
	// 空清理项拒绝
	if _, err := tm.StartCleanup(nil, []string{"not_exist"}, 1, "admin"); err == nil {
		t.Fatal("无效清理项应拒绝")
	}
}
