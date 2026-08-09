package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"frpmon/internal/actions"
	"frpmon/internal/store"
)

var errPreviewOne = errors.New("预览一次只能选择一台机器")

func (s *Server) handleCleanupItems(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.tasks.ListItems()})
}

type cleanupReq struct {
	MachineIDs []int64 `json:"machineIds"`
	ItemIDs    []string `json:"itemIds"`
}

func (s *Server) handleCleanupPreview(w http.ResponseWriter, r *http.Request) {
	var req cleanupReq
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.MachineIDs) != 1 {
		errJSON(w, http.StatusBadRequest, errPreviewOne)
		return
	}
	results, err := s.tasks.Preview(req.MachineIDs[0], req.ItemIDs)
	if err != nil {
		errJSON(w, http.StatusBadGateway, err)
		return
	}
	u := userOf(r)
	_ = s.db.Log(u.ID, u.Username, "cleanup_preview", "machine#"+strconv.FormatInt(req.MachineIDs[0], 10), "预览")
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) handleCleanupStart(w http.ResponseWriter, r *http.Request) {
	var req cleanupReq
	if !readJSON(w, r, &req) {
		return
	}
	u := userOf(r)
	task, err := s.tasks.StartCleanup(req.MachineIDs, req.ItemIDs, u.ID, u.Username)
	if err != nil {
		errJSON(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

type scanReq struct {
	MachineIDs []int64 `json:"machineIds"`
	Mode       string  `json:"mode"`
}

func (s *Server) handleScanStart(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	var req scanReq
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.MachineIDs) == 0 {
		errJSON(w, http.StatusBadRequest, errors.New("请选择至少一台机器"))
		return
	}
	task, err := s.tasks.StartScan(req.MachineIDs, req.Mode, u.ID, u.Username)
	if err != nil {
		errJSON(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"taskId": task.ID})
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": s.tasks.ListTasks(limit)})
}

func (s *Server) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	m, err := s.db.GetMachineByID(id)
	if err != nil {
		errJSON(w, http.StatusNotFound, err)
		return
	}
	rep, err := actions.RunHealth(s.db, s.cfg, m)
	if err != nil {
		errJSON(w, http.StatusBadRequest, err)
		return
	}
	// 存档
	itemsJSON, _ := json.Marshal(rep.Items)
	_ = s.db.SaveHealthReport(&store.HealthReport{
		TS: rep.TS, MachineID: m.ID, Machine: rep.Machine,
		Score: rep.Score, Overall: rep.Overall, ItemsJSON: string(itemsJSON),
	})
	u := userOf(r)
	_ = s.db.Log(u.ID, u.Username, "health_check", m.Name, "评分 "+strconv.Itoa(rep.Score)+" ("+rep.Overall+")")
	writeJSON(w, http.StatusOK, map[string]any{"report": rep})
}

func (s *Server) handleHealthReports(w http.ResponseWriter, r *http.Request) {
	machineID := int64(0)
	if v := r.URL.Query().Get("machineId"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			machineID = n
		}
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	reports, err := s.db.ListHealthReports(machineID, limit)
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err)
		return
	}
	if reports == nil {
		reports = []*store.HealthReport{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": reports})
}
