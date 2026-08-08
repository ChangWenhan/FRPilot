package web

import (
	"net/http"
	"strconv"

	"frpmon/internal/store"
)

// handleDiagnose 对指定机器执行 AI 诊断（基于最近体检报告）。
func (s *Server) handleDiagnose(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	res, err := s.ai.Diagnose(id)
	if err != nil {
		errJSON(w, http.StatusBadGateway, err)
		return
	}
	// 存档
	if res.Text != "" {
		_ = s.db.SaveDiagnostic(&store.AIDiagnostic{
			TS: res.TS, MachineID: id, Machine: res.Machine,
			Overall: res.Report, Score: res.Score, Text: res.Text, Flagged: res.Flagged,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"diagnosis": res})
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	machineID := int64(0)
	if v := r.URL.Query().Get("machineId"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			machineID = n
		}
	}
	limit := 10
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	diags, err := s.db.ListDiagnostics(machineID, limit)
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err)
		return
	}
	if diags == nil {
		diags = []*store.AIDiagnostic{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"diagnostics": diags})
}
