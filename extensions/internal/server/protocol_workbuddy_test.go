package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
)

// Sanitized shapes observed on WorkBuddy's wire; IDs, content and usage are synthetic.
const workBuddyEmptyDelta = `"role":"assistant","content":"","reasoning_content":"","function_call":null,"refusal":"","tool_calls":[],"extra_fields":null`
const workBuddyFinalDelta = `"role":"assistant","content":"","reasoning_content":"","function_call":{"name":"","arguments":""},"refusal":"","tool_calls":[],"extra_fields":null`

func workBuddyFrame(delta, finish, usage string) string {
	return fmt.Sprintf(`data: {"id":"wire-synthetic","model":"glm-5.2","object":"chat.completion.chunk","created":1,"choices":[{"index":0,"delta":{%s},"finish_reason":%q}],"usage":%s}`+"\n\n", delta, finish, usage)
}

func TestWorkBuddyEmptyWireFields(t *testing.T) {
	raw := workBuddyFrame(workBuddyEmptyDelta, "", "null") +
		workBuddyFrame(strings.Replace(workBuddyEmptyDelta, `"content":""`, `"content":"WIRE_OK"`, 1), "", "null") +
		workBuddyFrame(workBuddyFinalDelta, "stop", `{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9}`) + reviewDone
	for _, f := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAIResponses} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", f, stream), func(t *testing.T) {
				w, _ := reviewRealHandler(t, f, stream, strings.NewReader(raw), "")
				if !reviewSuccess(w) || !strings.Contains(w.Body.String(), "WIRE_OK") || !strings.Contains(w.Body.String(), `"input_tokens":7`) || !strings.Contains(w.Body.String(), `"output_tokens":2`) {
					t.Fatal(w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestWorkBuddyNonemptyWireFieldsStillFail(t *testing.T) {
	for _, f := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAIResponses} {
		for _, stream := range []bool{false, true} {
			for _, delta := range []string{
				strings.Replace(workBuddyEmptyDelta, `"extra_fields":null`, `"extra_fields":{"x":"value"}`, 1),
				strings.Replace(workBuddyEmptyDelta, `"extra_fields":null`, `"extra_fields":{}`, 1),
				strings.Replace(workBuddyEmptyDelta, `"function_call":null`, `"function_call":{"name":"exec","arguments":""}`, 1),
				strings.Replace(workBuddyEmptyDelta, `"function_call":null`, `"function_call":{"name":"","arguments":"{}"}`, 1),
				strings.Replace(workBuddyEmptyDelta, `"function_call":null`, `"function_call":{"name":"","arguments":"","extra":null}`, 1),
				strings.Replace(workBuddyEmptyDelta, `"refusal":""`, `"refusal":"refused"`, 1),
				workBuddyEmptyDelta + `,"unrecognized":null`,
			} {
				raw := workBuddyFrame(delta, "", "null") + workBuddyFrame(workBuddyFinalDelta, "stop", "null") + reviewDone
				w, _ := reviewRealHandler(t, f, stream, strings.NewReader(raw), "")
				if reviewSuccess(w) {
					t.Fatal(f, stream, delta, w.Body.String())
				}
			}
			for _, finish := range []string{"", " ", "unknown"} {
				raw := workBuddyFrame(workBuddyEmptyDelta, "", "null") + workBuddyFrame(workBuddyFinalDelta, finish, "null") + reviewDone
				w, _ := reviewRealHandler(t, f, stream, strings.NewReader(raw), "")
				if reviewSuccess(w) {
					t.Fatal(f, stream, finish, w.Body.String())
				}
			}
		}
	}
}

func TestWorkBuddyToolWireRoundTrip(t *testing.T) {
	toolDelta := func(tool string) string {
		return strings.Replace(workBuddyEmptyDelta, `"tool_calls":[]`, `"tool_calls":[`+tool+`]`, 1)
	}
	raw := workBuddyFrame(toolDelta(`{"id":"call_wire","type":"function","function":{"name":"probe_echo","arguments":""},"index":0}`), "", "null")
	for _, fragment := range []string{`{`, `"value"`, `:`, `"wire_value"`, `}`} {
		args, _ := json.Marshal(fragment)
		raw += workBuddyFrame(toolDelta(`{"function":{"name":"","arguments":`+string(args)+`},"index":0}`), "", "null")
	}
	raw += workBuddyFrame(workBuddyFinalDelta, "tool_calls", `{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9}`) + reviewDone
	for _, f := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAIResponses} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", f, stream), func(t *testing.T) {
				w, _ := reviewRealHandler(t, f, stream, strings.NewReader(raw), "")
				if w.Code != 200 || strings.Contains(w.Body.String(), `"type":"error"`) {
					t.Fatal(w.Code, w.Body.String())
				}
				var result map[string]any
				var claudeTool map[string]any
				var arguments strings.Builder
				if !stream {
					if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
				} else {
					for _, line := range strings.Split(w.Body.String(), "\n") {
						if !strings.HasPrefix(line, "data: ") {
							continue
						}
						var event map[string]any
						if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
							t.Fatal(err)
						}
						switch event["type"] {
						case "response.completed":
							result = event["response"].(map[string]any)
						case "content_block_start":
							block := event["content_block"].(map[string]any)
							if block["type"] == "tool_use" {
								claudeTool = block
							}
						case "content_block_delta":
							delta := event["delta"].(map[string]any)
							if delta["type"] == "input_json_delta" {
								arguments.WriteString(delta["partial_json"].(string))
							}
						}
					}
				}
				var history map[string]any
				if f == types.RelayFormatClaude {
					if stream {
						if !strings.Contains(w.Body.String(), "event: message_stop") || claudeTool == nil {
							t.Fatal(w.Body.String())
						}
						var input map[string]any
						if err := json.Unmarshal([]byte(arguments.String()), &input); err != nil {
							t.Fatal(err)
						}
						claudeTool["input"] = input
					} else {
						for _, item := range result["content"].([]any) {
							block := item.(map[string]any)
							if block["type"] == "tool_use" {
								claudeTool = block
							}
						}
					}
					if claudeTool == nil || claudeTool["id"] != "call_wire" || claudeTool["name"] != "probe_echo" {
						t.Fatal(claudeTool)
					}
					if claudeTool["input"].(map[string]any)["value"] != "wire_value" {
						t.Fatal(claudeTool)
					}
					history = map[string]any{"model": "glm-5.2", "max_tokens": 32, "messages": []any{
						map[string]any{"role": "user", "content": "hi"}, map[string]any{"role": "assistant", "content": []any{claudeTool}},
						map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": claudeTool["id"], "content": "wire_result"}}}}}
				} else {
					if result == nil || result["status"] != "completed" {
						t.Fatal(w.Body.String())
					}
					var call map[string]any
					for _, item := range result["output"].([]any) {
						block := item.(map[string]any)
						if block["type"] == "function_call" {
							call = block
						}
					}
					if call == nil || call["call_id"] != "call_wire" || call["name"] != "probe_echo" || call["arguments"] != `{"value":"wire_value"}` {
						t.Fatal(call)
					}
					history = map[string]any{"model": "glm-5.2", "input": []any{map[string]any{"role": "user", "content": "hi"}, call,
						map[string]any{"type": "function_call_output", "call_id": call["call_id"], "output": "wire_result"}}}
				}
				encoded, _ := json.Marshal(history)
				followup, sent := reviewRealHandler(t, f, false, strings.NewReader(reviewText+reviewFinish+reviewDone), string(encoded))
				if followup.Code != 200 {
					t.Fatal(followup.Code, followup.Body.String())
				}
				messages := sent["messages"].([]any)
				assistant := messages[1].(map[string]any)
				tool := messages[2].(map[string]any)
				call := assistant["tool_calls"].([]any)[0].(map[string]any)
				fn := call["function"].(map[string]any)
				if call["id"] != "call_wire" || fn["name"] != "probe_echo" || fn["arguments"] != `{"value":"wire_value"}` || tool["tool_call_id"] != "call_wire" || tool["content"] != "wire_result" {
					t.Fatal(sent)
				}
			})
		}
	}
}
