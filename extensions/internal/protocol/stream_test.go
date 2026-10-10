package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
)

const textFrame = `data: {"id":"chat-test","model":"test","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}` + "\n\n"
const finishFrame = `data: {"id":"chat-test","model":"test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n"
const doneFrame = "data: [DONE]\n\n"

func terminal(body string) bool {
	return strings.Contains(body, "event: response.completed") || strings.Contains(body, "event: response.incomplete") || strings.Contains(body, "event: message_stop")
}
func events(t *testing.T, body string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data: ") {
			var m map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &m); err != nil {
				t.Fatal(err)
			}
			out = append(out, m)
		}
	}
	return out
}
func finalUsage(t *testing.T, body string) map[string]any {
	t.Helper()
	for _, e := range events(t, body) {
		if e["type"] == "message_delta" {
			return e["usage"].(map[string]any)
		}
		if e["type"] == "response.completed" || e["type"] == "response.incomplete" {
			return e["response"].(map[string]any)["usage"].(map[string]any)
		}
	}
	t.Fatalf("no final usage: %s", body)
	return nil
}
func TestFirstDeltaIsImmediate(t *testing.T) {
	for _, f := range formats {
		t.Run(string(f), func(t *testing.T) {
			dst := httptest.NewRecorder()
			Serve(dst, httptest.NewRequest("POST", "/", strings.NewReader(requestBody(f, true))), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, textFrame)
				if !strings.Contains(dst.Body.String(), "delta") || !strings.Contains(dst.Body.String(), "hello") || !dst.Flushed {
					t.Error("first text not flushed before handler returns")
				}
				io.WriteString(w, finishFrame)
			}), "key", 1<<20, f, "")
			if !terminal(dst.Body.String()) {
				t.Errorf("no terminal: %s", dst.Body.String())
			}
			if f == types.RelayFormatOpenAIResponses {
				for i, e := range events(t, dst.Body.String()) {
					if e["sequence_number"] != float64(i) {
						t.Errorf("sequence %d: %v", i, e["sequence_number"])
					}
				}
			}
		})
	}
}
func TestTerminalWaitsForLateUsage(t *testing.T) {
	for _, f := range formats {
		dst := httptest.NewRecorder()
		Serve(dst, httptest.NewRequest("POST", "/", strings.NewReader(requestBody(f, true))), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, textFrame+finishFrame)
			if terminal(dst.Body.String()) {
				t.Error("premature terminal")
			}
			io.WriteString(w, `data: {"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2}}`+"\n\n")
			if terminal(dst.Body.String()) {
				t.Error("usage caused premature terminal")
			}
			io.WriteString(w, doneFrame)
		}), "key", 1<<20, f, "")
		u := finalUsage(t, dst.Body.String())
		if u["input_tokens"] != float64(3) || u["output_tokens"] != float64(2) {
			t.Errorf("usage %v", u)
		}
	}
}
func TestLateZeroUsageWins(t *testing.T) {
	for _, f := range formats {
		w := run(f, true, func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, textFrame+`data: {"choices":[],"usage":{"prompt_tokens":9,"completion_tokens":7,"total_tokens":16}}`+"\n\n"+finishFrame+`data: {"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":2,"total_tokens":0}}`+"\n\n"+doneFrame)
		})
		u := finalUsage(t, w.Body.String())
		if u["input_tokens"] != float64(0) || u["output_tokens"] != float64(2) || (f == types.RelayFormatOpenAIResponses && u["total_tokens"] != float64(0)) {
			t.Errorf("late zero lost: %v", u)
		}
	}
}
func TestPartialUsagePreservesObservedFields(t *testing.T) {
	for _, f := range formats {
		w := run(f, true, func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, textFrame+`data: {"choices":[],"usage":{"prompt_tokens":9,"completion_tokens":7}}`+"\n\n"+finishFrame+`data: {"choices":[],"usage":{"completion_tokens":2}}`+"\n\n")
		})
		u := finalUsage(t, w.Body.String())
		if u["input_tokens"] != float64(9) || u["output_tokens"] != float64(2) || (f == types.RelayFormatOpenAIResponses && u["total_tokens"] != float64(11)) {
			t.Errorf("partial usage lost: %v", u)
		}
	}
}
func TestStreamUnknownUsageRemainsNull(t *testing.T) {
	for _, f := range formats {
		w := run(f, true, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, textFrame+finishFrame) })
		u := finalUsage(t, w.Body.String())
		if v, ok := u["input_tokens"]; !ok || v != nil {
			t.Errorf("input %v", u)
		}
		if v, ok := u["output_tokens"]; !ok || v != nil {
			t.Errorf("output %v", u)
		}
	}
}
func TestTruncatedStreamCannotComplete(t *testing.T) {
	for _, f := range formats {
		for name, body := range map[string]string{
			"DONE without finish": textFrame + doneFrame, "no finish": textFrame, "residual": textFrame + finishFrame + "data: {", "error": textFrame + `data: {"error":{"message":"secret-token"}}` + "\n\n", "choice after finish": textFrame + finishFrame + textFrame, "data after DONE": textFrame + finishFrame + doneFrame + `data: {"choices":[],"usage":{"completion_tokens":2}}` + "\n\n", "bad delta": `data: {"choices":[{"delta":null,"finish_reason":"stop"}]}` + "\n\n", "bad choice": `data: {"choices":[null]}` + "\n\n", "negative usage": textFrame + `data: {"choices":[],"usage":{"prompt_tokens":-1}}` + "\n\n", "event error": textFrame + "event: error\ndata: secret-token\n\n",
		} {
			t.Run(string(f)+name, func(t *testing.T) {
				w := run(f, true, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
				if terminal(w.Body.String()) || !strings.Contains(w.Body.String(), "error") || strings.Contains(w.Body.String(), "secret-token") {
					t.Errorf("invalid stream: %d %s", w.Code, w.Body.String())
				}
			})
		}
	}
}
func TestIncompleteFinishReasons(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, reason := range []string{"length", "content_filter", "unknown"} {
			w := run(types.RelayFormatOpenAIResponses, stream, func(w http.ResponseWriter, r *http.Request) {
				body := goodResponse
				if stream {
					body = textFrame + finishFrame
				}
				io.WriteString(w, strings.Replace(body, `"stop"`, `"`+reason+`"`, 1))
			})
			if reason == "unknown" {
				if terminal(w.Body.String()) || !strings.Contains(w.Body.String(), "error") {
					t.Error("unknown reason accepted")
				}
			} else if !strings.Contains(w.Body.String(), `"status":"incomplete"`) {
				t.Errorf("%s stream=%v: %s", reason, stream, w.Body.String())
			}
		}
	}
}

