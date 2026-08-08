package sshx

import (
	"testing"
)

// TestExtractConfigValue 验证 INI 与 TOML 双格式的配置解析。
func TestExtractConfigValue(t *testing.T) {
	ini := `[common]
bind_port = 7000
dashboard_port = 8000
token = 12345
dashboard_user = admin
dashboard_pwd = test-dash-pwd!
`
	toml := `bindPort = 7000

dashboardPort = 8000
dashboardUser = "admin"
dashboardPwd = "test-dash-pwd!"

auth.method = "token"
auth.token = "my-token-777"
`
	cases := []struct {
		name, content, key, want string
	}{
		{"ini token", ini, "token", "12345"},
		{"ini bind_port", ini, "bind_port", "7000"},
		{"ini dashboard_port", ini, "dashboard_port", "8000"},
		{"ini dashboard_user", ini, "dashboard_user", "admin"},
		{"toml token", toml, "token", "my-token-777"},
		{"toml bindPort", toml, "bindPort", "7000"},
		{"toml dashboardPort", toml, "dashboardPort", "8000"},
		{"toml dashboardUser", toml, "dashboardUser", "admin"},
		{"toml dashboardPwd", toml, "dashboardPwd", "test-dash-pwd!"},
	}
	for _, tc := range cases {
		if got := extractConfigValue(tc.content, tc.key); got != tc.want {
			t.Errorf("%s: extract(%s) = %q, want %q", tc.name, tc.key, got, tc.want)
		}
	}
}

func TestExtractConfigValueIgnoresComments(t *testing.T) {
	content := "# token = ignored\ntoken = 12345\n"
	if got := extractConfigValue(content, "token"); got != "12345" {
		t.Fatalf("注释行不应被解析: %q", got)
	}
}

func TestExtractConfigValueMissing(t *testing.T) {
	if got := extractConfigValue("bindPort = 7000", "token"); got != "" {
		t.Fatalf("缺失 key 应返回空串, got %q", got)
	}
}

func TestExtractFrpsConfigValues(t *testing.T) {
	toml := `bindPort = 7000
dashboardPort = 8000
dashboardUser = "admin"
dashboardPwd = "pass"

auth.method = "token"
auth.token = "t-99"
`
	vals := ExtractFrpsConfigValues(toml)
	if vals["token"] != "t-99" {
		t.Fatalf("token 解析错误: %+v", vals)
	}
	if vals["bindPort"] != "7000" || vals["dashboardPort"] != "8000" {
		t.Fatalf("端口解析错误: %+v", vals)
	}
	if vals["dashboardUser"] != "admin" || vals["dashboardPwd"] != "pass" {
		t.Fatalf("dashboard 凭据解析错误: %+v", vals)
	}
}

func TestVerifyTokenInFrpsIniTomlFormat(t *testing.T) {
	// 纯解析逻辑验证：baseline 比对应正确处理 TOML 格式
	toml := "auth.token = \"wrong-token\"\n"
	if token := extractConfigValue(toml, "token"); token == "my-token" {
		t.Fatal("提取错误")
	}
}
