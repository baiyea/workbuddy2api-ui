package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"workbuddy2api/internal/auth"
	"workbuddy2api/internal/upstream"
)

// globalModelsHandlerFake 探测 fake：记录模型目录 GET 请求次数/路径/鉴权头，并可控响应。
// 只服务于模型目录端点；chat 路径一律 404（本测试不触发 chat）。
type globalModelsHandlerFake struct {
	up *upstream.Client

	mu   sync.Mutex
	cnt  int    // 模型目录探测请求计数（含 console 与 /v2 家族）
	path string // 最近一次探测路径
	auth string // 最近一次探测鉴权头
	host string // 最近一次探测 Host

	status int
	body   string
}

func newGlobalModelsHandlerFake(t *testing.T, status int, body string) *globalModelsHandlerFake {
	t.Helper()
	cf := &globalModelsHandlerFake{up: &upstream.Client{}, status: status, body: body}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cf.mu.Lock()
		cf.cnt++
		cf.path = r.URL.Path
		cf.auth = r.Header.Get("Authorization")
		cf.host = r.Host
		cf.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(cf.status)
		_, _ = io.WriteString(w, cf.body)
	}))
	cf.up = &upstream.Client{
		HTTP:           &http.Client{},
		ChatBaseCN:     "http://cn.invalid", // CN 动态探测若被触发，本地解析失败即回落，绝不外连
		ChatBaseGlobal: strings.TrimSuffix(ts.URL, "/"),
		GlobalEnabled:  true,
	}
	return cf
}

func (cf *globalModelsHandlerFake) snapshot() (cnt int, path, auth, host string) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	return cf.cnt, cf.path, cf.auth, cf.host
}

