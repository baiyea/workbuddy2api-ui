package protocol

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
)

func TestCompletionRequiresActualOutput(t *testing.T) {
	for _, f := range formats {
		for _, content := range []string{"missing", "null", "empty"} {
			for _, reasoning := range []string{"missing", "null", "empty", "real"} {
				t.Run(string(f)+"/"+content+"/"+reasoning, func(t *testing.T) {
					message := map[string]any{"role": "assistant"}
					if content == "null" {
						message["content"] = nil
					}
					if content == "empty" {
						message["content"] = ""
					}
					if reasoning == "null" {
						message["reasoning_content"] = nil
					}
					if reasoning == "empty" {
						message["reasoning_content"] = ""
					}
					if reasoning == "real" {
						message["reasoning_content"] = "actual thought"
					}
					raw, _ := json.Marshal(message)
					body := `{"id":"chat-test","choices":[{"message":` + string(raw) + `,"finish_reason":"stop"}]}`
					w := run(f, false, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
					want := 502
					if content == "empty" || reasoning == "real" {
						want = 200
					}
					if w.Code != want {
						t.Fatalf("want=%d status=%d body=%s", want, w.Code, w.Body)
					}
					if reasoning == "real" && !strings.Contains(w.Body.String(), "actual thought") {
						t.Fatalf("reasoning lost: %s", w.Body)
					}
				})
			}
		}
		for _, content := range []string{"", `"content":null,`} {
			t.Run(string(f)+"/tool-only/"+content, func(t *testing.T) {
				body := `{"id":"chat-test","choices":[{"message":{"role":"assistant",` + content + `"tool_calls":[{"id":"call_a","type":"function","function":{"name":"check","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`
				w := run(f, false, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
				if w.Code != 200 || !strings.Contains(w.Body.String(), "call_a") {
					t.Fatalf("tool-only=%d %s", w.Code, w.Body)
				}
			})
		}
	}
}

func TestCompletionRetainsUnsupportedOutputErrors(t *testing.T) {
	for _, f := range formats {
		for _, content := range []string{`"content":null`, `"content":"hello","function_call":{"name":"exec","arguments":"{}"}`, `"content":"hello","refusal":"private refusal"`, `"content":"hello","reasoning_content":{}`} {
			t.Run(string(f)+content, func(t *testing.T) {
				body := strings.Replace(goodResponse, `"content":"hello"`, content, 1)
				w := run(f, false, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
				if w.Code != 502 || strings.Contains(w.Body.String(), "private") {
					t.Fatalf("status=%d body=%s", w.Code, w.Body)
				}
			})
		}
	}
}

func TestClaudeResponsePreservesRequestedModel(t *testing.T) {
	w := run(types.RelayFormatClaude, false, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.Replace(goodResponse, `"model":"test"`, `"model":"upstream-alias"`, 1))
	})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"model":"test"`) {
		t.Fatalf("response=%s", w.Body)
	}
}
