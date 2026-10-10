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

const failureMessage = "模型调用失败，请稍后重试"

// Serve converts one authenticated request in-process. conversationID must come from trusted context.
func Serve(w http.ResponseWriter, r *http.Request, next http.Handler, apiKey string, maxBodyBytes int64, format types.RelayFormat, conversationID string) {
	if maxBodyBytes <= 0 {
		maxBodyBytes = 8 << 20
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if int64(len(raw)) > maxBodyBytes {
		WriteError(w, 413, "请求体超过大小限制", format)
		return
	}
	if err != nil || !utf8.Valid(raw) {
		WriteError(w, 400, "请求体必须是 UTF-8 JSON 对象", format)
		return
	}
	input, model, stream, err := parseRequest(raw, format)
	if err != nil {
		WriteError(w, 400, err.Error(), format)
		return
	}
	meta := &convmeta.Values{OriginModelName: model, IsStream: stream, Options: &convmeta.Options{ToolLossPolicy: types.ConversionLossPolicySafe}}
	result, err := relayconvert.ConvertRequest(r.Context(), meta, types.RelayFormatOpenAI, input)
	if err == nil {
		err = types.RejectConversionLoss(types.ConversionLossPolicySafe, result.Diagnostics)
	}
	if err != nil {
		WriteError(w, 400, err.Error(), format)
		return
	}
	chat, ok := result.Value.(*dto.GeneralOpenAIRequest)
	if !ok {
		WriteError(w, 500, failureMessage, format)
		return
	}
	if chat.MaxCompletionTokens != nil {
		chat.MaxTokens = chat.MaxCompletionTokens
		chat.MaxCompletionTokens = nil
	}
	converted, err := json.Marshal(chat)
	if err != nil {
		WriteError(w, 400, "请求转换失败", format)
		return
	}
	if conversationID != "" {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(converted, &fields)
		fields["conversationId"], _ = json.Marshal(conversationID)
		converted, _ = json.Marshal(fields)
	}
	if int64(len(converted)) > maxBodyBytes {
		WriteError(w, 413, "转换后请求体超过大小限制", format)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	req := r.Clone(ctx)
	req.URL.Path = "/v1/chat/completions"
	req.URL.RawPath = ""
	req.URL.RawQuery = ""
	req.URL.ForceQuery = false
	req.RequestURI = req.URL.RequestURI()
	req.Header = http.Header{"Content-Type": []string{"application/json"}, "Authorization": []string{"Bearer " + apiKey}}
	req.Body = io.NopCloser(bytes.NewReader(converted))
	req.GetBody = nil
	req.ContentLength = int64(len(converted))
	req.TransferEncoding = nil
	req.Trailer = nil
	if stream {
		state, err := relayconvert.NewResponseStreamState(types.RelayFormatOpenAI, format, relayconvert.ResponseStreamOptions{Model: model, EmitSequenceNumber: true})
		if err != nil {
			WriteError(w, 500, failureMessage, format)
			return
		}
		sink := &streamWriter{dst: w, header: make(http.Header), ctx: ctx, cancel: cancel, meta: meta, format: format, state: state, model: model}
		next.ServeHTTP(sink, req)
		sink.finish()
		return
	}
	capture := &responseBuffer{header: make(http.Header), ctx: ctx, cancel: cancel}
	next.ServeHTTP(capture, req)
	if capture.err != nil || ctx.Err() != nil || capture.status < 200 || capture.status >= 300 {
		WriteError(w, errorStatus(capture.status), failureMessage, format)
		return
	}
	var output dto.OpenAITextResponse
	var usage usagePresence
	if validateCompletion(capture.body.Bytes()) != nil || json.Unmarshal(capture.body.Bytes(), &output) != nil || usage.observe(capture.body.Bytes()) != nil {
		cancel()
		WriteError(w, 502, failureMessage, format)
		return
	}
	convertedResponse, err := relayconvert.ConvertResponse(ctx, meta, format, &output)
	if err == nil {
		err = types.RejectConversionLoss(types.ConversionLossPolicySafe, convertedResponse.Diagnostics)
	}
	if err != nil {
		cancel()
		WriteError(w, 502, failureMessage, format)
		return
	}
	response, err := publicJSON(convertedResponse.Value, usage, format)
	if err != nil {
		cancel()
		WriteError(w, 502, failureMessage, format)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(response)
}

func errorStatus(status int) int {
	if status >= 400 && status <= 599 {
		return status
	}
	return 502
}

const maxResponseBytes = 32 << 20

type responseBuffer struct {
	header http.Header
	status int
	body   bytes.Buffer
	ctx    context.Context
	cancel context.CancelFunc
	err    error
}

func (w *responseBuffer) Header() http.Header { return w.header }
func (w *responseBuffer) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
}
func (w *responseBuffer) Write(p []byte) (int, error) {
	if w.err == nil {
		w.err = w.ctx.Err()
	}
	if w.err == nil && len(p) > maxResponseBytes-w.body.Len() {
		w.err = errors.New("response exceeds limit")
	}
	if w.err != nil {
		w.cancel()
		return 0, w.err
	}
	if w.status == 0 {
		w.status = 200
	}
	return w.body.Write(p)
}

func validFinish(reason string) bool {
	switch reason {
	case "stop", "length", "tool_calls", "content_filter":
		return true
	}
	return false
}
func validateCompletion(raw []byte) error {
	var v *struct {
		ID      string `json:"id"`
		Error   any    `json:"error"`
		Choices []*struct {
			Index   int             `json:"index"`
			Message json.RawMessage `json:"message"`
			Finish  string          `json:"finish_reason"`
		} `json:"choices"`
	}
	if !utf8.Valid(raw) || json.Unmarshal(raw, &v) != nil || v == nil || v.ID == "" || v.Error != nil || len(v.Choices) != 1 || v.Choices[0] == nil {
		return errors.New("invalid completion")
	}
	c := v.Choices[0]
	if c.Index != 0 || !validFinish(c.Finish) {
		return errors.New("invalid choice")
	}
	var msg map[string]json.RawMessage
	if json.Unmarshal(c.Message, &msg) != nil || msg == nil {
		return errors.New("invalid message")
	}
	var role string
	if json.Unmarshal(msg["role"], &role) != nil || role != "assistant" {
		return errors.New("invalid role")
	}
	if raw, ok := msg["content"]; ok && string(raw) != "null" {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return errors.New("invalid content")
		}
	}
	if _, ok := msg["content"]; !ok && len(msg["tool_calls"]) == 0 && len(msg["reasoning_content"]) == 0 {
		return errors.New("missing message content")
	}
	return nil
}

func strictJSON(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("尾随 JSON 不受支持")
	}
	return nil
}

