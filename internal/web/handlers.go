package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"frpmon/internal/config"
	"frpmon/internal/registry"
	"frpmon/internal/store"
)

var errPasswordWrong = errors.New("旧密码不正确")

func (s *Server) handleMachines(w http.ResponseWriter, r *http.Request) {
	ms, err := s.db.ListMachines()
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err)
		return
	}
	type vm struct {
		*store.Machine
		HasCredentials bool `json:"hasCredentials"`
		HasSudo        bool `json:"hasSudo"`
	}
	out := make([]vm, 0, len(ms))
	for _, m := range ms {
		out = append(out, vm{Machine: m,
			HasCredentials: m.SSHUser != "" && m.SSHPassEnc != "",
			HasSudo:        m.SudoPassEnc != "",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"machines": out})
}

func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	client, err := registry.NewFrpsClient(s.cfg)
	if err != nil {
		errJSON(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.registry.Discover(client)
	if err != nil {
		errJSON(w, http.StatusBadGateway, err)
		return
	}
	_ = s.db.Log(u.ID, u.Username, "discover", "frps", formatDiscover(res))
	writeJSON(w, http.StatusOK, res)
}

func formatDiscover(r *registry.DiscoveryResult) string {
	return "发现 " + strconv.Itoa(r.Total) + " 个隧道，新增 " + strconv.Itoa(len(r.NewFound))
}

type credReq struct {
	SSHUser  string `json:"sshUser"`
	SSHPass  string `json:"sshPass"`
	SudoPass string `json:"sudoPass"`
}

func (s *Server) handleMachineCredentials(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	var req credReq
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.registry.UpdateCredentials(id, req.SSHUser, req.SSHPass, req.SudoPass); err != nil {
		errJSON(w, http.StatusBadRequest, err)
		return
	}
	u := userOf(r)
	_ = s.db.Log(u.ID, u.Username, "set_credentials", "machine#"+strconv.FormatInt(id, 10), "user="+req.SSHUser)
	writeJSON(w, http.StatusOK, map[string]string{"message": "凭据已保存，可启用监控"})
}

type enableReq struct {
	Enabled bool `json:"enabled"`
}

func (s *Server) handleMachineEnable(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	var req enableReq
	if !readJSON(w, r, &req) {
		return
	}
	m, err := s.registry.SetEnabled(id, req.Enabled)
	if err != nil {
		errJSON(w, http.StatusBadRequest, err)
		return
	}
	u := userOf(r)
	action := "enable_monitor"
	if !req.Enabled {
		action = "disable_monitor"
	}
	_ = s.db.Log(u.ID, u.Username, action, m.Name, "")
	// 同步采集循环（启动/停止）
	s.collector.SyncMachine(m)
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleMachineSnapshot(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	m, err := s.db.GetMachineByID(id)
	if err != nil {
		errJSON(w, http.StatusNotFound, err)
		return
	}
	snap, err := s.db.GetSnapshot(id)
	if err != nil {
		if err == store.ErrNotFound {
			writeJSON(w, http.StatusOK, map[string]any{
				"machine": m, "data": nil,
				"collecting": s.collector.IsCollecting(id),
			})
			return
		}
		errJSON(w, http.StatusInternalServerError, err)
		return
	}
	var data any
	_ = json.Unmarshal([]byte(snap.Data), &data)
	writeJSON(w, http.StatusOK, map[string]any{
		"machine": m, "data": data,
		"ts":         snap.TS,
		"collecting": s.collector.IsCollecting(id),
	})
}

func (s *Server) handleCollectNow(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	m, err := s.db.GetMachineByID(id)
	if err != nil {
		errJSON(w, http.StatusNotFound, err)
		return
	}
	if m.SSHUser == "" || m.SSHPassEnc == "" {
		errJSON(w, http.StatusBadRequest, errors.New("该机器未配置 SSH 凭据"))
		return
	}
	u := userOf(r)
	if err := s.collector.CollectNow(m); err != nil {
		_ = s.db.Log(u.ID, u.Username, "collect_now", m.Name, "失败: "+err.Error())
		errJSON(w, http.StatusBadGateway, err)
		return
	}
	_ = s.db.Log(u.ID, u.Username, "collect_now", m.Name, "ok")
	writeJSON(w, http.StatusOK, map[string]string{"message": "采集完成"})
}

func (s *Server) handleMachineMetrics(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	hours := 24
	if v := r.URL.Query().Get("hours"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 168 {
			hours = n
		}
	}
	pts, err := s.db.GetMetrics(id, hours)
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err)
		return
	}
	if pts == nil {
		pts = []*store.MetricPoint{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"points": pts})
}

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

