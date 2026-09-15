package main

import (
	"strings"
	"testing"
)

func TestEnvironmentConfig(t *testing.T) {
	for key, value := range map[string]string{"WB2A_CORE_URL": "http://core:7863", "WB2A_ADMIN_KEY": strings.Repeat("a", 32), "WB2A_API_KEY": "existing-api-key", "WB2A_BRIDGE_KEY": strings.Repeat("b", 32), "WB2A_PUBLIC_ORIGIN": "https://gateway.test", "WB2A_LISTEN": ":17863"} {
		t.Setenv(key, value)
	}
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
