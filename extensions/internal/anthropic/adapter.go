// Package anthropic adapts the supported Messages text subset in-process.
package anthropic

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type conversationKey struct{}

// WithConversation is for authenticated internal callers supplying a validated ID.
// Public request headers never supply this context value.
func WithConversation(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, conversationKey{}, id)
}

type requestData struct {
	Model     *string         `json:"model"`
	MaxTokens *int64          `json:"max_tokens"`
	System    json.RawMessage `json:"system"`
	Stream    json.RawMessage `json:"stream"`
	Messages  []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
}

type completion struct {
	ID      string `json:"id"`
	Error   any    `json:"error"`
	Choices []struct {
		Message *struct {
			Role         string            `json:"role"`
			Content      *string           `json:"content"`
			ToolCalls    []json.RawMessage `json:"tool_calls"`
			Refusal      string            `json:"refusal"`
			FunctionCall json.RawMessage   `json:"function_call"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     *int64 `json:"prompt_tokens"`
		CompletionTokens *int64 `json:"completion_tokens"`
	} `json:"usage"`
}

const failureMessage = "模型调用失败，请稍后重试"

func (out *completion) valid() bool {
	if out.ID == "" || out.Error != nil || len(out.Choices) != 1 {
		return false
	}
	choice := out.Choices[0]
	msg := choice.Message
	if msg == nil || msg.Content == nil || (msg.Role != "" && msg.Role != "assistant") || len(msg.ToolCalls) > 0 || msg.Refusal != "" {
		return false
	}
	if len(msg.FunctionCall) > 0 {
		var call struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}
		if strictJSON(msg.FunctionCall, &call) != nil || call.Name != "" || call.Arguments != "" {
			return false
		}
	}
	if choice.FinishReason != "stop" && choice.FinishReason != "length" {
		return false
	}
	return (out.Usage.PromptTokens == nil || *out.Usage.PromptTokens >= 0) && (out.Usage.CompletionTokens == nil || *out.Usage.CompletionTokens >= 0)
}

func strictJSON(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func textContent(raw json.RawMessage) (string, error) {
	var text *string
	if json.Unmarshal(raw, &text) == nil && text != nil {
		return *text, nil
	}
	var blocks []struct {
		Type string  `json:"type"`
		Text *string `json:"text"`
	}
	if strictJSON(raw, &blocks) != nil || len(blocks) == 0 {
		return "", errors.New("expected text")
	}
	var out strings.Builder
	for _, block := range blocks {
		if block.Type != "text" || block.Text == nil {
			return "", errors.New("expected text block")
		}
		out.WriteString(*block.Text)
	}
	return out.String(), nil
}

func writeError(w http.ResponseWriter, code int, message string) {
	typ := "api_error"
	switch code {
	case 401:
		typ = "authentication_error"
	case 400, 405:
		typ = "invalid_request_error"
	case 413:
		typ = "request_too_large"
	case 429:
		typ = "rate_limit_error"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]string{"type": typ, "message": message}})
}

// New intercepts only /v1/messages; existing OpenAI routes remain untouched.
func New(next http.Handler, apiKey string, maxBodyBytes int64) http.Handler {
	if maxBodyBytes <= 0 {
		maxBodyBytes = 8 << 20
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeError(w, 405, "仅支持 POST 请求")
			return
		}
		expected, supplied := sha256.Sum256([]byte(apiKey)), sha256.Sum256([]byte(r.Header.Get("x-api-key")))
		if apiKey == "" || len(r.Header.Values("x-api-key")) != 1 || subtle.ConstantTimeCompare(expected[:], supplied[:]) != 1 {
			writeError(w, 401, "API Key 无效")
			return
		}
		_, beta := r.Header[http.CanonicalHeaderKey("anthropic-beta")]
		if len(r.Header.Values("anthropic-version")) != 1 || r.Header.Get("anthropic-version") != "2023-06-01" || beta || r.URL.RawQuery != "" || r.URL.ForceQuery || r.Header.Get("Content-Encoding") != "" {
			writeError(w, 400, "版本、beta、查询参数或编码不受支持")
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
		if int64(len(raw)) > maxBodyBytes {
			writeError(w, 413, "请求体超过大小限制")
			return
		}
		var input requestData
		if err != nil || !utf8.Valid(raw) || strictJSON(raw, &input) != nil || input.Model == nil || strings.TrimSpace(*input.Model) == "" || input.MaxTokens == nil || *input.MaxTokens <= 0 || len(input.Messages) == 0 {
			writeError(w, 400, "请求字段无效，仅支持普通文本消息")
			return
		}
		stream := false
		if len(input.Stream) > 0 {
			var value *bool
			if json.Unmarshal(input.Stream, &value) != nil || value == nil {
				writeError(w, 400, "stream 必须是布尔值")
				return
			}
			stream = *value
		}
		messages := make([]message, 0, len(input.Messages)+1)
		if len(input.System) > 0 {
			text, err := textContent(input.System)
			if err != nil {
				writeError(w, 400, "system 仅支持文本")
				return
			}
			messages = append(messages, message{Role: "system", Content: text})
		}
		for _, item := range input.Messages {
			text, err := textContent(item.Content)
			if err != nil || (item.Role != "user" && item.Role != "assistant") {
				writeError(w, 400, "messages 仅支持 user/assistant 文本消息")
				return
			}
			messages = append(messages, message{Role: item.Role, Content: text})
		}
		converted := map[string]any{"model": *input.Model, "max_tokens": *input.MaxTokens, "messages": messages, "stream": stream}
		if id, _ := r.Context().Value(conversationKey{}).(string); id != "" {
			converted["conversationId"] = id
		}
		body, _ := json.Marshal(converted)
		if int64(len(body)) > maxBodyBytes {
			writeError(w, 413, "转换后的请求体超过大小限制")
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		req := r.Clone(ctx)
		req.URL.Path, req.URL.RawPath, req.RequestURI = "/v1/chat/completions", "", "/v1/chat/completions"
		req.Body, req.ContentLength = io.NopCloser(bytes.NewReader(body)), int64(len(body))
		req.GetBody, req.TransferEncoding, req.Trailer = nil, nil, nil
		req.Header = http.Header{"Content-Type": {"application/json"}, "Authorization": {"Bearer " + apiKey}}
		if stream {
			writer := newStreamWriter(w, *input.Model, cancel)
			writer.ctx = ctx
			next.ServeHTTP(writer, req)
			_ = writer.finish()
			return
		}
		capture := &responseBuffer{header: make(http.Header), ctx: ctx, cancel: cancel}
		next.ServeHTTP(capture, req)
		if capture.err != nil {
			writeError(w, 502, failureMessage)
			return
		}
		if capture.status != 200 {
			code := capture.status
			if code < 400 || code > 599 {
				code = 502
			}
			writeError(w, code, failureMessage)
			return
		}
		var output completion
		if !utf8.Valid(capture.body.Bytes()) || json.Unmarshal(capture.body.Bytes(), &output) != nil || !output.valid() {
			writeError(w, 502, failureMessage)
			return
		}
		stop := "end_turn"
		if output.Choices[0].FinishReason == "length" {
			stop = "max_tokens"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": output.ID, "type": "message", "role": "assistant", "model": input.Model,
			"content": []textBlock{{Type: "text", Text: *output.Choices[0].Message.Content}}, "stop_reason": stop, "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": output.Usage.PromptTokens, "output_tokens": output.Usage.CompletionTokens}})
	})
}

type responseBuffer struct {
	header http.Header
	status int
	body   bytes.Buffer
	ctx    context.Context
	cancel context.CancelFunc
	err    error
}

func (w *responseBuffer) Header() http.Header { return w.header }
func (w *responseBuffer) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *responseBuffer) Write(p []byte) (int, error) {
	if w.err == nil {
		w.err = w.ctx.Err()
	}
	if w.err == nil && len(p) > (32<<20)-w.body.Len() {
		w.err = errors.New("response exceeds limit")
		w.cancel()
	}
	if w.err != nil {
		return 0, w.err
	}
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.body.Write(p)
}
