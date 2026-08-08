package auth

import (
	"testing"
	"time"

	"frpmon/internal/store"
)

func newTestSvc(t *testing.T) (*Service, *store.DB) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewService(db), db
}

func TestFirstUserIsAdmin(t *testing.T) {
	svc, _ := newTestSvc(t)
	u, err := svc.Register("admin1", "pass1234", "open")
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != RoleAdmin {
		t.Fatal("首个用户应为管理员")
	}
	u2, err := svc.Register("user1", "pass1234", "open")
	if err != nil {
		t.Fatal(err)
	}
	if u2.Role != RoleUser {
		t.Fatal("后续用户应为普通用户")
	}
}

func TestRegisterClosed(t *testing.T) {
	svc, _ := newTestSvc(t)
	if _, err := svc.Register("admin1", "pass1234", "open"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Register("u1", "pass1234", "closed"); err != ErrTooManyUsers {
		t.Fatal("closed 模式下注册应被拒绝, got", err)
	}
}

func TestRegisterApproval(t *testing.T) {
	svc, _ := newTestSvc(t)
	if _, err := svc.Register("admin1", "pass1234", "open"); err != nil {
		t.Fatal(err)
	}
	u, err := svc.Register("u1", "pass1234", "approval")
	if err != nil {
		t.Fatal(err)
	}
	if u.Status != StatusPending {
		t.Fatal("approval 模式下新用户应为 pending")
	}
	// pending 用户不能登录
	if _, _, err := svc.Login("u1", "pass1234", 7, 5, 10); err != ErrPendingApproval {
		t.Fatal("pending 用户登录应被拒绝, got", err)
	}
}

func TestWeakPassword(t *testing.T) {
	svc, _ := newTestSvc(t)
	for _, pw := range []string{"short", "12345678", "abcdefgh", ""} {
		if _, err := svc.Register("u", pw, "open"); err != ErrWeakPassword {
			t.Fatalf("密码 %q 应被拒绝, got %v", pw, err)
		}
	}
}

func TestLoginAndSession(t *testing.T) {
	svc, _ := newTestSvc(t)
	if _, err := svc.Register("alice", "pass1234", "open"); err != nil {
		t.Fatal(err)
	}
	_, token, err := svc.Login("alice", "pass1234", 7, 5, 10)
	if err != nil {
		t.Fatal(err)
	}
	u := svc.Auth(token)
	if u == nil || u.Username != "alice" {
		t.Fatal("有效 token 应通过鉴权")
	}
	if err := svc.Logout(token); err != nil {
		t.Fatal(err)
	}
	if svc.Auth(token) != nil {
		t.Fatal("注销后 token 应失效")
	}
}

func TestLoginRateLimit(t *testing.T) {
	svc, _ := newTestSvc(t)
	if _, err := svc.Register("bob", "pass1234", "open"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		_, _, err := svc.Login("bob", "wrong!", 7, 5, 10)
		if err != ErrInvalidCreds {
			t.Fatalf("第 %d 次错误密码应返回 ErrInvalidCreds, got %v", i+1, err)
		}
	}
	// 第 6 次触发锁定
	if _, _, err := svc.Login("bob", "pass1234", 7, 5, 10); err != ErrAccountLocked {
		t.Fatal("应触发账户锁定, got", err)
	}
}

func TestDeleteUserRules(t *testing.T) {
	svc, _ := newTestSvc(t)
	admin, err := svc.Register("root1", "pass1234", "open")
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.Register("norm", "pass1234", "open")
	if err != nil {
		t.Fatal(err)
	}
	// 普通用户不能删管理员
	if err := svc.DeleteUser(admin.ID, u.ID, RoleUser); err == nil {
		t.Fatal("普通用户删管理员应被拒绝")
	}
	// 管理员删普通用户
	if err := svc.DeleteUser(u.ID, admin.ID, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	// 普通用户删自己（允许）
	u2, err := svc.Register("norm2", "pass1234", "open")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteUser(u2.ID, u2.ID, RoleUser); err != nil {
		t.Fatal("普通用户删自己应允许")
	}
	// 最后一个管理员不能删
	if err := svc.DeleteUser(admin.ID, admin.ID, RoleAdmin); err == nil {
		t.Fatal("最后一个管理员不能被删除")
	}
}

func TestSessionExpiry(t *testing.T) {
	svc, _ := newTestSvc(t)
	if _, err := svc.Register("carol", "pass1234", "open"); err != nil {
		t.Fatal(err)
	}
	// TTL 为 0 天 → 立即过期
	_, token, err := svc.Login("carol", "pass1234", 0, 5, 10)
	if err != nil {
		t.Fatal(err)
	}
	if svc.Auth(token) != nil {
		t.Fatal("过期会话应失效")
	}
}

func TestEncryptedSecretRoundTrip(t *testing.T) {
	_, db := newTestSvc(t)
	enc, err := db.EncryptSecret("test-pass-12345")
	if err != nil {
		t.Fatal(err)
	}
	if enc == "test-pass-12345" {
		t.Fatal("密码不应明文存储")
	}
	got, err := db.DecryptSecret(enc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "test-pass-12345" {
		t.Fatal("解密不一致")
	}
}

func TestSessionExpiresAt(t *testing.T) {
	svc, _ := newTestSvc(t)
	if _, err := svc.Register("dave", "pass1234", "open"); err != nil {
		t.Fatal(err)
	}
	_, token, err := svc.Login("dave", "pass1234", 7, 5, 10)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := svc.db.GetSession(token)
	if err != nil {
		t.Fatal(err)
	}
	if !sess.ExpiresAt.After(time.Now()) {
		t.Fatal("7 天 TTL 的会话应未过期")
	}
}
