package web

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"frpmon/internal/actions"
	"frpmon/internal/ai"
	"frpmon/internal/auth"
	"frpmon/internal/collector"
	"frpmon/internal/config"
	"frpmon/internal/qos"
	"frpmon/internal/registry"
	"frpmon/internal/store"
	"frpmon/internal/traffic"
)

//go:embed all:dist
var distFS embed.FS

type Server struct {
	db        *store.DB
	auth      *auth.Service
	cfg       *config.Manager
	registry  *registry.Service
	collector *collector.Collector
	traffic   *traffic.Service
	tasks     *actions.TaskManager
	ai        *ai.Service
	qos       *qos.Service
	mux       *http.ServeMux
	static    http.Handler
}

func NewServer(db *store.DB, authSvc *auth.Service, cfg *config.Manager, reg *registry.Service, col *collector.Collector, traff *traffic.Service, tasks *actions.TaskManager, aiSvc *ai.Service, qosSvc *qos.Service) *Server {
	s := &Server{db: db, auth: authSvc, cfg: cfg, registry: reg, collector: col, traffic: traff, tasks: tasks, ai: aiSvc, qos: qosSvc}
	s.routes()
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	s.static = http.FileServer(http.FS(sub))
	return s
}

func (s *Server) routes() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/healthz", s.handleHealthz)

	mux.HandleFunc("POST /api/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.requireAuth(s.handleLogout))
	mux.HandleFunc("GET /api/auth/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("DELETE /api/auth/me", s.requireAuth(s.handleDeleteSelf))
	mux.HandleFunc("PUT /api/auth/password", s.requireAuth(s.handleChangePassword))

	mux.HandleFunc("GET /api/machines", s.requireAuth(s.handleMachines))
	mux.HandleFunc("POST /api/machines/discover", s.requireAdmin(s.handleDiscover))
	mux.HandleFunc("POST /api/machines/{id}/credentials", s.requireAdmin(s.handleMachineCredentials))
	mux.HandleFunc("POST /api/machines/{id}/enable", s.requireAdmin(s.handleMachineEnable))
	mux.HandleFunc("GET /api/machines/{id}/snapshot", s.requireAuth(s.handleMachineSnapshot))
	mux.HandleFunc("POST /api/machines/{id}/collect-now", s.requireAdmin(s.handleCollectNow))
	mux.HandleFunc("GET /api/machines/{id}/metrics", s.requireAuth(s.handleMachineMetrics))

	mux.HandleFunc("GET /api/settings", s.requireAdmin(s.handleGetSettings))
	mux.HandleFunc("POST /api/settings", s.requireAdmin(s.handleSaveSettings))
	mux.HandleFunc("POST /api/settings/test-frps", s.requireAdmin(s.handleTestFrps))
	mux.HandleFunc("POST /api/settings/detect-frps", s.requireAdmin(s.handleDetectFrps))
	mux.HandleFunc("POST /api/settings/verify-token", s.requireAdmin(s.handleVerifyToken))

	mux.HandleFunc("GET /api/users", s.requireAdmin(s.handleListUsers))
	mux.HandleFunc("POST /api/users/{id}/approve", s.requireAdmin(s.handleApproveUser))
	mux.HandleFunc("POST /api/users/{id}/role", s.requireAdmin(s.handleSetRole))
	mux.HandleFunc("DELETE /api/users/{id}", s.requireAdmin(s.handleDeleteUser))

	mux.HandleFunc("GET /api/audit", s.requireAdmin(s.handleAudit))
	mux.HandleFunc("GET /api/status", s.requireAuth(s.handleStatus))

	mux.HandleFunc("GET /api/traffic", s.requireAuth(s.handleTraffic))
	mux.HandleFunc("GET /api/traffic/history", s.requireAuth(s.handleTrafficHistory))
	mux.HandleFunc("GET /api/qos/status", s.requireAuth(s.handleQosStatus))

	mux.HandleFunc("GET /api/actions/cleanup-items", s.requireAuth(s.handleCleanupItems))
	mux.HandleFunc("POST /api/actions/cleanup/preview", s.requireAdmin(s.handleCleanupPreview))
	mux.HandleFunc("POST /api/actions/cleanup", s.requireAdmin(s.handleCleanupStart))
	mux.HandleFunc("POST /api/actions/scan", s.requireAdmin(s.handleScanStart))
	mux.HandleFunc("GET /api/actions/tasks", s.requireAuth(s.handleTasks))
	mux.HandleFunc("POST /api/actions/health/{id}", s.requireAuth(s.handleHealthCheck))
	mux.HandleFunc("GET /api/actions/health/reports", s.requireAuth(s.handleHealthReports))

	mux.HandleFunc("POST /api/ai/diagnose/{id}", s.requireAuth(s.handleDiagnose))
	mux.HandleFunc("GET /api/ai/diagnostics", s.requireAuth(s.handleDiagnostics))

	mux.HandleFunc("GET /", s.handleStatic)
	s.mux = mux
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w, r, s.cfg)
	s.mux.ServeHTTP(w, r)
}

// ---- 中间件 ----

func (s *Server) sessionUser(r *http.Request) *store.User {
	return s.auth.Auth(sessionToken(r))
}

func sessionToken(r *http.Request) string {
	if c, err := r.Cookie("frpmon_token"); err == nil && c.Value != "" {
		return c.Value
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return ""
}

func setSecurityHeaders(w http.ResponseWriter, r *http.Request, cfg *config.Manager) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
	}
	if cfg.Get().TLS.Enabled {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := s.sessionUser(r)
		if u == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录"})
			return
		}
		s.extendSessionCookie(w, r)
		next(w, r.WithContext(withUser(r, u)))
	}
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := s.sessionUser(r)
		if u == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录"})
			return
		}
		if u.Role != auth.RoleAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "需要管理员权限"})
			return
		}
		s.extendSessionCookie(w, r)
		next(w, r.WithContext(withUser(r, u)))
	}
}

// extendSessionCookie 在每次 cookie 鉴权成功后把登录 cookie 的滑动窗口
// 重新刷满（默认 5 分钟）。Bearer token（CLI）不携带 cookie，不受影响；
// 纯 HTTP 头操作、无数据库写入，开销可忽略。
func (s *Server) extendSessionCookie(w http.ResponseWriter, r *http.Request) {
	if _, err := r.Cookie("frpmon_token"); err != nil {
		return
	}
	c := s.cfg.Get()
	http.SetCookie(w, sessionCookie(sessionToken(r), c.SessionGraceMinutes*60, c.TLS.Enabled || r.TLS != nil))
}

// ---- 工具 ----

type ctxKey int

const userKey ctxKey = 1

func withUser(r *http.Request, u *store.User) context.Context {
	return context.WithValue(r.Context(), userKey, u)
}

func userOf(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey).(*store.User)
	return u
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	p := r.URL.Path
	if strings.Contains(p, "..") {
		http.NotFound(w, r)
		return
	}
	// hash 命名的构建资源：内容不变可永久缓存（文件名含内容 hash）
	if strings.HasPrefix(p, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		s.static.ServeHTTP(w, r)
		return
	}
	// index.html（含 history 路由回退）：不缓存，保证永远拿到最新页面
	if p == "/" || !distExists(p) {
		b, err := distFS.ReadFile("dist/index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
		return
	}
	s.static.ServeHTTP(w, r)
}

func distExists(p string) bool {
	_, err := distFS.Open("dist" + p)
	return err == nil
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Body == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体为空"})
		return false
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON 解析失败: " + err.Error()})
		return false
	}
	return true
}

func errJSON(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
