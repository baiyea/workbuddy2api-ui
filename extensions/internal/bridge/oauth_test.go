package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"workbuddy2api/internal/auth"
	"workbuddy2api/internal/oauth"
	"workbuddy2api/internal/pool"
	"workbuddy2api/internal/server"
	"workbuddy2api/internal/upstream"
)

const testKey = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const sseOK = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你好\"}}]}\n\ndata: [DONE]\n\n"

func newTestBridge(cfg Config) *handler {
	cfg.Key = testKey
	if cfg.Public == nil {
		cfg.Public = server.NewHandler(server.Config{Pool: cfg.Pool, Upstream: cfg.Upstream, APIKey: cfg.APIKey, GlobalEnabled: cfg.GlobalEnabled})
	}
	return New(context.Background(), cfg).(*handler)
}

func bridgeRequest(h http.Handler, method, path, body, owner string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+testKey)
	r.Header.Set("X-Console-Owner", owner)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

func TestOAuthOwnerIsolationAndCancellation(t *testing.T) {
	h := newTestBridge(Config{})
	owner, other := strings.Repeat("o", 32), strings.Repeat("x", 32)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.flows["flow"] = &loginFlow{id: "flow", owner: owner, ctx: ctx, cancel: cancel, expires: time.Now().Add(time.Minute), status: "waiting"}
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/internal/v1/oauth/flow", ""},
		{"POST", "/internal/v1/oauth/flow/region", `{"region":"SG"}`},
		{"DELETE", "/internal/v1/owners/" + owner + "/flows", ""},
	} {
		rr := bridgeRequest(h, tc.method, tc.path, tc.body, other)
		if rr.Code != 404 {
			t.Fatalf("%s: status %d", tc.path, rr.Code)
		}
	}
	if ctx.Err() != nil {
		t.Fatal("another owner canceled flow")
	}
	rr := bridgeRequest(h, "DELETE", "/internal/v1/owners/"+owner+"/flows", "", owner)
	if rr.Code != 200 || ctx.Err() == nil {
		t.Fatal("owner cancellation failed")
	}
	if rr := bridgeRequest(h, "GET", "/internal/v1/oauth/flow", "", owner); rr.Code != 404 {
		t.Fatal("canceled flow remains accessible")
	}
}

func TestOAuthRejectsOwnerAndInvalidBodiesBeforeDependencies(t *testing.T) {
	h := newTestBridge(Config{})
	for _, owner := range []string{"", "short", strings.Repeat("x", 129), strings.Repeat("/", 32)} {
		if rr := bridgeRequest(h, "POST", "/internal/v1/oauth", `{"realm":"cn"}`, owner); rr.Code != 400 {
			t.Fatalf("invalid owner: %d", rr.Code)
		}
	}
	owner := strings.Repeat("o", 32)
	for _, body := range []string{`{"realm":"cn","secret":"x"}`, `{"realm":"cn"} {}`, strings.Repeat(" ", 8193) + `{}`, `{"realm":"invalid"}`, `{"realm":"global"}`} {
		if rr := bridgeRequest(h, "POST", "/internal/v1/oauth", body, owner); rr.Code != 400 {
			t.Fatalf("invalid body: %d %s", rr.Code, rr.Body)
		}
	}
}

func TestOAuthFlowLimitExpiryAndTrustedURL(t *testing.T) {
	var unsafe atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		link := "https://www.codebuddy.cn/login"
		if unsafe.Load() {
			link = "https://evil.test/login"
		}
		fmt.Fprintf(w, `{"code":0,"data":{"state":"private","authUrl":%q}}`, link)
	}))
	defer ts.Close()
	p := pool.New("")
	defer p.Close()
	h := newTestBridge(Config{Pool: p, Upstream: upstream.New(), AuthDir: t.TempDir()})
	h.newOAuth = func(realm string) (*oauth.Client, error) {
		c, err := oauth.New(realm)
		if err == nil {
			c.Base = ts.URL
		}
		return c, err
	}
	owner := strings.Repeat("o", 32)
	defer bridgeRequest(h, "DELETE", "/internal/v1/owners/"+owner+"/flows", "", owner)
	for i := 0; i < 8; i++ {
		if rr := bridgeRequest(h, "POST", "/internal/v1/oauth", `{"realm":"cn"}`, owner); rr.Code != 200 {
			t.Fatalf("flow %d: %d", i, rr.Code)
		}
	}
	if rr := bridgeRequest(h, "POST", "/internal/v1/oauth", `{"realm":"cn"}`, owner); rr.Code != 429 {
		t.Fatal("ninth active flow accepted")
	}
	var expired *loginFlow
	for _, f := range h.flows {
		expired = f
		break
	}
	if remaining := time.Until(expired.expires); remaining < 9*time.Minute || remaining > 10*time.Minute {
		t.Fatal("flow expiry not ten minutes")
	}
	expired.expires = time.Now().Add(-time.Second)
	if rr := bridgeRequest(h, "GET", "/internal/v1/oauth/"+expired.id, "", owner); rr.Code != 404 || expired.ctx.Err() == nil {
		t.Fatal("expired flow not canceled")
	}
	unsafe.Store(true)
	if rr := bridgeRequest(h, "POST", "/internal/v1/oauth", `{"realm":"cn"}`, owner); rr.Code != 502 || len(h.flows) != 7 {
		t.Fatal("untrusted URL retained a flow")
	}
}

