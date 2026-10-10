package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCoreResponsesUsesSharedHandlerOnlyWithBridge(t *testing.T) {
	t.Setenv("WB2A_CORE", "")
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "bridge"}[enabled], func(t *testing.T) {
			key := ""
			if enabled {
				key = strings.Repeat("b", 32)
			}
			t.Setenv("WB2A_BRIDGE_KEY", key)
			cfg := &Config{APIKey: "public-api"}
			cfg.Server.MaxBodyMB = 1
			calls := 0
			public := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if !enabled {
					w.WriteHeader(418)
					return
				}
				if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer public-api" {
					t.Errorf("wrong shared request: %s %v", r.URL, r.Header)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body["conversationId"] != nil || len(r.Header) != 2 {
					t.Errorf("private identity leaked: %v %v", body, r.Header)
				}
				io.WriteString(w, `{"id":"chat-test","model":"test","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`)
			})
			h, err := wrapCore(context.Background(), cfg, nil, nil, nil, public, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"test","input":"hi"}`))
			r.Header.Set("Authorization", "Bearer public-api")
			r.Header.Set("X-Console-Conversation", "forged")
			r.Header.Set("X-Bridge-Key", strings.Repeat("b", 32))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if enabled && (w.Code != 200 || !strings.Contains(w.Body.String(), `"object":"response"`)) {
				t.Errorf("not adapted: %d %s", w.Code, w.Body)
			}
			if !enabled && w.Code != 418 {
				t.Errorf("source changed: %d", w.Code)
			}
			if calls != 1 {
				t.Errorf("calls=%d", calls)
			}
		})
	}
}
