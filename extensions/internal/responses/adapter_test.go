package responses

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const completion = `{"id":"chat-test","model":"test","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`

func invoke(h http.Handler, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer key")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decode(t *testing.T, r io.Reader) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.NewDecoder(r).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRouteAndRequestMapping(t *testing.T) {
	body := `{"model":"cn:model","input":[{"role":"user","content":[{"type":"input_text","text":"look"},{"type":"input_image","image_url":"https://example.test/image.png"}]}],"max_output_tokens":17,"tools":[{"type":"function","name":"weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}},"strict":true}],"parallel_tool_calls":true,"text":{"format":{"type":"json_schema","name":"answer","schema":{"type":"object","properties":{"answer":{"type":"string"}}},"strict":true}}}`
	called := false
	h := New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("wrong route: %s %v", r.URL, r.Header)
		}
		v := decode(t, r.Body)
		if v["model"] != "cn:model" || v["max_tokens"] != float64(17) || v["parallel_tool_calls"] != true {
			t.Errorf("lost controls: %v", v)
		}
		parts := v["messages"].([]any)[0].(map[string]any)["content"].([]any)
		if parts[0].(map[string]any)["text"] != "look" || parts[1].(map[string]any)["type"] != "image_url" || parts[1].(map[string]any)["image_url"] != "https://example.test/image.png" {
			t.Errorf("lost media: %v", parts)
		}
		tool := v["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
		if tool["name"] != "weather" || tool["parameters"].(map[string]any)["type"] != "object" || tool["strict"] != true {
			t.Errorf("lost tool: %v", tool)
		}
		format := v["response_format"].(map[string]any)
		if format["type"] != "json_schema" || format["json_schema"].(map[string]any)["strict"] != true {
			t.Errorf("lost schema: %v", format)
		}
		io.WriteString(w, completion)
	}), "key", 1<<20)
	w := invoke(h, body)
	if !called || w.Code != 200 || decode(t, w.Body)["object"] != "response" {
		t.Fatalf("called=%v response=%d %s", called, w.Code, w.Body)
	}
	for _, path := range []string{"/v1/models", "/v1/chat/completions", "/v1/responses/resp_1"} {
		r := httptest.NewRequest("GET", path+"?raw=1", nil)
		r.Header.Set("X-Test", "preserved")
		w = httptest.NewRecorder()
		New(http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
			if got != r {
				t.Error("passthrough request cloned")
			}
			w.WriteHeader(418)
		}), "key", 1).ServeHTTP(w, r)
		if w.Code != 418 {
			t.Errorf("passthrough %s: %d", path, w.Code)
		}
	}
}

func TestRouteBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, encoding string
		auth                         []string
		limit                        int64
		status                       int
	}{
		{"method", "GET", "/v1/responses", "", nil, 1024, 405},
		{"missing", "POST", "/v1/responses", "", nil, 1024, 401},
		{"wrong", "POST", "/v1/responses", "", []string{"Bearer wrong"}, 1024, 401},
		{"duplicate", "POST", "/v1/responses", "", []string{"Bearer key", "Bearer key"}, 1024, 401},
		{"joined", "POST", "/v1/responses", "", []string{"Bearer key, Bearer key"}, 1024, 401},
		{"query", "POST", "/v1/responses?x=1", "", []string{"Bearer key"}, 1024, 400},
		{"empty query", "POST", "/v1/responses?", "", []string{"Bearer key"}, 1024, 400},
		{"encoding", "POST", "/v1/responses", "gzip", []string{"Bearer key"}, 1024, 400},
		{"limit", "POST", "/v1/responses", "", []string{"Bearer key"}, 3, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"model":"test","input":"hi"}`))
			for _, auth := range tc.auth {
				r.Header.Add("Authorization", auth)
			}
			if tc.encoding != "" {
				r.Header.Set("Content-Encoding", tc.encoding)
			}
			w := httptest.NewRecorder()
			New(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("rejected request reached next") }), "key", tc.limit).ServeHTTP(w, r)
			if w.Code != tc.status || decode(t, w.Body)["error"] == nil {
				t.Errorf("status=%d want=%d", w.Code, tc.status)
			}
			if tc.status == 405 && w.Header().Get("Allow") != "POST" {
				t.Error("missing Allow")
			}
		})
	}
}

func TestToolHistoryPreservesParallelCalls(t *testing.T) {
	body := `{"model":"test","input":[{"type":"function_call","call_id":"call_a","name":"first","arguments":"{\"a\":1}"},{"type":"function_call","call_id":"call_b","name":"second","arguments":"{\"b\":2}"},{"type":"function_call_output","call_id":"call_a","output":"one"},{"type":"function_call_output","call_id":"call_b","output":"two"}]}`
	w := invoke(New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		messages := decode(t, r.Body)["messages"].([]any)
		if len(messages) != 3 {
			t.Fatalf("messages=%v", messages)
		}
		calls := messages[0].(map[string]any)["tool_calls"].([]any)
		for i, want := range []struct{ id, name, args, output string }{{"call_a", "first", `{"a":1}`, "one"}, {"call_b", "second", `{"b":2}`, "two"}} {
			call := calls[i].(map[string]any)
			f := call["function"].(map[string]any)
			result := messages[i+1].(map[string]any)
			if call["id"] != want.id || f["name"] != want.name || f["arguments"] != want.args || result["tool_call_id"] != want.id || result["content"] != want.output {
				t.Errorf("association lost: %v %v", call, result)
			}
		}
		io.WriteString(w, completion)
	}), "key", 1<<20), body)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestConcurrentCustomToolsDoNotLeak(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	h := New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := decode(t, r.Body)
		label := v["messages"].([]any)[0].(map[string]any)["content"].(string)
		toolName := v["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"].(string)
		entered <- struct{}{}
		<-release
		args := label
		if label == "custom" {
			args = `{"input":"custom"}`
		}
		out := map[string]any{"id": "chat-test", "model": "test", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "call_" + label, "type": "function", "function": map[string]any{"name": toolName, "arguments": args}}}}, "finish_reason": "tool_calls"}}}
		json.NewEncoder(w).Encode(out)
	}), "key", 1<<20)
	var wg sync.WaitGroup
	for _, custom := range []bool{true, false} {
		wg.Add(1)
		go func(custom bool) {
			defer wg.Done()
			kind, label := "function", "function"
			if custom {
				kind, label = "custom", "custom"
			}
			w := invoke(h, fmt.Sprintf(`{"model":"test","input":%q,"tools":[{"type":%q,"name":"exec"}]}`, label, kind))
			if w.Code != 200 {
				t.Errorf("%d %s", w.Code, w.Body)
				return
			}
			item := decode(t, w.Body)["output"].([]any)[0].(map[string]any)
			want, key := "function_call", "arguments"
			if custom {
				want, key = "custom_tool_call", "input"
			}
			if item["type"] != want || item["name"] != "exec" || item["call_id"] != "call_"+label || item[key] != label {
				t.Errorf("crossed state: %v", item)
			}
		}(custom)
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			close(release)
			wg.Wait()
			t.Fatal("concurrent requests did not reach the handler")
		}
	}
	close(release)
	wg.Wait()
}

func TestCompleteResponseStatus(t *testing.T) {
	for _, reason := range []string{"stop", "length", "content_filter"} {
		w := invoke(New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, strings.Replace(completion, `"stop"`, fmt.Sprintf("%q", reason), 1))
		}), "key", 1<<20), `{"model":"test","input":"hi"}`)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		v := decode(t, w.Body)
		want := "completed"
		if reason != "stop" {
			want = "incomplete"
		}
		if v["status"] != want || v["output"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"] != "hello" {
			t.Errorf("status/text lost: %v", v)
		}
	}
}

func TestGeneratedPlaintextHistoryRoundtrip(t *testing.T) {
	first := invoke(New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"chat-test","model":"test","choices":[{"index":0,"message":{"role":"assistant","reasoning_content":"think carefully","content":"I will check","tool_calls":[{"id":"call_a","type":"function","function":{"name":"check","arguments":"{\"x\":1}"}}]},"finish_reason":"tool_calls"}]}`)
	}), "key", 1<<20), `{"model":"test","input":"hi"}`)
	if first.Code != 200 {
		t.Fatalf("first=%d %s", first.Code, first.Body)
	}
	history := decode(t, first.Body)["output"].([]any)
	history = append(history, map[string]any{"type": "function_call_output", "call_id": "call_a", "output": "checked"}, map[string]any{"role": "user", "content": "continue"})
	body, _ := json.Marshal(map[string]any{"model": "test", "input": history})
	called := false
	second := invoke(New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		messages := decode(t, r.Body)["messages"].([]any)
		if len(messages) != 3 {
			t.Fatalf("history split/lost: %v", messages)
		}
		assistant := messages[0].(map[string]any)
		if assistant["role"] != "assistant" || assistant["reasoning_content"] != "think carefully" || assistant["content"] != "I will check" {
			t.Errorf("reasoning/text lost: %v", assistant)
		}
		call := assistant["tool_calls"].([]any)[0].(map[string]any)
		if call["id"] != "call_a" || call["function"].(map[string]any)["arguments"] != `{"x":1}` || messages[1].(map[string]any)["tool_call_id"] != "call_a" {
			t.Errorf("tool link lost: %v", messages)
		}
		io.WriteString(w, completion)
	}), "key", 1<<20), string(body))
	if !called || second.Code != 200 {
		t.Errorf("history rejected: called=%v %d %s", called, second.Code, second.Body)
	}
}