func TestOAuthOwnerCancellationPreventsInFlightInstallation(t *testing.T) {
	started := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/plugin/auth/state" {
			fmt.Fprint(w, `{"code":0,"data":{"state":"private","authUrl":"https://www.codebuddy.cn/login"}}`)
			return
		}
		close(started)
		<-r.Context().Done()
	}))
	defer ts.Close()
	p := pool.New("")
	defer p.Close()
	dir := t.TempDir()
	h := newTestBridge(Config{Pool: p, Upstream: upstream.New(), AuthDir: dir})
	h.newOAuth = func(realm string) (*oauth.Client, error) {
		c, e := oauth.New(realm)
		if e == nil {
			c.Base = ts.URL
		}
		return c, e
	}
	owner := strings.Repeat("o", 32)
	rr := bridgeRequest(h, "POST", "/internal/v1/oauth", `{"realm":"cn"}`, owner)
	var flow struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(rr.Body.Bytes(), &flow) != nil || flow.ID == "" {
		t.Fatal("flow not started")
	}
	done := make(chan struct{})
	go func() { defer close(done); bridgeRequest(h, "GET", "/internal/v1/oauth/"+flow.ID, "", owner) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("poll did not start")
	}
	bridgeRequest(h, "DELETE", "/internal/v1/owners/"+owner+"/flows", "", owner)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("poll ignored owner cancellation")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 0 || len(p.List()) != 0 {
		t.Fatal("canceled flow installed credentials")
	}
}

func TestFinishLoginPreservesModelLimits(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":0,"data":{"Response":{"Data":{"Accounts":[{"CapacityRemain":100}]}}}}`)
	}))
	defer ts.Close()
	p := pool.New("")
	defer p.Close()
	a := &auth.Auth{UID: "one", AccessToken: "token"}
	p.Add(a)
	p.CooldownSoftForModel(a.UID, 600*time.Second, time.Now().Add(time.Hour), "glm-5.3", "6004")
	up := upstream.New()
	up.HTTP, up.BillingBaseCN = ts.Client(), ts.URL
	h := newTestBridge(Config{Pool: p, Upstream: up})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.finishLogin(&loginFlow{ctx: ctx, cancel: cancel, account: a, client: &oauth.Client{Realm: "cn"}})
	st, _ := p.Status(a.UID)
	if st.Credits != 100 || len(st.RateLimitedModels) != 1 {
		t.Fatalf("balance update removed limits: %+v", st)
	}
}

