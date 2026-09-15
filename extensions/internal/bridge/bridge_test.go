package bridge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBridgeRejectsWrongKey(t *testing.T) {
	h := New(context.Background(), Config{Key: strings.Repeat("b", 32)})
	req := httptest.NewRequest("GET", "/internal/v1/info", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", w.Code)
	}
}

type unreadBody struct{ t *testing.T }

func (b unreadBody) Read([]byte) (int, error) { b.t.Fatal("unauthorized body read"); return 0, nil }
func (b unreadBody) Close() error             { return nil }

func TestBridgeFailsClosedBeforeReadingBody(t *testing.T) {
	for _, key := range []string{"", "short", testKey} {
		h := New(context.Background(), Config{Key: key})
		req := httptest.NewRequest("POST", "/internal/v1/oauth", nil)
		req.Body = unreadBody{t}
		req.Header.Set("Authorization", "Bearer wrong")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatalf("key length %d: status=%d", len(key), w.Code)
		}
	}
	h := New(context.Background(), Config{})
	req := httptest.NewRequest("GET", "/internal/v1/info", nil)
	req.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("empty key accepted: %d", w.Code)
	}
}

func TestBridgeRoutesAndPublicCredentialIsolation(t *testing.T) {
	public := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer core-api" {
			t.Error("core API credential not substituted")
		}
		if r.Header.Get("X-Console-Owner") != "" {
			t.Error("owner forwarded upstream")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(r.URL.Path))
	})
	h := New(context.Background(), Config{Key: testKey, APIKey: "core-api", Public: public, UpstreamCommit: "locked", PatchIdentity: "overlay", GlobalEnabled: true})
	for _, tc := range []struct{ method, path, want string }{
		{"GET", "models", "/v1/models"}, {"POST", "chat", "/v1/chat/completions"}, {"GET", "status", "/status"},
	} {
		r := httptest.NewRequest(tc.method, "/internal/v1/"+tc.path, nil)
		r.Header.Set("Authorization", "Bearer "+testKey)
		r.Header.Set("X-Console-Owner", strings.Repeat("o", 32))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || w.Body.String() != tc.want {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body)
		}
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			t.Fatal("mutated incoming request")
		}
	}
	info := bridgeRequest(h, "GET", "/internal/v1/info", "", "")
	for _, want := range []string{`"protocol":1`, `"upstream_commit":"locked"`, `"patch_identity":"overlay"`, `"global_enabled":true`} {
		if !strings.Contains(info.Body.String(), want) {
			t.Fatalf("info: %s", info.Body)
		}
	}
	for _, path := range []string{"/internal/v1/private", "/internal/v1/info/extra", "/v1/models", "/internal/v2/info"} {
		rr := bridgeRequest(h, "GET", path, "", "")
		if rr.Code != 404 || !strings.Contains(rr.Body.String(), `"error"`) {
			t.Fatalf("%s: %d %s", path, rr.Code, rr.Body)
		}
	}
}
