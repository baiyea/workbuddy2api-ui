package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"workbuddy2api/internal/scheduler"
	"workbuddy2api/internal/taskrun"
)

func TestBridgeRejectsWrongKey(t *testing.T) {
	h := New(context.Background(), Config{Key: strings.Repeat("b", 32)})
	req := httptest.NewRequest("GET", "/internal/v1/info", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", w.Code)
	}
}

type unreadBody struct{ t *testing.T }

func (b unreadBody) Read([]byte) (int, error) { b.t.Fatal("unauthorized body read"); return 0, nil }
func (b unreadBody) Close() error             { return nil }

func TestBridgeFailsClosedBeforeReadingBody(t *testing.T) {
	for _, key := range []string{"", "short", testKey} {
		h := New(context.Background(), Config{Key: key})
		req := httptest.NewRequest("POST", "/internal/v1/oauth", nil)
		req.Body = unreadBody{t}
		req.Header.Set("Authorization", "Bearer wrong")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatalf("key length %d: status=%d", len(key), w.Code)
		}
	}
	h := New(context.Background(), Config{})
	req := httptest.NewRequest("GET", "/internal/v1/info", nil)
	req.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("empty key accepted: %d", w.Code)
	}
}

func TestBridgeRoutesAndPublicCredentialIsolation(t *testing.T) {
	public := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer core-api" {
			t.Error("core API credential not substituted")
		}
		if r.Header.Get("X-Console-Owner") != "" {
			t.Error("owner forwarded upstream")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(r.URL.Path))
	})
	h := New(context.Background(), Config{Key: testKey, APIKey: "core-api", Public: public, UpstreamCommit: "locked", PatchIdentity: "overlay", GlobalEnabled: true})
	for _, tc := range []struct{ method, path, want string }{
		{"GET", "models", "/v1/models"}, {"POST", "chat", "/v1/chat/completions"}, {"GET", "status", "/status"},
	} {
		r := httptest.NewRequest(tc.method, "/internal/v1/"+tc.path, nil)
		r.Header.Set("Authorization", "Bearer "+testKey)
		r.Header.Set("X-Console-Owner", strings.Repeat("o", 32))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || w.Body.String() != tc.want {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body)
		}
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			t.Fatal("mutated incoming request")
		}
	}
	info := bridgeRequest(h, "GET", "/internal/v1/info", "", "")
	for _, want := range []string{`"protocol":1`, `"upstream_commit":"locked"`, `"patch_identity":"overlay"`, `"global_enabled":true`} {
		if !strings.Contains(info.Body.String(), want) {
			t.Fatalf("info: %s", info.Body)
		}
	}
	for _, path := range []string{"/internal/v1/private", "/internal/v1/info/extra", "/v1/models", "/internal/v2/info"} {
		rr := bridgeRequest(h, "GET", path, "", "")
		if rr.Code != 404 || !strings.Contains(rr.Body.String(), `"error"`) {
			t.Fatalf("%s: %d %s", path, rr.Code, rr.Body)
		}
	}
}