func TestCanceledFinishLoginDoesNotSendRequests(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"code":0,"data":{}}`)
	}))
	defer ts.Close()
	p := pool.New("")
	defer p.Close()
	a := &auth.Auth{UID: "one", AccessToken: "token"}
	p.Add(a)
	up := upstream.New()
	up.HTTP, up.BillingBaseCN = ts.Client(), ts.URL
	h := newTestBridge(Config{Pool: p, Upstream: up})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &loginFlow{ctx: ctx, cancel: cancel, account: a, client: &oauth.Client{Realm: "cn"}}
	h.finishLogin(f)
	if calls.Load() != 0 || f.status == "complete" {
		t.Fatal("canceled flow continued post-login requests")
	}
}
func TestAdminLoginFlowInstallsAccountAndStreamsChat(t *testing.T) {
	var pending atomic.Bool
	pending.Store(true)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/plugin/auth/state":
			fmt.Fprint(w, `{"code":0,"data":{"state":"private-state","authUrl":"https://www.codebuddy.cn/login"}}`)
		case "/v2/plugin/auth/token":
			if pending.Load() {
				fmt.Fprint(w, `{"code":11217,"msg":"11217:login ing...","requestId":"test-request"}`)
				return
			}
			fmt.Fprint(w, `{"code":0,"data":{"accessToken":"upstream-secret","refreshToken":"refresh-secret","expiresIn":3600}}`)
		case "/v2/plugin/login/account":
			fmt.Fprint(w, `{"code":0,"data":{"uid":"account-1","nickname":"Demo"}}`)
		case "/v2/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, sseOK)
		default:
			fmt.Fprint(w, `{"code":0,"data":{}}`)
		}
	}))
	defer ts.Close()
	p := pool.New("")
	defer p.Close()
	dir := t.TempDir()
	up := upstream.New()
	up.HTTP = ts.Client()
	up.ChatHTTP = ts.Client()
	up.ChatBaseCN = ts.URL
	up.BillingBaseCN = ts.URL
	h := newTestBridge(Config{Pool: p, Upstream: up, APIKey: "api-test-secret", AuthDir: dir})
	h.newOAuth = func(realm string) (*oauth.Client, error) {
		c, e := oauth.New(realm)
		if e == nil {
			c.Base = ts.URL
			c.HTTP = ts.Client()
		}
		return c, e
	}
	owner := strings.Repeat("o", 32)
	rr := bridgeRequest(h, "POST", "/internal/v1/oauth", `{"realm":"cn"}`, owner)
	if rr.Code != 200 {
		t.Fatalf("start: %d %s", rr.Code, rr.Body)
	}
	var flow struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rr.Body.Bytes(), &flow)
	other := strings.Repeat("x", 32)
	if rr := bridgeRequest(h, "GET", "/internal/v1/oauth/"+flow.ID, "{}", other); rr.Code != 404 {
		t.Fatal("flow leaked to another session")
	}
	rr = bridgeRequest(h, "GET", "/internal/v1/oauth/"+flow.ID, "{}", owner)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"status":"waiting"`) || p.AuthByUID("account-1") != nil {
		t.Fatalf("pending login became terminal or installed an account: %d %s", rr.Code, rr.Body)
	}
	pending.Store(false)
	h.mu.Lock()
	f := h.flows[flow.ID]
	h.mu.Unlock()
	f.mu.Lock()
	f.lastPoll = time.Now().Add(-3 * time.Second)
	f.mu.Unlock()
	rr = bridgeRequest(h, "GET", "/internal/v1/oauth/"+flow.ID, "{}", owner)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"status":"complete"`) {
		t.Fatalf("poll: %d %s", rr.Code, rr.Body)
	}
	if strings.Contains(rr.Body.String(), "upstream-secret") || strings.Contains(rr.Body.String(), "refresh-secret") {
		t.Fatal("token leaked")
	}
	if p.AuthByUID("account-1") == nil {
		t.Fatal("account not installed")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatal("account not persisted")
	}
	rr = bridgeRequest(h, "POST", "/internal/v1/chat", `{"model":"cn:glm-5.2","messages":[{"role":"user","content":"hi"}],"stream":true}`, owner)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "你好") {
		t.Fatalf("chat: %d %s", rr.Code, rr.Body)
	}
}

func TestAdminGlobalLoginWaitsForExplicitRegionBeforeRouting(t *testing.T) {
	var selected atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/plugin/auth/state":
			fmt.Fprint(w, `{"code":0,"data":{"state":"private","authUrl":"https://www.workbuddy.ai/login"}}`)
		case "/v2/plugin/auth/token":
			fmt.Fprint(w, `{"code":0,"data":{"accessToken":"secret","refreshToken":"refresh","expiresIn":3600}}`)
		case "/v2/plugin/login/account":
			fmt.Fprint(w, `{"code":0,"data":{"uid":"global-one"}}`)
		case "/auth/realms/copilot/overseas/user/register":
			if selected.Load() {
				fmt.Fprint(w, `{"code":200}`)
			} else {
				fmt.Fprint(w, `{"code":500,"msg":"region required"}`)
			}
		case "/billing/area/get-country-code":
			fmt.Fprint(w, `{"code":0,"data":{"code":0,"data":{"list":[{"IOS2":"SG","Code":65,"EnName":"Singapore"}]}}}`)
		case "/console/login/account":
			selected.Store(true)
			fmt.Fprint(w, `{"code":0}`)
		default:
			fmt.Fprint(w, `{"code":0,"data":{}}`)
		}
	}))
	defer ts.Close()
	p := pool.New("")
	defer p.Close()
	up := upstream.New()
	up.HTTP, up.BillingBaseGlobal = ts.Client(), ts.URL
	up.GlobalEnabled = true
	dir := t.TempDir()
	h := newTestBridge(Config{Pool: p, Upstream: up, AuthDir: dir, GlobalEnabled: true})
	h.newOAuth = func(realm string) (*oauth.Client, error) {
		c, err := oauth.New(realm)
		if err == nil {
			c.Base, c.HTTP = ts.URL, ts.Client()
		}
		return c, err
	}
	owner := strings.Repeat("o", 32)
	rr := bridgeRequest(h, "POST", "/internal/v1/oauth", `{"realm":"global"}`, owner)
	var flow struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(rr.Body.Bytes(), &flow) != nil || flow.ID == "" {
		t.Fatal("no flow")
	}
	rr = bridgeRequest(h, "GET", "/internal/v1/oauth/"+flow.ID, `{}`, owner)
	if !strings.Contains(rr.Body.String(), `"status":"needs_region"`) || p.ServableNow() || selected.Load() {
		t.Fatalf("premature activation: %s", rr.Body)
	}
	loaded, err := auth.LoadDir(dir)
	if err != nil || len(loaded) != 1 || !loaded[0].PendingActivation() {
		t.Fatal("pending credentials not safely persisted")
	}
	rr = bridgeRequest(h, "POST", "/internal/v1/oauth/"+flow.ID+"/region", `{"region":"XX"}`, owner)
	if rr.Code != 400 || selected.Load() {
		t.Fatal("unoffered region accepted")
	}
	rr = bridgeRequest(h, "POST", "/internal/v1/oauth/"+flow.ID+"/region", `{"region":"SG"}`, owner)
	if !strings.Contains(rr.Body.String(), `"status":"complete"`) || !p.ServableNow() {
		t.Fatalf("activation did not enable account: %s", rr.Body)
	}
}
