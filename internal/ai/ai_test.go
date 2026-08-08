package ai

import (
	"net/http"
	"net/http/httptest"
	"strings"
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
	return New(db, cfg), db, cfg
}

func TestDiagnoseRequiresSetup(t *testing.T) {
	svc, db, _ := newTestSvc(t)
	m, _, _ := db.UpsertMachineFromDiscovery("ssh_a", 6005)
	// 未启用
	if _, err := svc.Diagnose(m.ID); err == nil {
		t.Fatal("未启用时应报错")
	}
	// 启用但缺配置
	_ = svc.cfg.Update(func(c *config.AppConfig) { c.AI.Enabled = true })
	if _, err := svc.Diagnose(m.ID); err == nil {
		t.Fatal("缺 Provider/Model 时应报错")
	}
	_ = svc.cfg.Update(func(c *config.AppConfig) {
		c.AI.Enabled = true
		c.AI.ProviderURL = "http://127.0.0.1:1"
		c.AI.Model = "test-model"
	})
	// 缺 API Key
	if _, err := svc.Diagnose(m.ID); err == nil {
		t.Fatal("缺 API Key 时应报错")
	}
	// 无体检报告
	_ = db.SetSetting("ai_api_key", "placeholder")
	if _, err := svc.Diagnose(m.ID); err == nil {
		t.Fatal("无体检报告时应报错")
	}
}

func TestContainsCommandLike(t *testing.T) {
	// 纯分析文字 → 不标记
	clean := "CPU 使用率偏高，可能原因是后台任务过多。建议检查占用进程。"
	if containsCommandLike(clean) {
		t.Fatal("纯文字不应标记")
	}
	// 含命令 → 标记
	for _, cmd := range []string{
		"rm -rf /tmp/x",
		"sudo systemctl restart fail2ban",
		"apt-get clean",
		"执行 `df -h` 查看",
		"docker system prune",
		"kill -9 1234",
	} {
		if !containsCommandLike(cmd) {
			t.Fatalf("含命令内容应被标记: %q", cmd)
		}
	}
	// 普通英文单词不应误报
	if containsCommandLike("running service check") {
		t.Fatal("普通英文不应误报")
	}
}

func TestPromptForbidsCommands(t *testing.T) {
	p := buildPrompt("node1", "warn", 80, `[{"title":"CPU 过高"}]`, `{"cpu":90}`)
	if !strings.Contains(p, "禁止") || !strings.Contains(p, "命令") {
		t.Fatalf("提示词应包含命令禁止约束")
	}
	if !strings.Contains(p, "node1") || !strings.Contains(p, "80") {
		t.Fatal("提示词应包含机器与评分上下文")
	}
}

func TestCallLLMWithMock(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 校验鉴权头
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"诊断结果：CPU 负载偏高。"}}]}`))
	}))
	defer ts.Close()
	text, err := callLLM(ts.URL, "m1", "sk-test", "prompt", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "CPU 负载偏高") {
		t.Fatalf("解析 LLM 响应失败: %q", text)
	}
}

func TestCallLLMError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer ts.Close()
	if _, err := callLLM(ts.URL, "m1", "bad", "p", 5); err == nil {
		t.Fatal("错误响应应返回错误")
	}
}
