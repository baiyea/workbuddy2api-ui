package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"workbuddy2api/internal/upstream"
)

const textRequest = `{"model":"global:mock","max_tokens":32,"system":"简短回答","messages":[{"role":"user","content":[{"type":"text","text":"你好"}]}]}`
const textResponse = `{"id":"test-1","choices":[{"message":{"role":"assistant","content":"你好"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`

func request(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	r.Header.Set("x-api-key", "fixture-api")
	r.Header.Set("anthropic-version", "2023-06-01")
	return r
}

func TestTextRequestAndResponse(t *testing.T) {
	called := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture-api" {
			t.Fatal("wrong adapter destination or authentication")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"model": "global:mock", "max_tokens": float64(32), "stream": false,
			"messages": []any{map[string]any{"role": "system", "content": "简短回答"}, map[string]any{"role": "user", "content": "你好"}}}
		if !reflect.DeepEqual(body, want) {
			t.Fatalf("converted request = %#v", body)
		}
		io.WriteString(w, textResponse)
	})
	w := httptest.NewRecorder()
	New(next, "fixture-api", 8<<20).ServeHTTP(w, request(textRequest))
	if w.Code != 200 || called != 1 {
		t.Fatalf("status=%d calls=%d body=%s", w.Code, called, w.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"id": "test-1", "type": "message", "role": "assistant", "model": "global:mock",
		"content": []any{map[string]any{"type": "text", "text": "你好"}}, "stop_reason": "end_turn", "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": float64(3), "output_tokens": float64(2)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("response = %#v", got)
	}
}

func TestExistingPathsPassThroughUnchanged(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/models"} {
		t.Run(path, func(t *testing.T) {
			r := httptest.NewRequest("POST", path+"?old=query", strings.NewReader("old raw body"))
			r.Header.Set("Authorization", "Bearer old-key")
			next := http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
				if got != r {
					t.Fatal("existing request was replaced")
				}
				body, _ := io.ReadAll(got.Body)
				w.Header().Set("X-Existing", "unchanged")
				w.WriteHeader(418)
				w.Write(body)
			})
			w := httptest.NewRecorder()
			New(next, "fixture-api", 1).ServeHTTP(w, r)
			if w.Code != 418 || w.Body.String() != "old raw body" || w.Header().Get("X-Existing") != "unchanged" {
				t.Fatal("existing response changed")
			}
		})
	}
}

