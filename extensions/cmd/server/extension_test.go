package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"workbuddy2api/internal/pool"
	"workbuddy2api/internal/scheduler"
	"workbuddy2api/internal/server"
	"workbuddy2api/internal/taskrun"
	"workbuddy2api/internal/upstream"
)

func TestInitializeKeysMigratesLegacyWithoutRotation(t *testing.T) {
	dataDir, keyDir := t.TempDir(), filepath.Join(t.TempDir(), "keys")
	old := []byte(`{"admin_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","api_key":"legacy-api"}`)
	legacy := filepath.Join(dataDir, "console-keys.json")
	if err := os.WriteFile(legacy, old, 0600); err != nil {
		t.Fatal(err)
	}
	first, err := initializeKeys(dataDir, keyDir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := initializeKeys(dataDir, keyDir, "", "")
	if err != nil || first != second {
		t.Fatalf("non-idempotent initialization: %v", err)
	}
	if first.AdminKey != strings.Repeat("a", 32) || first.APIKey != "legacy-api" || len(first.BridgeKey) < 32 || first.BridgeKey == first.AdminKey || first.BridgeKey == first.APIKey {
		t.Fatalf("invalid migrated keys: admin=%t api=%t bridge_length=%d", first.AdminKey == strings.Repeat("a", 32), first.APIKey == "legacy-api", len(first.BridgeKey))
	}
	if got, err := os.ReadFile(legacy); err != nil || string(got) != string(old) {
		t.Fatalf("legacy key file changed: %v", err)
	}
	info, err := os.Stat(filepath.Join(keyDir, "keys.json"))
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatalf("key file mode: %v %v", info, err)
	}
	if info, err = os.Stat(keyDir); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0700) {
		t.Fatalf("key directory mode: %v %v", info, err)
	}
}