// WriteError writes the target protocol's public error envelope.
func WriteError(w http.ResponseWriter, code int, message string, format types.RelayFormat) {
	typ := "api_error"
	switch code {
	case 400, 405:
		typ = "invalid_request_error"
	case 401:
		typ = "authentication_error"
	case 413:
		typ = "request_too_large"
	case 429:
		typ = "rate_limit_error"
	}
	body := map[string]any{"error": map[string]any{"type": typ, "message": message}}
	if format == types.RelayFormatClaude {
		body["type"] = "error"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func parseRequest(raw []byte, format types.RelayFormat) (any, string, bool, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, "", false, errors.New("请求体必须是单个 JSON 对象")
	}
	for _, key := range []string{"stream", "parallel_tool_calls", "store", "background", "enable_thinking"} {
		if v, ok := fields[key]; ok && string(v) != "true" && string(v) != "false" {
			return nil, "", false, errors.New(key + " 必须是布尔值")
		}
	}
	if format == types.RelayFormatOpenAIResponses {
		if err := validateResponses(fields); err != nil {
			return nil, "", false, err
		}
		delete(fields, "background")
		raw, _ = json.Marshal(fields)
		var req dto.OpenAIResponsesRequest
		if err := strictJSON(raw, &req); err != nil {
			return nil, "", false, err
		}
		if strings.TrimSpace(req.Model) == "" {
			return nil, "", false, errors.New("model 不能为空")
		}
		return &req, req.Model, req.Stream != nil && *req.Stream, nil
	}
	if format != types.RelayFormatClaude {
		return nil, "", false, errors.New("不支持的协议")
	}
	for key := range fields {
		switch key {
		case "model", "messages", "max_tokens", "system", "stream":
		default:
			return nil, "", false, errors.New("不支持字段 " + key)
		}
	}
	var req dto.ClaudeRequest
	if err := strictJSON(raw, &req); err != nil {
		return nil, "", false, err
	}
	if strings.TrimSpace(req.Model) == "" || req.MaxTokens == nil || *req.MaxTokens == 0 || len(req.Messages) == 0 {
		return nil, "", false, errors.New("model、max_tokens 和 messages 必填")
	}
	if v, ok := fields["system"]; ok {
		if err := validateText(v, "system", "text"); err != nil {
			return nil, "", false, err
		}
	}
	for _, msg := range req.Messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			return nil, "", false, errors.New("messages.role 无效")
		}
		v, _ := json.Marshal(msg.Content)
		if err := validateText(v, "messages.content", "text"); err != nil {
			return nil, "", false, err
		}
	}
	return &req, req.Model, req.Stream != nil && *req.Stream, nil
}