func TestRejectInvalidRequestsBeforeNext(t *testing.T) {
	base := `{"model":"global:mock","max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`
	field := func(name, value string) string {
		var body map[string]json.RawMessage
		json.Unmarshal([]byte(base), &body)
		if value == "" {
			delete(body, name)
		} else {
			body[name] = json.RawMessage(value)
		}
		raw, _ := json.Marshal(body)
		return string(raw)
	}
	tests := []struct {
		name, body string
		edit       func(*http.Request)
		key        string
		limit      int64
		code       int
	}{
		{name: "wrong key", edit: func(r *http.Request) { r.Header.Set("x-api-key", "wrong") }, code: 401},
		{name: "admin key", edit: func(r *http.Request) { r.Header.Set("x-api-key", "mock-admin-key") }, code: 401},
		{name: "bridge key", edit: func(r *http.Request) { r.Header.Set("x-api-key", "mock-bridge-key") }, code: 401},
		{name: "missing key", edit: func(r *http.Request) { r.Header.Del("x-api-key") }, code: 401},
		{name: "bearer alone", edit: func(r *http.Request) { r.Header.Del("x-api-key"); r.Header.Set("Authorization", "Bearer fixture-api") }, code: 401},
		{name: "ambiguous key", edit: func(r *http.Request) { r.Header.Add("x-api-key", "wrong") }, code: 401},
		{name: "no configured key", key: "empty", code: 401},
		{name: "missing version", edit: func(r *http.Request) { r.Header.Del("anthropic-version") }, code: 400},
		{name: "wrong version", edit: func(r *http.Request) { r.Header.Set("anthropic-version", "2099-01-01") }, code: 400},
		{name: "ambiguous version", edit: func(r *http.Request) { r.Header.Add("anthropic-version", "2023-06-01") }, code: 400},
		{name: "beta", edit: func(r *http.Request) { r.Header.Set("anthropic-beta", "tools") }, code: 400},
		{name: "empty beta", edit: func(r *http.Request) { r.Header.Set("anthropic-beta", "") }, code: 400},
		{name: "method", edit: func(r *http.Request) { r.Method = "GET" }, code: 405},
		{name: "query", edit: func(r *http.Request) { r.URL.RawQuery = "ignored=1" }, code: 400},
		{name: "bare query", edit: func(r *http.Request) { r.URL.ForceQuery = true }, code: 400},
		{name: "compressed", edit: func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }, code: 400},
		{name: "missing model", body: field("model", ""), code: 400},
		{name: "empty model", body: field("model", `""`), code: 400},
		{name: "blank model", body: field("model", `"  "`), code: 400},
		{name: "null model", body: field("model", `null`), code: 400},
		{name: "missing max_tokens", body: field("max_tokens", ""), code: 400},
		{name: "null max_tokens", body: field("max_tokens", `null`), code: 400},
		{name: "fraction max_tokens", body: field("max_tokens", `1.5`), code: 400},
		{name: "negative max_tokens", body: field("max_tokens", `-1`), code: 400},
		{name: "zero max_tokens", body: field("max_tokens", `0`), code: 400},
		{name: "string max_tokens", body: field("max_tokens", `"32"`), code: 400},
		{name: "missing messages", body: field("messages", ""), code: 400},
		{name: "null messages", body: field("messages", `null`), code: 400},
		{name: "empty messages", body: field("messages", `[]`), code: 400},
		{name: "null message", body: field("messages", `[null]`), code: 400},
		{name: "stream null", body: field("stream", `null`), code: 400},
		{name: "stream string", body: field("stream", `"false"`), code: 400},
		{name: "stream number", body: field("stream", `1`), code: 400},
		{name: "stream not implemented", body: field("stream", `true`), code: 400},
		{name: "unknown field", body: field("temperature", `0.5`), code: 400},
		{name: "private conversation field", body: field("conversationId", `"spoofed"`), code: 400},
		{name: "null unsupported field", body: field("tools", `null`), code: 400},
		{name: "two JSON documents", body: base + ` {}`, code: 400},
		{name: "null document", body: `null`, code: 400},
		{name: "UTF8", body: strings.Replace(base, "hi", string([]byte{0xff}), 1), code: 400},
		{name: "null system", body: field("system", `null`), code: 400},
		{name: "unknown system block field", body: field("system", `[{"type":"text","text":"hi","cache_control":{}}]`), code: 400},
		{name: "system role in messages", body: field("messages", `[{"role":"system","content":"hi"}]`), code: 400},
		{name: "unknown message field", body: field("messages", `[{"role":"user","content":"hi","name":"extra"}]`), code: 400},
		{name: "missing content", body: field("messages", `[{"role":"user"}]`), code: 400},
		{name: "null content", body: field("messages", `[{"role":"user","content":null}]`), code: 400},
		{name: "image", body: field("messages", `[{"role":"user","content":[{"type":"image","source":{}}]}]`), code: 400},
		{name: "tool", body: field("messages", `[{"role":"assistant","content":[{"type":"tool_use","id":"call"}]}]`), code: 400},
		{name: "unknown text block field", body: field("messages", `[{"role":"user","content":[{"type":"text","text":"hi","extra":1}]}]`), code: 400},
		{name: "missing text", body: field("messages", `[{"role":"user","content":[{"type":"text"}]}]`), code: 400},
		{name: "null text", body: field("messages", `[{"role":"user","content":[{"type":"text","text":null}]}]`), code: 400},
		{name: "raw over limit", body: base, limit: 8, code: 413},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.body
			if body == "" {
				body = base
			}
			r := request(body)
			if tc.edit != nil {
				tc.edit(r)
			}
			key := "fixture-api"
			if tc.key == "empty" {
				key = ""
			}
			limit := tc.limit
			if limit == 0 {
				limit = 8 << 20
			}
			calls := 0
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; io.WriteString(w, textResponse) })
			w := httptest.NewRecorder()
			New(next, key, limit).ServeHTTP(w, r)
			if w.Code != tc.code || calls != 0 {
				t.Fatalf("status=%d want=%d next calls=%d", w.Code, tc.code, calls)
			}
			assertError(t, w, tc.code)
		})
	}
}

func assertError(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	var body struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("error is not JSON: %s", w.Body)
	}
	want := "api_error"
	switch status {
	case 401:
		want = "authentication_error"
	case 400, 405:
		want = "invalid_request_error"
	case 413:
		want = "request_too_large"
	case 429:
		want = "rate_limit_error"
	}
	if body.Type != "error" || body.Error.Type != want || body.Error.Message == "" {
		t.Fatalf("bad error envelope: %s", w.Body)
	}
}

