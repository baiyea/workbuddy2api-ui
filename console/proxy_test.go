package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testConfig(target string) Config {
	u, _ := url.Parse(target)
	return Config{CoreURL: u, AdminKey: strings.Repeat("a", 32), APIKey: strings.Repeat("p", 32), BridgeKey: strings.Repeat("b", 32)}
}
func testConsole(t *testing.T, core http.HandlerFunc) (http.Handler, Config) {
	t.Helper()
	ts := httptest.NewServer(core)
	t.Cleanup(ts.Close)
	cfg := testConfig(ts.URL)
	h, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return h, cfg
}
func mockInfo(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/internal/v1/info" {
		return false
	}
	fmt.Fprint(w, `{"protocol":1,"global_enabled":true}`)
	return true
}
func TestPublicProxyDoesNotBorrowCredentials(t *testing.T) {
	h, _ := testConsole(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("unexpected auth %q", got)
		}
		w.WriteHeader(401)
	})
	for _, path := range []string{"/v1/models", "/status"} {
		w := adminRequest(h, "GET", path, "", nil, "")
		if w.Code != 401 {
			t.Fatalf("%s status=%d", path, w.Code)
		}
	}
}
func TestPublicProxyPreservesBearerBodyAndStripsPrivilegedHeaders(t *testing.T) {
	const body = `{"model":"cn:glm-5.2","stream":true}`
	h, _ := testConsole(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.URL.RawQuery != "x=1" {
			t.Errorf("target %s", r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer client-api-key" {
			t.Error("lost client Bearer")
		}
		for _, name := range []string{"X-Console-Owner", "X-Bridge-Key", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Forwarded", "X-Real-IP", "Cookie", "X-CSRF-Token"} {
			if r.Header.Get(name) != "" {
				t.Errorf("leaked %s", name)
			}
		}
		got, _ := io.ReadAll(r.Body)
		if string(got) != body {
			t.Errorf("body=%s", got)
		}
		w.Header().Set("X-Core-Test", "preserved")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":"limited"}`)
	})
	r := httptest.NewRequest("POST", "http://console.test/v1/chat/completions?x=1", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer client-api-key")
	for _, name := range []string{"X-Console-Owner", "X-Bridge-Key", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Forwarded", "X-Real-IP", "Cookie", "X-CSRF-Token"} {
		r.Header.Set(name, "attacker")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 429 || w.Header().Get("X-Core-Test") != "preserved" || w.Body.String() != `{"error":"limited"}` {
		t.Fatalf("response %d %s", w.Code, w.Body)
	}
}
func TestOnlyFixedPathsCanReachCore(t *testing.T) {
	h, _ := testConsole(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected core request: %s", r.URL) })
	for _, path := range []string{"/internal/v1/info", "/v1/../internal/v1/info", "/v1/%2e%2e/internal/v1/info", "/v1/%252e%252e/internal/v1/info", "/v1/%2finternal/v1/info", "/%69nternal/v1/info", "//internal/v1/info", "/admin/oauth/a%2f..%2finfo/poll"} {
		w := adminRequest(h, "GET", path, "", nil, "")
		if w.Code < 400 {
			t.Errorf("unsafe path %s accepted or redirected: %d", path, w.Code)
		}
	}
}
func TestManagementMappingsAndOwnerIsolation(t *testing.T) {
	var mu sync.Mutex
	owner := ""
	canceled := ""
	h, _ := testConsole(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("b", 32) {
			t.Error("missing bridge credential")
		}
		if mockInfo(w, r) {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		got := r.Header.Get("X-Console-Owner")
		if !regexp.MustCompile(`^[A-Za-z0-9_-]{32,128}$`).MatchString(got) || got == strings.Repeat("x", 40) {
			t.Errorf("invalid server owner %q", got)
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-CSRF-Token") != "" {
			t.Error("browser credentials leaked")
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /internal/v1/oauth":
			owner = got
			fmt.Fprint(w, `{"id":"flow-one","status":"waiting"}`)
		case "GET /internal/v1/oauth/flow-one":
			if got != owner {
				w.WriteHeader(404)
				return
			}
			if r.ContentLength != 0 {
				t.Error("poll GET retained body")
			}
			fmt.Fprint(w, `{"status":"waiting"}`)
		case "POST /internal/v1/oauth/flow-one/region":
			fmt.Fprint(w, `{"status":"complete"}`)
		case "DELETE /internal/v1/owners/" + got + "/flows":
			canceled = got
			fmt.Fprint(w, `{"ok":true}`)
		case "GET /internal/v1/status", "GET /internal/v1/models", "POST /internal/v1/chat":
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("wrong mapping: %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	})
	cookie, csrf := login(t, h)
	r := httptest.NewRequest("POST", "http://console.test/admin/oauth", strings.NewReader(`{"realm":"cn"}`))
	r.AddCookie(cookie)
	r.Header.Set("Origin", "http://console.test")
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("X-Console-Owner", strings.Repeat("x", 40))
	r.Header.Set("Authorization", "Bearer attacker")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	mu.Lock()
	if owner == cookie.Value || owner == csrf {
		t.Error("owner reused browser credential")
	}
	mu.Unlock()
	other, otherCSRF := login(t, h)
	if w := adminRequest(h, "POST", "/admin/oauth/flow-one/poll", "{}", other, otherCSRF); w.Code != 404 {
		t.Fatal("cross-session flow accepted", w.Code)
	}
	for _, tc := range []struct{ method, path, body string }{{"POST", "/admin/oauth/flow-one/poll", "{}"}, {"POST", "/admin/oauth/flow-one/region", `{"region":"SG"}`}, {"GET", "/admin/status", ""}, {"GET", "/admin/models", ""}, {"POST", "/admin/chat", `{"stream":true}`}} {
		if w := adminRequest(h, tc.method, tc.path, tc.body, cookie, csrf); w.Code != 200 {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body)
		}
	}
	if w := adminRequest(h, "POST", "/admin/logout", "{}", cookie, csrf); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	mu.Lock()
	defer mu.Unlock()
	if canceled != owner {
		t.Error("logout did not cancel owner's flows")
	}
}

func TestTaskManagementMappingsAndGuards(t *testing.T) {
	type request struct{ method, path, query, body string }
	seen := make(chan request, 4)
	h, _ := testConsole(t, func(w http.ResponseWriter, r *http.Request) {
		if mockInfo(w, r) {
			return
		}
		body, _ := io.ReadAll(r.Body)
		seen <- request{r.Method, r.URL.Path, r.URL.RawQuery, string(body)}
		if r.Method == "POST" {
			w.WriteHeader(202)
		}
		fmt.Fprint(w, `{}`)
	})
	if w := adminRequest(h, "GET", "/admin/tasks", "", nil, ""); w.Code != 401 {
		t.Fatalf("unauthenticated tasks status=%d", w.Code)
	}
	cookie, csrf := login(t, h)
	if w := adminRequest(h, "POST", "/admin/tasks/checkin/runs", `{"request_id":"rrrrrrrrrrrrrrrr"}`, cookie, ""); w.Code != 403 {
		t.Fatalf("missing CSRF status=%d", w.Code)
	}
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/admin/tasks", ""},
		{"POST", "/admin/tasks/checkin/runs", `{"request_id":"rrrrrrrrrrrrrrrr"}`},
		{"GET", "/admin/task-runs?before=run-one&limit=20", ""},
		{"GET", "/admin/task-runs/run-one", ""},
	} {
		w := adminRequest(h, tc.method, tc.path, tc.body, cookie, csrf)
		if (tc.method == "POST" && w.Code != 202) || (tc.method == "GET" && w.Code != 200) {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body)
		}
	}
	want := []request{
		{"GET", "/internal/v1/tasks", "", ""},
		{"POST", "/internal/v1/tasks/checkin/runs", "", `{"request_id":"rrrrrrrrrrrrrrrr"}`},
		{"GET", "/internal/v1/task-runs", "before=run-one&limit=20", ""},
		{"GET", "/internal/v1/task-runs/run-one", "", ""},
	}
	for _, expected := range want {
		if got := <-seen; got != expected {
			t.Fatalf("mapping got=%+v want=%+v", got, expected)
		}
	}
}
func TestProtocolFailureOnlyDisablesManagement(t *testing.T) {
	for _, info := range []string{`{"protocol":2}`, `{"protocol":1.5}`, `not-json`, `{"protocol":1} trailing`} {
		t.Run(info, func(t *testing.T) {
			h, _ := testConsole(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/internal/v1/info" {
					fmt.Fprint(w, info)
					return
				}
				if strings.HasPrefix(r.URL.Path, "/internal/") {
					t.Error("management proceeded after bad protocol")
				}
				fmt.Fprint(w, `{"data":[]}`)
			})
			cookie, csrf := login(t, h)
			for _, path := range []string{"/admin/status", "/admin/models", "/admin/session"} {
				if w := adminRequest(h, "GET", path, "", cookie, csrf); w.Code != 503 || !strings.Contains(w.Body.String(), "协议") {
					t.Fatalf("management %s: %d %s", path, w.Code, w.Body)
				}
			}
			if w := adminRequest(h, "GET", "/v1/models", "", nil, ""); w.Code != 200 {
				t.Fatal("public API blocked by protocol")
			}
		})
	}
}
func TestCoreUnavailableAndConsoleLiveness(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	cfg := testConfig(ts.URL)
	ts.Close()
	h, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := login(t, h)
	for _, path := range []string{"/v1/models", "/healthz", "/admin/status"} {
		if w := adminRequest(h, "GET", path, "", cookie, csrf); w.Code != 503 || strings.Contains(w.Body.String(), cfg.CoreURL.Host) {
			t.Fatalf("unavailable %s: %d %s", path, w.Code, w.Body)
		}
	}
	if w := adminRequest(h, "GET", "/livez", "", nil, ""); w.Code != 200 {
		t.Fatal("console liveness depends on core")
	}
}
func TestStreamingDisconnectCancelsCore(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/admin/chat"} {
		t.Run(path, func(t *testing.T) {
			canceled := make(chan struct{})
			h, _ := testConsole(t, func(w http.ResponseWriter, r *http.Request) {
				if mockInfo(w, r) {
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[]}\n\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(canceled)
			})
			cookie, csrf := login(t, h)
			ts := httptest.NewServer(h)
			defer ts.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			r, _ := http.NewRequestWithContext(ctx, "POST", ts.URL+path, strings.NewReader(`{"stream":true}`))
			r.AddCookie(cookie)
			r.Header.Set("Origin", ts.URL)
			r.Header.Set("X-CSRF-Token", csrf)
			response, err := ts.Client().Do(r)
			if err != nil {
				t.Fatal("first frame was buffered", err)
			}
			line, err := bufio.NewReader(response.Body).ReadString('\n')
			if err != nil || !strings.HasPrefix(line, "data:") {
				t.Fatalf("first frame: %q %v", line, err)
			}
			response.Body.Close()
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("client disconnect did not cancel core")
			}
		})
	}
}

