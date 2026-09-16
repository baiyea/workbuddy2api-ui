package anthropic

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"workbuddy2api/internal/upstream"
)

const streamRequest = `{"model":"global:mock","max_tokens":32,"messages":[{"role":"user","content":"你好"}],"stream":true}`
const firstText = "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你\"}}]}\n\n"
const secondText = "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"好\"}}]}\n\n"
const stopFrame = "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"
const doneFrame = "data: [DONE]\n\n"

func events(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var result []map[string]any
	for _, frame := range strings.Split(strings.TrimSpace(raw), "\n\n") {
		lines := strings.Split(frame, "\n")
		if len(lines) != 2 || !strings.HasPrefix(lines[0], "event: ") || !strings.HasPrefix(lines[1], "data: ") {
			t.Fatalf("invalid SSE frame: %q", frame)
		}
		var value map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[1], "data: ")), &value); err != nil {
			t.Fatal(err)
		}
		if value["type"] != strings.TrimPrefix(lines[0], "event: ") {
			t.Fatalf("event/data type mismatch: %q", frame)
		}
		result = append(result, value)
	}
	return result
}

func TestStreamFirstTextArrivesBeforeNextContinues(t *testing.T) {
	release := make(chan struct{}, 1)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["stream"] != true {
			t.Error("stream was not forwarded")
			return
		}
		for _, b := range []byte(firstText) {
			if _, err := w.Write([]byte{b}); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		for _, frame := range []string{secondText, stopFrame, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n\n", doneFrame} {
			for _, b := range []byte(frame) {
				if _, err := w.Write([]byte{b}); err != nil {
					return
				}
			}
		}
	})
	server := httptest.NewServer(New(next, "fixture-api", 8<<20))
	defer server.Close()
	defer close(release)
	r, _ := http.NewRequest("POST", server.URL+"/v1/messages", strings.NewReader(streamRequest))
	r.Header = request(streamRequest).Header
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream response: %d %s", response.StatusCode, response.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(response.Body)
	var raw strings.Builder
	for !strings.Contains(raw.String(), `"text":"你"`) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("first text never arrived before release: %v", err)
		}
		raw.WriteString(line)
	}
	release <- struct{}{}
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	raw.Write(rest)
	got := events(t, raw.String())
	var kinds []any
	for _, event := range got {
		kinds = append(kinds, event["type"])
	}
	want := []any{"message_start", "content_block_start", "content_block_delta", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("event order: %v", kinds)
	}
	start := got[0]["message"].(map[string]any)
	if start["model"] != "global:mock" || !strings.HasPrefix(start["id"].(string), "msg_") || start["stop_reason"] != nil {
		t.Fatalf("bad message identity: %v", start)
	}
	if !reflect.DeepEqual(start["usage"], map[string]any{"input_tokens": nil, "output_tokens": nil}) || !reflect.DeepEqual(got[5]["usage"], map[string]any{"input_tokens": float64(3), "output_tokens": float64(2)}) {
		t.Fatalf("late usage lost: %v", got)
	}
}

func TestStreamUsageAndFraming(t *testing.T) {
	for _, tc := range []struct {
		name, raw, reason string
		input, output     any
	}{
		{"unknown", firstText + stopFrame + doneFrame, "end_turn", nil, nil},
		{"empty block", stopFrame + doneFrame, "end_turn", nil, nil},
		{"length without DONE", firstText + strings.ReplaceAll(stopFrame, `"stop"`, `"length"`), "max_tokens", nil, nil},
		{"zero", firstText + stopFrame + "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":0}}\n\n" + doneFrame, "end_turn", float64(0), float64(0)},
		{"cumulative", firstText + "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1}}\n\n" + stopFrame + "data: {\"choices\":[],\"usage\":{\"completion_tokens\":2}}\n\n" + doneFrame, "end_turn", float64(3), float64(2)},
		{"CRLF multiline comments", ": keepalive\r\n\r\nevent: message\r\ndata: {\"choices\":\r\ndata: [{\"delta\":{\"content\":\"你\"}}]}\r\n\r\n" + stopFrame + doneFrame, "end_turn", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, tc.raw) }), "fixture-api", 0).ServeHTTP(w, request(streamRequest))
			if w.Code != 200 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			got := events(t, w.Body.String())
			last := got[len(got)-2]
			if last["type"] != "message_delta" || got[len(got)-1]["type"] != "message_stop" || last["delta"].(map[string]any)["stop_reason"] != tc.reason || !reflect.DeepEqual(last["usage"], map[string]any{"input_tokens": tc.input, "output_tokens": tc.output}) {
				t.Fatalf("final event: %v", got)
			}
		})
	}
}

