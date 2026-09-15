// Package bridge exposes only the authenticated, versioned core console protocol.
package bridge

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"workbuddy2api/internal/oauth"
	"workbuddy2api/internal/pool"
	"workbuddy2api/internal/scheduler"
	"workbuddy2api/internal/taskrun"
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
	Tasks          *taskrun.Runner
	History        *taskrun.Store
	TaskError      error
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
var taskRequestPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,80}$`)
var taskRunPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

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
	h.mux.HandleFunc("GET /internal/v1/tasks", h.listTasks)
	h.mux.HandleFunc("POST /internal/v1/tasks/{id}/runs", h.startTask)
	h.mux.HandleFunc("GET /internal/v1/task-runs", h.listTaskRuns)
	h.mux.HandleFunc("GET /internal/v1/task-runs/{id}", h.getTaskRun)
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

func knownTask(id string) bool {
	switch id {
	case "checkin", "travel", "activity", "keepalive", "school", "cat":
		return true
	}
	return false
}

func (h *handler) taskUnavailable(w http.ResponseWriter, ready bool) bool {
	if h.cfg.TaskError != nil {
		bridgeError(w, 503, "任务记录暂不可用，请稍后重试")
		return true
	}
	if !ready {
		bridgeError(w, 503, "任务服务尚未就绪，请稍后重试")
		return true
	}
	return false
}

func (h *handler) listTasks(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		bridgeError(w, 400, "查询参数无效")
		return
	}
	if h.taskUnavailable(w, h.cfg.Scheduler != nil && h.cfg.Tasks != nil && h.cfg.History != nil) {
		return
	}
	latest := []taskrun.Run{}
	seen := map[string]bool{}
	before := ""
	for len(seen) < 6 {
		page, next, err := h.cfg.History.Page(before, 100)
		if err != nil {
			bridgeError(w, 503, "任务记录暂不可用，请稍后重试")
			return
		}
		for _, run := range page {
			if !seen[run.TaskID] {
				seen[run.TaskID] = true
				latest = append(latest, run)
			}
		}
		if next == "" {
			break
		}
		before = next
	}
	writeJSON(w, 200, map[string]any{"items": h.cfg.Scheduler.TaskCatalog(time.Now()), "active_run": h.cfg.Tasks.Active(), "latest_runs": latest})
}

func (h *handler) startTask(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		bridgeError(w, 400, "查询参数无效")
		return
	}
	id := r.PathValue("id")
	if !knownTask(id) {
		bridgeError(w, 404, "任务不存在")
		return
	}
	var body struct {
		RequestID string `json:"request_id"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !taskRequestPattern.MatchString(body.RequestID) {
		bridgeError(w, 400, "请求标识无效")
		return
	}
	if h.taskUnavailable(w, h.cfg.Tasks != nil && h.cfg.History != nil) {
		return
	}
	run, err := h.cfg.Tasks.StartManual(id, body.RequestID)
	if err == nil {
		writeJSON(w, 202, run)
		return
	}
	var busy *taskrun.BusyError
	switch {
	case errors.As(err, &busy):
		bridgeErrorRun(w, 409, "已有任务正在运行", busy.RunID)
	case errors.Is(err, taskrun.ErrDisabled):
		bridgeError(w, 409, "任务已禁用")
	case errors.Is(err, taskrun.ErrUnknownTask):
		bridgeError(w, 404, "任务不存在")
	case errors.Is(err, taskrun.ErrRequestConflict):
		bridgeError(w, 409, "请求标识已用于其他任务")
	default:
		bridgeError(w, 503, "任务记录暂不可用，请稍后重试")
	}
}

func taskPage(r *http.Request) (before string, limit int, ok bool) {
	query := r.URL.Query()
	for key, values := range query {
		if (key != "before" && key != "limit") || len(values) != 1 {
			return "", 0, false
		}
	}
	before = query.Get("before")
	if before != "" && !taskRunPattern.MatchString(before) {
		return "", 0, false
	}
	limit = 20
	if raw := query.Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			return "", 0, false
		}
	}
	return before, limit, true
}

func (h *handler) listTaskRuns(w http.ResponseWriter, r *http.Request) {
	before, limit, ok := taskPage(r)
	if !ok {
		bridgeError(w, 400, "分页参数无效")
		return
	}
	if h.taskUnavailable(w, h.cfg.History != nil) {
		return
	}
	items, next, err := h.cfg.History.Page(before, limit)
	if err != nil {
		bridgeError(w, 400, "分页游标无效")
		return
	}
	var nextBefore *string
	if next != "" {
		nextBefore = &next
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_before": nextBefore})
}

func (h *handler) getTaskRun(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		bridgeError(w, 400, "查询参数无效")
		return
	}
	id := r.PathValue("id")
	if !taskRunPattern.MatchString(id) {
		bridgeError(w, 400, "运行记录标识无效")
		return
	}
	if h.taskUnavailable(w, h.cfg.History != nil) {
		return
	}
	run, ok := h.cfg.History.Get(id)
	if !ok {
		bridgeError(w, 404, "运行记录不存在")
		return
	}
	writeJSON(w, 200, run)
}

func writeJSON(w http.ResponseWriter, code int, out any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(out)
}
func bridgeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
func bridgeErrorRun(w http.ResponseWriter, code int, msg, runID string) {
	writeJSON(w, code, map[string]string{"error": msg, "run_id": runID})
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
