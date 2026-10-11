// Diagnostic overlay for internal/protocol; never included in production.
package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func TestGeminiCapturedExtraFields(t *testing.T) {
	raw, err := os.ReadFile(os.Getenv("PROBE_CAPTURE_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	var control bytes.Buffer
	found := false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "data: ") && line != "data: [DONE]" {
			var chunk map[string]json.RawMessage
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk); err != nil {
				t.Fatal(err)
			}
			var choices []map[string]json.RawMessage
			if err := json.Unmarshal(chunk["choices"], &choices); err != nil {
				t.Fatal(err)
			}
			for _, choice := range choices {
				var delta map[string]json.RawMessage
				if err := json.Unmarshal(choice["delta"], &delta); err != nil {
					t.Fatal(err)
				}
				if extra := delta["extra_fields"]; len(extra) > 0 && string(extra) != "null" {
					found = true
					normalized, err := normalizeEmptyDelta(choice["delta"])
					if err != nil {
						t.Fatal(err)
					}
					var decoded dto.ChatCompletionsStreamResponseChoiceDelta
					err = strictJSON(normalized, &decoded)
					if err == nil || err.Error() != `json: unknown field "extra_fields"` {
						t.Fatalf("unexpected rejection: %v", err)
					}
					t.Logf("captured delta rejected: %v", err)
					// Offline counterfactual only: this does not establish safe signature loss.
					delete(delta, "extra_fields")
					choice["delta"], _ = json.Marshal(delta)
				}
			}
			chunk["choices"], _ = json.Marshal(choices)
			encoded, _ := json.Marshal(chunk)
			line = "data: " + string(encoded)
		}
		control.WriteString(line + "\n")
	}
	if !found {
		t.Fatal("no nonempty extra_fields captured")
	}
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatClaude} {
		for _, sample := range []struct {
			name     string
			raw      []byte
			rejected bool
		}{{"captured", raw, true}, {"offline_without_extra_fields", control.Bytes(), false}} {
			ctx, cancel := context.WithCancel(context.Background())
			s := &streamWriter{ctx: ctx, cancel: cancel, format: format, validateOnly: true}
			_, err := s.Write(sample.raw)
			if err == nil {
				err = s.validateEnd()
			}
			cancel()
			if sample.rejected && (err == nil || err.Error() != "invalid delta") {
				t.Fatalf("%s %s: expected invalid delta, got %v", format, sample.name, err)
			}
			if !sample.rejected && err != nil {
				t.Fatalf("%s %s: %v", format, sample.name, err)
			}
			t.Logf("%s %s: %v", format, sample.name, err)
		}
	}
}
