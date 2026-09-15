package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"workbuddy2api/internal/bridge"
	"workbuddy2api/internal/pool"
	"workbuddy2api/internal/scheduler"
	"workbuddy2api/internal/upstream"
)

// Build metadata is supplied by the overlay image build using -ldflags -X.
var upstreamCommit, patchIdentity string

func coreBridgeKey(cfg *Config) (string, error) {
	key := os.Getenv("WB2A_BRIDGE_KEY")
	if key == "" {
		return "", nil
	}
	if len(key) < 32 || strings.TrimSpace(key) != key || key == cfg.APIKey || key == strings.TrimSpace(os.Getenv("WB2A_ADMIN_KEY")) {
		return "", errors.New("桥接密钥至少需要 32 个字符，且必须与 API 和管理密钥不同")
	}
	return key, nil
}

// initializeCore runs before auth.LoadDir. The deployment credential lifecycle
// extends this boundary; source mode remains opt-in through WB2A_BRIDGE_KEY.
func initializeCore(cfg *Config) error {
	key, err := coreBridgeKey(cfg)
	if err != nil || key == "" {
		return err
	}
	for _, dir := range []string{cfg.AuthDir, filepath.Dir(cfg.StateFile)} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	return nil
}

func wrapCore(ctx context.Context, cfg *Config, p *pool.Pool, up *upstream.Client, sch *scheduler.Scheduler, public http.Handler) (http.Handler, error) {
	key, err := coreBridgeKey(cfg)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return public, nil
	}
	internal := bridge.New(ctx, bridge.Config{Key: key, APIKey: cfg.APIKey, AuthDir: cfg.AuthDir, UpstreamCommit: upstreamCommit, PatchIdentity: patchIdentity, GlobalEnabled: cfg.Global.Enabled, Pool: p, Upstream: up, Scheduler: sch, Public: public})
	mux := http.NewServeMux()
	mux.Handle("/internal/", internal)
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{\"service\":\"workbuddy2api\",\"status\":\"running\"}\n"))
	})
	mux.Handle("/", public)
	return mux, nil
}