type failedWriter struct {
	*httptest.ResponseRecorder
	mode string
}

func (w failedWriter) Write(p []byte) (int, error) {
	switch w.mode {
	case "short":
		return len(p) - 1, nil
	case "write":
		return 0, io.ErrClosedPipe
	}
	return w.ResponseRecorder.Write(p)
}
func (w failedWriter) FlushError() error {
	if w.mode == "flush" {
		return errors.New("flush failed")
	}
	return nil
}
func TestWriteAndFlushFailureCancelNext(t *testing.T) {
	for _, f := range formats {
		for _, mode := range []string{"short", "write", "flush"} {
			dst := failedWriter{httptest.NewRecorder(), mode}
			Serve(dst, httptest.NewRequest("POST", "/", strings.NewReader(requestBody(f, true))), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, err := io.WriteString(w, textFrame)
				if err == nil || r.Context().Err() == nil {
					t.Errorf("%s %s did not cancel", f, mode)
				}
				io.WriteString(w, finishFrame)
			}), "key", 1<<20, f, "")
			if terminal(dst.Body.String()) {
				t.Error("success after write failure")
			}
		}
	}
}
func TestStreamTotalLimit(t *testing.T) {
	for _, f := range formats {
		for _, frame := range []bool{true, false} {
			w := run(f, true, func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, textFrame)
				var err error
				if frame {
					_, err = io.WriteString(w, "data: "+strings.Repeat("x", 8<<20))
				} else {
					small := ":" + strings.Repeat("x", 1021) + "\n\n"
					for i := 0; i < 32769 && err == nil; i++ {
						_, err = io.WriteString(w, small)
					}
				}
				if err == nil || r.Context().Err() == nil {
					t.Errorf("frame=%v limit did not cancel", frame)
				}
				io.WriteString(w, finishFrame)
			})
			if terminal(w.Body.String()) {
				t.Error("success after capacity failure")
			}
		}
	}
}
func TestMixedClaudeDeltaPreservesAllParts(t *testing.T) {
	w := run(types.RelayFormatClaude, true, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `data: {"id":"chat-test","model":"test","choices":[{"index":0,"delta":{"reasoning_content":"think","content":"text","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"f","arguments":"{}"}}]}}]}`+"\n\n"+strings.Replace(finishFrame, `"stop"`, `"tool_calls"`, 1))
	})
	s := w.Body.String()
	for _, part := range []string{"thinking_delta", "text_delta", "input_json_delta", "message_stop"} {
		if !strings.Contains(s, part) {
			t.Errorf("lost %s: %s", part, s)
		}
	}
}
func TestCanceledStreamCannotComplete(t *testing.T) {
	for _, f := range formats {
		ctx, cancel := context.WithCancel(context.Background())
		dst := httptest.NewRecorder()
		Serve(dst, httptest.NewRequest("POST", "/", strings.NewReader(requestBody(f, true))).WithContext(ctx), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, textFrame+finishFrame)
			cancel()
			if r.Context().Err() == nil {
				t.Error("cancellation lost")
			}
		}), "key", 1<<20, f, "")
		cancel()
		if terminal(dst.Body.String()) {
			t.Error("canceled stream completed")
		}
	}
}

func TestResponsesErrorSequence(t *testing.T) {
	w := run(types.RelayFormatOpenAIResponses, true, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, textFrame+doneFrame) })
	for i, e := range events(t, w.Body.String()) {
		if e["sequence_number"] != float64(i) {
			t.Errorf("event %v lacks sequence %d", e["type"], i)
		}
	}
}
