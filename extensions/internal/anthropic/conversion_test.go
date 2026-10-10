package anthropic

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func convertFixture(t *testing.T, body, output string) (map[string]any, map[string]any) {
	t.Helper()
	var chat map[string]any
	w := httptest.NewRecorder()
	New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&chat); err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, output)
	}), "fixture-api", 0).ServeHTTP(w, request(body))
	if w.Code != 200 || chat == nil {
		t.Fatalf("conversion status=%d body=%s", w.Code, w.Body)
	}
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return chat, result
}

func TestToolDefinitionsCallsAndResults(t *testing.T) {
	body := `{"model":"global:mock","max_tokens":32,"tools":[{"name":"check","description":"check status","input_schema":{"type":"object","properties":{"q":{"type":"string"}}}}],"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":[{"type":"text","text":"checking"},{"type":"tool_use","id":"call_a","name":"check","input":{"q":"hi"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_a","content":"checked"}]}]}`
	output := `{"id":"chat_a","choices":[{"message":{"role":"assistant","content":"again","tool_calls":[{"id":"call_b","type":"function","function":{"name":"check","arguments":"{\"q\":\"next\"}"}}]},"finish_reason":"tool_calls"}]}`
	chat, result := convertFixture(t, body, output)
	tools := chat["tools"].([]any)
	if tools[0].(map[string]any)["function"].(map[string]any)["name"] != "check" {
		t.Fatalf("tools=%v", tools)
	}
	msgs := chat["messages"].([]any)
	assistant := msgs[1].(map[string]any)
	if !strings.Contains(string(mustJSON(t, assistant["content"])), "checking") {
		t.Fatalf("text lost: %v", assistant)
	}
	call := assistant["tool_calls"].([]any)[0].(map[string]any)
	if call["id"] != "call_a" || call["function"].(map[string]any)["arguments"] != `{"q":"hi"}` {
		t.Fatalf("call=%v", call)
	}
	tool := msgs[2].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != "call_a" || tool["content"] != "checked" {
		t.Fatalf("result link=%v", tool)
	}
	if result["stop_reason"] != "tool_use" || !strings.Contains(string(mustJSON(t, result["content"])), `"id":"call_b"`) {
		t.Fatalf("output=%v", result)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestImagesAndGenerationParameters(t *testing.T) {
	body := `{"model":"global:mock","max_tokens":32,"temperature":0.5,"top_p":0.8,"top_k":4,"stop_sequences":["END"],"messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGk="}},{"type":"image","source":{"type":"url","url":"https://example.com/image.png"}}]}]}`
	chat, _ := convertFixture(t, body, textResponse)
	for key, want := range map[string]any{"temperature": float64(0.5), "top_p": float64(0.8), "top_k": float64(4), "stop": "END", "max_tokens": float64(32)} {
		if chat[key] != want {
			t.Fatalf("%s=%v", key, chat[key])
		}
	}
	content := chat["messages"].([]any)[0].(map[string]any)["content"].([]any)
	for i, want := range []string{"data:image/png;base64,aGk=", "https://example.com/image.png"} {
		if content[i+1].(map[string]any)["image_url"].(map[string]any)["url"] != want {
			t.Fatalf("images=%v", content)
		}
	}
}

func TestThinkingOutput(t *testing.T) {
	body := `{"model":"global:mock","max_tokens":4096,"thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"previous thought"},{"type":"text","text":"previous answer"}]},{"role":"user","content":"continue"}]}`
	output := `{"id":"chat_a","choices":[{"message":{"role":"assistant","content":"answer","reasoning_content":"new thought"},"finish_reason":"stop"}]}`
	chat, result := convertFixture(t, body, output)
	if chat["reasoning_effort"] == nil || chat["messages"].([]any)[0].(map[string]any)["reasoning_content"] != "previous thought" {
		t.Fatalf("thinking intent/history=%v", chat)
	}
	blocks := result["content"].([]any)
	if blocks[0].(map[string]any)["type"] != "thinking" || blocks[0].(map[string]any)["thinking"] != "new thought" {
		t.Fatalf("thinking output=%v", result)
	}
	if strings.Contains(string(mustJSON(t, result)), `"signature"`) {
		t.Fatal("signature fabricated")
	}
}

func TestToolStreamKeepsLateZeroUsage(t *testing.T) {
	frames := "data: " + `{"id":"chat_a","choices":[{"index":0,"delta":{"reasoning_content":"think","content":"checking","tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"check","arguments":"{"}}]}}],"usage":{"prompt_tokens":4,"completion_tokens":2}}` + "\n\n" +
		"data: " + `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: " + `{"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0}}` + "\n\n" + doneFrame
	w := httptest.NewRecorder()
	New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, frames) }), "fixture-api", 0).ServeHTTP(w, request(streamRequest))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	got := events(t, w.Body.String())
	last := got[len(got)-2]
	if got[len(got)-1]["type"] != "message_stop" || last["usage"].(map[string]any)["input_tokens"] != float64(0) || last["usage"].(map[string]any)["output_tokens"] != float64(0) || last["delta"].(map[string]any)["stop_reason"] != "tool_use" {
		t.Fatalf("final=%v", got)
	}
	for _, fragment := range []string{`"thinking":"think"`, `"text":"checking"`, `"id":"call_a"`, `"partial_json":"{"`, `"partial_json":"}"`} {
		if !strings.Contains(w.Body.String(), fragment) {
			t.Fatalf("missing %s: %s", fragment, w.Body)
		}
	}
}
