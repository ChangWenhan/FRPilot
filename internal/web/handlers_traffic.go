package web

import (
	"net/http"
	"strconv"

	"frpmon/internal/store"
)

// handleTraffic 返回带宽流向快照（各 proxy 流量/速率/占比/异常 + Top 流向）。
func (s *Server) handleTraffic(w http.ResponseWriter, r *http.Request) {
	snap, err := s.traffic.Latest()
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err)
		return
	}
	if snap == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ts": nil, "flows": []any{}, "message": "暂无流量数据（frps dashboard 未配置或轮询未开始）",
		})
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// handleTrafficHistory 返回流量历史时间序列（聚合或单 proxy）。
func (s *Server) handleTrafficHistory(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if v := r.URL.Query().Get("hours"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 168 {
			hours = n
		}
	}
	proxy := r.URL.Query().Get("proxy")
	pts, err := s.db.GetTrafficHistory(proxy, hours)
	if err != nil {
		if err == store.ErrNotFound {
			writeJSON(w, http.StatusOK, map[string]any{"points": []any{}})
			return
		}
		errJSON(w, http.StatusInternalServerError, err)
		return
	}
	if pts == nil {
		pts = []*store.TrafficPoint{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"points": pts})
}
