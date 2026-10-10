// Package anthropic authenticates Messages requests and uses the shared converter.
package anthropic

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"

	"github.com/QuantumNous/new-api/relaykit/types"
	"workbuddy2api/internal/protocol"
)

type conversationKey struct{}

// WithConversation accepts a validated ID from authenticated internal callers only.
func WithConversation(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, conversationKey{}, id)
}

func writeError(w http.ResponseWriter, code int, message string) {
	protocol.WriteError(w, code, message, types.RelayFormatClaude)
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
		if len(r.Header.Values("anthropic-version")) != 1 || r.Header.Get("anthropic-version") != "2023-06-01" {
			writeError(w, 400, "anthropic-version 必须恰为 2023-06-01，且只能出现一次")
			return
		}
		if _, beta := r.Header[http.CanonicalHeaderKey("anthropic-beta")]; beta {
			writeError(w, 400, "anthropic-beta 请求头不受支持：本接口采用 anthropic-version 2023-06-01 的协议转换，出现该头即拒绝。无条件附带 anthropic-beta 的客户端需先在客户端侧移除")
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			writeError(w, 400, "不支持查询参数")
			return
		}
		if r.Header.Get("Content-Encoding") != "" {
			writeError(w, 400, "不支持 Content-Encoding 压缩请求体")
			return
		}
		id, _ := r.Context().Value(conversationKey{}).(string)
		protocol.Serve(w, r, next, apiKey, maxBodyBytes, types.RelayFormatClaude, id)
	})
}