func TestTextBlocksAndMultiturnPreserveOrder(t *testing.T) {
	body := `{"model":"cn:mock","max_tokens":2147483648,"system":[{"type":"text","text":"a"},{"type":"text","text":"b"}],"messages":[{"role":"user","content":"你好"},{"role":"assistant","content":[{"type":"text","text":"x"},{"type":"text","text":"y"}]},{"role":"user","content":"继续"}],"stream":false}`
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got struct {
			Messages  []map[string]string `json:"messages"`
			MaxTokens int64               `json:"max_tokens"`
		}
		json.NewDecoder(r.Body).Decode(&got)
		want := []map[string]string{{"role": "system", "content": "ab"}, {"role": "user", "content": "你好"}, {"role": "assistant", "content": "xy"}, {"role": "user", "content": "继续"}}
		if !reflect.DeepEqual(got.Messages, want) || got.MaxTokens != 2147483648 {
			t.Fatalf("lost supported content: %#v", got)
		}
		io.WriteString(w, textResponse)
	})
	w := httptest.NewRecorder()
	New(next, "fixture-api", 0).ServeHTTP(w, request(body))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}

func TestConvertedBodyAlsoHasLimit(t *testing.T) {
	// JSON escaping '<' expands the validated body; the original fits this limit.
	body := `{"model":"cn:mock","max_tokens":32,"messages":[{"role":"user","content":"` + strings.Repeat("<", 100) + `"}]}`
	calls := 0
	w := httptest.NewRecorder()
	New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; io.WriteString(w, textResponse) }), "fixture-api", int64(len(body))).ServeHTTP(w, request(body))
	if w.Code != 413 || calls != 0 {
		t.Fatalf("converted body escaped limit: status=%d calls=%d", w.Code, calls)
	}
	assertError(t, w, 413)
}

func TestResponseMapping(t *testing.T) {
	withoutUsage := `{"id":"test-1","choices":[{"message":{"content":"你好"},"finish_reason":"stop"}]}`
	tests := []struct {
		name, body    string
		code          int
		reason        string
		input, output any
	}{
		{"stop", textResponse, 200, "end_turn", float64(3), float64(2)},
		{"length", strings.Replace(textResponse, `"stop"`, `"length"`, 1), 200, "max_tokens", float64(3), float64(2)},
		{"missing usage", withoutUsage, 200, "end_turn", nil, nil},
		{"null usage", strings.TrimSuffix(withoutUsage, "}") + `,"usage":null}`, 200, "end_turn", nil, nil},
		{"zero usage", strings.ReplaceAll(strings.ReplaceAll(textResponse, `tokens":3`, `tokens":0`), `tokens":2`, `tokens":0`), 200, "end_turn", float64(0), float64(0)},
		{"partial usage", strings.Replace(textResponse, `"prompt_tokens":3,`, "", 1), 200, "end_turn", nil, float64(2)},
		{"null token", strings.Replace(textResponse, `"prompt_tokens":3`, `"prompt_tokens":null`, 1), 200, "end_turn", nil, float64(2)},
		{"negative token", strings.Replace(textResponse, `"prompt_tokens":3`, `"prompt_tokens":-1`, 1), 502, "", nil, nil},
		{"fraction token", strings.Replace(textResponse, `"prompt_tokens":3`, `"prompt_tokens":1.5`, 1), 502, "", nil, nil},
		{"empty optional fields", strings.Replace(textResponse, `"content":"你好"`, `"content":"你好","tool_calls":[],"refusal":"","function_call":{"name":"","arguments":""},"reasoning_content":""`, 1), 200, "end_turn", float64(3), float64(2)},
		{"null optional fields", strings.Replace(textResponse, `"content":"你好"`, `"content":"你好","tool_calls":null,"refusal":null,"function_call":null,"reasoning_content":null`, 1), 200, "end_turn", float64(3), float64(2)},
		{"reasoning is not content", strings.Replace(textResponse, `"content":"你好"`, `"content":"你好","reasoning_content":"private-reasoning"`, 1), 200, "end_turn", float64(3), float64(2)},
		{"tools", strings.Replace(textResponse, `"content":"你好"`, `"content":"你好","tool_calls":[{"id":"call","function":{"name":"exec","arguments":"{}"}}]`, 1), 502, "", nil, nil},
		{"legacy function", strings.Replace(textResponse, `"content":"你好"`, `"content":"你好","function_call":{"name":"exec","arguments":"{}"}`, 1), 502, "", nil, nil},
		{"refusal", strings.Replace(textResponse, `"content":"你好"`, `"content":"你好","refusal":"private refusal"`, 1), 502, "", nil, nil},
		{"missing choices", `{"id":"test-1"}`, 502, "", nil, nil},
		{"many choices", `{"id":"test-1","choices":[{"message":{"content":"one"},"finish_reason":"stop"},{"message":{"content":"two"},"finish_reason":"stop"}]}`, 502, "", nil, nil},
		{"null choice", `{"id":"test-1","choices":[null]}`, 502, "", nil, nil},
		{"null message", `{"id":"test-1","choices":[{"message":null,"finish_reason":"stop"}]}`, 502, "", nil, nil},
		{"nontext content", strings.Replace(textResponse, `"content":"你好"`, `"content":[{"type":"text","text":"wrong shape"}]`, 1), 502, "", nil, nil},
		{"missing content", strings.Replace(textResponse, `"content":"你好"`, `"reasoning_content":"private-reasoning"`, 1), 502, "", nil, nil},
		{"null content", strings.Replace(textResponse, `"content":"你好"`, `"content":null`, 1), 502, "", nil, nil},
		{"wrong role", strings.Replace(textResponse, `"role":"assistant"`, `"role":"user"`, 1), 502, "", nil, nil},
		{"tool finish", strings.Replace(textResponse, `"stop"`, `"tool_calls"`, 1), 502, "", nil, nil},
		{"unknown finish", strings.Replace(textResponse, `"stop"`, `"content_filter"`, 1), 502, "", nil, nil},
		{"null finish", strings.Replace(textResponse, `"stop"`, `null`, 1), 502, "", nil, nil},
		{"empty id", strings.Replace(textResponse, `"id":"test-1"`, `"id":""`, 1), 502, "", nil, nil},
		{"error with text", strings.TrimSuffix(textResponse, "}") + `,"error":{"message":"secret"}}`, 502, "", nil, nil},
		{"invalid UTF8", strings.Replace(textResponse, "你好", string([]byte{0xff}), 1), 502, "", nil, nil},
		{"trailing JSON", textResponse + ` {}`, 502, "", nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, tc.body) }), "fixture-api", 8<<20).ServeHTTP(w, request(textRequest))
			if w.Code != tc.code {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.code, w.Body)
			}
			if tc.code != 200 {
				assertError(t, w, tc.code)
				if strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "secret") {
					t.Fatal("upstream details leaked")
				}
				return
			}
			var body map[string]any
			json.Unmarshal(w.Body.Bytes(), &body)
			usage := body["usage"].(map[string]any)
			if body["stop_reason"] != tc.reason || usage["input_tokens"] != tc.input || usage["output_tokens"] != tc.output || body["content"].([]any)[0].(map[string]any)["text"] != "你好" {
				t.Fatalf("bad mapped result: %s", w.Body)
			}
			if strings.Contains(w.Body.String(), "private-reasoning") {
				t.Fatal("reasoning exposed")
			}
		})
	}
}

