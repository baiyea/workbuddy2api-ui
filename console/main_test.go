package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadDeploymentKeysAppliesOverridesWithoutWritingKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	original := []byte(`{"admin_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","api_key":"base-api","bridge_key":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	keys, err := readDeploymentKeys(path, time.Second, strings.Repeat("c", 32), "short-api")
	if err != nil {
		t.Fatal(err)
	}
	if keys.AdminKey != strings.Repeat("c", 32) || keys.APIKey != "short-api" || keys.BridgeKey != strings.Repeat("b", 32) {
		t.Fatal("runtime overrides did not produce the expected effective keys")
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != string(original) {
		t.Fatalf("console changed the read-only key file: %v", err)
	}
}

func TestReadDeploymentKeysWaitIsBoundedAndRejectsCorruption(t *testing.T) {
	start := time.Now()
	if _, err := readDeploymentKeys(filepath.Join(t.TempDir(), "missing.json"), 40*time.Millisecond, "", ""); err == nil {
		t.Fatal("missing key file accepted")
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond || elapsed > time.Second {
		t.Fatalf("unexpected wait duration: %v", elapsed)
	}
	path := filepath.Join(t.TempDir(), "keys.json")
	if err := os.WriteFile(path, []byte(`{broken`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readDeploymentKeys(path, time.Second, "", ""); err == nil {
		t.Fatal("corrupt key file accepted")
	}
	oversized := append([]byte(`{"admin_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","api_key":"api","bridge_key":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`), bytes.Repeat([]byte(" "), 5000)...)
	if err := os.WriteFile(path, oversized, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readDeploymentKeys(path, time.Second, "", ""); err == nil {
		t.Fatal("oversized key file accepted")
	}
}

func TestEnvironmentConfig(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "keys.json")
	if err := os.WriteFile(keyPath, []byte(`{"admin_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","api_key":"base-api","bridge_key":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"WB2A_CORE_URL": "http://core:7863", "WB2A_ADMIN_KEY": strings.Repeat("a", 32), "WB2A_API_KEY": "existing-api-key", "WB2A_BRIDGE_KEY": strings.Repeat("b", 32), "WB2A_PUBLIC_ORIGIN": "https://gateway.test", "WB2A_LISTEN": ":17863"} {
		t.Setenv(key, value)
	}
	t.Setenv("WB2A_KEY_FILE", keyPath)
	cfg, listen, err := configFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if listen != ":17863" || cfg.CoreURL.String() != "http://core:7863" || cfg.APIKey != "existing-api-key" || cfg.PublicOrigin != "https://gateway.test" {
		t.Fatalf("configuration not preserved: listen=%s", listen)
	}
	if _, err := NewServer(cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WB2A_CORE_URL", "://bad")
	if _, _, err := configFromEnv(); err == nil {
		t.Fatal("invalid URL accepted")
	}
	t.Setenv("WB2A_CORE_URL", "")
	t.Setenv("WB2A_LISTEN", "")
	cfg, listen, err = configFromEnv()
	if err != nil || listen != ":7863" || cfg.CoreURL.String() != "http://core:7863" {
		t.Fatal("defaults", listen, err)
	}
}

func TestEnvironmentConfigWithoutKeyFilePreservesSourceMode(t *testing.T) {
	t.Setenv("WB2A_KEY_FILE", "")
	t.Setenv("WB2A_ADMIN_KEY", strings.Repeat("a", 32))
	t.Setenv("WB2A_API_KEY", "source-api")
	t.Setenv("WB2A_BRIDGE_KEY", strings.Repeat("b", 32))
	cfg, _, err := configFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdminKey != strings.Repeat("a", 32) || cfg.APIKey != "source-api" || cfg.BridgeKey != strings.Repeat("b", 32) {
		t.Fatalf("source environment mode changed: %+v", cfg)
	}
}