// ---- 设置 ----

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	c := s.cfg.Get()
	tokenSet := c.Frps.Token != ""
	writeJSON(w, http.StatusOK, map[string]any{
		"listenAddr":     c.ListenAddr,
		"registration":   c.Registration,
		"sessionTTLDays": c.SessionTTLDays,
		"health":         c.Health,
		"cleanupCustom":  c.CleanupCustom,
		"ai": map[string]any{
			"enabled":     c.AI.Enabled,
			"providerUrl": c.AI.ProviderURL,
			"model":       c.AI.Model,
			"timeoutSec":  c.AI.TimeoutSec,
			"apiKeyMask":  s.aiKeyMask(),
			"hasKey":      s.aiKeyMask() != "",
		},
		"frps": map[string]any{
			"dashboardUrl":      c.Frps.DashboardURL,
			"dashboardUser":     c.Frps.DashboardUser,
			"dashboardPassMask": maskSecret(c.Frps.DashboardPass),
			"dashboardPassSet":  c.Frps.DashboardPass != "",
			"sshHost":           c.Frps.SSHHost,
			"sshPort":           c.Frps.SSHPort,
			"sshUser":           c.Frps.SSHUser,
			"sshPassMask":       maskSecret(c.Frps.SSHPass),
			"sshPassSet":        c.Frps.SSHPass != "",
			"configPath":        c.Frps.ConfigPath,
			"tokenMask":         maskSecret(c.Frps.Token),
			"tokenSet":          tokenSet,
			"tokenReadonly":     tokenSet,
		},
	})
}

func maskSecret(value string) string {
	if value == "" {
		return ""
	}
	return "••••••••"
}

// aiKeyMask 返回 API Key 掩码（加密存储，仅显示尾 4 位）。
func (s *Server) aiKeyMask() string {
	enc, err := s.db.GetSetting("ai_api_key")
	if err != nil || enc == "" {
		return ""
	}
	key, err := s.db.DecryptSecret(enc)
	if err != nil || key == "" {
		return ""
	}
	if len(key) <= 4 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}

