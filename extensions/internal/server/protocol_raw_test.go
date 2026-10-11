package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"workbuddy2api/internal/auth"
	"workbuddy2api/internal/protocol"
	"workbuddy2api/internal/upstream"
)

const reviewText = `data: {"id":"chat-review","model":"glm-5.2","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}` + "\n\n"
const reviewFinish = `data: {"id":"chat-review","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n"
const reviewDone = "data: [DONE]\n\n"

type reviewReadFailure struct{}

func (reviewReadFailure) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type reviewCountingReader struct {
	io.Reader
	n      int
	closed bool
}

func (r *reviewCountingReader) Close() error { r.closed = true; return nil }
func (r *reviewCountingReader) Read(p []byte) (int, error) {
	n, e := r.Reader.Read(p)
	r.n += n
	return n, e
}

func reviewRealHandler(t *testing.T, format types.RelayFormat, stream bool, body io.Reader, input string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var sent map[string]any
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Fatal(err)
		}
		rc, ok := body.(io.ReadCloser)
		if !ok {
			rc = io.NopCloser(body)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: rc}, nil
	})
	client := &http.Client{Transport: transport}
	up := &upstream.Client{HTTP: client, ChatHTTP: client, ChatBaseCN: "https://fixture.invalid", BillingBaseCN: "https://fixture.invalid"}
	p := testPoolWith(&auth.Auth{UID: "review-synthetic", AccessToken: "fixture-token", ExpiresAt: 9999999999})
	next := NewHandler(Config{Pool: p, Upstream: up, APIKey: "fixture-api"})
	if input == "" {
		if format == types.RelayFormatClaude {
			input = fmt.Sprintf(`{"model":"glm-5.2","max_tokens":32,"stream":%t,"messages":[{"role":"user","content":"hi"}]}`, stream)
		} else {
			input = fmt.Sprintf(`{"model":"glm-5.2","stream":%t,"input":"hi"}`, stream)
		}
	}
	w := httptest.NewRecorder()
	protocol.Serve(w, httptest.NewRequest("POST", "/", strings.NewReader(input)), next, "fixture-api", 1<<20, format, "")
	return w, sent
}

func reviewSuccess(w *httptest.ResponseRecorder) bool {
	s := w.Body.String()
	return w.Code == 200 && (strings.Contains(s, `"status":"completed"`) || strings.Contains(s, `"stop_reason":"end_turn"`) || strings.Contains(s, "event: message_stop"))
}

func TestProtocolRawRealTerminal(t *testing.T) {
	for _, f := range []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatClaude} {
		for _, tc := range []struct {
			name   string
			stream bool
			source func() io.Reader
		}{
			{"ordinary_missing_finish", false, func() io.Reader { return strings.NewReader(reviewText + reviewDone) }},
			{"ordinary_bad_data_after_finish", false, func() io.Reader {
				return strings.NewReader(reviewText + reviewFinish + "data: {broken\n\n" + reviewDone)
			}},
			{"stream_data_after_DONE", true, func() io.Reader { return strings.NewReader(reviewText + reviewFinish + reviewDone + reviewText) }},
			{"stream_read_error_after_finish", true, func() io.Reader {
				return io.MultiReader(strings.NewReader(reviewText+reviewFinish), reviewReadFailure{})
			}},
		} {
			t.Run(string(f)+"/"+tc.name, func(t *testing.T) {
				w, _ := reviewRealHandler(t, f, tc.stream, tc.source(), "")
				if reviewSuccess(w) {
					t.Errorf("false success: status=%d body=%s", w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestProtocolRawRealPartialUsage(t *testing.T) {
	raw := reviewText + `data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}` + "\n\n" + `data: {"choices":[],"usage":{"completion_tokens":0}}` + "\n\n" + reviewDone
	w, _ := reviewRealHandler(t, types.RelayFormatOpenAIResponses, false, strings.NewReader(raw), "")
	var obj map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &obj)
	usage, _ := obj["usage"].(map[string]any)
	if usage["input_tokens"] != float64(5) || usage["output_tokens"] != float64(0) || usage["total_tokens"] != float64(12) {
		t.Errorf("explicit input/total lost: %s", w.Body.String())
	}
}

func TestProtocolRawRealRawBounds(t *testing.T) {
	t.Run("ordinary_total", func(t *testing.T) {
		raw := reviewText + strings.Repeat(":"+strings.Repeat("x", 1022)+"\n\n", 33000) + reviewFinish + reviewDone
		counted := &reviewCountingReader{Reader: strings.NewReader(raw)}
		w, _ := reviewRealHandler(t, types.RelayFormatOpenAIResponses, false, counted, "")
		if reviewSuccess(w) || counted.n > 32<<20+65536 {
			t.Errorf("raw cumulative bound bypassed: bytes_read=%d status=%d body=%s", counted.n, w.Code, w.Body.String())
		}
	})
	t.Run("stream_unterminated_frame", func(t *testing.T) {
		counted := &reviewCountingReader{Reader: strings.NewReader("data: " + strings.Repeat("x", 9<<20))}
		w, _ := reviewRealHandler(t, types.RelayFormatOpenAIResponses, true, counted, "")
		if counted.n > 8<<20+65536 {
			t.Errorf("frame allocated/read before bound: bytes_read=%d status=%d", counted.n, w.Code)
		}
	})
}

func TestProtocolRawClaudeStreamArguments(t *testing.T) {
	for _, args := range []string{"", `{"q":`, `[]`} {
		t.Run(args, func(t *testing.T) {
			a, _ := json.Marshal(args)
			raw := `data: {"id":"chat-review","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"check","arguments":` + string(a) + `}}]}}]}` + "\n\n" + strings.Replace(reviewFinish, `"stop"`, `"tool_calls"`, 1) + reviewDone
			w, _ := reviewRealHandler(t, types.RelayFormatClaude, true, strings.NewReader(raw), "")
			if strings.Contains(w.Body.String(), "event: message_stop") {
				t.Errorf("invalid object arguments completed: %s", w.Body.String())
			}
		})
	}
}

