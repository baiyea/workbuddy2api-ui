package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"workbuddy2api/internal/anthropic"
	"workbuddy2api/internal/bridge"
	"workbuddy2api/internal/durablefs"
	"workbuddy2api/internal/pool"
	"workbuddy2api/internal/scheduler"
	"workbuddy2api/internal/taskrun"
	"workbuddy2api/internal/upstream"
)

// Build metadata is supplied by the overlay image build using -ldflags -X.
var upstreamCommit, patchIdentity string

type deploymentKeys struct {
	AdminKey  string `json:"admin_key"`
	APIKey    string `json:"api_key"`
	BridgeKey string `json:"bridge_key"`
}

type legacyKeys struct {
	AdminKey string `json:"admin_key"`
	APIKey   string `json:"api_key"`
}

func validateKeys(keys deploymentKeys) error {
	if len(keys.AdminKey) < 32 || keys.APIKey == "" || len(keys.BridgeKey) < 32 || keys.AdminKey == keys.APIKey || keys.AdminKey == keys.BridgeKey || keys.APIKey == keys.BridgeKey {
		return errors.New("管理和桥接密钥须至少 32 字节，API Key 须非空，三种密钥须互不相同")
	}
	return nil
}

func decodeKeys(path string, out any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return fmt.Errorf("key file is not regular: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("key file contains trailing data")
	}
	return nil
}

func randomKey() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func effectiveKeys(base deploymentKeys, adminOverride, apiOverride string) (deploymentKeys, error) {
	effective := base
	if adminOverride != "" {
		effective.AdminKey = adminOverride
	}
	if apiOverride != "" {
		effective.APIKey = apiOverride
	}
	return effective, validateKeys(effective)
}

func writeKeys(path string, keys deploymentKeys) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".keys-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		err = json.NewEncoder(f).Encode(keys)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return durablefs.Publish(tmp, path, false)
}

func initializeKeys(dataDir, keyDir, adminOverride, apiOverride string) (deploymentKeys, error) {
	for _, dir := range []string{dataDir, keyDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return deploymentKeys{}, err
		}
	}
	if err := os.Chmod(keyDir, 0700); err != nil {
		return deploymentKeys{}, err
	}
	keyPath := filepath.Join(keyDir, "keys.json")
	var base deploymentKeys
	targetErr := decodeKeys(keyPath, &base)
	if targetErr != nil && !errors.Is(targetErr, os.ErrNotExist) {
		return deploymentKeys{}, errors.New("keys.json 损坏，请恢复备份；不会自动更换现有密钥")
	}
	var old legacyKeys
	legacyPath := filepath.Join(dataDir, "console-keys.json")
	legacyErr := decodeKeys(legacyPath, &old)
	if legacyErr != nil && !errors.Is(legacyErr, os.ErrNotExist) {
		return deploymentKeys{}, errors.New("console-keys.json 损坏，请恢复备份；不会自动更换现有密钥")
	}
	if targetErr == nil {
		if err := validateKeys(base); err != nil {
			return deploymentKeys{}, errors.New("keys.json 损坏，请恢复备份；不会自动更换现有密钥")
		}
		if legacyErr == nil && (old.AdminKey != base.AdminKey || old.APIKey != base.APIKey) {
			return deploymentKeys{}, errors.New("旧密钥与部署密钥冲突；不会自动覆盖")
		}
		return effectiveKeys(base, adminOverride, apiOverride)
	}
	if legacyErr == nil {
		base.AdminKey, base.APIKey = old.AdminKey, old.APIKey
		if len(old.AdminKey) < 32 || old.APIKey == "" || old.AdminKey == old.APIKey {
			return deploymentKeys{}, errors.New("console-keys.json 损坏，请恢复备份；不会自动更换现有密钥")
		}
	} else {
		var err error
		if base.AdminKey, err = randomKey(); err != nil {
			return deploymentKeys{}, err
		}
		if base.APIKey, err = randomKey(); err != nil {
			return deploymentKeys{}, err
		}
	}
	var err error
	if base.BridgeKey, err = randomKey(); err != nil {
		return deploymentKeys{}, err
	}
	if err := validateKeys(base); err != nil {
		return deploymentKeys{}, err
	}
	effective, err := effectiveKeys(base, adminOverride, apiOverride)
	if err != nil {
		return deploymentKeys{}, err
	}
	if err := writeKeys(keyPath, base); err != nil {
		return deploymentKeys{}, err
	}
	return effective, nil
}

