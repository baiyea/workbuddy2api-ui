package protocol

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
)

func TestResponsesPlaintextReasoningValidation(t *testing.T) {
	for _, summary := range []string{`[]`, `[{"type":"summary_text","text":""}]`, `[{"type":"summary_text","text":"think"}]`} {
		w := httptest.NewRecorder()
		called := false
		Serve(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"model":"test","input":[{"type":"reasoning","id":"rs_1","status":"completed","summary":`+summary+`}]}`)), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; io.WriteString(w, goodResponse) }), "key", 1<<20, types.RelayFormatOpenAIResponses, "")
		if !called || w.Code != 200 {
			t.Errorf("valid summary rejected: %s %d %s", summary, w.Code, w.Body)
		}
	}
}

func TestResponsesOpaqueAndMalformedReasoningRejectBeforeNext(t *testing.T) {
	for _, item := range []struct{ body, path string }{
		{`"summary":[],"encrypted_content":"secret"`, ".encrypted_content"},
		{`"summary":[],"encrypted_content":null`, ".encrypted_content"},
		{`"summary":[],"content":[]`, ".content"},
		{`"summary":[],"signature":"secret"`, ".signature"},
		{`"summary":[],"opaque":"secret"`, ".opaque"},
		{`"summary":[],"role":"assistant"`, ".role"},
		{`"summary":[],"quality":"high"`, ".quality"},
		{`"summary":[],"size":null`, ".size"},
		{`"summary":null`, ".summary"},
		{`"summary":"think"`, ".summary"},
		{`"summary":[null]`, ".summary[0]"},
		{`"summary":[{"type":"text","text":"think"}]`, ".summary[0]"},
		{`"summary":[{"type":"summary_text","text":null}]`, ".summary[0]"},
		{`"summary":[{"type":"summary_text","text":"think","opaque":"secret"}]`, ".summary[0].opaque"},
	} {
		w := httptest.NewRecorder()
		Serve(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"model":"test","input":[{"type":"reasoning",`+item.body+`}]}`)), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("invalid reasoning reached next") }), "key", 1<<20, types.RelayFormatOpenAIResponses, "")
		if w.Code != 400 || !strings.Contains(w.Body.String(), "input[0]"+item.path) {
			t.Errorf("missing path %s: %d %s", item.path, w.Code, w.Body)
		}
	}
}
