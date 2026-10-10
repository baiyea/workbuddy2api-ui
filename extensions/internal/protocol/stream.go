package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/types"
)

const maxStreamFrame = 8 << 20

// streamWriter consumes Handler writes synchronously, bounding both frames and accumulated state.
type streamWriter struct {
	dst                      http.ResponseWriter
	header                   http.Header
	ctx                      context.Context
	cancel                   context.CancelFunc
	meta                     *convmeta.Values
	format                   types.RelayFormat
	state                    *relayconvert.ResponseStreamState
	usage                    usagePresence
	model, id, reason        string
	pending                  []byte
	lineStart, total, status int
	sequence                 int
	started, done            bool
	err                      error
}

func (s *streamWriter) Header() http.Header { return s.header }
func (s *streamWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
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
		s.status = 200
	}
	if s.status < 200 || s.status >= 300 {
		return 0, s.fail(errors.New("upstream status"), false)
	}
	if len(p) > maxResponseBytes-s.total {
		return 0, s.fail(errors.New("stream exceeds total limit"), false)
	}
	s.total += len(p)
	for i, b := range p {
		if len(s.pending) == maxStreamFrame {
			return i, s.fail(errors.New("frame exceeds limit"), false)
		}
		s.pending = append(s.pending, b)
		if b != '\n' {
			continue
		}
		line := bytes.TrimSuffix(s.pending[s.lineStart:len(s.pending)-1], []byte{'\r'})
		if len(line) > 0 {
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
	if s.err != nil || !s.started {
		return
	}
	if err := s.ctx.Err(); err != nil {
		s.fail(err, true)
		return
	}
	if err := http.NewResponseController(s.dst).Flush(); err != nil {
		s.fail(err, true)
	}
}
func (s *streamWriter) frame(raw []byte) error {
	var data []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSuffix(line, "\r")
		key, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		if key == "event" && value == "error" {
			return errors.New("upstream error event")
		}
		if key == "data" {
			data = append(data, value)
		}
	}
	if len(data) == 0 {
		return nil
	}
	if s.done {
		return errors.New("data after DONE")
	}
	payload := []byte(strings.Join(data, "\n"))
	if string(payload) == "[DONE]" {
		if s.reason == "" {
			return errors.New("DONE without finish")
		}
		s.done = true
		return nil
	}
	var envelope *struct {
		Error   any `json:"error"`
		Choices []*struct {
			Index  int             `json:"index"`
			Delta  json.RawMessage `json:"delta"`
			Finish *string         `json:"finish_reason"`
		} `json:"choices"`
		Usage json.RawMessage `json:"usage"`
	}
	if !utf8.Valid(payload) || json.Unmarshal(payload, &envelope) != nil || envelope == nil || envelope.Error != nil || len(envelope.Choices) > 1 {
		return errors.New("invalid stream chunk")
	}
	if err := s.usage.observe(payload); err != nil {
		return err
	}
	if len(envelope.Choices) == 0 {
		if len(envelope.Usage) == 0 || string(envelope.Usage) == "null" {
			return errors.New("empty stream chunk")
		}
		return nil
	}
	choice := envelope.Choices[0]
	if choice == nil || choice.Index != 0 || s.reason != "" {
		return errors.New("invalid choice or choice after finish")
	}
	var delta *dto.ChatCompletionsStreamResponseChoiceDelta
	if strictJSON(choice.Delta, &delta) != nil || delta == nil || (delta.Role != "" && delta.Role != "assistant") {
		return errors.New("invalid delta")
	}
	if choice.Finish != nil {
		if !validFinish(*choice.Finish) {
			return errors.New("invalid finish reason")
		}
		s.reason = *choice.Finish
	}
	var chunk dto.ChatCompletionsStreamResponse
	if json.Unmarshal(payload, &chunk) != nil {
		return errors.New("invalid chunk")
	}
	chunk.Usage = nil
	chunk.Choices[0].FinishReason = nil
	if chunk.Id != "" {
		s.id = chunk.Id
	}
	if chunk.Model == "" {
		chunk.Model = s.model
	}
	if s.format == types.RelayFormatClaude {
		// Claude's converter selects a single delta branch; preserve all mixed components in order.
		var parts []dto.ChatCompletionsStreamResponseChoiceDelta
		if delta.ReasoningContent != nil || delta.Reasoning != nil {
			parts = append(parts, dto.ChatCompletionsStreamResponseChoiceDelta{ReasoningContent: delta.ReasoningContent, Reasoning: delta.Reasoning})
		}
		if delta.Content != nil || len(delta.Annotations) > 0 {
			parts = append(parts, dto.ChatCompletionsStreamResponseChoiceDelta{Content: delta.Content, Annotations: delta.Annotations})
		}
		if len(delta.ToolCalls) > 0 {
			parts = append(parts, dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: delta.ToolCalls})
		}
		if len(parts) == 0 {
			parts = append(parts, *delta)
		}
		for _, part := range parts {
			chunk.Choices[0].Delta = part
			if err := s.convert(&chunk); err != nil {
				return err
			}
		}
		return nil
	}
	return s.convert(&chunk)
}
func (s *streamWriter) convert(chunk *dto.ChatCompletionsStreamResponse) error {
	results, err := relayconvert.ConvertStreamResponseChunk(s.ctx, s.meta, s.state, chunk)
	if err != nil {
		return err
	}
	return s.emitResults(results)
}
func (s *streamWriter) emitResults(results []relayconvert.ResponseResult) error {
	for _, result := range results {
		if err := types.RejectConversionLoss(types.ConversionLossPolicySafe, result.Diagnostics); err != nil {
			return err
		}
		var kind string
		var value any
		switch v := result.Value.(type) {
		case relayconvert.ChatToResponsesStreamEvent:
			kind = v.Type
			value = v.Payload
		case *dto.ClaudeResponse:
			kind = v.Type
			value = v
		default:
			return errors.New("unexpected stream result")
		}
		raw, err := publicJSON(value, s.usage, s.format)
		if err != nil {
			return err
		}
		if err = s.ctx.Err(); err != nil {
			return s.fail(err, true)
		}
		if !s.started {
			s.dst.Header().Set("Content-Type", "text/event-stream")
			s.dst.Header().Set("Cache-Control", "no-cache")
			s.dst.Header().Set("X-Accel-Buffering", "no")
			s.started = true
		}
		frame := []byte("event: " + kind + "\ndata: " + string(raw) + "\n\n")
		n, err := s.dst.Write(frame)
		if err == nil && n != len(frame) {
			err = io.ErrShortWrite
		}
		if err != nil {
			return s.fail(err, true)
		}
		s.Flush()
		if s.err != nil {
			return s.err
		}
		s.sequence++
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
		WriteError(s.dst, errorStatus(s.status), failureMessage, s.format)
	} else {
		body := map[string]any{"type": "error", "error": map[string]string{"type": "api_error", "message": failureMessage}}
		if s.format == types.RelayFormatOpenAIResponses {
			body = map[string]any{"type": "error", "code": "server_error", "message": failureMessage, "param": nil, "sequence_number": s.sequence}
		}
		raw, _ := json.Marshal(body)
		if _, writeErr := io.WriteString(s.dst, "event: error\ndata: "+string(raw)+"\n\n"); writeErr == nil {
			_ = http.NewResponseController(s.dst).Flush()
		}
	}
	return err
}
func (s *streamWriter) finish() {
	if s.err != nil {
		return
	}
	if err := s.ctx.Err(); err != nil {
		s.fail(err, true)
		return
	}
	if (s.status != 0 && (s.status < 200 || s.status >= 300)) || len(bytes.TrimSpace(s.pending)) > 0 || s.reason == "" {
		s.fail(errors.New("failed or truncated stream"), false)
		return
	}
	chunk := &dto.ChatCompletionsStreamResponse{Id: s.id, Model: s.model, Choices: []dto.ChatCompletionsStreamResponseChoice{{FinishReason: &s.reason}}}
	if err := s.convert(chunk); err != nil {
		s.fail(err, false)
		return
	}
	results, err := relayconvert.FinalizeStreamResponse(s.ctx, s.meta, s.state)
	if err == nil {
		err = s.emitResults(results)
	}
	if err != nil {
		s.fail(err, false)
	}
}