func TestGeneratedStreamHistoryKeepsInterleavedAssistantTurn(t *testing.T) {
	parts := map[string]string{
		"call_a":    `{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"first","arguments":"{\"a\":1}"}}]}`,
		"call_b":    `{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"second","arguments":"{\"b\":2}"}}]}`,
		"reasoning": `{"reasoning_content":"think"}`,
		"text":      `{"content":"checking"}`,
	}
	for _, order := range [][]string{
		{"call_a", "reasoning", "call_b", "text"},
		{"call_a", "text", "reasoning", "call_b"},
		{"text", "call_a", "reasoning", "call_b"},
		{"reasoning", "call_a", "text", "call_b"},
		{"call_a", "call_b", "text", "reasoning"},
		{"text", "reasoning", "call_a", "call_b"},
	} {
		t.Run(strings.Join(order, "-"), func(t *testing.T) {
			first := invoke(New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for _, part := range order {
					io.WriteString(w, `data: {"id":"chat-test","model":"test","choices":[{"index":0,"delta":`+parts[part]+`}]}`+"\n\n")
				}
				io.WriteString(w, `data: {"id":"chat-test","model":"test","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
			}), "key", 1<<20), `{"model":"test","input":"hi","stream":true}`)
			var history []any
			for _, line := range strings.Split(first.Body.String(), "\n") {
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				v := decode(t, strings.NewReader(strings.TrimPrefix(line, "data: ")))
				if v["type"] == "response.completed" {
					history = v["response"].(map[string]any)["output"].([]any)
				}
			}
			if len(history) != 4 {
				t.Fatalf("missing generated history: %s", first.Body)
			}
			for i, part := range order {
				item := history[i].(map[string]any)
				want := "function_call"
				if part == "reasoning" {
					want = "reasoning"
				}
				if part == "text" {
					want = "message"
				}
				if item["type"] != want || (want == "function_call" && item["call_id"] != part) {
					t.Fatalf("generated order changed: %v", history)
				}
			}
			// Resubmit the generated objects unchanged, including serializer fields and order.
			history = append(history, map[string]any{"type": "function_call_output", "call_id": "call_a", "output": "one"}, map[string]any{"type": "function_call_output", "call_id": "call_b", "output": "two"})
			body, _ := json.Marshal(map[string]any{"model": "test", "input": history})
			called := false
			second := invoke(New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				messages := decode(t, r.Body)["messages"].([]any)
				if len(messages) != 3 {
					t.Fatalf("one assistant turn split: %v", messages)
				}
				assistant := messages[0].(map[string]any)
				if assistant["role"] != "assistant" || assistant["content"] != "checking" || assistant["reasoning_content"] != "think" {
					t.Errorf("text/reasoning lost: %v", assistant)
				}
				calls, ok := assistant["tool_calls"].([]any)
				if !ok || len(calls) != 2 {
					t.Fatalf("parallel calls split: %v", messages)
				}
				for i, want := range []struct{ id, name, args, result string }{{"call_a", "first", `{"a":1}`, "one"}, {"call_b", "second", `{"b":2}`, "two"}} {
					call := calls[i].(map[string]any)
					function := call["function"].(map[string]any)
					result := messages[i+1].(map[string]any)
					if call["id"] != want.id || function["name"] != want.name || function["arguments"] != want.args || result["role"] != "tool" || result["tool_call_id"] != want.id || result["content"] != want.result {
						t.Errorf("tool batch/result lost: %v", messages)
					}
				}
				io.WriteString(w, completion)
			}), "key", 1<<20), string(body))
			if !called || second.Code != 200 {
				t.Errorf("history rejected: called=%v %d %s", called, second.Code, second.Body)
			}
		})
	}
}

func TestUnsupportedStateRejectedBeforeNext(t *testing.T) {
	for _, tail := range []string{`"store":true`, `"background":true`, `"previous_response_id":"r"`, `"conversation":null`, `"prompt":{}`, `"context_management":[]`} {
		w := invoke(New(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("state reached next") }), "key", 1<<20), `{"model":"test","input":"hi",`+tail+`}`)
		if w.Code != 400 {
			t.Errorf("%s: %d %s", tail, w.Code, w.Body)
		}
	}
	w := invoke(New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, completion) }), "key", 1<<20), `{"model":"test","input":"hi","store":false,"background":false}`)
	if w.Code != 200 {
		t.Errorf("false rejected: %d %s", w.Code, w.Body)
	}
}

func TestStreamAggregatesTextReasoningAndParallelTools(t *testing.T) {
	for _, reason := range []string{"tool_calls", "length"} {
		t.Run(reason, func(t *testing.T) {
			dst := httptest.NewRecorder()
			h := New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				frames := []string{
					`{"id":"chat-test","model":"test","choices":[{"index":0,"delta":{"reasoning_content":"think"}}]}`,
					`{"id":"chat-test","choices":[{"index":0,"delta":{"content":"hello"}}]}`,
					`{"id":"chat-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"first","arguments":"{\"a\":"}},{"index":1,"id":"call_b","type":"function","function":{"name":"second","arguments":"{\"b\":"}}]}}]}`,
					`{"id":"chat-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}},{"index":1,"function":{"arguments":"2}"}}]}}]}`,
				}
				for _, frame := range frames {
					io.WriteString(w, "data: "+frame+"\n\n")
				}
				if !dst.Flushed || !strings.Contains(dst.Body.String(), "delta") {
					t.Error("content not immediate")
				}
				io.WriteString(w, fmt.Sprintf(`data: {"id":"chat-test","choices":[{"index":0,"delta":{},"finish_reason":%q}]}`, reason)+"\n\n")
				if strings.Contains(dst.Body.String(), "event: response.completed") || strings.Contains(dst.Body.String(), "event: response.incomplete") {
					t.Error("premature terminal")
				}
				io.WriteString(w, "data: [DONE]\n\n")
			}), "key", 1<<20)
			r := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"test","input":"hi","stream":true}`))
			r.Header.Set("Authorization", "Bearer key")
			h.ServeHTTP(dst, r)
			added, done := map[float64]any{}, map[float64]any{}
			var final map[string]any
			seq, terminals := 0, 0
			var text, reasoning strings.Builder
			args := map[float64]string{}
			for _, line := range strings.Split(dst.Body.String(), "\n") {
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				v := decode(t, strings.NewReader(strings.TrimPrefix(line, "data: ")))
				if v["sequence_number"] != float64(seq) {
					t.Errorf("sequence=%v want=%d", v, seq)
				}
				seq++
				switch v["type"] {
				case "response.output_item.added":
					added[v["output_index"].(float64)] = v["item"]
				case "response.output_item.done":
					done[v["output_index"].(float64)] = v["item"]
				case "response.output_text.delta":
					text.WriteString(v["delta"].(string))
				case "response.reasoning_summary_text.delta":
					reasoning.WriteString(v["delta"].(string))
				case "response.function_call_arguments.delta":
					args[v["output_index"].(float64)] += v["delta"].(string)
				case "response.completed", "response.incomplete":
					terminals++
					final = v["response"].(map[string]any)
				case "error":
					t.Errorf("stream error: %v", v)
				}
			}
			if final == nil {
				t.Fatalf("no final: %s", dst.Body)
			}
			want := "completed"
			if reason == "length" {
				want = "incomplete"
			}
			if terminals != 1 || final["status"] != want {
				t.Errorf("wrong terminal: %v", final)
			}
			if text.String() != "hello" || reasoning.String() != "think" {
				t.Errorf("lost deltas: %s %s", text.String(), reasoning.String())
			}
			output := final["output"].([]any)
			if len(output) != 4 || len(added) != 4 || len(done) != 4 {
				t.Fatalf("output count: %v added=%v done=%v", output, added, done)
			}
			for i, item := range output {
				if !reflect.DeepEqual(done[float64(i)], item) {
					t.Errorf("final differs at %d: %v vs %v", i, item, done[float64(i)])
				}
			}
			calls := []map[string]any{}
			for i, raw := range output {
				item := raw.(map[string]any)
				if item["type"] == "function_call" {
					calls = append(calls, item)
					if args[float64(i)] != item["arguments"] {
						t.Errorf("argument aggregation lost: %v", item)
					}
				}
			}
			if len(calls) != 2 || calls[0]["call_id"] != "call_a" || calls[0]["name"] != "first" || calls[0]["arguments"] != `{"a":1}` || calls[1]["call_id"] != "call_b" || calls[1]["name"] != "second" || calls[1]["arguments"] != `{"b":2}` {
				t.Errorf("parallel output lost: %v", calls)
			}
		})
	}
}