type saveSettingsReq struct {
	Registration *string `json:"registration"`
	Frps         struct {
		DashboardURL  *string `json:"dashboardUrl"`
		DashboardUser *string `json:"dashboardUser"`
		DashboardPass *string `json:"dashboardPass"`
		SSHHost       *string `json:"sshHost"`
		SSHPort       *int    `json:"sshPort"`
		SSHUser       *string `json:"sshUser"`
		SSHPass       *string `json:"sshPass"`
		ConfigPath    *string `json:"configPath"`
		Token         *string `json:"token"`
	} `json:"frps"`
	Health        *config.HealthThresholds   `json:"health"`
	CleanupCustom []config.CustomCleanupItem `json:"cleanupCustom"`
	AI            *struct {
		Enabled     *bool   `json:"enabled"`
		ProviderURL *string `json:"providerUrl"`
		Model       *string `json:"model"`
		TimeoutSec  *int    `json:"timeoutSec"`
		APIKey      *string `json:"apiKey"` // 传入新值则更新（掩码值忽略）
	} `json:"ai"`
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var req saveSettingsReq
	if !readJSON(w, r, &req) {
		return
	}
	curr := s.cfg.Get()
	registration := curr.Registration
	if req.Registration != nil {
		registration = *req.Registration
	}
	if registration != "open" && registration != "approval" && registration != "closed" {
		errJSON(w, http.StatusBadRequest, errors.New("非法注册模式"))
		return
	}
	err := s.cfg.Update(func(c *config.AppConfig) {
		c.Registration = registration
		if req.Frps.DashboardURL != nil {
			c.Frps.DashboardURL = strings.TrimSpace(*req.Frps.DashboardURL)
		}
		if req.Frps.DashboardUser != nil {
			c.Frps.DashboardUser = strings.TrimSpace(*req.Frps.DashboardUser)
		}
		if req.Frps.DashboardPass != nil && *req.Frps.DashboardPass != "" && !strings.HasPrefix(*req.Frps.DashboardPass, "••") && !strings.HasPrefix(*req.Frps.DashboardPass, "****") {
			c.Frps.DashboardPass = *req.Frps.DashboardPass
		}
		if req.Frps.SSHHost != nil {
			c.Frps.SSHHost = strings.TrimSpace(*req.Frps.SSHHost)
		}
		if req.Frps.SSHPort != nil {
			c.Frps.SSHPort = *req.Frps.SSHPort
		}
		if req.Frps.SSHUser != nil {
			c.Frps.SSHUser = strings.TrimSpace(*req.Frps.SSHUser)
		}
		if req.Frps.SSHPass != nil && *req.Frps.SSHPass != "" && !strings.HasPrefix(*req.Frps.SSHPass, "••") && !strings.HasPrefix(*req.Frps.SSHPass, "****") {
			c.Frps.SSHPass = *req.Frps.SSHPass
		}
		if req.Frps.ConfigPath != nil {
			c.Frps.ConfigPath = strings.TrimSpace(*req.Frps.ConfigPath)
		}
		// token 基线只接受显式新值；GET 返回的掩码永远不会被当成新 token。
		if req.Frps.Token != nil && *req.Frps.Token != "" && !strings.HasPrefix(*req.Frps.Token, "••") && !strings.HasPrefix(*req.Frps.Token, "****") {
			c.Frps.Token = *req.Frps.Token
		}
		if req.Health != nil {
			c.Health = *req.Health
		}
		if req.CleanupCustom != nil {
			c.CleanupCustom = req.CleanupCustom
		}
		if req.AI != nil {
			if req.AI.Enabled != nil {
				c.AI.Enabled = *req.AI.Enabled
			}
			if req.AI.ProviderURL != nil {
				c.AI.ProviderURL = strings.TrimSpace(*req.AI.ProviderURL)
			}
			if req.AI.Model != nil {
				c.AI.Model = strings.TrimSpace(*req.AI.Model)
			}
			if req.AI.TimeoutSec != nil && *req.AI.TimeoutSec > 0 {
				c.AI.TimeoutSec = *req.AI.TimeoutSec
			}
		}
	})
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err)
		return
	}
	// API Key 单独加密存储（掩码值不覆盖）
	if req.AI != nil && req.AI.APIKey != nil && *req.AI.APIKey != "" && !strings.HasPrefix(*req.AI.APIKey, "****") && !strings.HasPrefix(*req.AI.APIKey, "••") {
		enc, err := s.db.EncryptSecret(*req.AI.APIKey)
		if err != nil {
			errJSON(w, http.StatusInternalServerError, err)
			return
		}
		_ = s.db.SetSetting("ai_api_key", enc)
	}
	u := userOf(r)
	_ = s.db.Log(u.ID, u.Username, "update_settings", "ai", "AI 诊断配置已更新")
	writeJSON(w, http.StatusOK, map[string]string{"message": "设置已保存（热修改生效，无需重启）"})
}

// handleDetectFrps 一键自动检测：从 frps 进程定位配置，读取全部连接信息并写入配置。
func (s *Server) handleDetectFrps(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	info, err := s.registry.AutoDetectFrpsInfo()
	if err != nil {
		errJSON(w, http.StatusBadGateway, err)
		return
	}
	_ = s.db.Log(u.ID, u.Username, "detect_frps", "frps", "自动检测并写入配置")
	writeJSON(w, http.StatusOK, map[string]any{
		"message":       "已自动读取 frps 配置并写入设置",
		"configPath":    info["configPath"],
		"bindPort":      info["bindPort"],
		"dashboardPort": info["dashboardPort"],
		"dashboardUser": info["dashboardUser"],
		"tokenSet":      info["token"] != "",
		"tokenMask":     maskSecret(info["token"]),
	})
}

