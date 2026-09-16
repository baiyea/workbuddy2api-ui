package main

// Optional browser fixture. It never contacts accounts, containers, or upstream services.
// WB2A_BROWSER_PREVIEW=1 go -C console test -run TestAdminBrowserPreview -v -timeout 20m
import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type previewTask struct {
	ID       string     `json:"id"`
	Enabled  bool       `json:"enabled"`
	Hours    []int      `json:"hours"`
	Timezone string     `json:"timezone"`
	NextAt   *time.Time `json:"next_at"`
}
type previewRun struct {
	ID           string           `json:"id"`
	TaskID       string           `json:"task_id"`
	Source       string           `json:"source"`
	RequestID    string           `json:"request_id"`
	StartedAt    time.Time        `json:"started_at"`
	FinishedAt   *time.Time       `json:"finished_at"`
	DurationMS   *int64           `json:"duration_ms"`
	Status       string           `json:"status"`
	Accounts     []map[string]any `json:"accounts"`
	Log          string           `json:"log"`
	LogTruncated bool             `json:"log_truncated"`
}

func TestAdminBrowserPreview(t *testing.T) {
	if os.Getenv("WB2A_BROWSER_PREVIEW") != "1" {
		t.Skip("opt-in browser fixture")
	}
	now := time.Now().UTC()
	next := now.Add(time.Hour)
	tasks := []previewTask{
		{"checkin", true, []int{9, 21}, "Asia/Shanghai", &next},
		{"travel", true, []int{9, 21}, "Asia/Shanghai", &next},
		{"activity", true, []int{10}, "Asia/Shanghai", &next},
		{"keepalive", false, []int{22}, "Asia/Shanghai", nil},
		{"school", true, []int{12}, "Asia/Shanghai", nil},
		{"cat", true, []int{1}, "Asia/Shanghai", &next},
	}
	taskIDs := []string{"cat", "school", "travel", "activity", "checkin", "keepalive"}
	statuses := []string{"success", "unknown", "partial_failure", "failed", "skipped", "interrupted", "running"}
	runs := make([]previewRun, 0, 28)
	for i := 0; i < 24; i++ {
		started := now.Add(-time.Duration(i+1) * time.Hour)
		finished, duration := started.Add(2*time.Minute), int64(120000)
		status := statuses[i%len(statuses)]
		if status == "running" {
			status = "success"
		}
		run := previewRun{ID: fmt.Sprintf("preview-history-%02d", i), TaskID: taskIDs[i%len(taskIDs)], Source: "scheduled", StartedAt: started, FinishedAt: &finished, DurationMS: &duration, Status: status, Accounts: []map[string]any{}, Log: "mock_history_event"}
		switch i {
		case 0:
			run.Log, run.LogTruncated = strings.Repeat("mock-safe-log ", 400), true
		case 1:
			run.Status, run.DurationMS = "unknown", nil
			run.Accounts = []map[string]any{{"uid": "mock-school", "status": "unknown", "detail": "result_unconfirmed", "before": nil, "after": nil, "reward": nil}}
		case 2:
			run.Status = "partial_failure"
			run.Accounts = []map[string]any{
				{"uid": "mock-ok", "status": "success", "detail": "mock_confirmed", "before": map[string]any{"value": 100, "observed_at": started}, "after": map[string]any{"value": 100, "observed_at": finished}, "reward": 0},
				{"uid": "mock-failed", "status": "failed", "detail": "mock_network_failure", "before": nil, "after": nil, "reward": nil},
			}
		}
		runs = append(runs, run)
	}
	var mu sync.Mutex
	requests := map[string]string{}
	var serial atomic.Int32
	var lostOnce atomic.Bool
	var inFlight atomic.Int32

	findRun := func(id string) (previewRun, bool) {
		for _, run := range runs {
			if run.ID == id {
				return run, true
			}
		}
		return previewRun{}, false
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/internal/") && r.Header.Get("Authorization") != "Bearer "+strings.Repeat("b", 32) {
			w.WriteHeader(401)
			return
		}
		if mockInfo(w, r) {
			return
		}
		switch {
		case r.URL.Path == "/internal/v1/status":
			fmt.Fprintf(w, `{"accounts":[{"uid":"demo-account","nickname":"演示账号（模拟）","realm":"cn","credits":1200,"credits_known":true,"in_flight":%d,"success_count":2,"err_total":0,"disabled":false,"cooling":false}],"total":1,"healthy":1,"cooling":0,"disabled":0,"in_flight_full":0,"realm_totals":{"cn":{"total":1,"healthy":1,"cooling":0,"disabled":0,"in_flight_full":0},"global":{"total":0,"healthy":0,"cooling":0,"disabled":0,"in_flight_full":0}},"sticky_sessions":0,"redis_mode":"noop"}`, inFlight.Load())
		case r.URL.Path == "/internal/v1/models" || r.URL.Path == "/v1/models":
			fmt.Fprint(w, `{"object":"list","data":[{"id":"cn:glm-5.2","object":"model","reasoning_supported_efforts":["low","high"]},{"id":"global:claude-sonnet-4.6","object":"model"}]}`)
		case r.URL.Path == "/internal/v1/chat":
			inFlight.Add(1)
			defer inFlight.Add(-1)
			w.Header().Set("Content-Type", "text/event-stream")
			for _, part := range []string{"你好！", "这是控制台的模拟回答。", "流式显示和页面交互已连通。"} {
				fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q}}]}\n\n", part)
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
					t.Log("mock chat canceled: browser disconnect propagated to core")
					return
				case <-time.After(3 * time.Second):
				}
			}
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":20,\"total_tokens\":25}}\n\ndata: [DONE]\n\n")
		case r.URL.Path == "/internal/v1/messages":
			inFlight.Add(1)
			defer inFlight.Add(-1)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_mock\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"mock-model\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":null,\"output_tokens\":null}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
			for _, part := range []string{"你好！", "这是 Anthropic 协议的模拟回答。", "文本与流式交互已连通。"} {
				fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\n", part)
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
					t.Log("mock messages canceled: browser disconnect propagated to core")
					return
				case <-time.After(3 * time.Second):
				}
			}
			fmt.Fprint(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":null,\"output_tokens\":null}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		case r.Method == "GET" && r.URL.Path == "/internal/v1/tasks":
			mu.Lock()
			defer mu.Unlock()
			var active *previewRun
			latest, seen := []previewRun{}, map[string]bool{}
			for i := range runs {
				if runs[i].Status == "running" && active == nil {
					copy := runs[i]
					active = &copy
				}
				if !seen[runs[i].TaskID] {
					seen[runs[i].TaskID] = true
					latest = append(latest, runs[i])
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"items": tasks, "active_run": active, "latest_runs": latest})
		case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/internal/v1/tasks/") && strings.HasSuffix(r.URL.Path, "/runs"):
			taskID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/internal/v1/tasks/"), "/runs")
			var body struct {
				RequestID string `json:"request_id"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"error":"bad mock body"}`)
				return
			}
			mu.Lock()
			if oldID := requests[body.RequestID]; oldID != "" {
				run, _ := findRun(oldID)
				mu.Unlock()
				w.WriteHeader(202)
				json.NewEncoder(w).Encode(run)
				return
			}
			for _, run := range runs {
				if run.Status == "running" {
					mu.Unlock()
					w.WriteHeader(409)
					json.NewEncoder(w).Encode(map[string]any{"error": "已有 mock 任务正在运行", "run_id": run.ID})
					return
				}
			}
			id := fmt.Sprintf("preview-manual-%02d", serial.Add(1))
			started := time.Now().UTC()
			run := previewRun{ID: id, TaskID: taskID, Source: "manual", RequestID: body.RequestID, StartedAt: started, Status: "running", Accounts: []map[string]any{}, Log: "mock_execution_count_1"}
			requests[body.RequestID] = id
			runs = append([]previewRun{run}, runs...)
			mu.Unlock()
			if taskID == "checkin" && !lostOnce.Swap(true) {
				mu.Lock()
				finished, duration := started.Add(time.Second), int64(1000)
				runs[0].FinishedAt, runs[0].DurationMS, runs[0].Status = &finished, &duration, "success"
				runs[0].Accounts = []map[string]any{{"uid": "mock-checkin", "status": "success", "detail": "accepted_before_connection_loss", "before": nil, "after": map[string]any{"value": 0, "observed_at": finished}, "reward": 0}}
				mu.Unlock()
				panic(http.ErrAbortHandler)
			}
			if taskID == "activity" {
				go func(id string) {
					time.Sleep(3 * time.Second)
					mu.Lock()
					defer mu.Unlock()
					for i := range runs {
						if runs[i].ID == id {
							finished, duration := time.Now().UTC(), int64(3000)
							runs[i].FinishedAt, runs[i].DurationMS, runs[i].Status = &finished, &duration, "partial_failure"
							runs[i].Accounts = []map[string]any{{"uid": "mock-ok", "status": "success", "detail": "one_click_only", "before": nil, "after": nil, "reward": nil}, {"uid": "mock-failed", "status": "failed", "detail": "mock_partial_failure", "before": nil, "after": nil, "reward": nil}}
						}
					}
				}(id)
			}
			w.WriteHeader(202)
			json.NewEncoder(w).Encode(run)
		case r.Method == "GET" && r.URL.Path == "/internal/v1/task-runs":
			mu.Lock()
			defer mu.Unlock()
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if limit == 0 {
				limit = 20
			}
			start := 0
			if before := r.URL.Query().Get("before"); before != "" {
				start = len(runs)
				for i := range runs {
					if runs[i].ID == before {
						start = i + 1
						break
					}
				}
			}
			end := start + limit
			if end > len(runs) {
				end = len(runs)
			}
			var nextBefore any
			if end < len(runs) && end > start {
				nextBefore = runs[end-1].ID
			}
			json.NewEncoder(w).Encode(map[string]any{"items": runs[start:end], "next_before": nextBefore})
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/internal/v1/task-runs/"):
			mu.Lock()
			defer mu.Unlock()
			run, ok := findRun(strings.TrimPrefix(r.URL.Path, "/internal/v1/task-runs/"))
			if !ok {
				w.WriteHeader(404)
				fmt.Fprint(w, `{"error":"mock run not found"}`)
				return
			}
			json.NewEncoder(w).Encode(run)
		default:
			if strings.HasPrefix(r.URL.Path, "/internal/v1/owners/") {
				fmt.Fprint(w, `{"ok":true}`)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/internal/v1/oauth") {
				w.WriteHeader(503)
				fmt.Fprint(w, `{"error":"浏览器夹具不发起真实授权；OAuth 由自动测试验证"}`)
				return
			}
			w.WriteHeader(404)
			fmt.Fprint(w, `{"error":"mock route not found"}`)
		}
	}))
	defer ts.Close()
	cfg := testConfig(ts.URL)
	cfg.AdminKey = "browser-preview-key-mock-only-12345"
	cfg.APIKey = "preview-api-key-mock-only"
	h, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:17864")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())
	t.Log("Browser fixture: http://127.0.0.1:17864 · admin key: browser-preview-key-mock-only-12345 (mock only)")
	t.Log("Tasks: checkin loses its first accepted response then retries same ID; activity stays running 3s for rapid repeat; seeded partial/unknown/truncated records and 24-item pagination survive reloads.")
	<-time.After(15 * time.Minute)
}
