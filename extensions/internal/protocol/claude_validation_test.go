package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
)

func TestClaudeNestedValidation(t *testing.T) {
	base := `{"model":"mock","max_tokens":4096,"messages":[{"role":"user","content":"hi"}]}`
	for _, tc := range []struct{ key, value, path string }{
		{"model max_tokens", `"ignored"`, "model max_tokens"},
		{"tools", `[{"name":"check","input_schema":{"type":"object"}}]`, ""},
		{"tools", `[null]`, "tools[0]"},
		{"tools", `[{"name":"check","input_schema":null}]`, "tools[0].input_schema"},
		{"tools", `[{"name":"check","input_schema":{},"strict":true}]`, "tools[0].strict"},
		{"tools", `[{"name":"check","input_schema":{},"unexpected":1}]`, "tools[0].unexpected"},
		{"tool_choice", `{"type":"any"}`, "tool_choice"},
		{"temperature", `0.5`, ""},
		{"temperature", `null`, "temperature"},
		{"top_p", `1.2`, "top_p"},
		{"top_k", `-1`, "top_k"},
		{"stop_sequences", `[null]`, "stop_sequences[0]"},
		{"thinking", `{"type":"enabled","budget_tokens":1024}`, ""},
		{"thinking", `{"type":"enabled","budget_tokens":null}`, "thinking.budget_tokens"},
		{"thinking", `{"type":"adaptive","extra":true}`, "thinking.extra"},
		{"output_config", `{"effort":"high"}`, ""},
		{"output_config", `{"format":{"type":"json_schema"}}`, "output_config.format"},
		{"messages", `[{"role":"assistant","content":[{"type":"thinking","thinking":"thought","signature":""},{"type":"text","text":"answer"}]}]`, ""},
		{"messages", `[{"role":"assistant","content":[{"type":"thinking","thinking":"thought","signature":"opaque"}]}]`, "messages[0].content[0].signature"},
		{"messages", `[{"role":"assistant","content":[{"type":"redacted_thinking","data":"opaque"}]}]`, "messages[0].content[0]"},
		{"messages", `[{"role":"user","content":[{"type":"thinking","thinking":"thought"}]}]`, "messages[0].content[0]"},
		{"messages", `[{"role":"assistant","content":[{"type":"tool_use","id":"call_a","name":"check","input":null}]}]`, "messages[0].content[0].input"},
		{"messages", `[{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_a","content":[{"type":"text","text":"ok"}],"is_error":false}]}]`, ""},
		{"messages", `[{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_a","content":"bad","is_error":true}]}]`, "messages[0].content[0].is_error"},
		{"messages", `[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGk="}}]}]`, ""},
		{"messages", `[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"%%%"}}]}]`, "messages[0].content[0].source.data"},
		{"messages", `[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://example.com/a.png"}}]}]`, ""},
		{"messages", `[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"file:///private"}}]}]`, "messages[0].content[0].source.url"},
		{"messages", `[{"role":"user","content":[{"type":"document","source":{"type":"text","media_type":"text/plain","data":"document"}}]}]`, ""},
		{"messages", `[{"role":"user","content":[{"type":"document","source":{"type":"file","file_id":"opaque"}}]}]`, "messages[0].content[0].source"},
		{"system", `[{"type":"text","text":"hi","extra":1}]`, "system[0].extra"},
		{"messages", `[{"role":"user","content":[{"type":"text","text":"hi","extra":1}]}]`, "messages[0].content[0].extra"},
	} {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			var fields map[string]json.RawMessage
			_ = json.Unmarshal([]byte(base), &fields)
			fields[tc.key] = json.RawMessage(tc.value)
			raw, _ := json.Marshal(fields)
			_, _, _, err := parseRequest(raw, types.RelayFormatClaude)
			if tc.path == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.path) {
				t.Fatalf("want %s, got %v", tc.path, err)
			}
		})
	}
}