func TestLogoutCancelsManagementWaitingOnProbe(t *testing.T) {
	var probes atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	h, _ := testConsole(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/v1/info" {
			if probes.Add(1) == 2 {
				close(entered)
				select {
				case <-r.Context().Done():
					return
				case <-release:
				}
			}
			mockInfo(w, r)
			return
		}
		if r.URL.Path == "/internal/v1/oauth" {
			t.Error("revoked session still started OAuth")
		}
		fmt.Fprint(w, `{"ok":true}`)
	})
	cookie, csrf := login(t, h)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- adminRequest(h, "GET", "/admin/models", "", cookie, csrf) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	if w := adminRequest(h, "POST", "/admin/logout", "{}", cookie, csrf); w.Code != 200 {
		t.Fatal("logout", w.Code)
	}
	select {
	case w := <-done:
		if w.Code != 401 && w.Code != 503 {
			t.Fatal("revoked request accepted", w.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("logout left management request running")
	}
}

func TestLogoutOrdersInFlightOAuthAndRejectsWaitingControl(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var starts atomic.Int32
	var canceled atomic.Bool
	h, _ := testConsole(t, func(w http.ResponseWriter, r *http.Request) {
		if mockInfo(w, r) {
			return
		}
		if r.URL.Path == "/internal/v1/oauth" {
			if starts.Add(1) != 1 {
				t.Error("queued control started after logout")
				return
			}
			close(started)
			<-release
			if canceled.Load() {
				t.Error("owner deleted before pending control finished")
			}
		} else if strings.HasPrefix(r.URL.Path, "/internal/v1/owners/") {
			canceled.Store(true)
		}
		fmt.Fprint(w, `{"ok":true}`)
	})
	cookie, csrf := login(t, h)
	first := make(chan *httptest.ResponseRecorder, 1)
	second := make(chan *httptest.ResponseRecorder, 1)
	logout := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- adminRequest(h, "POST", "/admin/oauth", `{"realm":"cn"}`, cookie, csrf) }()
	<-started
	// Exercise a waiter already admitted by authentication, before the gate is acquired.
	s := h.(*server)
	s.mu.Lock()
	snapshot := *s.sessions[cookie.Value]
	s.mu.Unlock()
	r := httptest.NewRequest("POST", "http://console.test/admin/oauth", strings.NewReader(`{"realm":"cn"}`))
	r.AddCookie(cookie)
	r = r.WithContext(context.WithValue(r.Context(), sessionContextKey{}, snapshot))
	go func() { w := httptest.NewRecorder(); s.management("POST", "/internal/v1/oauth")(w, r); second <- w }()
	go func() { logout <- adminRequest(h, "POST", "/admin/logout", "{}", cookie, csrf) }()
	deadline := time.After(time.Second)
	for {
		s.mu.Lock()
		_, active := s.sessions[cookie.Value]
		s.mu.Unlock()
		if !active {
			break
		}
		select {
		case <-deadline:
			t.Fatal("logout did not invalidate immediately")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if canceled.Load() {
		t.Error("logout overtook active OAuth")
	}
	close(release)
	for _, done := range []<-chan *httptest.ResponseRecorder{first, logout} {
		select {
		case w := <-done:
			if w.Code != 200 {
				t.Fatal("control/logout", w.Code, w.Body)
			}
		case <-time.After(time.Second):
			t.Fatal("gate did not release")
		}
	}
	select {
	case w := <-second:
		if w.Code != 401 {
			t.Fatal("revoked waiter", w.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter blocked")
	}
	if !canceled.Load() || starts.Load() != 1 {
		t.Fatal("owner cleanup or ordering missing")
	}
}

func TestProtocolProbeRejectsOversizedResponseAndRedirect(t *testing.T) {
	for _, kind := range []string{"oversized", "redirect"} {
		t.Run(kind, func(t *testing.T) {
			h, _ := testConsole(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/internal/v1/info" {
					t.Error("followed untrusted protocol redirect")
					return
				}
				if kind == "redirect" {
					w.Header().Set("Location", "/other")
					w.WriteHeader(302)
					return
				}
				fmt.Fprint(w, `{"protocol":1}`+strings.Repeat(" ", 9000))
			})
			cookie, csrf := login(t, h)
			if w := adminRequest(h, "GET", "/admin/session", "", cookie, csrf); w.Code != 503 {
				t.Fatal("unsafe protocol response accepted", w.Code)
			}
		})
	}
}

func TestLogoutCleanupSurvivesBrowserDisconnect(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var completed atomic.Bool
	h, _ := testConsole(t, func(w http.ResponseWriter, r *http.Request) {
		if mockInfo(w, r) {
			return
		}
		close(entered)
		select {
		case <-r.Context().Done():
			t.Error("browser canceled owner cleanup")
			return
		case <-release:
		}
		completed.Store(true)
		fmt.Fprint(w, `{"ok":true}`)
	})
	cookie, csrf := login(t, h)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("POST", "http://console.test/admin/logout", strings.NewReader("{}"))
	r = r.WithContext(ctx)
	r.AddCookie(cookie)
	r.Header.Set("Origin", "http://console.test")
	r.Header.Set("X-CSRF-Token", csrf)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); h.ServeHTTP(w, r); done <- w }()
	<-entered
	cancel()
	close(release)
	select {
	case w := <-done:
		if w.Code != 200 || !completed.Load() {
			t.Fatal("cleanup did not finish", w.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup stalled")
	}
}

func TestLogoutFailureStillInvalidatesLocalSession(t *testing.T) {
	h, _ := testConsole(t, func(w http.ResponseWriter, r *http.Request) {
		if mockInfo(w, r) {
			return
		}
		w.WriteHeader(500)
		fmt.Fprint(w, `{"error":"private diagnostic"}`)
	})
	cookie, csrf := login(t, h)
	w := adminRequest(h, "POST", "/admin/logout", "{}", cookie, csrf)
	if w.Code != 503 || strings.Contains(w.Body.String(), "private diagnostic") || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("cleanup failure hidden or leaked", w.Code, w.Body)
	}
	if w := adminRequest(h, "GET", "/admin/session", "", cookie, csrf); w.Code != 401 {
		t.Fatal("failed cleanup kept browser session live")
	}
}
