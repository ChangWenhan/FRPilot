package frpsapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ServerInfo struct {
	Version         string         `json:"version"`
	BindPort        int            `json:"bindPort"`
	TotalTrafficIn  int64          `json:"totalTrafficIn"`
	TotalTrafficOut int64          `json:"totalTrafficOut"`
	CurConns        int            `json:"curConns"`
	ClientCounts    int            `json:"clientCounts"`
	ProxyTypeCount  map[string]int `json:"proxyTypeCount"`
}

type Proxy struct {
	Name            string `json:"name"`
	Conf            ProxyConf `json:"conf"`
	ClientVersion   string `json:"clientVersion"`
	TodayTrafficIn  int64  `json:"todayTrafficIn"`
	TodayTrafficOut int64  `json:"todayTrafficOut"`
	CurConns        int    `json:"curConns"`
	LastStartTime   string `json:"lastStartTime"`
	LastCloseTime   string `json:"lastCloseTime"`
	Status          string `json:"status"`
}

type ProxyConf struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	LocalIP    string `json:"localIP"`
	RemotePort int    `json:"remotePort"`
}

type Client struct {
	BaseURL string
	User    string
	Pass    string
	Token   string
	HTTP    *http.Client
}

func NewClient(baseURL, user, pass, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		User:    user,
		Pass:    pass,
		Token:   token,
		HTTP:    &http.Client{Timeout: 8 * time.Second},
	}
}

func (c *Client) get(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.User, c.Pass)
	if c.Token != "" {
		req.Header.Set("X-Frp-Token", c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("连接 frps dashboard 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("frps dashboard 鉴权失败（检查 dashboardUser/dashboardPwd）")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("frps dashboard 返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

func (c *Client) ServerInfo() (*ServerInfo, error) {
	info := &ServerInfo{}
	if err := c.get("/api/serverinfo", info); err != nil {
		return nil, err
	}
	return info, nil
}

// Proxies 拉取全部 tcp 代理（每台 frpc 机器一个隧道）。
func (c *Client) Proxies() ([]Proxy, error) {
	var resp struct {
		Proxies []Proxy `json:"proxies"`
	}
	if err := c.get("/api/proxy/tcp", &resp); err != nil {
		return nil, err
	}
	return resp.Proxies, nil
}

// Test 连接测试：拉 serverinfo，返回汇总信息。
func (c *Client) Test() (*ServerInfo, error) { return c.ServerInfo() }
