package anthropic

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

const maxStreamFrame = 8 << 20

// streamWriter consumes the existing Handler synchronously. It stores only one
// unfinished frame, never the whole answer, and emits only converted events.
type streamWriter struct {
	dst                           http.ResponseWriter
	header                        http.Header
	ctx                           context.Context
	cancel                        context.CancelFunc
	model, id, stopReason         string
	pending                       []byte
	lineStart, status, errorBytes int
	started, done, completed      bool
	inputTokens, outputTokens     *int64
	err                           error
}

func newStreamWriter(w http.ResponseWriter, model string, cancel context.CancelFunc) *streamWriter {
	return &streamWriter{dst: w, header: make(http.Header), ctx: context.Background(), model: model, cancel: cancel}
}

func (s *streamWriter) Header() http.Header { return s.header }
func (s *streamWriter) WriteHeader(status int) {
	if s.status == 0 {
		s.status = status
	}
}

func (s *streamWriter) Write(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	if err := s.ctx.Err(); err != nil {
		return 0, s.fail(err, true)
	}
	if s.status == 0 {
		s.status = http.StatusOK
	}
	if s.status < 200 || s.status >= 300 {
		if len(p) > maxStreamFrame-s.errorBytes {
			return 0, s.fail(errors.New("upstream error body exceeds limit"), false)
		}
		s.errorBytes += len(p)
		return len(p), nil
	}
	for i, b := range p {
		if len(s.pending) == maxStreamFrame {
			return i, s.fail(errors.New("SSE frame exceeds limit"), false)
		}
		s.pending = append(s.pending, b)
		if b != '\n' {
			continue
		}
		line := bytes.TrimSuffix(s.pending[s.lineStart:len(s.pending)-1], []byte{'\r'})
		if len(line) != 0 {
			s.lineStart = len(s.pending)
			continue
		}
		if err := s.frame(s.pending[:s.lineStart]); err != nil {
			return i + 1, s.fail(err, false)
		}
		s.pending = s.pending[:0]
		s.lineStart = 0
	}
	return len(p), nil
}

func (s *streamWriter) Flush() {
	if !s.started || s.err != nil {
		return
	}
	if err := s.ctx.Err(); err != nil {
		s.fail(err, true)
		return
	}
	if err := http.NewResponseController(s.dst).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.fail(err, true)
	}
}

func (s *streamWriter) emit(kind string, fields map[string]any) error {
	fields["type"] = kind
	raw, _ := json.Marshal(fields)
	frame := []byte("event: " + kind + "\ndata: " + string(raw) + "\n\n")
	n, err := s.dst.Write(frame)
	if err == nil && n != len(frame) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return s.fail(err, true)
	}
	s.Flush()
	return s.err
}

