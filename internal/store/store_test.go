package store

import "testing"

func TestAuditLogRoundTrip(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Log(1, "admin", "login", "admin", "ok"); err != nil {
		t.Fatal(err)
	}
	if err := db.Log(2, "user1", "set_credentials", "machine#1", "user=root"); err != nil {
		t.Fatal(err)
	}
	logs, err := db.ListAudit(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("应有 2 条审计, got %d", len(logs))
	}
	if logs[0].Action != "set_credentials" || logs[0].Username != "user1" {
		t.Fatalf("审计应按时间倒序: %+v", logs[0])
	}
}

func TestMachineStatusLifecycle(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, isNew, err := db.UpsertMachineFromDiscovery("ssh_m1", 6005)
	if err != nil || !isNew {
		t.Fatal("首次发现应新建", err)
	}
	if m.Status != MachinePending {
		t.Fatalf("初始状态应为 pending, got %s", m.Status)
	}
	if err := db.SetMachineCredentials(m.ID, "root", "enc", ""); err != nil {
		t.Fatal(err)
	}
	if m2, _ := db.GetMachineByID(m.ID); m2.Status != MachineConfigured {
		t.Fatalf("填凭据后应为 configured, got %s", m2.Status)
	}
	// 重复发现不应新建、不应覆盖凭据
	m2, isNew, err := db.UpsertMachineFromDiscovery("ssh_m1", 6005)
	if err != nil || isNew {
		t.Fatal("重复发现不应新建")
	}
	if m2.SSHUser != "root" {
		t.Fatal("重复发现不应清空凭据")
	}
}

func TestSettingKV(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if v, _ := db.GetSetting("none"); v != "" {
		t.Fatal("缺失 key 应返回空串")
	}
	if err := db.SetSetting("k", "v"); err != nil {
		t.Fatal(err)
	}
	if v, _ := db.GetSetting("k"); v != "v" {
		t.Fatal("写入后应能读回")
	}
	if err := db.SetSetting("k", "v2"); err != nil {
		t.Fatal(err)
	}
	if v, _ := db.GetSetting("k"); v != "v2" {
		t.Fatal("覆盖写入应生效")
	}
}