func TestProtocolRawClaudeInterleavedTools(t *testing.T) {
	raw := `data: {"id":"chat-review","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"check","arguments":"{\"q\":"}}]}}]}` + "\n\n" + reviewText + `data: {"id":"chat-review","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"hi\"}"}}]}}]}` + "\n\n" + strings.Replace(reviewFinish, `"stop"`, `"tool_calls"`, 1) + reviewDone
	w, _ := reviewRealHandler(t, types.RelayFormatClaude, true, strings.NewReader(raw), "")
	if !strings.Contains(w.Body.String(), "event: message_stop") || !strings.Contains(w.Body.String(), `\"hi\"}`) {
		t.Errorf("argument continuation dropped before success: %s", w.Body.String())
	}
}

func TestProtocolRawInlineResponsesFile(t *testing.T) {
	input := `{"model":"glm-5.2","input":[{"role":"user","content":[{"type":"input_file","filename":"note.txt","file_data":"data:text/plain;base64,aGk="}]}]}`
	w, _ := reviewRealHandler(t, types.RelayFormatOpenAIResponses, false, strings.NewReader(reviewText+reviewFinish+reviewDone), input)
	if w.Code != 200 {
		t.Errorf("inline file unavailable: %d %s", w.Code, w.Body.String())
	}
}

func TestProtocolRawResponsesImageDetail(t *testing.T) {
	input := `{"model":"glm-5.2","input":[{"role":"user","content":[{"type":"input_image","image_url":"https://fixture.invalid/image.png","detail":"high"}]}]}`
	w, sent := reviewRealHandler(t, types.RelayFormatOpenAIResponses, false, strings.NewReader(reviewText+reviewFinish+reviewDone), input)
	encoded, _ := json.Marshal(sent)
	if w.Code == 200 && !strings.Contains(string(encoded), `"detail":"high"`) {
		t.Errorf("image detail silently dropped: %s", encoded)
	}
}

