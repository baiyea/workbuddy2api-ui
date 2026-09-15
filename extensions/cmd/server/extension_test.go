package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workbuddy2api/internal/pool"
	"workbuddy2api/internal/server"
	"workbuddy2api/internal/upstream"
)

func TestCoreSourceModeRemainsUnchanged(t *testing.T) {
	t.Setenv("WB2A_BRIDGE_KEY", "")
	cfg := &Config{AuthDir: filepath.Join(t.TempDir(), "absent")}
	public := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(418) })
	if err := initializeCore(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg.AuthDir); !os.IsNotExist(err) {
		t.Fatal("source mode initialized directory")
	}
	h, err := wrapCore(context.Background(), cfg, nil, nil, nil, public)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/livez", nil))
	if rr.Code != 418 {
		t.Fatal("source handler changed")
	}
}

func TestCoreRejectsInvalidBridgeKeysBeforeInitialization(t *testing.T) {
	for _, tc := range []struct{ key, api, admin string }{
		{"short", "api", "admin"},
		{strings.Repeat("b", 32), strings.Repeat("b", 32), "admin"},
		{strings.Repeat("b", 32), "api", strings.Repeat("b", 32)},
	} {
		t.Run(tc.key, func(t *testing.T) {
			t.Setenv("WB2A_BRIDGE_KEY", tc.key)
			t.Setenv("WB2A_ADMIN_KEY", tc.admin)
			cfg := &Config{APIKey: tc.api, AuthDir: filepath.Join(t.TempDir(), "auth"), StateFile: filepath.Join(t.TempDir(), "data", "pool.json")}
			if initializeCore(cfg) == nil {
				t.Fatal("invalid bridge key accepted")
			}
			if _, err := os.Stat(cfg.AuthDir); !os.IsNotExist(err) {
				t.Fatal("initialized before validating")
			}
			if _, err := wrapCore(context.Background(), cfg, nil, nil, nil, http.NotFoundHandler()); err == nil {
				t.Fatal("wrapper accepted invalid bridge key")
			}
		})
	}
}

func TestCoreInitializesEarlyAndSeparatesLivenessAndPublicAuth(t *testing.T) {
	t.Setenv("WB2A_BRIDGE_KEY", strings.Repeat("b", 32))
	t.Setenv("WB2A_ADMIN_KEY", strings.Repeat("a", 32))
	cfg := &Config{APIKey: "public-api", AuthDir: filepath.Join(t.TempDir(), "auth"), StateFile: filepath.Join(t.TempDir(), "data", "pool.json")}
	if err := initializeCore(cfg); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{cfg.AuthDir, filepath.Dir(cfg.StateFile)} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("directory not initialized: %s", dir)
		}
	}
	p := pool.New("")
	defer p.Close()
	up := upstream.New()
	public := server.NewHandler(server.Config{Pool: p, Upstream: up, APIKey: cfg.APIKey})
	h, err := wrapCore(context.Background(), cfg, p, up, nil, public)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, key string
		code      int
	}{
		{"/livez", "", 200}, {"/healthz", "", 503}, {"/status", "", 401},
		{"/v1/models", strings.Repeat("b", 32), 401},
		{"/internal/v1/status", strings.Repeat("b", 32), 200},
		{"/internal/v1/info", "public-api", 401},
	} {
		req := httptest.NewRequest("GET", tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+tc.key)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != tc.code {
			t.Fatalf("%s: got %d want %d: %s", tc.path, rr.Code, tc.code, rr.Body)
		}
	}
}
