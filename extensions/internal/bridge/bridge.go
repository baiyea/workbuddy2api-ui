// Package bridge exposes only the authenticated, versioned core console protocol.
package bridge

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"sync"

	"workbuddy2api/internal/oauth"
	"workbuddy2api/internal/pool"
	"workbuddy2api/internal/scheduler"
	"workbuddy2api/internal/upstream"
)

type Config struct {
	Key            string
	APIKey         string
	AuthDir        string
	UpstreamCommit string
	PatchIdentity  string
	GlobalEnabled  bool
	Pool           *pool.Pool
	Upstream       *upstream.Client
	Scheduler      *scheduler.Scheduler
	Public         http.Handler
}

type handler struct {
	ctx      context.Context
	cfg      Config
	mux      *http.ServeMux
	mu       sync.Mutex
	flows    map[string]*loginFlow
	newOAuth func(string) (*oauth.Client, error)
}

var ownerPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{32,128}$`)

func New(ctx context.Context, cfg Config) http.Handler {
	h := &handler{ctx: ctx, cfg: cfg, mux: http.NewServeMux(), flows: make(map[string]*loginFlow), newOAuth: oauth.New}
	h.mux.HandleFunc("GET /internal/v1/info", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"protocol": 1, "upstream_commit": cfg.UpstreamCommit, "patch_identity": cfg.PatchIdentity, "global_enabled": cfg.GlobalEnabled})
	})
	h.mux.HandleFunc("GET /internal/v1/status", h.forward("/status"))
	h.mux.HandleFunc("GET /internal/v1/models", h.forward("/v1/models"))
	h.mux.HandleFunc("POST /internal/v1/chat", h.forward("/v1/chat/completions"))
	h.mux.HandleFunc("POST /internal/v1/oauth", h.withOwner(h.startLogin))
	h.mux.HandleFunc("GET /internal/v1/oauth/{id}", h.withOwner(h.pollLogin))
	h.mux.HandleFunc("POST /internal/v1/oauth/{id}/region", h.withOwner(h.completeRegion))
	h.mux.HandleFunc("DELETE /internal/v1/owners/{owner}/flows", h.withOwner(h.cancelOwner))
	h.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { bridgeError(w, 404, "接口不存在") })
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	expected, supplied := sha256.Sum256([]byte("Bearer "+h.cfg.Key)), sha256.Sum256([]byte(r.Header.Get("Authorization")))
	if len(h.cfg.Key) < 32 || h.cfg.Key == h.cfg.APIKey || subtle.ConstantTimeCompare(expected[:], supplied[:]) != 1 {
		bridgeError(w, 401, "桥接鉴权失败")
		return
	}
	h.mux.ServeHTTP(w, r)
}

func (h *handler) withOwner(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ownerPattern.MatchString(r.Header.Get("X-Console-Owner")) {
			bridgeError(w, 400, "缺少有效的管理会话身份")
			return
		}
		next(w, r)
	}
}

func (h *handler) forward(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.cfg.Public == nil {
			bridgeError(w, 503, "核心服务尚未就绪")
			return
		}
		req := r.Clone(r.Context())
		req.URL.Path, req.URL.RawPath, req.RequestURI = path, "", path
		req.Header.Set("Authorization", "Bearer "+h.cfg.APIKey)
		req.Header.Del("X-Console-Owner")
		h.cfg.Public.ServeHTTP(w, req)
	}
}

func writeJSON(w http.ResponseWriter, code int, out any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(out)
}
func bridgeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		bridgeError(w, 400, "请求内容无效")
		return false
	}
	return true
}