func TestProtocolRawPairedHistorySurvivesFullHandler(t *testing.T) {
	for _, f := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAIResponses} {
		t.Run(string(f), func(t *testing.T) {
			input := `{"model":"glm-5.2","input":[{"role":"user","content":"hi"},{"type":"reasoning","summary":[{"type":"summary_text","text":"think"}]},{"role":"assistant","content":"checking"},{"type":"function_call","call_id":"call_a","name":"check","arguments":"{\"q\":\"old\"}"},{"type":"function_call_output","call_id":"call_a","output":"checked"}]}`
			if f == types.RelayFormatClaude {
				input = `{"model":"glm-5.2","max_tokens":32,"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":[{"type":"thinking","thinking":"think"},{"type":"text","text":"checking"},{"type":"tool_use","id":"call_a","name":"check","input":{"q":"old"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_a","content":"checked"}]}]}`
			}
			w, sent := reviewRealHandler(t, f, false, strings.NewReader(reviewText+reviewFinish+reviewDone), input)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			messages := sent["messages"].([]any)
			if len(messages) != 3 {
				t.Fatal(messages)
			}
			assistant := messages[1].(map[string]any)
			tool := messages[2].(map[string]any)
			calls := assistant["tool_calls"].([]any)
			call := calls[0].(map[string]any)
			fn := call["function"].(map[string]any)
			if assistant["reasoning_content"] != "think" || call["id"] != "call_a" || fn["name"] != "check" || fn["arguments"] != `{"q":"old"}` || tool["tool_call_id"] != "call_a" || tool["content"] != "checked" {
				t.Fatal(sent)
			}
			raw, _ := json.Marshal(sent)
			t.Logf("paired history preserved: %s", raw)
		})
	}
}

func TestProtocolRawUsageParity(t *testing.T) {
	raw := reviewText + `data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}` + "\n\n" + `data: {"choices":[],"usage":{"completion_tokens":0}}` + "\n\n" + reviewDone
	for _, f := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAIResponses} {
		for _, stream := range []bool{false, true} {
			w, _ := reviewRealHandler(t, f, stream, strings.NewReader(raw), "")
			if !reviewSuccess(w) || !strings.Contains(w.Body.String(), `"input_tokens":5`) || !strings.Contains(w.Body.String(), `"output_tokens":0`) {
				t.Fatal(f, stream, w.Body.String())
			}
			if f == types.RelayFormatOpenAIResponses && !strings.Contains(w.Body.String(), `"total_tokens":12`) {
				t.Fatal(w.Body.String())
			}
		}
	}
}

func TestProtocolRawAllTerminationAndBounds(t *testing.T) {
	for _, f := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAIResponses} {
		for _, stream := range []bool{false, true} {
			for _, raw := range []string{reviewText + reviewDone, reviewText + reviewFinish + "data: {broken\n\n", reviewText + reviewFinish + reviewDone + reviewText, reviewText + reviewFinish + "data: ", reviewText + reviewFinish + reviewDone + "event: error\n\n"} {
				w, _ := reviewRealHandler(t, f, stream, strings.NewReader(raw), "")
				if reviewSuccess(w) {
					t.Fatal(f, stream, w.Body.String())
				}
			}
			w, _ := reviewRealHandler(t, f, stream, io.MultiReader(strings.NewReader(reviewText+reviewFinish), reviewReadFailure{}), "")
			if reviewSuccess(w) {
				t.Fatal(f, stream, w.Body.String())
			}
			w, _ = reviewRealHandler(t, f, stream, strings.NewReader(reviewText+reviewFinish), "")
			if !reviewSuccess(w) {
				t.Fatal("valid EOF rejected", f, stream, w.Body.String())
			}
			for _, raw := range []string{"data: " + strings.Repeat("x", 9<<20), reviewText + strings.Repeat(":"+strings.Repeat("x", 1022)+"\n\n", 33000) + reviewFinish + reviewDone} {
				limit := 32 << 20
				if strings.HasPrefix(raw, "data: xxx") {
					limit = 8 << 20
				}
				counted := &reviewCountingReader{Reader: strings.NewReader(raw)}
				w, _ := reviewRealHandler(t, f, stream, counted, "")
				if reviewSuccess(w) || counted.n > limit+(32<<10) || !counted.closed {
					t.Fatal(f, stream, counted.n, w.Code)
				}
			}
		}
	}
}

