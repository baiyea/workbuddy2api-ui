// Package responses adapts stateless Responses creation in-process.
package responses

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"

	"github.com/QuantumNous/new-api/relaykit/types"
	"workbuddy2api/internal/protocol"
)

// New intercepts only /v1/responses and reuses the existing Chat handler.
func New(next http.Handler, apiKey string, maxBodyBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			next.ServeHTTP(w, r)
			return
		}
		const format types.RelayFormat = types.RelayFormatOpenAIResponses
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			protocol.WriteError(w, 405, "仅支持 POST 请求", format)
			return
		}
		expected := sha256.Sum256([]byte("Bearer " + apiKey))
		supplied := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		if apiKey == "" || len(r.Header.Values("Authorization")) != 1 || subtle.ConstantTimeCompare(expected[:], supplied[:]) != 1 {
			protocol.WriteError(w, 401, "API Key 无效", format)
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			protocol.WriteError(w, 400, "不支持查询参数", format)
			return
		}
		if r.Header.Get("Content-Encoding") != "" {
			protocol.WriteError(w, 400, "不支持 Content-Encoding 压缩请求体", format)
			return
		}
		protocol.Serve(w, r, next, apiKey, maxBodyBytes, format, "")
	})
}