func taskBridge(t *testing.T, sc scheduler.Config, execute func(context.Context, string) (scheduler.TaskResult, error)) (*handler, *taskrun.Store) {
	t.Helper()
	store, err := taskrun.OpenStore(filepath.Join(t.TempDir(), "runs.json"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sch := scheduler.New(sc)
	if execute == nil {
		execute = func(context.Context, string) (scheduler.TaskResult, error) { return scheduler.TaskResult{}, nil }
	}
	runner := taskrun.NewRunner(context.Background(), store, sch.TaskCatalog, execute)
	return newTestBridge(Config{Scheduler: sch, Tasks: runner, History: store}), store
}

func TestTaskBridgeRejectsBeforeExecution(t *testing.T) {
	var executions atomic.Int32
	h, _ := taskBridge(t, scheduler.Config{TravelDisabled: true}, func(context.Context, string) (scheduler.TaskResult, error) {
		executions.Add(1)
		return scheduler.TaskResult{}, nil
	})
	requestID := strings.Repeat("r", 16)
	for _, tc := range []struct {
		name string
		path string
		body string
		code int
	}{
		{"unknown task", "/internal/v1/tasks/not-a-task/runs", `{"request_id":"` + requestID + `"}`, 404},
		{"missing request id", "/internal/v1/tasks/checkin/runs", `{}`, 400},
		{"short request id", "/internal/v1/tasks/checkin/runs", `{"request_id":"short"}`, 400},
		{"non ascii request id", "/internal/v1/tasks/checkin/runs", `{"request_id":"` + strings.Repeat("好", 16) + `"}`, 400},
		{"unknown field", "/internal/v1/tasks/checkin/runs", `{"request_id":"` + requestID + `","script":"evil"}`, 400},
		{"trailing json", "/internal/v1/tasks/checkin/runs", `{"request_id":"` + requestID + `"}{}`, 400},
		{"oversized body", "/internal/v1/tasks/checkin/runs", `{"request_id":"` + requestID + `","padding":"` + strings.Repeat("x", 8192) + `"}`, 400},
		{"query field", "/internal/v1/tasks/checkin/runs?url=https://evil.test", `{"request_id":"` + requestID + `"}`, 400},
		{"disabled task", "/internal/v1/tasks/travel/runs", `{"request_id":"` + requestID + `"}`, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := bridgeRequest(h, "POST", tc.path, tc.body, "")
			if rr.Code != tc.code || !strings.Contains(rr.Body.String(), `"error"`) {
				t.Fatalf("status=%d body=%s", rr.Code, rr.Body)
			}
		})
	}
	if executions.Load() != 0 {
		t.Fatalf("rejected requests executed business %d times", executions.Load())
	}
}

func TestTaskBridgeIdempotencyBusyAndFixedStorageErrors(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var executions atomic.Int32
	h, _ := taskBridge(t, scheduler.Config{}, func(context.Context, string) (scheduler.TaskResult, error) {
		if executions.Add(1) == 1 {
			close(started)
		}
		<-release
		return scheduler.TaskResult{}, nil
	})
	requestID := strings.Repeat("i", 16)
	first := bridgeRequest(h, "POST", "/internal/v1/tasks/checkin/runs", `{"request_id":"`+requestID+`"}`, "")
	if first.Code != 202 {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body)
	}
	var accepted taskrun.Run
	if err := json.Unmarshal(first.Body.Bytes(), &accepted); err != nil || accepted.ID == "" {
		t.Fatalf("invalid accepted run: %v %s", err, first.Body)
	}
	<-started
	retry := bridgeRequest(h, "POST", "/internal/v1/tasks/checkin/runs", `{"request_id":"`+requestID+`"}`, "")
	var duplicate taskrun.Run
	if retry.Code != 202 || json.Unmarshal(retry.Body.Bytes(), &duplicate) != nil || duplicate.ID != accepted.ID {
		t.Fatalf("retry status=%d body=%s", retry.Code, retry.Body)
	}
	busy := bridgeRequest(h, "POST", "/internal/v1/tasks/activity/runs", `{"request_id":"`+strings.Repeat("b", 16)+`"}`, "")
	if busy.Code != 409 || !strings.Contains(busy.Body.String(), `"run_id":"`+accepted.ID+`"`) {
		t.Fatalf("busy status=%d body=%s", busy.Code, busy.Body)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for h.cfg.Tasks.Active() != nil && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if h.cfg.Tasks.Active() != nil {
		t.Fatal("accepted task did not finish")
	}
	if executions.Load() != 1 {
		t.Fatalf("same intent executed %d times", executions.Load())
	}

	brokenPath := filepath.Join(t.TempDir(), "blocked", "runs.json")
	store, err := taskrun.OpenStore(brokenPath, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Dir(brokenPath), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	sch := scheduler.New(scheduler.Config{})
	var brokenExecutions atomic.Int32
	broken := newTestBridge(Config{Scheduler: sch, History: store, Tasks: taskrun.NewRunner(context.Background(), store, sch.TaskCatalog, func(context.Context, string) (scheduler.TaskResult, error) {
		brokenExecutions.Add(1)
		return scheduler.TaskResult{}, nil
	})})
	rr := bridgeRequest(broken, "POST", "/internal/v1/tasks/checkin/runs", `{"request_id":"`+strings.Repeat("s", 16)+`"}`, "")
	if rr.Code != 503 || rr.Body.String() != "{\"error\":\"任务记录暂不可用，请稍后重试\"}\n" || brokenExecutions.Load() != 0 {
		t.Fatalf("storage failure leaked or executed: %d %s count=%d", rr.Code, rr.Body, brokenExecutions.Load())
	}

	startup := newTestBridge(Config{TaskError: errors.New("secret disk path")})
	rr = bridgeRequest(startup, "GET", "/internal/v1/tasks", "", "")
	if rr.Code != 503 || strings.Contains(rr.Body.String(), "secret") {
		t.Fatalf("startup error leaked: %d %s", rr.Code, rr.Body)
	}
}

func TestTaskBridgeCatalogLatestAndHistoryPagination(t *testing.T) {
	h, store := taskBridge(t, scheduler.Config{TravelDisabled: true, SchoolHours: []int{12}}, nil)
	now := time.Now().UTC()
	for i, taskID := range []string{"checkin", "checkin", "activity"} {
		finished := now.Add(time.Duration(i) * time.Second)
		duration := int64(i)
		run := taskrun.Run{ID: "run-" + string(rune('a'+i)), TaskID: taskID, Source: "manual", RequestID: strings.Repeat(string(rune('a'+i)), 16), StartedAt: finished, FinishedAt: &finished, DurationMS: &duration, Status: "skipped", Accounts: []scheduler.AccountResult{}}
		if err := store.Put(run, now); err != nil {
			t.Fatal(err)
		}
	}
	rr := bridgeRequest(h, "GET", "/internal/v1/tasks", "", "")
	if rr.Code != 200 {
		t.Fatalf("tasks status=%d body=%s", rr.Code, rr.Body)
	}
	var catalog struct {
		Items  []scheduler.TaskInfo `json:"items"`
		Active *taskrun.Run         `json:"active_run"`
		Latest []taskrun.Run        `json:"latest_runs"`
	}
	if json.Unmarshal(rr.Body.Bytes(), &catalog) != nil || len(catalog.Items) != 6 || catalog.Active != nil || len(catalog.Latest) != 2 {
		t.Fatalf("catalog response=%s", rr.Body)
	}
	if catalog.Items[1].ID != "travel" || catalog.Items[1].Enabled || catalog.Items[1].NextAt != nil {
		t.Fatalf("disabled task truth lost: %+v", catalog.Items[1])
	}
	if catalog.Latest[0].ID != "run-c" || catalog.Latest[1].ID != "run-b" {
		t.Fatalf("latest per task wrong: %+v", catalog.Latest)
	}

	for _, raw := range []string{"?limit=0", "?limit=101", "?limit=x", "?before=missing", "?script=evil", "?limit=1&limit=2"} {
		rr := bridgeRequest(h, "GET", "/internal/v1/task-runs"+raw, "", "")
		if rr.Code != 400 {
			t.Fatalf("invalid page %q status=%d body=%s", raw, rr.Code, rr.Body)
		}
	}
	page := bridgeRequest(h, "GET", "/internal/v1/task-runs?limit=2", "", "")
	var result struct {
		Items      []taskrun.Run `json:"items"`
		NextBefore *string       `json:"next_before"`
	}
	if page.Code != 200 || json.Unmarshal(page.Body.Bytes(), &result) != nil || len(result.Items) != 2 || result.NextBefore == nil || *result.NextBefore != "run-b" {
		t.Fatalf("first page=%d %s", page.Code, page.Body)
	}
	last := bridgeRequest(h, "GET", "/internal/v1/task-runs?limit=2&before="+*result.NextBefore, "", "")
	if last.Code != 200 || !strings.Contains(last.Body.String(), `"next_before":null`) || !strings.Contains(last.Body.String(), `"id":"run-a"`) {
		t.Fatalf("last page=%d %s", last.Code, last.Body)
	}
	for _, id := range []string{"", "bad!id", "missing"} {
		path := "/internal/v1/task-runs/" + id
		rr := bridgeRequest(h, "GET", path, "", "")
		want := 404
		if id == "bad!id" {
			want = 400
		}
		if rr.Code != want {
			t.Fatalf("detail %q status=%d body=%s", id, rr.Code, rr.Body)
		}
	}
	detail := bridgeRequest(h, "GET", "/internal/v1/task-runs/run-c", "", "")
	if detail.Code != 200 || !strings.Contains(detail.Body.String(), `"task_id":"activity"`) {
		t.Fatalf("detail=%d %s", detail.Code, detail.Body)
	}
}