func TestProtocolRawInlineMediaPayloads(t *testing.T) {
	for _, tc := range []struct {
		data, expected string
		valid          bool
	}{
		{"data:text/plain;base64,aGk=", `"text":"hi"`, true},
		{"data:application/pdf;base64,JVBERi0xLjQK", `"file_data":"data:application/pdf;base64,JVBERi0xLjQK"`, true},
		{"https://fixture.invalid/file.pdf", "", false},
		{"data:text/plain;base64,!!!!", "", false},
		{"data:text/plain;base64,/w==", "", false},
		{"data:application/octet-stream;base64,aGk=", "", false},
		{"data:text/plain,hello", "", false},
	} {
		input := fmt.Sprintf(`{"model":"glm-5.2","input":[{"role":"user","content":[{"type":"input_file","filename":"note","file_data":%q}]}]}`, tc.data)
		w, sent := reviewRealHandler(t, types.RelayFormatOpenAIResponses, false, strings.NewReader(reviewText+reviewFinish), input)
		encoded, _ := json.Marshal(sent)
		if tc.valid {
			if w.Code != 200 || !strings.Contains(string(encoded), tc.expected) {
				t.Fatal(tc.data, w.Code, w.Body.String(), string(encoded))
			}
		} else if w.Code != 400 || sent != nil {
			t.Fatal(tc.data, w.Code, sent)
		}
	}
}

func TestProtocolRawImageDetailValues(t *testing.T) {
	for _, detail := range []string{`"low"`, `"high"`, `"auto"`, `"invalid"`, `null`, `1`, `{}`} {
		input := `{"model":"glm-5.2","input":[{"role":"user","content":[{"type":"input_image","image_url":"https://fixture.invalid/image.png","detail":` + detail + `}]}]}`
		w, sent := reviewRealHandler(t, types.RelayFormatOpenAIResponses, false, strings.NewReader(reviewText+reviewFinish), input)
		encoded, _ := json.Marshal(sent)
		if detail == `"low"` || detail == `"high"` || detail == `"auto"` {
			if w.Code != 200 || !strings.Contains(string(encoded), `"detail":`+detail) {
				t.Fatal(w.Code, string(encoded))
			}
		} else if w.Code != 400 || sent != nil || !strings.Contains(w.Body.String(), ".detail") {
			t.Fatal(w.Code, w.Body.String(), sent)
		}
	}
}

func TestProtocolRawToolArgumentSemantics(t *testing.T) {
	for _, f := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAIResponses} {
		for _, stream := range []bool{false, true} {
			for _, args := range []string{"", `{"q":`, `[]`, `null`, `{"q":"hi"}`} {
				a, _ := json.Marshal(args)
				raw := `data: {"id":"chat-review","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"check","arguments":` + string(a) + `}}]}}]}` + "\n\n" + strings.Replace(reviewFinish, `"stop"`, `"tool_calls"`, 1) + reviewDone
				w, _ := reviewRealHandler(t, f, stream, strings.NewReader(raw), "")
				success := w.Code == 200 && !strings.Contains(w.Body.String(), `"type":"error"`) && (strings.Contains(w.Body.String(), `"status":"completed"`) || strings.Contains(w.Body.String(), `"stop_reason":"tool_use"`))
				want := f == types.RelayFormatOpenAIResponses || args == `{"q":"hi"}`
				if success != want {
					t.Fatal(f, stream, args, w.Code, w.Body.String())
				}
			}
		}
	}
}

func TestProtocolRawMultilineDataParity(t *testing.T) {
	raw := "data:" + `{"id":"chat-review",` + "\ndata: " + `"choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":"length"}]}` + "\n\n"
	for _, f := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAIResponses} {
		for _, stream := range []bool{false, true} {
			w, _ := reviewRealHandler(t, f, stream, strings.NewReader(raw), "")
			expected := `"status":"incomplete"`
			if f == types.RelayFormatClaude {
				expected = `"stop_reason":"max_tokens"`
			}
			if w.Code != 200 || !strings.Contains(w.Body.String(), expected) || !strings.Contains(w.Body.String(), "hello") {
				t.Fatal(f, stream, w.Code, w.Body.String())
			}
		}
	}
}