// modelsProbeBody 构造探测端点对象数组响应。
func modelsProbeBody(ids ...string) string {
	var b strings.Builder
	b.WriteString(`{"code":0,"data":{"models":[`)
	for i, id := range ids {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"id":"` + id + `","name":"` + id + `"}`)
	}
	b.WriteString(`]}}`)
	return b.String()
}

// TestModelListTwoFamilies 断言 /v1/models 同时含 cn:* 与 global:* 两族；
// global 名单 = 探测结果 ∪ 静态 21 名（去重）；探测走 global base（httptest host）。
func TestModelListTwoFamilies(t *testing.T) {
	auth.SetGlobalEnabled(true)
	t.Cleanup(func() { auth.SetGlobalEnabled(true) })
	resetModelsCache()

	cf := newGlobalModelsHandlerFake(t, 200, modelsProbeBody("gpt-5.4", "probe-only-x"))
	p := testPoolWith(
		&auth.Auth{UID: "cn1", AccessToken: "at_cn", Domain: "www.codebuddy.cn", ExpiresAt: 9999999999},
		&auth.Auth{UID: "g1", AccessToken: "at_gl", Domain: "www.workbuddy.ai", ExpiresAt: 9999999999},
	)
	h := NewHandler(Config{Pool: p, Upstream: cf.up, GlobalEnabled: true})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /v1/models code=%d", rec.Code)
	}
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := jsonUnmarshal(rec.Body.String(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	var cnIDs, globIDs []string
	for _, m := range resp.Data {
		id, _ := m["id"].(string)
		switch {
		case strings.HasPrefix(id, "global:"):
			globIDs = append(globIDs, strings.TrimPrefix(id, "global:"))
		case strings.HasPrefix(id, "cn:"):
			cnIDs = append(cnIDs, strings.TrimPrefix(id, "cn:"))
		}
	}
	if len(cnIDs) == 0 {
		t.Error("no cn:* models in /v1/models")
	}
	if len(globIDs) == 0 {
		t.Fatal("no global:* models in /v1/models")
	}
	// global 名单须同时含探测独有与静态 21 名（去重）。
	if !contains(globIDs, "probe-only-x") {
		t.Errorf("global models missing probe-only-x: %v", globIDs)
	}
	if !contains(globIDs, "default-model") || !contains(globIDs, "kimi-k2.6") {
		t.Errorf("global models missing static §7.2 names: %v", globIDs)
	}
	if countOf(globIDs, "gpt-5.4") != 1 {
		t.Errorf("global models dedupe failed: gpt-5.4 count=%d", countOf(globIDs, "gpt-5.4"))
	}

	// 探测走 global base（httptest Host）+ Bearer 鉴权头 + /v2 家族首选。
	cnt, path, authz, host := cf.snapshot()
	if cnt == 0 || path != "/v2/enterprises/personal/models" {
		t.Errorf("probe path=%q cnt=%d want /v2/enterprises/personal/models", path, cnt)
	}
	if authz != "Bearer at_gl" {
		t.Errorf("probe authz=%q want Bearer at_gl", authz)
	}
	if host == "" || host == "fake.example" {
		t.Errorf("probe host=%q want global base (httptest)", host)
	}
	// 无 CN 动态调用（CN 侧需要健康 CN 号触发 FetchModels；本池有 cn 号但 fake 未服务该端点，
	// fetchDynamicModels 会失败回退静态——这里不断言 CN 调用，避免耦合 CN 缓存重置时序）。
}

// TestModelListNoGlobalAccountZeroProbe 无 global 账号：直接静态名单，探测零调用。
func TestModelListNoGlobalAccountZeroProbe(t *testing.T) {
	auth.SetGlobalEnabled(true)
	t.Cleanup(func() { auth.SetGlobalEnabled(true) })
	resetModelsCache()

	cf := newGlobalModelsHandlerFake(t, 200, modelsProbeBody("gpt-5.4"))
	p := testPoolWith(
		&auth.Auth{UID: "cn1", AccessToken: "at_cn", Domain: "www.codebuddy.cn", ExpiresAt: 9999999999},
	)
	h := NewHandler(Config{Pool: p, Upstream: cf.up, GlobalEnabled: true})

	got := h.modelList()
	var globIDs []string
	for _, m := range got {
		if id, ok := m["id"].(string); ok && strings.HasPrefix(id, "global:") {
			globIDs = append(globIDs, strings.TrimPrefix(id, "global:"))
		}
	}
	if len(globIDs) != len(upstream.GlobalModelNames) {
		t.Fatalf("no-global-account: global ids=%d want %d (static only)", len(globIDs), len(upstream.GlobalModelNames))
	}
	if !reflect.DeepEqual(globIDs, upstream.GlobalModelNames) {
		t.Errorf("no-global-account: global names != static GlobalModelNames")
	}
	cnt, _, _, _ := cf.snapshot()
	if cnt != 0 {
		t.Errorf("no-global-account: probe calls=%d want 0 (zero upstream calls)", cnt)
	}
}

// TestModelListProbeFailureFallsBackStatic 探测失败（家族全 500）→ global 名单 = 静态 21 名。
func TestModelListProbeFailureFallsBackStatic(t *testing.T) {
	auth.SetGlobalEnabled(true)
	t.Cleanup(func() { auth.SetGlobalEnabled(true) })
	resetModelsCache()

	cf := newGlobalModelsHandlerFake(t, 500, `{"code":500,"msg":"boom"}`)
	p := testPoolWith(
		&auth.Auth{UID: "g1", AccessToken: "at_gl", Domain: "www.workbuddy.ai", ExpiresAt: 9999999999},
	)
	h := NewHandler(Config{Pool: p, Upstream: cf.up, GlobalEnabled: true})

	got := h.modelList()
	var globIDs []string
	for _, m := range got {
		if id, ok := m["id"].(string); ok && strings.HasPrefix(id, "global:") {
			globIDs = append(globIDs, strings.TrimPrefix(id, "global:"))
		}
	}
	if !reflect.DeepEqual(globIDs, upstream.GlobalModelNames) {
		t.Errorf("probe-failure: global names != static GlobalModelNames: %v", globIDs)
	}
}

// TestModelListProbeCacheWithinTTL 首次探测成功 → 同 Client 二次 /v1/models 不重复探测（零新上游请求）。
func TestModelListProbeCacheWithinTTL(t *testing.T) {
	auth.SetGlobalEnabled(true)
	t.Cleanup(func() { auth.SetGlobalEnabled(true) })
	resetModelsCache()

	cf := newGlobalModelsHandlerFake(t, 200, modelsProbeBody("gpt-5.4", "probe-only-x"))
	p := testPoolWith(
		&auth.Auth{UID: "g1", AccessToken: "at_gl", Domain: "www.workbuddy.ai", ExpiresAt: 9999999999},
	)
	h := NewHandler(Config{Pool: p, Upstream: cf.up, GlobalEnabled: true})

	h.modelList()
	cnt1, _, _, _ := cf.snapshot()
	if cnt1 != 1 {
		t.Fatalf("first probe calls=%d want 1", cnt1)
	}
	h.modelList()
	cnt2, _, _, _ := cf.snapshot()
	if cnt2 != cnt1 {
		t.Errorf("cache: second modelList probe calls=%d want %d (hit 1h cache)", cnt2, cnt1)
	}
}

// jsonUnmarshal 单一用途反序列化（httptest body 而非直接 resp）。
func jsonUnmarshal(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func countOf(list []string, s string) int {
	n := 0
	for _, v := range list {
		if v == s {
			n++
		}
	}
	return n
}
