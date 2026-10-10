package protocol

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
)

func usageOf(t *testing.T, body string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(body), &value); err != nil {
		t.Fatal(err)
	}
	u, ok := value["usage"].(map[string]any)
	if !ok {
		t.Fatalf("missing usage: %s", body)
	}
	return u
}
func TestUnknownUsageRemainsNull(t *testing.T) {
	for _, f := range formats {
		t.Run(string(f), func(t *testing.T) {
			w := run(f, false, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, goodResponse) })
			u := usageOf(t, w.Body.String())
			for _, key := range []string{"input_tokens", "output_tokens"} {
				if v, ok := u[key]; !ok || v != nil {
					t.Errorf("%s=%v present=%v", key, v, ok)
				}
			}
			if f == types.RelayFormatOpenAIResponses {
				if v, ok := u["total_tokens"]; !ok || v != nil {
					t.Errorf("total=%v present=%v", v, ok)
				}
			}
		})
	}
}
func TestNonStreamingUsagePresence(t *testing.T) {
	for _, tc := range []struct {
		usage          string
		in, out, total any
	}{
		{`{"prompt_tokens":0,"completion_tokens":2,"total_tokens":0}`, float64(0), float64(2), float64(0)},
		{`{"prompt_tokens":3}`, float64(3), nil, nil},
		{`{"completion_tokens":2}`, nil, float64(2), nil},
		{`{"prompt_tokens":3,"completion_tokens":2}`, float64(3), float64(2), float64(5)},
	} {
		for _, f := range formats {
			w := run(f, false, func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, strings.TrimSuffix(goodResponse, "}")+`,"usage":`+tc.usage+`}`)
			})
			u := usageOf(t, w.Body.String())
			if u["input_tokens"] != tc.in || u["output_tokens"] != tc.out || (f == types.RelayFormatOpenAIResponses && u["total_tokens"] != tc.total) {
				t.Errorf("%s %s: %v", f, tc.usage, u)
			}
		}
	}
}
func TestNonStreamingErrorIsSanitized(t *testing.T) {
	for _, f := range formats {
		for _, tc := range []struct {
			status int
			body   string
		}{
			{500, `{"error":{"message":"secret account token"}}`}, {200, `{"error":{"message":"secret account token"}}`}, {200, `null`}, {200, `{"choices":[]}`}, {200, strings.Replace(goodResponse, `"stop"`, `"unknown"`, 1)}, {200, strings.TrimSuffix(goodResponse, "}") + `,"usage":{"prompt_tokens":-1}}`}, {200, strings.Replace(goodResponse, `"message":{"role":"assistant","content":"hello"}`, `"message":null`, 1)},
		} {
			w := run(f, false, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Secret", "secret")
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			if w.Code < 400 || strings.Contains(w.Body.String(), "secret") || w.Header().Get("X-Secret") != "" {
				t.Errorf("%s invalid response accepted/leaked: %d %s", f, w.Code, w.Body.String())
			}
		}
	}
}
func TestJSONResponseLimitCancelsNext(t *testing.T) {
	for _, f := range formats {
		w := run(f, false, func(w http.ResponseWriter, r *http.Request) {
			_, err := io.WriteString(w, strings.Repeat("x", 32<<20+1))
			if err == nil || r.Context().Err() == nil {
				t.Error("oversized response did not cancel")
			}
		})
		if w.Code != 502 {
			t.Errorf("status=%d", w.Code)
		}
	}
}