func TestRealAggregateOutput(t *testing.T) {
	// Exercise the actual ordinary Handler's response producer, without accounts/network.
	result, err := upstream.Aggregate(strings.NewReader("data: {\"id\":\"real-aggregate\",\"model\":\"mock\",\"choices\":[{\"delta\":{\"content\":\"你好\",\"reasoning_content\":\"private-reasoning\"},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(result) }), "fixture-api", 8<<20).ServeHTTP(w, request(textRequest))
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != 200 || body["stop_reason"] != "max_tokens" || body["model"] != "global:mock" || strings.Contains(w.Body.String(), "private-reasoning") {
		t.Fatalf("actual Aggregate response: %s", w.Body)
	}
}

func TestUpstreamErrorsAreSanitizedWithoutForwardingHeaders(t *testing.T) {
	for _, status := range []int{302, 400, 401, 403, 404, 413, 429, 500, 502, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			w := httptest.NewRecorder()
			New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Set-Cookie", "secret-cookie")
				w.Header().Set("Authorization", "secret-key")
				w.Header().Set("Location", "https://private.invalid")
				w.Header().Set("X-Account", "secret-account")
				w.WriteHeader(status)
				io.WriteString(w, `{"error":{"type":"upstream_parse","message":"secret-raw-upstream"}}`)
			}), "fixture-api", 8<<20).ServeHTTP(w, request(textRequest))
			want := status
			if status < 400 {
				want = 502
			}
			if w.Code != want {
				t.Fatalf("status=%d want=%d", w.Code, want)
			}
			assertError(t, w, want)
			var body map[string]any
			json.Unmarshal(w.Body.Bytes(), &body)
			if body["error"].(map[string]any)["message"] != "模型调用失败，请稍后重试" {
				t.Fatalf("unsafe error: %s", w.Body)
			}
			for _, key := range []string{"Set-Cookie", "Authorization", "Location", "X-Account"} {
				if w.Header().Get(key) != "" {
					t.Fatal("upstream header leaked: " + key)
				}
			}
		})
	}
}