func keyDir() string {
	if value := os.Getenv("WB2A_KEY_DIR"); value != "" {
		return value
	}
	return "/run/wb2a"
}

func coreBridgeKey(cfg *Config) (string, error) {
	if os.Getenv("WB2A_CORE") == "true" {
		var base deploymentKeys
		if err := decodeKeys(filepath.Join(keyDir(), "keys.json"), &base); err != nil {
			return "", err
		}
		keys, err := effectiveKeys(base, os.Getenv("WB2A_ADMIN_KEY"), os.Getenv("WB2A_API_KEY"))
		if err != nil {
			return "", err
		}
		if cfg.APIKey != keys.APIKey {
			return "", errors.New("core API Key 与部署密钥不一致")
		}
		return keys.BridgeKey, nil
	}
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
	if os.Getenv("WB2A_CORE") == "true" {
		dataDir, deploymentDir := filepath.Dir(cfg.StateFile), keyDir()
		apiOverride := os.Getenv("WB2A_API_KEY")
		if apiOverride == "" && cfg.APIKey != "" {
			var target deploymentKeys
			err := decodeKeys(filepath.Join(deploymentDir, "keys.json"), &target)
			baseAPI := target.APIKey
			if errors.Is(err, os.ErrNotExist) {
				var old legacyKeys
				err = decodeKeys(filepath.Join(dataDir, "console-keys.json"), &old)
				baseAPI = old.APIKey
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return errors.New("部署密钥文件损坏，请恢复备份")
			}
			if err != nil || cfg.APIKey != baseAPI {
				return errors.New("Docker 双服务不能只使用 config.json 的 api_key；请通过共享 WB2A_API_KEY 设置")
			}
		}
		keys, err := initializeKeys(dataDir, deploymentDir, os.Getenv("WB2A_ADMIN_KEY"), apiOverride)
		if err != nil {
			return err
		}
		cfg.APIKey = keys.APIKey
	}
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

// The callback is installed before scheduler.New; the local Runner is assigned
// before scheduler.Run starts. A broken history must never fall back to dispatch.
func coreTaskSchedule(tasks **taskrun.Runner) func(context.Context, string, time.Time) {
	if os.Getenv("WB2A_CORE") != "true" && os.Getenv("WB2A_BRIDGE_KEY") == "" {
		return nil
	}
	return func(ctx context.Context, id string, at time.Time) {
		if *tasks == nil {
			log.Print("task_schedule_storage_unavailable")
			return
		}
		(*tasks).Scheduled(ctx, id, at)
	}
}

func newCoreTasks(ctx context.Context, cfg *Config, sch *scheduler.Scheduler) (*taskrun.Runner, *taskrun.Store, error) {
	key, err := coreBridgeKey(cfg)
	if err != nil || key == "" {
		return nil, nil, err
	}
	history, err := taskrun.OpenStore(filepath.Join(filepath.Dir(cfg.StateFile), "tasks", "runs.json"), time.Now())
	if err != nil {
		log.Print("task_history_unavailable")
		return nil, nil, err
	}
	return taskrun.NewRunner(ctx, history, sch.TaskCatalog, sch.ExecuteTask), history, nil
}

func wrapCore(ctx context.Context, cfg *Config, p *pool.Pool, up *upstream.Client, sch *scheduler.Scheduler, public http.Handler, tasks *taskrun.Runner, history *taskrun.Store, taskError error) (http.Handler, error) {
	key, err := coreBridgeKey(cfg)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return public, nil
	}
	public = anthropic.New(public, cfg.APIKey, int64(cfg.Server.MaxBodyMB)<<20)
	internal := bridge.New(ctx, bridge.Config{Key: key, APIKey: cfg.APIKey, MaxBodyBytes: int64(cfg.Server.MaxBodyMB) << 20, AuthDir: cfg.AuthDir, UpstreamCommit: upstreamCommit, PatchIdentity: patchIdentity, GlobalEnabled: cfg.Global.Enabled, Pool: p, Upstream: up, Scheduler: sch, Tasks: tasks, History: history, TaskError: taskError, Public: public})
	mux := http.NewServeMux()
	mux.Handle("/internal/", internal)
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{\"service\":\"workbuddy2api\",\"status\":\"running\"}\n"))
	})
	mux.Handle("/", public)
	return mux, nil
}
