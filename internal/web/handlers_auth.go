package web

import (
	"net/http"

	"frpmon/internal/auth"
)

type registerReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if !readJSON(w, r, &req) {
		return
	}
	mode := s.cfg.Get().Registration
	u, err := s.auth.Register(req.Username, req.Password, mode)
	if err != nil {
		errJSON(w, http.StatusBadRequest, err)
		return
	}
	_ = s.db.Log(0, req.Username, "register", req.Username, "mode="+mode)
	writeJSON(w, http.StatusOK, map[string]any{
		"username": u.Username,
		"role":     u.Role,
		"status":   u.Status,
		"message":  "注册成功" + map[bool]string{true: "，你已成为管理员", false: ""}[u.Role == auth.RoleAdmin],
	})
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if !readJSON(w, r, &req) {
		return
	}
	c := s.cfg.Get()
	u, token, err := s.auth.Login(req.Username, req.Password, c.SessionTTLDays, c.LoginMaxFails, c.LoginLockMinute)
	if err != nil {
		_ = s.db.Log(0, req.Username, "login_fail", req.Username, err.Error())
		errJSON(w, http.StatusUnauthorized, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "frpmon_token",
		Value:    token,
		Path:     "/",
		MaxAge:   c.SessionTTLDays * 86400,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	_ = s.db.Log(u.ID, u.Username, "login", u.Username, "ok")
	// 同时返回 token（前端存 localStorage 用 Bearer 认证，
	// 兼容新版浏览器对 HTTP 站点 Cookie 的限制）
	writeJSON(w, http.StatusOK, map[string]any{
		"username": u.Username,
		"role":     u.Role,
		"status":   u.Status,
		"token":    token,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	if c, err := r.Cookie("frpmon_token"); err == nil {
		_ = s.auth.Logout(c.Value)
	}
	_ = s.db.Log(u.ID, u.Username, "logout", u.Username, "ok")
	http.SetCookie(w, &http.Cookie{Name: "frpmon_token", Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]string{"message": "已退出登录"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       u.ID,
		"username": u.Username,
		"role":     u.Role,
		"createdAt": u.CreatedAt,
		"lastLogin": u.LastLogin,
	})
}

func (s *Server) handleDeleteSelf(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	if err := s.auth.DeleteUser(u.ID, u.ID, u.Role); err != nil {
		errJSON(w, http.StatusBadRequest, err)
		return
	}
	_ = s.db.Log(u.ID, u.Username, "delete_self", u.Username, "销户")
	if c, err := r.Cookie("frpmon_token"); err == nil {
		_ = s.auth.Logout(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "frpmon_token", Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]string{"message": "账户已注销"})
}

type changePwdReq struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req changePwdReq
	if !readJSON(w, r, &req) {
		return
	}
	u := userOf(r)
	if !s.auth.VerifyPassword(u, req.OldPassword) {
		errJSON(w, http.StatusBadRequest, errPasswordWrong)
		return
	}
	if err := s.auth.SetPassword(u.ID, req.NewPassword); err != nil {
		errJSON(w, http.StatusBadRequest, err)
		return
	}
	_ = s.db.Log(u.ID, u.Username, "change_password", u.Username, "ok")
	writeJSON(w, http.StatusOK, map[string]string{"message": "密码已修改"})
}
