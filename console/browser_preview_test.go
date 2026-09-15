package main

// Optional browser fixture migrated from internal/server/browser_preview_test.go.
// No real accounts, containers, credentials, or upstream traffic.
// WB2A_BROWSER_PREVIEW=1 go -C console test -run TestAdminBrowserPreview -v -timeout 20m
import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdminBrowserPreview(t *testing.T) {
	if os.Getenv("WB2A_BROWSER_PREVIEW") != "1" {
		t.Skip("opt-in browser fixture")
	}
	var inFlight atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/internal/") && r.Header.Get("Authorization") != "Bearer "+strings.Repeat("b", 32) {
			w.WriteHeader(401)
			return
		}
		if mockInfo(w, r) {
			return
		}
		switch r.URL.Path {
		case "/internal/v1/status":
			fmt.Fprintf(w, `{"accounts":[{"uid":"demo-account","nickname":"演示账号（模拟）","realm":"cn","credits":1200,"credits_known":true,"in_flight":%d,"success_count":2,"err_total":0,"disabled":false,"cooling":false}],"total":1,"healthy":1,"cooling":0,"disabled":0,"in_flight_full":0,"realm_totals":{"cn":{"total":1,"healthy":1,"cooling":0,"disabled":0,"in_flight_full":0},"global":{"total":0,"healthy":0,"cooling":0,"disabled":0,"in_flight_full":0}},"sticky_sessions":0,"redis_mode":"noop"}`, inFlight.Load())
		case "/internal/v1/models", "/v1/models":
			fmt.Fprint(w, `{"object":"list","data":[{"id":"cn:glm-5.2","object":"model","reasoning_supported_efforts":["low","high"]},{"id":"global:claude-sonnet-4.6","object":"model"}]}`)
		case "/internal/v1/chat":
			inFlight.Add(1)
			defer inFlight.Add(-1)
			w.Header().Set("Content-Type", "text/event-stream")
			for _, part := range []string{"你好！", "这是控制台的模拟回答。", "流式显示和页面交互已连通。"} {
				fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q}}]}\n\n", part)
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
					t.Log("mock chat canceled: browser disconnect propagated to core")
					return
				case <-time.After(3 * time.Second):
				}
			}
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":20,\"total_tokens\":25}}\n\ndata: [DONE]\n\n")
		default:
			if strings.HasPrefix(r.URL.Path, "/internal/v1/owners/") {
				fmt.Fprint(w, `{"ok":true}`)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/internal/v1/oauth") {
				w.WriteHeader(503)
				fmt.Fprint(w, `{"error":"浏览器夹具不发起真实授权；OAuth 由自动测试验证"}`)
				return
			}
			w.WriteHeader(404)
			fmt.Fprint(w, `{"error":"mock route not found"}`)
		}
	}))
	defer ts.Close()
	cfg := testConfig(ts.URL)
	cfg.AdminKey = "browser-preview-key-mock-only-12345"
	cfg.APIKey = "preview-api-key-mock-only"
	h, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:17864")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())
	t.Log("Browser fixture: http://127.0.0.1:17864 · admin key: browser-preview-key-mock-only-12345 (mock only)")
	<-time.After(15 * time.Minute)
}