func TestInitializeKeysRejectsCorruptionAndConflicts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		legacy     []byte
		target     []byte
		admin, api string
	}{
		{"corrupt legacy", []byte(`{broken`), nil, "", ""},
		{"corrupt target", nil, []byte(`{broken`), "", ""},
		{"oversized target", nil, append([]byte(`{"admin_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","api_key":"api","bridge_key":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`), bytes.Repeat([]byte(" "), 5000)...), "", ""},
		{"legacy target mismatch", []byte(`{"admin_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","api_key":"legacy-api"}`), []byte(`{"admin_key":"cccccccccccccccccccccccccccccccc","api_key":"legacy-api","bridge_key":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`), "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataDir, keyDir := t.TempDir(), filepath.Join(t.TempDir(), "keys")
			if tc.legacy != nil {
				if err := os.WriteFile(filepath.Join(dataDir, "console-keys.json"), tc.legacy, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.target != nil {
				if err := os.MkdirAll(keyDir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(keyDir, "keys.json"), tc.target, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := initializeKeys(dataDir, keyDir, tc.admin, tc.api); err == nil {
				t.Fatal("unsafe keys accepted")
			}
		})
	}
}

func TestInitializeKeysValidatesNewOverrides(t *testing.T) {
	for _, tc := range []struct{ name, admin, api string }{
		{"short admin", "short", "api"},
		{"empty api override generates", strings.Repeat("a", 32), ""},
		{"same admin api", strings.Repeat("a", 32), strings.Repeat("a", 32)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataDir, keyDir := t.TempDir(), filepath.Join(t.TempDir(), "keys")
			keys, err := initializeKeys(dataDir, keyDir, tc.admin, tc.api)
			if tc.name == "empty api override generates" {
				if err != nil || keys.AdminKey != tc.admin || keys.APIKey == "" {
					t.Fatalf("valid overrides rejected: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid overrides accepted")
			}
		})
	}
}

func TestInitializeKeysAppliesOverridesWithoutChangingPersistedFiles(t *testing.T) {
	dataDir, keyDir := t.TempDir(), filepath.Join(t.TempDir(), "keys")
	legacy := []byte(`{"admin_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","api_key":"legacy-api"}`)
	if err := os.WriteFile(filepath.Join(dataDir, "console-keys.json"), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	base, err := initializeKeys(dataDir, keyDir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(keyDir, "keys.json")
	persisted, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	overrideAdmin, overrideAPI := strings.Repeat("c", 32), "short-api"
	effective, err := initializeKeys(dataDir, keyDir, overrideAdmin, overrideAPI)
	if err != nil {
		t.Fatal(err)
	}
	if effective.AdminKey != overrideAdmin || effective.APIKey != overrideAPI || effective.BridgeKey != base.BridgeKey {
		t.Fatal("runtime overrides were not applied consistently")
	}
	if after, err := os.ReadFile(keyPath); err != nil || string(after) != string(persisted) {
		t.Fatalf("runtime override changed persisted keys: %v", err)
	}
	if after, err := os.ReadFile(filepath.Join(dataDir, "console-keys.json")); err != nil || string(after) != string(legacy) {
		t.Fatalf("runtime override changed legacy keys: %v", err)
	}
}

func TestCoreDockerModeRejectsCoreOnlyConfigAPIKeyWithoutWritingKeys(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WB2A_CORE", "true")
	t.Setenv("WB2A_KEY_DIR", filepath.Join(root, "keys"))
	t.Setenv("WB2A_ADMIN_KEY", "")
	t.Setenv("WB2A_API_KEY", "")
	cfg := &Config{APIKey: "core-only-config-api", AuthDir: filepath.Join(root, "auths"), StateFile: filepath.Join(root, "data", "state.json")}
	if err := initializeCore(cfg); err == nil || !strings.Contains(err.Error(), "WB2A_API_KEY") {
		t.Fatalf("core-only API key was not rejected with guidance: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "keys", "keys.json")); !os.IsNotExist(err) {
		t.Fatalf("rejected configuration wrote keys: %v", err)
	}
}

func TestCoreDockerModeRejectsConfigAPIKeyConflictWithoutChangingPersistedKeys(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WB2A_CORE", "true")
	t.Setenv("WB2A_KEY_DIR", filepath.Join(root, "keys"))
	t.Setenv("WB2A_ADMIN_KEY", "")
	t.Setenv("WB2A_API_KEY", "")
	base, err := initializeKeys(filepath.Join(root, "data"), filepath.Join(root, "keys"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "keys", "keys.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &Config{APIKey: base.APIKey + "-different", AuthDir: filepath.Join(root, "auths"), StateFile: filepath.Join(root, "data", "state.json")}
	if err := initializeCore(cfg); err == nil || !strings.Contains(err.Error(), "WB2A_API_KEY") {
		t.Fatalf("persisted API conflict was not rejected with guidance: %v", err)
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != string(before) {
		t.Fatalf("rejected configuration changed persisted keys: %v", err)
	}
}

func TestCoreSourceModeRemainsUnchanged(t *testing.T) {
	t.Setenv("WB2A_BRIDGE_KEY", "")
	cfg := &Config{AuthDir: filepath.Join(t.TempDir(), "absent")}
	public := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(418) })
	if err := initializeCore(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg.AuthDir); !os.IsNotExist(err) {
		t.Fatal("source mode initialized directory")
	}
	h, err := wrapCore(context.Background(), cfg, nil, nil, nil, public, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/livez", nil))
	if rr.Code != 418 {
		t.Fatal("source handler changed")
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("POST", "/v1/messages", nil))
	if rr.Code != 418 {
		t.Fatal("source mode gained Anthropic wrapper")
	}
}

func TestCoreAnthropicUsesSharedHandlerAndIsolatedCredentials(t *testing.T) {
	t.Setenv("WB2A_CORE", "")
	t.Setenv("WB2A_BRIDGE_KEY", strings.Repeat("b", 32))
	cfg := &Config{APIKey: "public-api"}
	cfg.Server.MaxBodyMB = 1
	const body = `{"model":"cn:model","max_tokens":4,"messages":[{"role":"user","content":"hello"}]}`
	calls := 0
	public := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer public-api" {
			t.Errorf("wrong shared handler request: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		if got["conversationId"] != nil || r.Header.Get("X-Console-Owner") != "" || r.Header.Get("X-Bridge-Key") != "" {
			t.Error("public caller injected private identity")
		}
		io.WriteString(w, `{"id":"chatcmpl-core","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`)
	})
	h, err := wrapCore(context.Background(), cfg, nil, nil, nil, public, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		key  string
		code int
	}{{"public-api", 200}, {strings.Repeat("b", 32), 401}, {"", 401}} {
		r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
		r.Header.Set("x-api-key", tc.key)
		r.Header.Set("anthropic-version", "2023-06-01")
		r.Header.Set("Authorization", "Bearer "+strings.Repeat("b", 32))
		r.Header.Set("X-Console-Owner", strings.Repeat("o", 32))
		r.Header.Set("X-Bridge-Key", strings.Repeat("b", 32))
		r.Header.Set("X-Console-Conversation", "forged")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("key=%q: %d %s", tc.key, w.Code, w.Body)
		}
		if tc.code == 200 && !strings.Contains(w.Body.String(), `"type":"message"`) {
			t.Fatalf("not adapted: %s", w.Body)
		}
	}
	if calls != 1 {
		t.Fatalf("handler calls=%d", calls)
	}
	for _, path := range []string{"/v1/messages", "/internal/v1/messages"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(strings.Repeat(" ", (1<<20)+1)))
		r.Header.Set("x-api-key", "public-api")
		r.Header.Set("anthropic-version", "2023-06-01")
		r.Header.Set("Authorization", "Bearer "+strings.Repeat("b", 32))
		r.Header.Set("X-Console-Owner", strings.Repeat("o", 32))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 413 {
			t.Errorf("configured body limit %s: %d %s", path, w.Code, w.Body)
		}
	}
}

func TestCoreRejectsInvalidBridgeKeysBeforeInitialization(t *testing.T) {
	for _, tc := range []struct{ key, api, admin string }{
		{"short", "api", "admin"},
		{strings.Repeat("b", 32), strings.Repeat("b", 32), "admin"},
		{strings.Repeat("b", 32), "api", strings.Repeat("b", 32)},
	} {
		t.Run(tc.key, func(t *testing.T) {
			t.Setenv("WB2A_BRIDGE_KEY", tc.key)
			t.Setenv("WB2A_ADMIN_KEY", tc.admin)
			cfg := &Config{APIKey: tc.api, AuthDir: filepath.Join(t.TempDir(), "auth"), StateFile: filepath.Join(t.TempDir(), "data", "pool.json")}
			if initializeCore(cfg) == nil {
				t.Fatal("invalid bridge key accepted")
			}
			if _, err := os.Stat(cfg.AuthDir); !os.IsNotExist(err) {
				t.Fatal("initialized before validating")
			}
			if _, err := wrapCore(context.Background(), cfg, nil, nil, nil, http.NotFoundHandler(), nil, nil, nil); err == nil {
				t.Fatal("wrapper accepted invalid bridge key")
			}
		})
	}
}

func TestCoreInitializesEarlyAndSeparatesLivenessAndPublicAuth(t *testing.T) {
	t.Setenv("WB2A_BRIDGE_KEY", strings.Repeat("b", 32))
	t.Setenv("WB2A_ADMIN_KEY", strings.Repeat("a", 32))
	cfg := &Config{APIKey: "public-api", AuthDir: filepath.Join(t.TempDir(), "auth"), StateFile: filepath.Join(t.TempDir(), "data", "pool.json")}
	if err := initializeCore(cfg); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{cfg.AuthDir, filepath.Dir(cfg.StateFile)} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("directory not initialized: %s", dir)
		}
	}
	p := pool.New("")
	defer p.Close()
	up := upstream.New()
	public := server.NewHandler(server.Config{Pool: p, Upstream: up, APIKey: cfg.APIKey})
	h, err := wrapCore(context.Background(), cfg, p, up, nil, public, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, key string
		code      int
	}{
		{"/livez", "", 200}, {"/healthz", "", 503}, {"/status", "", 401},
		{"/v1/models", strings.Repeat("b", 32), 401},
		{"/internal/v1/status", strings.Repeat("b", 32), 200},
		{"/internal/v1/info", "public-api", 401},
	} {
		req := httptest.NewRequest("GET", tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+tc.key)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != tc.code {
			t.Fatalf("%s: got %d want %d: %s", tc.path, rr.Code, tc.code, rr.Body)
		}
	}
}

func TestCoreTaskScheduleUsesAssignedRunnerAndCoreLifecycle(t *testing.T) {
	t.Setenv("WB2A_CORE", "")
	t.Setenv("WB2A_BRIDGE_KEY", strings.Repeat("b", 32))
	t.Setenv("WB2A_ADMIN_KEY", "")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg := &Config{StateFile: filepath.Join(t.TempDir(), "pool.json")}
	var tasks *taskrun.Runner
	callback := coreTaskSchedule(&tasks)
	if callback == nil {
		t.Fatal("opt-in scheduling not attached")
	}
	sch := scheduler.New(scheduler.Config{Scheduled: callback})
	var history *taskrun.Store
	var err error
	tasks, history, err = newCoreTasks(ctx, cfg, sch)
	if err != nil || tasks == nil || history == nil {
		t.Fatalf("task initialization: %v", err)
	}
	callback(ctx, "checkin", time.Now()) // Empty real scheduler, no account or network work.
	rows, _, err := history.Page("", 20)
	if err != nil || len(rows) != 1 || rows[0].Status != "skipped" || rows[0].Source != "scheduled" {
		t.Fatalf("callback did not use assigned runner: %+v %v", rows, err)
	}
	cancel()
	if _, err := tasks.StartManual("checkin", "request-1234567890"); err != context.Canceled {
		t.Fatalf("core lifecycle not propagated: %v", err)
	}
}

func TestCoreTaskSourceModeLeavesOriginalScheduler(t *testing.T) {
	t.Setenv("WB2A_CORE", "")
	t.Setenv("WB2A_BRIDGE_KEY", "")
	var tasks *taskrun.Runner
	if coreTaskSchedule(&tasks) != nil {
		t.Fatal("source scheduler overridden")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := &Config{StateFile: filepath.Join(t.TempDir(), "absent", "pool.json")}
	runner, history, err := newCoreTasks(ctx, cfg, nil)
	if err != nil || runner != nil || history != nil {
		t.Fatal("source mode created task runtime")
	}
	if _, err := os.Stat(filepath.Dir(cfg.StateFile)); !os.IsNotExist(err) {
		t.Fatal("source mode wrote task data")
	}
}

func TestCorruptTaskHistorySkipsSchedulesAndPreservesPublic(t *testing.T) {
	t.Setenv("WB2A_CORE", "")
	t.Setenv("WB2A_BRIDGE_KEY", strings.Repeat("b", 32))
	t.Setenv("WB2A_ADMIN_KEY", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := &Config{StateFile: filepath.Join(t.TempDir(), "pool.json")}
	path := filepath.Join(filepath.Dir(cfg.StateFile), "tasks", "runs.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("broken-history"), 0600); err != nil {
		t.Fatal(err)
	}
	var tasks *taskrun.Runner
	callback := coreTaskSchedule(&tasks)
	sch := scheduler.New(scheduler.Config{Scheduled: callback})
	tasks, history, taskErr := newCoreTasks(ctx, cfg, sch)
	if taskErr == nil || tasks != nil || history != nil || callback == nil {
		t.Fatal("corrupt history silently enabled tasks")
	}
	callback(ctx, "checkin", time.Now())
	public := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(418) })
	h, err := wrapCore(ctx, cfg, nil, nil, sch, public, tasks, history, taskErr)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/v1/models", nil))
	if rr.Code != 418 {
		t.Fatal("broken history stopped public handler")
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "broken-history" {
		t.Fatal("corrupt history overwritten")
	}
}
