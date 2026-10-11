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

	"workbuddy2api/internal/upstream"

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
		req = req.WithContext(upstream.WithRawResponse(req.Context(), func(_ http.ResponseWriter, body io.Reader) error {
			_, err := io.CopyBuffer(sink, body, make([]byte, 32<<10))
			if err != nil {
				return sink.fail(err, false)
			}
			return nil
		}))
		next.ServeHTTP(sink, req)
		sink.finish()
		return
	}
	capture := &responseBuffer{header: make(http.Header), ctx: ctx, cancel: cancel}
	req = req.WithContext(upstream.WithRawResponse(req.Context(), func(_ http.ResponseWriter, body io.Reader) error {
		var raw bytes.Buffer
		validator := &streamWriter{ctx: ctx, cancel: cancel, format: format, validateOnly: true, validated: &raw}
		_, err := io.CopyBuffer(validator, body, make([]byte, 32<<10))
		if err == nil {
			err = validator.validateEnd()
		}
		if err != nil {
			capture.err = err
			cancel()
			return err
		}
		output, err := upstream.AggregateValidated(bytes.NewReader(raw.Bytes()))
		if err == nil {
			err = json.NewEncoder(capture).Encode(output)
		}
		if err != nil {
			capture.err = err
			cancel()
		}
		return err
	}))
	next.ServeHTTP(capture, req)
	if capture.err != nil || ctx.Err() != nil || capture.status < 200 || capture.status >= 300 {
		WriteError(w, errorStatus(capture.status), failureMessage, format)
		return
	}
	var output dto.OpenAITextResponse
	var usage usagePresence
	if validateCompletion(capture.body.Bytes(), format) != nil || json.Unmarshal(capture.body.Bytes(), &output) != nil || usage.observe(capture.body.Bytes()) != nil {
		cancel()
		WriteError(w, 502, failureMessage, format)
		return
	}
	if format == types.RelayFormatClaude {
		output.Model = model
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
func validateCompletion(raw []byte, format types.RelayFormat) error {
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
	for _, key := range []string{"refusal", "reasoning_content"} {
		if raw, ok := msg[key]; ok && string(raw) != "null" {
			var text string
			if json.Unmarshal(raw, &text) != nil || (key == "refusal" && text != "") {
				return errors.New("invalid " + key)
			}
		}
	}
	if raw, ok := msg["function_call"]; ok && string(raw) != "null" {
		var call struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}
		if strictJSON(raw, &call) != nil || call.Name != "" || call.Arguments != "" {
			return errors.New("unsupported legacy function call")
		}
	}
	if raw, ok := msg["content"]; ok && string(raw) != "null" {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return errors.New("invalid content")
		}
	}
	tools, err := decodeToolCalls(msg["tool_calls"])
	if err != nil {
		return err
	}
	var reasoning *string
	_ = json.Unmarshal(msg["reasoning_content"], &reasoning)
	if (len(msg["content"]) == 0 || string(msg["content"]) == "null") && len(tools) == 0 && (reasoning == nil || *reasoning == "") {
		return errors.New("missing message content")
	}
	if c.Finish == "tool_calls" && len(tools) == 0 {
		return errors.New("missing tool calls")
	}
	for _, tool := range tools {
		if strings.TrimSpace(tool.ID) == "" || tool.Type != "function" || tool.Function == nil || strings.TrimSpace(tool.Function.Name) == "" || tool.Function.Arguments == nil {
			return errors.New("incomplete tool call")
		}
		if format == types.RelayFormatClaude {
			var input map[string]any
			if json.Unmarshal([]byte(*tool.Function.Arguments), &input) != nil || input == nil {
				return errors.New("invalid Claude tool input")
			}
		}
	}
	return nil
}

// Preserve required-field presence before RelayKit's permissive tool decoding.
type toolCallFields struct {
	Index    *int   `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function *struct {
		Name      string  `json:"name"`
		Arguments *string `json:"arguments"`
	} `json:"function"`
}

func decodeToolCalls(raw json.RawMessage) ([]*toolCallFields, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var calls []*toolCallFields
	if json.Unmarshal(raw, &calls) != nil {
		return nil, errors.New("invalid tool calls")
	}
	for _, call := range calls {
		if call == nil {
			return nil, errors.New("invalid tool call")
		}
	}
	return calls, nil
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
	var req dto.ClaudeRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, "", false, err
	}
	if err := validateClaudeRequest(raw, &req); err != nil {
		return nil, "", false, err
	}
	return &req, req.Model, req.Stream != nil && *req.Stream, nil
}
