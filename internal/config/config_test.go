package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"frpmon/internal/store"
)

// TestTokenPersistAndReload 验证 token 会持久化到加密 settings，
// 但不会再写回 config.json 明文。
func TestTokenPersistAndReload(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := SyncEncryptedSecrets(m, db); err != nil {
		t.Fatal(err)
	}
	// 初始未设置
	if got := m.Get().Frps.Token; got != "" {
		t.Fatalf("初始 token 应为空, got %q", got)
	}
	// 设置 token（模拟自动检测/手动设置）
	err = m.Update(func(c *AppConfig) {
		c.Frps.Token = "my-custom-token-789"
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Get().Frps.Token; got != "my-custom-token-789" {
		t.Fatalf("token 未生效: %q", got)
	}
	// 重新加载并从加密 settings 恢复
	m2, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := SyncEncryptedSecrets(m2, db); err != nil {
		t.Fatal(err)
	}
	if got := m2.Get().Frps.Token; got != "my-custom-token-789" {
		t.Fatalf("token 未持久化: %q", got)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "my-custom-token-789") || strings.Contains(string(raw), "\"token\"") {
		t.Fatalf("config.json 不应包含 token 明文: %s", raw)
	}
	// 普通设置保存不应清空 token
	err = m2.Update(func(c *AppConfig) {
		c.Frps.DashboardURL = "http://127.0.0.1:8000"
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := m2.Get().Frps.Token; got != "my-custom-token-789" {
		t.Fatalf("普通保存不应清空 token: %q", got)
	}
}

func TestVerifyTokenBaseline(t *testing.T) {
	base := "my-token"
	if !VerifyTokenBaseline("my-token", base) {
		t.Error("一致的 token 应通过基线校验")
	}
	for _, bad := range []string{"", "12345", "other"} {
		if VerifyTokenBaseline(bad, base) {
			t.Errorf("%q 不应通过基线 %q 校验", bad, base)
		}
	}
	// 不同部署使用不同基线
	if !VerifyTokenBaseline("12345", "12345") {
		t.Error("默认值 12345 作为自己的基线时应通过")
	}
}

func TestHotReloadPersist(t *testing.T) {
	dir := t.TempDir()
	m, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Update(func(c *AppConfig) {
		c.Frps.DashboardURL = "http://127.0.0.1:8000"
		c.Registration = "approval"
	}); err != nil {
		t.Fatal(err)
	}
	// 重新加载，验证持久化
	m2, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := m2.Get()
	if c.Frps.DashboardURL != "http://127.0.0.1:8000" {
		t.Error("DashboardURL 未持久化")
	}
	if c.Registration != "approval" {
		t.Error("Registration 未持久化")
	}
	if _, err := filepath.Glob(filepath.Join(dir, "config.json")); err != nil {
		t.Error("config.json 应存在")
	}
}