func (s *Server) handleTestFrps(w http.ResponseWriter, r *http.Request) {
	client, err := registry.NewFrpsClient(s.cfg)
	if err != nil {
		errJSON(w, http.StatusBadRequest, err)
		return
	}
	info, err := client.Test()
	if err != nil {
		errJSON(w, http.StatusBadGateway, err)
		return
	}
	u := userOf(r)
	_ = s.db.Log(u.ID, u.Username, "test_frps", "frps", "版本 "+info.Version)
	writeJSON(w, http.StatusOK, map[string]any{
		"message":    "连接成功",
		"version":    info.Version,
		"clients":    info.ClientCounts,
		"curConns":   info.CurConns,
		"trafficIn":  info.TotalTrafficIn,
		"trafficOut": info.TotalTrafficOut,
	})
}

func (s *Server) handleVerifyToken(w http.ResponseWriter, r *http.Request) {
	token, err := s.registry.VerifyFrpsTokenBaseline()
	if err != nil {
		errJSON(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message":    "token 校验通过",
		"tokenMask":  maskSecret(token),
		"consistent": true,
	})
}

// ---- 用户管理 ----

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.db.ListUsers()
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

type approveReq struct {
	Approve bool `json:"approve"`
}

func (s *Server) handleApproveUser(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	var req approveReq
	if !readJSON(w, r, &req) {
		return
	}
	u := userOf(r)
	if err := s.auth.ApproveUser(id, req.Approve); err != nil {
		errJSON(w, http.StatusBadRequest, err)
		return
	}
	_ = s.db.Log(u.ID, u.Username, "approve_user", "user#"+strconv.FormatInt(id, 10),
		map[bool]string{true: "通过", false: "拒绝"}[req.Approve])
	writeJSON(w, http.StatusOK, map[string]string{"message": "已处理"})
}

type roleReq struct {
	Role string `json:"role"`
}

func (s *Server) handleSetRole(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	var req roleReq
	if !readJSON(w, r, &req) {
		return
	}
	u := userOf(r)
	if err := s.auth.SetRole(id, req.Role); err != nil {
		errJSON(w, http.StatusBadRequest, err)
		return
	}
	_ = s.db.Log(u.ID, u.Username, "set_role", "user#"+strconv.FormatInt(id, 10), "role="+req.Role)
	writeJSON(w, http.StatusOK, map[string]string{"message": "角色已更新"})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	u := userOf(r)
	if err := s.auth.DeleteUser(id, u.ID, u.Role); err != nil {
		errJSON(w, http.StatusBadRequest, err)
		return
	}
	_ = s.db.Log(u.ID, u.Username, "delete_user", "user#"+strconv.FormatInt(id, 10), "管理员删除")
	writeJSON(w, http.StatusOK, map[string]string{"message": "用户已删除"})
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	logs, err := s.db.ListAudit(limit)
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": logs})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ms, err := s.db.ListMachines()
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err)
		return
	}
	cnt := map[string]int{"pending": 0, "configured": 0, "enabled": 0, "disabled": 0, "total": len(ms)}
	for _, m := range ms {
		cnt[m.Status]++
	}
	info := map[string]any{
		"machines":   cnt,
		"frpsConfig": s.cfg.Get().Frps.DashboardURL != "",
		"tokenSet":   s.cfg.Get().Frps.Token != "",
		"tokenMask":  maskSecret(s.cfg.Get().Frps.Token),
	}
	if client, err := registry.NewFrpsClient(s.cfg); err == nil {
		if si, err := client.Test(); err == nil {
			info["frps"] = map[string]any{
				"version": si.Version, "clients": si.ClientCounts,
				"trafficIn": si.TotalTrafficIn, "trafficOut": si.TotalTrafficOut,
			}
		}
	}
	writeJSON(w, http.StatusOK, info)
}
