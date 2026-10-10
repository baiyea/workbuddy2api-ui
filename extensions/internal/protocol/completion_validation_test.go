package protocol

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
)

func TestCompletionRetainsUnsupportedOutputErrors(t *testing.T) {
	for _, f := range formats {
		for _, content := range []string{`"content":null`, `"content":"hello","function_call":{"name":"exec","arguments":"{}"}`, `"content":"hello","refusal":"private refusal"`, `"content":"hello","reasoning_content":{}`} {
			t.Run(string(f)+content, func(t *testing.T) {
				body := strings.Replace(goodResponse, `"content":"hello"`, content, 1)
				w := run(f, false, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
				if w.Code != 502 || strings.Contains(w.Body.String(), "private") {
					t.Fatalf("status=%d body=%s", w.Code, w.Body)
				}
			})
		}
	}
}

func TestClaudeResponsePreservesRequestedModel(t *testing.T) {
	w := run(types.RelayFormatClaude, false, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.Replace(goodResponse, `"model":"test"`, `"model":"upstream-alias"`, 1))
	})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"model":"test"`) {
		t.Fatalf("response=%s", w.Body)
	}
}
