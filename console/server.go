package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var webFiles embed.FS

type Config struct {
	CoreURL      *url.URL
	AdminKey     string
	APIKey       string // Only exposed by the authenticated, CSRF-protected access endpoint.
	BridgeKey    string
	PublicOrigin string
	Desktop      bool
}
type adminSession struct {
	csrf, owner string
	expires     time.Time
	ctx         context.Context
	cancel      context.CancelFunc
	oauthGate   chan struct{}
}
type loginLimit struct {
	since time.Time
	count int
}
type sessionContextKey struct{}
type server struct {
	cfg      Config
	mux      *http.ServeMux
	mu       sync.Mutex
	sessions map[string]*adminSession
	limits   map[string]loginLimit
	client   *http.Client
}

func NewServer(cfg Config) (http.Handler, error) {
	if cfg.CoreURL == nil || !validOriginURL(cfg.CoreURL) {
		return nil, errors.New("WB2A_CORE_URL 必须是无凭据、路径、查询和片段的 HTTP(S) 地址")
	}
	if !ValidateAdminOrigin(cfg.PublicOrigin) {
		return nil, errors.New("WB2A_PUBLIC_ORIGIN 必须是有效的 HTTP(S) origin")
	}
	if len(cfg.AdminKey) < 32 || len(cfg.BridgeKey) < 32 || cfg.APIKey == "" || cfg.AdminKey == cfg.BridgeKey || cfg.AdminKey == cfg.APIKey || cfg.BridgeKey == cfg.APIKey {
		return nil, errors.New("管理和桥接密钥须至少 32 字节，三种密钥须非空且互不相同")
	}
	target := *cfg.CoreURL
	target.Path = ""
	cfg.CoreURL = &target
	cfg.PublicOrigin = strings.TrimRight(cfg.PublicOrigin, "/")
	h := &server{cfg: cfg, mux: http.NewServeMux(), sessions: map[string]*adminSession{}, limits: map[string]loginLimit{}, client: &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	assets, err := fs.Sub(webFiles, "web")
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(assets))
	index, err := template.ParseFS(webFiles, "web/index.html")
	if err != nil {
		return nil, err
	}
	h.mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Only the deployment mode reaches the template, never configuration secrets.
		_ = index.Execute(w, struct{ Desktop bool }{cfg.Desktop})
	})
	for _, name := range []string{"app.js", "desktop-key.js", "style.css"} {
		h.mux.Handle("GET /"+name, fileServer)
	}
	h.mux.HandleFunc("GET /livez", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"service": "workbuddy2api-console", "status": "running"})
	})
	public := h.proxy(false)
	h.mux.Handle("/v1/", public)
	h.mux.Handle("GET /status", public)
	h.mux.Handle("GET /healthz", public)
	h.mux.HandleFunc("POST /admin/login", h.adminLogin)
	h.mux.HandleFunc("GET /admin/session", h.withAdmin(func(w http.ResponseWriter, r *http.Request) {
		info, err := h.coreInfo(r.Context())
		if err != nil {
			adminError(w, 503, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"csrf": sessionFrom(r).csrf, "global_enabled": info.GlobalEnabled})
	}))
	h.mux.HandleFunc("POST /admin/logout", h.withAdmin(h.adminLogout))
	h.mux.HandleFunc("POST /admin/access", h.withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if _, err := h.coreInfo(r.Context()); err != nil {
			adminError(w, 503, err.Error())
			return
		}
		writeJSON(w, 200, map[string]string{"api_key": cfg.APIKey})
	}))
	for _, route := range []struct{ pattern, method, path string }{
		{"GET /admin/status", "GET", "/internal/v1/status"},
		{"GET /admin/models", "GET", "/internal/v1/models"},
		{"POST /admin/chat", "POST", "/internal/v1/chat"},
		{"POST /admin/messages", "POST", "/internal/v1/messages"},
		{"POST /admin/oauth", "POST", "/internal/v1/oauth"},
		{"POST /admin/oauth/{id}/poll", "GET", "/internal/v1/oauth/{id}"},
		{"POST /admin/oauth/{id}/region", "POST", "/internal/v1/oauth/{id}/region"},
		{"GET /admin/tasks", "GET", "/internal/v1/tasks"},
		{"POST /admin/tasks/{id}/runs", "POST", "/internal/v1/tasks/{id}/runs"},
		{"GET /admin/task-runs", "GET", "/internal/v1/task-runs"},
		{"GET /admin/task-runs/{id}", "GET", "/internal/v1/task-runs/{id}"},
	} {
		h.mux.HandleFunc(route.pattern, h.withAdmin(h.management(route.method, route.path)))
	}
	return h, nil
}

func validOriginURL(u *url.URL) bool {
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.Hostname() != "" && u.User == nil && u.Opaque == "" && (u.Path == "" || u.Path == "/") && u.RawPath == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.RawFragment == ""
}
func ValidateAdminOrigin(raw string) bool {
	if raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	return err == nil && validOriginURL(u)
}

