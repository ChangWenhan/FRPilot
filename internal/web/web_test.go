package web

import (
	"bytes"
	"encoding/json"
	"frpmon/internal/actions"
	"frpmon/internal/ai"
	"frpmon/internal/collector"
	"frpmon/internal/traffic"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"frpmon/internal/auth"
	"frpmon/internal/config"
	"frpmon/internal/registry"
	"frpmon/internal/store"
)

func newTestServer(t *testing.T) (*httptest.Server, *http.Client, *store.DB) {
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
	authSvc := auth.NewService(db)
	reg := registry.NewService(db, cfg)
	col := collector.New(db, cfg)
	t.Cleanup(col.Stop)
	traff := traffic.New(db, cfg)
	tasks := actions.NewTaskManager(db, cfg)
	aiSvc := ai.New(db, cfg)
	srv := NewServer(db, authSvc, cfg, reg, col, traff, tasks, aiSvc)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts, ts.Client(), db
}

func postJSON(t *testing.T, ts *http.Client, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := ts.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestAuthFlow(t *testing.T) {
	ts, c, _ := newTestServer(t)

	// 未登录访问被拦截
	resp, err := c.Get(ts.URL + "/api/machines")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("未登录应 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 注册首个用户 → 管理员
	resp = postJSON(t, c, ts.URL+"/api/auth/register", map[string]string{"username": "root1", "password": "pass1234"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("注册失败: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 登录拿 cookie
	resp = postJSON(t, c, ts.URL+"/api/auth/login", map[string]string{"username": "root1", "password": "pass1234"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("登录失败: %d", resp.StatusCode)
	}
	var cookies []*http.Cookie
	cookies = append(cookies, resp.Cookies()...)
	resp.Body.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/me", nil)
	for _, ck := range cookies {
		req.AddCookie(ck)
	}
	resp2, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("me 接口失败: %d", resp2.StatusCode)
	}
	var me struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	json.NewDecoder(resp2.Body).Decode(&me)
	resp2.Body.Close()
	if me.Username != "root1" || me.Role != "admin" {
		t.Fatalf("me 信息错误: %+v", me)
	}
}

func TestRoleEnforcement(t *testing.T) {
	ts, c, _ := newTestServer(t)
	// 管理员
	postJSON(t, c, ts.URL+"/api/auth/register", map[string]string{"username": "root1", "password": "pass1234"}).Body.Close()
	// 普通用户
	postJSON(t, c, ts.URL+"/api/auth/register", map[string]string{"username": "user1", "password": "pass1234"}).Body.Close()

	login := func(u, p string) *http.Cookie {
		resp := postJSON(t, c, ts.URL+"/api/auth/login", map[string]string{"username": u, "password": p})
		resp.Body.Close()
		if len(resp.Cookies()) == 0 {
			t.Fatalf("%s 登录未拿到 cookie", u)
		}
		return resp.Cookies()[0]
	}

	authed := func(method, path string, ck *http.Cookie) *http.Response {
		req, _ := http.NewRequest(method, ts.URL+path, nil)
		req.AddCookie(ck)
		r, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	userCk := login("user1", "pass1234")
	adminCk := login("root1", "pass1234")

	// 普通用户可读机器列表
	if r := authed(http.MethodGet, "/api/machines", userCk); r.StatusCode != http.StatusOK {
		t.Fatalf("普通用户应能看机器列表, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}
	// 普通用户不能访问用户管理/设置
	if r := authed(http.MethodGet, "/api/users", userCk); r.StatusCode != http.StatusForbidden {
		t.Fatalf("普通用户访问 /api/users 应 403, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}
	if r := authed(http.MethodGet, "/api/settings", userCk); r.StatusCode != http.StatusForbidden {
		t.Fatalf("普通用户访问 /api/settings 应 403, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}
	// 管理员可访问
	if r := authed(http.MethodGet, "/api/users", adminCk); r.StatusCode != http.StatusOK {
		t.Fatalf("管理员应能访问 /api/users, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}
}

func TestMachineLifecycleAPI(t *testing.T) {
	ts, c, db := newTestServer(t)
	postJSON(t, c, ts.URL+"/api/auth/register", map[string]string{"username": "root1", "password": "pass1234"}).Body.Close()
	resp := postJSON(t, c, ts.URL+"/api/auth/login", map[string]string{"username": "root1", "password": "pass1234"})
	resp.Body.Close()
	ck := resp.Cookies()[0]

	authed := func(method, path string, body any) *http.Response {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, ts.URL+path, rd)
		req.AddCookie(ck)
		req.Header.Set("Content-Type", "application/json")
		r, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	// 未发现机器时空列表
	r := authed(http.MethodGet, "/api/machines", nil)
	var listResp struct {
		Machines []map[string]any `json:"machines"`
	}
	json.NewDecoder(r.Body).Decode(&listResp)
	r.Body.Close()
	if len(listResp.Machines) != 0 {
		t.Fatal("初始机器列表应为空")
	}

	// 通过服务端同一 DB 直接建档模拟发现（与 API 共享数据库）
	_, _, err := db.UpsertMachineFromDiscovery("ssh_wenhan_4090", 6005)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _ = db.UpsertMachineFromDiscovery("ssh_m2", 6006)

	// 填凭据
	r = authed(http.MethodPost, "/api/machines/1/credentials",
		map[string]string{"sshUser": "root", "sshPass": "s3cret"})
	if r.StatusCode != http.StatusOK {
		t.Fatalf("填凭据失败: %d", r.StatusCode)
	}
	r.Body.Close()

	// 启用监控（未配置过凭据的机器 id=2 应先失败）
	r = authed(http.MethodPost, "/api/machines/2/enable", map[string]bool{"enabled": true})
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("未配置凭据的机器启用应 400, got %d", r.StatusCode)
	}
	r.Body.Close()

	r = authed(http.MethodPost, "/api/machines/1/enable", map[string]bool{"enabled": true})
	if r.StatusCode != http.StatusOK {
		t.Fatalf("启用监控失败: %d", r.StatusCode)
	}
	r.Body.Close()

	// 列表应显示状态 enabled
	r = authed(http.MethodGet, "/api/machines", nil)
	json.NewDecoder(r.Body).Decode(&listResp)
	r.Body.Close()
	found := false
	for _, mm := range listResp.Machines {
		if mm["name"] == "ssh_wenhan_4090" && mm["status"] == "enabled" {
			found = true
		}
	}
	if !found {
		t.Fatal("列表应包含已启用机器")
	}
}

func TestSettingsTokenReadonly(t *testing.T) {
	ts, c, _ := newTestServer(t)
	postJSON(t, c, ts.URL+"/api/auth/register", map[string]string{"username": "root1", "password": "pass1234"}).Body.Close()
	resp := postJSON(t, c, ts.URL+"/api/auth/login", map[string]string{"username": "root1", "password": "pass1234"})
	resp.Body.Close()
	ck := resp.Cookies()[0]

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/settings", nil)
	req.AddCookie(ck)
	r, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	json.NewDecoder(r.Body).Decode(&s)
	r.Body.Close()
	frps := s["frps"].(map[string]any)
	if frps["tokenSet"] != false {
		t.Fatal("初始 token 应未设置")
	}
	if frps["tokenReadonly"] != false {
		t.Fatal("未设置时不应标记只读")
	}

	// 使用保存设置接口携带 token 设置基线（模拟手动设置/自动检测落库）
	req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/settings",
		bytes.NewReader([]byte(`{"registration":"open","frps":{"token":"abc-token-123"}}`)))
	req2.AddCookie(ck)
	req2.Header.Set("Content-Type", "application/json")
	r2, err := c.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("设置 token 失败: %d", r2.StatusCode)
	}

	// 再次读取：token 已设置且只读
	r3, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/settings", nil)
	r3.AddCookie(ck)
	resp3, err := c.Do(r3)
	if err != nil {
		t.Fatal(err)
	}
	var s3 map[string]any
	json.NewDecoder(resp3.Body).Decode(&s3)
	resp3.Body.Close()
	frps3 := s3["frps"].(map[string]any)
	if frps3["tokenBaseline"] != "abc-token-123" {
		t.Fatalf("token 基线应已设置, got %v", frps3["tokenBaseline"])
	}
	if frps3["tokenReadonly"] != true {
		t.Fatal("设置后 token 应标记只读")
	}

	// 普通保存（不带 token 字段）不应清空基线
	req4, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/settings",
		bytes.NewReader([]byte(`{"registration":"approval","frps":{}}`)))
	req4.AddCookie(ck)
	req4.Header.Set("Content-Type", "application/json")
	r4, _ := c.Do(req4)
	r4.Body.Close()
	req5, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/settings", nil)
	req5.AddCookie(ck)
	resp5, _ := c.Do(req5)
	var s5 map[string]any
	json.NewDecoder(resp5.Body).Decode(&s5)
	resp5.Body.Close()
	if frps5 := s5["frps"].(map[string]any); frps5["tokenBaseline"] != "abc-token-123" {
		t.Fatal("普通保存不应清空 token 基线")
	}
}

// TestStaticRoutes 验证嵌入式前端：根路径 200 且含 SPA 内容，
// history 路由（/machines 等）回退到 index.html 且不产生 301 重定向循环。
func TestStaticRoutes(t *testing.T) {
	ts, c, _ := newTestServer(t)
	for _, path := range []string{"/", "/machines", "/settings", "/login"} {
		resp, err := c.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s 应返回 200, got %d", path, resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if !strings.Contains(string(body), "<div id=\"app\">") {
			t.Fatalf("%s 应返回 SPA index.html 内容", path)
		}
	}
	// 未登录时 API 仍应 401（静态回退不应吞掉 API 路径）
	resp, err := c.Get(ts.URL + "/api/machines")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/api/machines 未登录应 401, got %d", resp.StatusCode)
	}
}