func (s *streamWriter) begin() error {
	if s.started {
		return nil
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	s.id = "msg_" + hex.EncodeToString(id[:])
	s.dst.Header().Set("Content-Type", "text/event-stream")
	s.dst.Header().Set("Cache-Control", "no-cache")
	s.dst.Header().Set("X-Accel-Buffering", "no")
	s.started = true
	if err := s.emit("message_start", map[string]any{"message": map[string]any{
		"id": s.id, "type": "message", "role": "assistant", "model": s.model,
		"content": []textBlock{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": nil, "output_tokens": nil},
	}}); err != nil {
		return err
	}
	return s.emit("content_block_start", map[string]any{"index": 0, "content_block": textBlock{Type: "text", Text: ""}})
}

type streamDelta struct {
	Role             string            `json:"role"`
	Content          *string           `json:"content"`
	ReasoningContent *string           `json:"reasoning_content"`
	Refusal          string            `json:"refusal"`
	ToolCalls        []json.RawMessage `json:"tool_calls"`
	FunctionCall     json.RawMessage   `json:"function_call"`
}

func (s *streamWriter) frame(raw []byte) error {
	var data []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSuffix(line, "\r")
		name, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		if name == "event" && value == "error" {
			return errors.New("upstream error event")
		}
		if name == "data" {
			data = append(data, value)
		}
	}
	if len(data) == 0 {
		return nil
	}
	if s.done {
		return errors.New("data after terminal marker")
	}
	payload := []byte(strings.Join(data, "\n"))
	if string(payload) == "[DONE]" {
		if s.stopReason == "" {
			return errors.New("terminal marker without finish reason")
		}
		s.done = true
		return nil
	}
	var chunk *struct {
		Error   any `json:"error"`
		Choices []*struct {
			Index        *int            `json:"index"`
			Delta        json.RawMessage `json:"delta"`
			FinishReason *string         `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     *int64 `json:"prompt_tokens"`
			CompletionTokens *int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if !utf8.Valid(payload) || json.Unmarshal(payload, &chunk) != nil || chunk == nil || chunk.Error != nil || len(chunk.Choices) > 1 || (len(chunk.Choices) == 0 && chunk.Usage == nil) {
		return errors.New("invalid upstream stream chunk")
	}
	var delta *streamDelta
	var reason string
	if len(chunk.Choices) == 1 {
		choice := chunk.Choices[0]
		if choice == nil || (choice.Index != nil && *choice.Index != 0) || strictJSON(choice.Delta, &delta) != nil || delta == nil || s.stopReason != "" {
			return errors.New("invalid choice or choice after finish")
		}
		if (delta.Role != "" && delta.Role != "assistant") || delta.Refusal != "" || len(delta.ToolCalls) > 0 {
			return errors.New("unsupported non-text delta")
		}
		if len(delta.FunctionCall) > 0 {
			var call struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}
			if strictJSON(delta.FunctionCall, &call) != nil || call.Name != "" || call.Arguments != "" {
				return errors.New("unsupported function call")
			}
		}
		if choice.FinishReason != nil {
			reason = *choice.FinishReason
		}
		if reason != "" && reason != "stop" && reason != "length" {
			return errors.New("unsupported finish reason")
		}
	}
	if usage := chunk.Usage; usage != nil {
		if (usage.PromptTokens != nil && *usage.PromptTokens < 0) || (usage.CompletionTokens != nil && *usage.CompletionTokens < 0) {
			return errors.New("invalid usage")
		}
		// Values are cumulative: keep the last observed count, never add deltas.
		if usage.PromptTokens != nil {
			s.inputTokens = usage.PromptTokens
		}
		if usage.CompletionTokens != nil {
			s.outputTokens = usage.CompletionTokens
		}
	}
	if err := s.begin(); err != nil {
		return err
	}
	if delta != nil && delta.Content != nil && *delta.Content != "" {
		if err := s.emit("content_block_delta", map[string]any{"index": 0, "delta": map[string]string{"type": "text_delta", "text": *delta.Content}}); err != nil {
			return err
		}
	}
	if reason == "stop" {
		s.stopReason = "end_turn"
	}
	if reason == "length" {
		s.stopReason = "max_tokens"
	}
	return nil
}

func (s *streamWriter) fail(err error, disconnected bool) error {
	if s.err != nil {
		return s.err
	}
	s.err = err
	s.cancel()
	s.pending = nil
	if disconnected {
		return err
	}
	if !s.started {
		status := s.status
		if status < 400 || status > 599 {
			status = 502
		}
		writeError(s.dst, status, failureMessage)
	} else {
		raw, _ := json.Marshal(map[string]any{"type": "error", "error": map[string]string{"type": "api_error", "message": failureMessage}})
		if _, writeErr := io.WriteString(s.dst, "event: error\ndata: "+string(raw)+"\n\n"); writeErr == nil {
			_ = http.NewResponseController(s.dst).Flush()
		}
	}
	return err
}

func (s *streamWriter) finish() error {
	if s.err != nil {
		return s.err
	}
	if s.completed {
		return nil
	}
	if err := s.ctx.Err(); err != nil {
		return s.fail(err, true)
	}
	if (s.status != 0 && (s.status < 200 || s.status >= 300)) || len(bytes.TrimSpace(s.pending)) > 0 || s.stopReason == "" {
		return s.fail(errors.New("upstream response failed or stream truncated"), false)
	}
	if err := s.emit("content_block_stop", map[string]any{"index": 0}); err != nil {
		return err
	}
	if err := s.emit("message_delta", map[string]any{"delta": map[string]any{"stop_reason": s.stopReason, "stop_sequence": nil}, "usage": map[string]any{"input_tokens": s.inputTokens, "output_tokens": s.outputTokens}}); err != nil {
		return err
	}
	if err := s.emit("message_stop", map[string]any{}); err != nil {
		return err
	}
	s.completed = true
	return nil
}