func (h *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	// Reject ambiguous encodings before ServeMux can clean or redirect them.
	if r.URL.EscapedPath() != r.URL.Path || strings.ContainsAny(r.URL.Path, "%\\") || strings.Contains(r.URL.Path, "//") {
		adminError(w, 400, "请求路径无效")
		return
	}
	for _, part := range strings.Split(r.URL.Path, "/") {
		if part == "." || part == ".." {
			adminError(w, 400, "请求路径无效")
			return
		}
	}
	h.mux.ServeHTTP(w, r)
}
func randomSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("system random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func equalSecret(a, b string) bool {
	x, y := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}
func sessionID(r *http.Request) string {
	c, err := r.Cookie("wb2a_admin")
	if err != nil {
		return ""
	}
	return c.Value
}
func sessionFrom(r *http.Request) adminSession {
	return r.Context().Value(sessionContextKey{}).(adminSession)
}
func writeJSON(w http.ResponseWriter, code int, out any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(out)
}
func adminError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
func decodeAdmin(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		adminError(w, 400, "请求内容无效")
		return false
	}
	return true
}
func (h *server) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	if h.cfg.PublicOrigin != "" {
		return origin == h.cfg.PublicOrigin
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return origin == scheme+"://"+r.Host
}
func (h *server) cleanupLocked() {
	now := time.Now()
	for id, s := range h.sessions {
		if now.After(s.expires) {
			s.cancel()
			delete(h.sessions, id)
		}
	}
	for ip, l := range h.limits {
		if now.Sub(l.since) > time.Minute {
			delete(h.limits, ip)
		}
	}
}
func (h *server) withAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.cleanupLocked()
		s, ok := h.sessions[sessionID(r)]
		var session adminSession
		if ok {
			session = *s
		}
		h.mu.Unlock()
		if !ok {
			adminError(w, 401, "管理会话已过期，请重新登录")
			return
		}
		if r.Method != "GET" && (!h.sameOrigin(r) || !equalSecret(r.Header.Get("X-CSRF-Token"), session.csrf)) {
			adminError(w, 403, "请求来源或验证信息无效")
			return
		}
		if r.URL.Path != "/admin/logout" {
			ctx, cancel := context.WithCancel(r.Context())
			stop := context.AfterFunc(session.ctx, cancel)
			defer stop()
			defer cancel()
			r = r.WithContext(ctx)
		}
		next(w, r.WithContext(context.WithValue(r.Context(), sessionContextKey{}, session)))
	}
}
func (h *server) adminLogin(w http.ResponseWriter, r *http.Request) {
	if !h.sameOrigin(r) {
		adminError(w, 403, "请求来源无效")
		return
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	h.mu.Lock()
	h.cleanupLocked()
	lim := h.limits[ip]
	if lim.count >= 10 || len(h.limits) >= 1024 || len(h.sessions) >= 64 {
		h.mu.Unlock()
		adminError(w, 429, "尝试过于频繁，请一分钟后再试")
		return
	}
	if lim.since.IsZero() {
		lim.since = time.Now()
	}
	lim.count++
	h.limits[ip] = lim
	h.mu.Unlock()
	var body struct {
		Key string `json:"key"`
	}
	if !decodeAdmin(w, r, &body) {
		return
	}
	if !equalSecret(body.Key, h.cfg.AdminKey) {
		adminError(w, 401, "管理密钥不正确")
		return
	}
	id, csrf, owner := randomSecret(), randomSecret(), randomSecret()
	h.mu.Lock()
	if len(h.sessions) >= 64 {
		h.mu.Unlock()
		adminError(w, 429, "管理会话过多，请稍后重试")
		return
	}
	expires := time.Now().Add(8 * time.Hour)
	ctx, cancel := context.WithDeadline(context.Background(), expires)
	h.sessions[id] = &adminSession{csrf: csrf, owner: owner, expires: expires, ctx: ctx, cancel: cancel, oauthGate: make(chan struct{}, 1)}
	h.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "wb2a_admin", Value: id, Path: "/admin", HttpOnly: true, Secure: r.TLS != nil || strings.HasPrefix(h.cfg.PublicOrigin, "https://"), SameSite: http.SameSiteStrictMode, MaxAge: 8 * 3600})
	// Authentication remains available during a core outage; actions still fail closed.
	info, _ := h.coreInfo(r.Context())
	writeJSON(w, 200, map[string]any{"csrf": csrf, "global_enabled": info.GlobalEnabled})
}
func (h *server) adminLogout(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	delete(h.sessions, sessionID(r))
	sessionFrom(r).cancel()
	h.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "wb2a_admin", Value: "", Path: "/admin", HttpOnly: true, Secure: r.TLS != nil || strings.HasPrefix(h.cfg.PublicOrigin, "https://"), SameSite: http.SameSiteStrictMode, MaxAge: -1})
	// Invalidate immediately, then order cleanup after any admitted OAuth control.
	// Cleanup must survive a browser disconnect, but cannot wait indefinitely.
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	gate := sessionFrom(r).oauthGate
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		adminError(w, 503, "授权流程取消超时，管理会话已退出")
		return
	}
	if _, err := h.coreInfo(ctx); err != nil {
		adminError(w, 503, err.Error())
		return
	}
	response, err := h.bridgeRequest(ctx, "DELETE", "/internal/v1/owners/"+sessionFrom(r).owner+"/flows", sessionFrom(r).owner)
	if err != nil {
		adminError(w, 503, "核心服务不可达，管理会话已退出")
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		adminError(w, 503, "核心授权流程取消失败，管理会话已退出")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
