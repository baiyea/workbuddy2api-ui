package protocol

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
)

var formats = []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatClaude}

func requestBody(f types.RelayFormat, stream bool) string {
	if f == types.RelayFormatClaude {
		if stream {
			return `{"model":"test","messages":[{"role":"user","content":"hi"}],"max_tokens":12,"stream":true}`
		}
		return `{"model":"test","messages":[{"role":"user","content":"hi"}],"max_tokens":12}`
	}
	if stream {
		return `{"model":"test","input":"hi","max_output_tokens":12,"stream":true}`
	}
	return `{"model":"test","input":"hi","max_output_tokens":12}`
}

const goodResponse = `{"id":"chat-test","model":"test","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`

func run(f types.RelayFormat, stream bool, next http.HandlerFunc) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	Serve(w, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(requestBody(f, stream))), next, "key", 1<<20, f, "")
	return w
}
func TestRequestIsolationAndConvertedLimit(t *testing.T) {
	for _, f := range formats {
		t.Run(string(f), func(t *testing.T) {
			type ownerKey struct{}
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), ownerKey{}, "owner"))
			defer cancel()
			r := httptest.NewRequest("POST", "/v1/responses?private=x", strings.NewReader(requestBody(f, false))).WithContext(ctx)
			r.Header.Set("X-Conversation-Id", "forged")
			r.Header.Set("X-Owner", "forged")
			r.Header.Set("Authorization", "secret")
			called := false
			Serve(httptest.NewRecorder(), r, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if r.Context().Value(ownerKey{}) != "owner" {
					t.Error("owner lost")
				}
				cancel()
				if r.Context().Err() == nil {
					t.Error("cancel lost")
				}
				if r.URL.Path != "/v1/chat/completions" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer key" || len(r.Header) != 2 {
					t.Errorf("request isolation failed: %s %v", r.URL, r.Header)
				}
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				if body["conversationId"] != "trusted" || body["max_tokens"] != float64(12) || body["max_completion_tokens"] != nil {
					t.Errorf("body mapping: %v", body)
				}
				io.WriteString(w, goodResponse)
			}), "key", 1<<20, f, "trusted")
			if !called {
				t.Fatal("handler not called")
			}
			called = false
			body := requestBody(f, false)
			w := httptest.NewRecorder()
			Serve(w, httptest.NewRequest("POST", "/", strings.NewReader(body)), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }), "key", int64(len(body)), f, strings.Repeat("x", 128))
			if called || w.Code != 413 {
				t.Errorf("converted limit: called=%v status=%d", called, w.Code)
			}
		})
	}
}
func TestInvalidRequestsDoNotReachHandler(t *testing.T) {
	for _, f := range formats {
		for _, body := range []string{"null", requestBody(f, false) + ` {}`, `{"model":"","input":"hi"}`, strings.Replace(requestBody(f, false), `"model":"test"`, `"model":"test","stream":"true"`, 1), strings.Replace(requestBody(f, false), `"model":"test"`, `"model":"test","conversationId":"forged"`, 1), strings.Replace(requestBody(f, false), `"model":"test"`, `"model":"test","stream":null`, 1)} {
			t.Run(string(f)+body, func(t *testing.T) {
				w := httptest.NewRecorder()
				called := false
				Serve(w, httptest.NewRequest("POST", "/", strings.NewReader(body)), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }), "key", 1<<20, f, "")
				if called || w.Code != 400 {
					t.Errorf("called=%v status=%d body=%s", called, w.Code, w.Body.String())
				}
			})
		}
	}
	for _, tail := range []string{`"input":null`, `"input":[null]`, `"input":[{"role":"bad","content":"hi"}]`, `"input":[{"role":"user","content":[null]}]`, `"input":"hi","store":true`, `"input":"hi","background":true`, `"input":"hi","parallel_tool_calls":"yes"`, `"input":"hi","previous_response_id":"r1"`, `"input":"hi","tools":{}`} {
		t.Run(tail, func(t *testing.T) {
			w := httptest.NewRecorder()
			Serve(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"model":"test",`+tail+`}`)), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("invalid request reached handler") }), "key", 1<<20, types.RelayFormatOpenAIResponses, "")
			if w.Code != 400 {
				t.Errorf("status %d", w.Code)
			}
		})
	}
}

func TestResponsesLooseFieldsRejectMalformedShapes(t *testing.T) {
	for _, tail := range []string{
		`"input":[{"type":"unknown"}]`, `"input":[{"type":"reasoning","summary":[{"type":"summary_text","text":"think"}]}]`,
		`"input":[{"type":"function_call","name":"f","call_id":17,"arguments":"{}"}]`,
		`"input":[{"type":"function_call_output","call_id":"c","output":null}]`,
		`"input":[{"type":"custom_tool_call","name":"f","call_id":"c","input":{}}]`,
		`"input":"hi","text":{"format":true}`, `"input":"hi","text":{"format":{"type":"json_schema","name":"f","schema":[],"strict":"yes"}}`,
		`"input":"hi","tools":[{"type":"function","name":"f","parameters":[]}]`,
		`"input":"hi","tools":[{"type":"function","name":"f","strict":"yes"}]`,
		`"input":"hi","max_output_tokens":0`,
	} {
		t.Run(tail, func(t *testing.T) {
			w := httptest.NewRecorder()
			Serve(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"model":"test",`+tail+`}`)), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("malformed request reached handler") }), "key", 1<<20, types.RelayFormatOpenAIResponses, "")
			if w.Code != 400 {
				t.Errorf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestRequestScopedCustomTools(t *testing.T) {
	for _, custom := range []bool{true, false} {
		body := `{"model":"test","input":"hi","store":false,"background":false`
		if custom {
			body += `,"tools":[{"type":"custom","name":"exec"}]`
		}
		body += `}`
		w := httptest.NewRecorder()
		Serve(w, httptest.NewRequest("POST", "/", strings.NewReader(body)), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"id":"c","model":"test","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"exec","arguments":"{\"input\":\"ls\"}"}}]},"finish_reason":"tool_calls"}]}`)
		}), "key", 1<<20, types.RelayFormatOpenAIResponses, "")
		want := `"type":"function_call"`
		if custom {
			want = `"type":"custom_tool_call"`
		}
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("custom=%v: %s", custom, w.Body.String())
		}
	}
}

func TestOriginalRequestLimitAndUTF8(t *testing.T) {
	for _, f := range formats {
		for _, tc := range []struct {
			body   string
			limit  int64
			status int
		}{{requestBody(f, false), 3, 413}, {"{\"model\":\"\xff\"}", 1024, 400}} {
			w := httptest.NewRecorder()
			Serve(w, httptest.NewRequest("POST", "/", strings.NewReader(tc.body)), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("invalid input reached handler") }), "key", tc.limit, f, "")
			if w.Code != tc.status {
				t.Errorf("status=%d", w.Code)
			}
		}
	}
}

func TestInvalidResponseToolsCannotComplete(t *testing.T) {
	for _, f := range formats {
		for _, calls := range []string{
			`{}`, `[null]`, `[{"id":"call_1","type":"function","function":{"arguments":"secret"}}]`,
			`[{"id":"call_1","type":"function","function":{"name":"f"}}]`,
			`[{"type":"function","function":{"name":"f","arguments":"{}"}}]`,
			`[{"id":"call_1","type":"invalid","function":{"name":"f","arguments":"{}"}}]`, `[]`,
		} {
			t.Run(string(f)+calls, func(t *testing.T) {
				var handlerContext context.Context
				w := run(f, false, func(w http.ResponseWriter, r *http.Request) {
					handlerContext = r.Context()
					io.WriteString(w, `{"id":"chat-test","model":"test","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":`+calls+`},"finish_reason":"tool_calls"}]}`)
				})
				if w.Code != 502 || !strings.Contains(w.Body.String(), "error") || strings.Contains(w.Body.String(), "secret") {
					t.Errorf("invalid tools accepted/leaked: %d %s", w.Code, w.Body.String())
				}
				if handlerContext.Err() == nil {
					t.Error("handler context not canceled")
				}
			})
		}
	}
}
