package web

import (
	"net"
	"net/http"
	"strings"

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
	u, token, err := s.auth.LoginWithClient(
		req.Username, req.Password, c.SessionTTLDays, c.LoginMaxFails,
		c.LoginIPMaxFails, c.LoginLockMinute, c.LoginWindowMin, clientAddress(r),
	)
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
		Secure:   c.TLS.Enabled || r.TLS != nil,
	})
	_ = s.db.Log(u.ID, u.Username, "login", u.Username, "ok")
	response := map[string]any{
		"username": u.Username,
		"role":     u.Role,
		"status":   u.Status,
	}
	// 浏览器只使用 HttpOnly Cookie，避免把会话令牌暴露给 localStorage/XSS。
	// CLI 明确声明客户端类型后仍可获得 Bearer 令牌，保持无状态命令调用兼容。
	if strings.EqualFold(r.Header.Get("X-FRPilot-Client"), "cli") {
		response["token"] = token
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	_ = s.auth.Logout(sessionToken(r))
	_ = s.db.Log(u.ID, u.Username, "logout", u.Username, "ok")
	s.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]string{"message": "已退出登录"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":        u.ID,
		"username":  u.Username,
		"role":      u.Role,
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
	_ = s.auth.Logout(sessionToken(r))
	s.clearSessionCookie(w, r)
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
	// 修改密码后吊销该用户的所有旧会话，避免已泄露的旧 token 继续可用。
	_ = s.db.DeleteUserSessions(u.ID)
	_ = s.db.Log(u.ID, u.Username, "change_password", u.Username, "ok")
	s.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]string{"message": "密码已修改"})
}

func clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	c := s.cfg.Get()
	http.SetCookie(w, &http.Cookie{
		Name:     "frpmon_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   c.TLS.Enabled || r.TLS != nil,
	})
}