func TestStreamRejectsInvalidOrTruncatedOutput(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		started   bool
	}{
		{"empty", "", false},
		{"only DONE", doneFrame, false},
		{"malformed JSON", "data: {secret\n\n", false},
		{"invalid UTF8", strings.Replace(firstText, "你", string([]byte{0xff}), 1), false},
		{"role only", "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n", true},
		{"no finish", firstText + doneFrame, true},
		{"EOF without finish", firstText, true},
		{"tools", firstText + "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"secret\"}]}}]}\n\n", true},
		{"function call", "data: {\"choices\":[{\"delta\":{\"function_call\":{\"name\":\"secret\"}}}]}\n\n", false},
		{"content filter", firstText + strings.ReplaceAll(stopFrame, `"stop"`, `"content_filter"`), true},
		{"error before text", "event: error\ndata: {\"error\":{\"message\":\"secret\"}}\n\n", false},
		{"error after text", firstText + "data: {\"error\":{\"message\":\"secret\"}}\n\n", true},
		{"oversized frame", "data: " + strings.Repeat("x", (8<<20)+1), false},
		{"text after finish", firstText + stopFrame + secondText, true},
		{"duplicate finish", firstText + stopFrame + stopFrame, true},
		{"duplicate DONE", firstText + stopFrame + doneFrame + doneFrame, true},
		{"data after DONE", firstText + stopFrame + doneFrame + secondText, true},
		{"partial trailing frame", firstText + stopFrame + "data: {", true},
		{"negative usage", firstText + "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":-1}}\n\n", true},
		{"fraction usage", firstText + "data: {\"choices\":[],\"usage\":{\"completion_tokens\":1.5}}\n\n", true},
		{"unknown delta", "data: {\"choices\":[{\"delta\":{\"audio\":{\"data\":\"secret\"}},\"finish_reason\":\"stop\"}]}\n\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var child context.Context
			w := httptest.NewRecorder()
			New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				child = r.Context()
				io.WriteString(w, tc.raw)
				w.(http.Flusher).Flush()
			}), "fixture-api", 0).ServeHTTP(w, request(streamRequest))
			if child == nil || child.Err() != context.Canceled {
				t.Fatal("next was not canceled")
			}
			if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "message_stop") {
				t.Fatalf("leak or false completion: %s", w.Body)
			}
			if tc.started {
				got := events(t, w.Body.String())
				if w.Code != 200 || got[len(got)-1]["type"] != "error" {
					t.Fatalf("missing stream error: %s", w.Body)
				}
			} else {
				if w.Code != 502 {
					t.Fatalf("expected pre-stream 502, got %d: %s", w.Code, w.Body)
				}
				assertError(t, w, 502)
			}
		})
	}
}

func TestActualUpstreamStreamCannotManufactureSuccess(t *testing.T) {
	for _, raw := range []string{"", firstText, "data: {\"error\":{\"message\":\"secret\"}}\n\n", firstText + stopFrame} {
		w := httptest.NewRecorder()
		New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = upstream.Stream(w, strings.NewReader(raw)) }), "fixture-api", 0).ServeHTTP(w, request(streamRequest))
		completed := strings.Contains(w.Body.String(), "event: message_stop")
		if completed != strings.Contains(raw, `"finish_reason":"stop"`) {
			t.Fatalf("manufactured/lost completion for %q: %s", raw, w.Body)
		}
	}
}