func TestResponseLimitCancelsNext(t *testing.T) {
	var nextCtx context.Context
	failed := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCtx = r.Context()
		chunk := bytes.Repeat([]byte("x"), 1<<20)
		for i := 0; i < 33; i++ {
			_, err := w.Write(chunk)
			if err != nil {
				failed = true
				if nextCtx.Err() != context.Canceled {
					t.Fatal("overflow did not cancel next")
				}
				return
			}
		}
	})
	w := httptest.NewRecorder()
	New(next, "fixture-api", 8<<20).ServeHTTP(w, request(textRequest))
	if w.Code != 502 || !failed || nextCtx.Err() != context.Canceled {
		t.Fatalf("unbounded capture: status=%d write_failed=%t", w.Code, failed)
	}
	assertError(t, w, 502)
}

func TestRequestIsolationAndTrustedConversation(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		t.Run(fmt.Sprint(trusted), func(t *testing.T) {
			r := request(textRequest)
			for _, key := range []string{"Authorization", "Cookie", "X-Console-Owner", "X-WB2A-Conversation-ID", "X-Conversation-ID", "X-Conversation-Request-ID", "X-Trace-ID", "Anthropic-Dangerous-Direct-Browser-Access", "Content-Length", "Accept-Encoding"} {
				r.Header.Set(key, "public-spoof")
			}
			r.URL.RawPath = "/v1/%6dessages"
			r.TransferEncoding = []string{"chunked"}
			r.Trailer = http.Header{"X-Console-Owner": {"trailer-spoof"}}
			r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("old secret body")), nil }
			if trusted {
				r = r.WithContext(WithConversation(r.Context(), "trusted-conversation"))
			}
			headers := r.Header.Clone()
			var nextCtx context.Context
			next := http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
				nextCtx = got.Context()
				if got == r || got.URL == r.URL {
					t.Fatal("request or URL not cloned")
				}
				if got.URL.Path != "/v1/chat/completions" || got.URL.RawPath != "" || got.RequestURI != "/v1/chat/completions" {
					t.Fatal("bad rewritten URL")
				}
				if !reflect.DeepEqual(got.Header, http.Header{"Content-Type": {"application/json"}, "Authorization": {"Bearer fixture-api"}}) {
					t.Fatalf("headers leaked: %v", got.Header)
				}
				if got.GetBody != nil || len(got.TransferEncoding) != 0 || len(got.Trailer) != 0 {
					t.Fatal("stale body/encoding/trailers survived conversion")
				}
				raw, _ := io.ReadAll(got.Body)
				if int64(len(raw)) != got.ContentLength {
					t.Fatal("stale content length")
				}
				var body map[string]any
				json.Unmarshal(raw, &body)
				if trusted {
					if body["conversationId"] != "trusted-conversation" {
						t.Fatal("trusted context conversation lost")
					}
				} else if _, ok := body["conversationId"]; ok {
					t.Fatal("public private header became conversation")
				}
				io.WriteString(w, textResponse)
			})
			w := httptest.NewRecorder()
			New(next, "fixture-api", 8<<20).ServeHTTP(w, r)
			if w.Code != 200 || !reflect.DeepEqual(r.Header, headers) || r.URL.Path != "/v1/messages" || r.Context().Err() != nil {
				t.Fatal("conversion mutated caller")
			}
			if nextCtx.Err() != context.Canceled {
				t.Fatal("adapter did not release child context")
			}
		})
	}
}

func TestTrustedConversationCountsTowardConvertedLimit(t *testing.T) {
	r := request(textRequest).WithContext(WithConversation(context.Background(), strings.Repeat("c", 1000)))
	calls := 0
	w := httptest.NewRecorder()
	New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; io.WriteString(w, textResponse) }), "fixture-api", int64(len(textRequest)+10)).ServeHTTP(w, r)
	if w.Code != 413 || calls != 0 {
		t.Fatalf("trusted metadata escaped bound: status=%d calls=%d", w.Code, calls)
	}
}

func TestCallerCancellationReachesNext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		if r.Context().Err() != context.Canceled {
			t.Fatal("caller cancellation lost")
		}
		if _, err := io.WriteString(w, textResponse); err == nil {
			t.Fatal("canceled response was accepted")
		}
	})
	w := httptest.NewRecorder()
	New(next, "fixture-api", 8<<20).ServeHTTP(w, request(textRequest).WithContext(ctx))
	if w.Code != 502 {
		t.Fatalf("status=%d", w.Code)
	}
}
