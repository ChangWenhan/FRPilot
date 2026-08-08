package frpsapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newMockServer(t *testing.T, requireAuth bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/serverinfo", func(w http.ResponseWriter, r *http.Request) {
		if requireAuth {
			u, p, ok := r.BasicAuth()
			if !ok || u != "admin" || p != "secret" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"version": "0.57.0", "bindPort": 7000,
			"totalTrafficIn": 1000, "totalTrafficOut": 500,
			"curConns": 3, "clientCounts": 2,
			"proxyTypeCount": map[string]int{"tcp": 2},
		})
	})
	mux.HandleFunc("/api/proxy/tcp", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"proxies": []map[string]any{
				{"name": "ssh_m1", "status": "online", "clientVersion": "0.57.0",
					"todayTrafficIn": 10, "todayTrafficOut": 20,
					"conf": map[string]any{"name": "ssh_m1", "type": "tcp", "localIP": "127.0.0.1", "remotePort": 6005}},
				{"name": "ssh_m2", "status": "offline",
					"conf": map[string]any{"name": "ssh_m2", "type": "tcp", "localIP": "127.0.0.1", "remotePort": 6006}},
			},
		})
	})
	return httptest.NewServer(mux)
}

func TestServerInfo(t *testing.T) {
	ts := newMockServer(t, false)
	defer ts.Close()
	c := NewClient(ts.URL, "", "", "")
	info, err := c.ServerInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "0.57.0" || info.ClientCounts != 2 || info.TotalTrafficIn != 1000 {
		t.Fatalf("解析错误: %+v", info)
	}
}

func TestAuthFailure(t *testing.T) {
	ts := newMockServer(t, true)
	defer ts.Close()
	c := NewClient(ts.URL, "admin", "wrong", "")
	if _, err := c.ServerInfo(); err == nil {
		t.Fatal("错误凭据应返回鉴权失败")
	}
}

func TestAuthSuccess(t *testing.T) {
	ts := newMockServer(t, true)
	defer ts.Close()
	c := NewClient(ts.URL, "admin", "secret", "")
	if _, err := c.ServerInfo(); err != nil {
		t.Fatal(err)
	}
}

func TestProxiesParse(t *testing.T) {
	ts := newMockServer(t, false)
	defer ts.Close()
	c := NewClient(ts.URL, "", "", "")
	ps, err := c.Proxies()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 {
		t.Fatalf("应解析 2 个代理, got %d", len(ps))
	}
	if ps[0].Name != "ssh_m1" || ps[0].Conf.RemotePort != 6005 || ps[0].Status != "online" {
		t.Fatalf("代理字段解析错误: %+v", ps[0])
	}
}