func TestStreamHTTPErrorDoesNotCommitSSE(t *testing.T) {
	for _, status := range []int{302, 401, 429, 500, 503} {
		w := httptest.NewRecorder()
		New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.(http.Flusher).Flush()
			w.Header().Set("Set-Cookie", "secret")
			w.WriteHeader(status)
			io.WriteString(w, `{"error":"secret"}`)
		}), "fixture-api", 0).ServeHTTP(w, request(streamRequest))
		want := status
		if status < 400 {
			want = 502
		}
		if w.Code != want || w.Header().Get("Set-Cookie") != "" || strings.Contains(w.Body.String(), "secret") {
			t.Fatalf("unsafe HTTP error: %d %s", w.Code, w.Body)
		}
		assertError(t, w, want)
	}
}

type brokenStreamWriter struct {
	header   http.Header
	writes   int
	flushErr bool
}

func (w *brokenStreamWriter) Header() http.Header { return w.header }
func (w *brokenStreamWriter) WriteHeader(int)     {}
func (w *brokenStreamWriter) Write(p []byte) (int, error) {
	w.writes++
	if !w.flushErr {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}
func (w *brokenStreamWriter) FlushError() error { return io.ErrClosedPipe }

func TestStreamWriteAndFlushFailureCancelNext(t *testing.T) {
	for _, flush := range []bool{false, true} {
		w := &brokenStreamWriter{header: make(http.Header), flushErr: flush}
		called := false
		New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			_, err := io.WriteString(w, firstText)
			if !errors.Is(err, io.ErrClosedPipe) {
				t.Errorf("write failure lost: %v", err)
			}
			select {
			case <-r.Context().Done():
			case <-time.After(time.Second):
				t.Error("write failure did not cancel next")
			}
			if _, err = io.WriteString(w, stopFrame); err == nil {
				t.Error("error state was reversible")
			}
		}), "fixture-api", 0).ServeHTTP(w, request(streamRequest))
		if !called || w.writes != 1 {
			t.Fatalf("writes=%d next=%v; attempted output after disconnect", w.writes, called)
		}
	}
}

func TestStreamClientDisconnectCancelsWaitingNext(t *testing.T) {
	canceled := make(chan struct{})
	release := make(chan struct{})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.WriteString(w, firstText); err != nil {
			return
		}
		select {
		case <-r.Context().Done():
			close(canceled)
		case <-release:
		}
	})
	server := httptest.NewServer(New(next, "fixture-api", 0))
	defer server.Close()
	defer close(release)
	r, _ := http.NewRequest("POST", server.URL+"/v1/messages", strings.NewReader(streamRequest))
	r.Header = request(streamRequest).Header
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(line, `"text":"你"`) {
			break
		}
	}
	response.Body.Close()
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("client disconnect did not cancel waiting next")
	}
}

func TestStreamParseFailureCancelsBeforeNextReturns(t *testing.T) {
	called := false
	w := httptest.NewRecorder()
	New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		io.WriteString(w, firstText)
		if _, err := io.WriteString(w, "data: {secret\n\n"); err == nil {
			t.Error("malformed frame accepted")
		}
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
			t.Error("parser did not cancel before next returned")
		}
		if _, err := io.WriteString(w, stopFrame); err == nil {
			t.Error("failed stream resumed")
		}
	}), "fixture-api", 0).ServeHTTP(w, request(streamRequest))
	if !called || strings.Count(w.Body.String(), "event: error") != 1 || strings.Contains(w.Body.String(), "message_stop") {
		t.Fatalf("invalid final error state: %s", w.Body)
	}
}

func TestStreamBoundsHTTPErrorBodyAndKeepsStatus(t *testing.T) {
	failed := false
	w := httptest.NewRecorder()
	New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		chunk := strings.Repeat("x", 1<<20)
		for i := 0; i < 9; i++ {
			if _, err := io.WriteString(w, chunk); err != nil {
				failed = true
				if r.Context().Err() != context.Canceled {
					t.Error("error body overflow did not cancel")
				}
				return
			}
		}
	}), "fixture-api", 0).ServeHTTP(w, request(streamRequest))
	if !failed || w.Code != 429 {
		t.Fatalf("unbounded error body/status: failed=%v code=%d", failed, w.Code)
	}
	assertError(t, w, 429)
}
