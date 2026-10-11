package server

import (
    "context"
    "io"
    "net/http"
    "net/http/httptest"
    "os"
    "strings"
    "testing"
    "time"

    "workbuddy2api/internal/auth"
    "workbuddy2api/internal/upstream"
)

func TestRealProbeRawCapture(t *testing.T) {
    accounts, err := auth.LoadDir(os.Getenv("PROBE_AUTH_DIR"))
    if err != nil || len(accounts) != 1 { t.Fatal("expected one test account snapshot") }
    client := upstream.New()
    client.SanitizeFingerprints = true
    client.ClientName = "WorkBuddy"
    handler := NewHandler(Config{Pool: testPoolWith(accounts[0]), Upstream: client, APIKey: "probe-local-only"})
    ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
    defer cancel()
    captured := false
    ctx = upstream.WithRawResponse(ctx, func(_ http.ResponseWriter, body io.Reader) error {
        raw, err := io.ReadAll(io.LimitReader(body, 4<<20))
        if err != nil { return err }
        captured = true
        return os.WriteFile(os.Getenv("PROBE_CAPTURE_FILE"), raw, 0600)
    })
    body := `{"model":"deepseek-v4.1-flash","stream":true,"max_tokens":256,"messages":[{"role":"user","content":"只输出 WB2A_OK"}]}`
    if file := os.Getenv("PROBE_REQUEST_FILE"); file != "" {
        data, err := os.ReadFile(file)
        if err != nil { t.Fatal("probe request unavailable") }
        body = string(data)
    }
    request := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)).WithContext(ctx)
    request.Header.Set("Authorization", "Bearer probe-local-only")
    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, request)
    if !captured { t.Fatalf("capture missing, status=%d", recorder.Code) }
}
