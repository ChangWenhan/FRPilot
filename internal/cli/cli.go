package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Client frpm CLI：瘦客户端，通过 HTTP 调用本机 FRPilot API。
// 权限与审计完全复用服务端逻辑。
type Client struct {
	Server string // 如 http://127.0.0.1:8443
	HTTP   *http.Client
}

const (
	tokenDir  = ".config/frpmon"
	tokenFile = "token"
)

func TokenPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, tokenDir, tokenFile)
}

func SaveToken(token string) error {
	p := TokenPath()
	if p == "" {
		return fmt.Errorf("无法确定用户目录")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(token), 0o600)
}

func LoadToken() (string, error) {
	b, err := os.ReadFile(TokenPath())
	if err != nil {
		return "", fmt.Errorf("未登录：请先运行 frpm login（%w）", err)
	}
	return strings.TrimSpace(string(b)), nil
}

func (c *Client) do(method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.Server+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token, err := LoadToken(); err == nil {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("连接 FRPilot 失败（%s）: %w", c.Server, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = strings.TrimSpace(string(raw))
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, e.Error)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (c *Client) Login(user, pass string) (string, error) {
	var resp struct {
		Username string `json:"username"`
		Role     string `json:"role"`
		Token    string `json:"token"`
	}
	req, err := http.NewRequest(http.MethodPost, c.Server+"/api/auth/login", bytes.NewReader([]byte(fmt.Sprintf(
		`{"username":%q,"password":%q}`, user, pass))))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-FRPilot-Client", "cli")
	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer httpResp.Body.Close()
	raw, _ := io.ReadAll(httpResp.Body)
	if httpResp.StatusCode >= 400 {
		return "", fmt.Errorf("登录失败: %s", strings.TrimSpace(string(raw)))
	}
	_ = json.Unmarshal(raw, &resp)
	if resp.Token != "" {
		return resp.Token, nil
	}
	for _, ck := range httpResp.Cookies() {
		if ck.Name == "frpmon_token" {
			return ck.Value, nil
		}
	}
	return "", fmt.Errorf("未收到会话令牌")
}

func (c *Client) Machines() ([]map[string]any, error) {
	var resp struct {
		Machines []map[string]any `json:"machines"`
	}
	if err := c.do(http.MethodGet, "/api/machines", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Machines, nil
}

func (c *Client) Status() (map[string]any, error) {
	var out map[string]any
	if err := c.do(http.MethodGet, "/api/status", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) Discover() (map[string]any, error) {
	var out map[string]any
	if err := c.do(http.MethodPost, "/api/machines/discover", struct{}{}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) TestFrps() (map[string]any, error) {
	var out map[string]any
	if err := c.do(http.MethodPost, "/api/settings/test-frps", struct{}{}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) VerifyToken() (map[string]any, error) {
	var out map[string]any
	if err := c.do(http.MethodPost, "/api/settings/verify-token", struct{}{}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SetCredentials(id string, sshUser, sshPass string) error {
	return c.do(http.MethodPost, "/api/machines/"+id+"/credentials",
		map[string]string{"sshUser": sshUser, "sshPass": sshPass}, nil)
}

func (c *Client) SetEnabled(id string, enabled bool) error {
	return c.do(http.MethodPost, "/api/machines/"+id+"/enable",
		map[string]bool{"enabled": enabled}, nil)
}

// Snapshot 拉取机器最新快照（含各模块数据）。
func (c *Client) Snapshot(id string) (map[string]any, error) {
	var out map[string]any
	if err := c.do(http.MethodGet, "/api/machines/"+id+"/snapshot", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CollectNow(id string) error {
	return c.do(http.MethodPost, "/api/machines/"+id+"/collect-now", struct{}{}, nil)
}

func (c *Client) Traffic() (map[string]any, error) {
	var out map[string]any
	if err := c.do(http.MethodGet, "/api/traffic", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) TrafficHistory(hours int) ([]map[string]any, error) {
	var resp struct {
		Points []map[string]any `json:"points"`
	}
	if err := c.do(http.MethodGet, fmt.Sprintf("/api/traffic/history?hours=%d", hours), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Points, nil
}

func (c *Client) Audit(limit int) ([]map[string]any, error) {
	var resp struct {
		Audit []map[string]any `json:"audit"`
	}
	if err := c.do(http.MethodGet, fmt.Sprintf("/api/audit?limit=%d", limit), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Audit, nil
}

func (c *Client) Settings() (map[string]any, error) {
	var out map[string]any
	if err := c.do(http.MethodGet, "/api/settings", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
