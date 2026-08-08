package registry

import (
	"testing"

	"frpmon/internal/config"
	"frpmon/internal/store"
)

func newTestSvc(t *testing.T) (*Service, *store.DB, *config.Manager) {
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
	return NewService(db, cfg), db, cfg
}

func TestDiscoverUpsert(t *testing.T) {
	_, db, _ := newTestSvc(t)
	// 直接模拟 store 层建档（Discover 的 HTTP 部分在 frpsapi 测试覆盖）
	m1, isNew, err := db.UpsertMachineFromDiscovery("ssh_m1", 6005)
	if err != nil {
		t.Fatal(err)
	}
	if !isNew {
		t.Fatal("新机器应标记为新建")
	}
	if m1.Status != store.MachinePending {
		t.Fatal("新机器应为 pending")
	}
	// 再次发现不重复建档
	_, isNew, err = db.UpsertMachineFromDiscovery("ssh_m1", 6005)
	if err != nil || isNew {
		t.Fatal("重复发现不应新建, isNew=", isNew)
	}
}

func TestCredentialsFlow(t *testing.T) {
	svc, db, _ := newTestSvc(t)
	m, _, err := db.UpsertMachineFromDiscovery("ssh_m1", 6005)
	if err != nil {
		t.Fatal(err)
	}
	// 无凭据时不能启用
	if _, err := svc.SetEnabled(m.ID, true); err == nil {
		t.Fatal("未配置凭据时应拒绝启用")
	}
	// 填凭据 → configured
	if err := svc.UpdateCredentials(m.ID, "root", "pass123"); err != nil {
		t.Fatal(err)
	}
	m2, _ := db.GetMachineByID(m.ID)
	if m2.Status != store.MachineConfigured {
		t.Fatal("填凭据后应为 configured")
	}
	if m2.SSHPassEnc == "pass123" || m2.SSHPassEnc == "" {
		t.Fatal("密码应加密存储")
	}
	// 启用 → enabled
	m3, err := svc.SetEnabled(m.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if m3.Status != store.MachineEnabled || !m3.Enabled {
		t.Fatal("启用后应为 enabled")
	}
	// 停用 → disabled
	m4, err := svc.SetEnabled(m.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if m4.Status != store.MachineDisabled || m4.Enabled {
		t.Fatal("停用后应为 disabled")
	}
	// 重新启用
	m5, err := svc.SetEnabled(m.ID, true)
	if err != nil || m5.Status != store.MachineEnabled {
		t.Fatal("重新启用失败", err)
	}
}

func TestCredentialsValidation(t *testing.T) {
	svc, db, _ := newTestSvc(t)
	m, _, err := db.UpsertMachineFromDiscovery("ssh_m1", 6005)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateCredentials(m.ID, "", "x"); err == nil {
		t.Fatal("空用户名应被拒绝")
	}
	if err := svc.UpdateCredentials(m.ID, "root", ""); err == nil {
		t.Fatal("空密码应被拒绝")
	}
}

func TestCredentialDecrypt(t *testing.T) {
	svc, db, _ := newTestSvc(t)
	m, _, err := db.UpsertMachineFromDiscovery("ssh_m1", 6005)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateCredentials(m.ID, "root", "s3cret!"); err != nil {
		t.Fatal(err)
	}
	m2, _ := db.GetMachineByID(m.ID)
	got, err := db.DecryptSecret(m2.SSHPassEnc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "s3cret!" {
		t.Fatal("凭据加解密不一致")
	}
}
