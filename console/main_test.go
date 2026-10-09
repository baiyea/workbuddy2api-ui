package main

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConsoleStartupLogsOnlyEffectiveAdminKey(t *testing.T) {
	for _, override := range []string{"", strings.Repeat("c", 32)} {
		t.Run("override="+override, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "keys.json")
			base := `{"admin_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","api_key":"never-log-api","bridge_key":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`
			if err := os.WriteFile(path, []byte(base), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("WB2A_LOG_TEST", "1")
			t.Setenv("WB2A_KEY_FILE", path)
			t.Setenv("WB2A_ADMIN_KEY", override)
			t.Setenv("WB2A_API_KEY", "never-log-api")
			t.Setenv("WB2A_CORE_URL", "http://core:7863")
			t.Setenv("WB2A_PUBLIC_ORIGIN", "")
			// Invalid listen address ends main after startup logging without binding a port.
			t.Setenv("WB2A_LISTEN", "invalid-address")
			output, err := exec.Command(os.Args[0], "-test.run=^TestConsoleLogProcess$").CombinedOutput()
			if err == nil {
				t.Fatal("expected invalid listen address to exit")
			}
			want := override
			if want == "" {
				want = strings.Repeat("a", 32)
			}
			if !strings.Contains(string(output), "管理密钥（仅交给管理员）: "+want) {
				t.Fatalf("effective admin key missing from startup log: %s", output)
			}
			if strings.Contains(string(output), "never-log-api") || strings.Contains(string(output), strings.Repeat("b", 32)) {
				t.Fatal("API or bridge key leaked to log")
			}
		})
	}
}

func TestConsoleLogProcess(t *testing.T) {
	if os.Getenv("WB2A_LOG_TEST") == "1" {
		main()
	}
}

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

func TestReadDeploymentKeysRejectsInvalidBaseDespiteValidOverrides(t *testing.T) {
	for name, original := range map[string][]byte{
		"short admin": []byte(`{"admin_key":"short","api_key":"base-api","bridge_key":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`),
		"empty api":   []byte(`{"admin_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","api_key":"","bridge_key":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`),
		"duplicate":   []byte(`{"admin_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","api_key":"base-api","bridge_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "keys.json")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := readDeploymentKeys(path, time.Second, strings.Repeat("c", 32), "valid-api"); err == nil {
				t.Fatal("valid overrides concealed an invalid persisted base")
			}
			if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, original) {
				t.Fatalf("rejected base was changed: %v", err)
			}
		})
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

func TestDesktopPageUsesLocalInstructionsOnlyWhenEnabled(t *testing.T) {
	for _, mode := range []string{"", "false", "true"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("WB2A_DESKTOP", mode)
			t.Setenv("WB2A_KEY_FILE", "")
			t.Setenv("WB2A_ADMIN_KEY", strings.Repeat("a", 32))
			t.Setenv("WB2A_API_KEY", "api")
			t.Setenv("WB2A_BRIDGE_KEY", strings.Repeat("b", 32))
			t.Setenv("WB2A_PUBLIC_ORIGIN", "")
			cfg, _, err := configFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			h, err := NewServer(cfg)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1/?admin_key=never-echo", nil))
			body := w.Body.String()
			if w.Code != 200 || strings.Contains(body, "never-echo") {
				t.Fatal("unsafe page response")
			}
			if got := strings.Contains(body, "docker compose logs console"); got != (mode != "true") {
				t.Fatalf("mode %q Docker help shown=%v", mode, got)
			}
			if mode == "true" && !strings.Contains(body, "托盘") {
				t.Fatal("desktop help does not explain tray")
			}
		})
	}
}

func TestDesktopParentPipeStopsConsole(t *testing.T) {
	for _, command := range []string{"", "ignored\nshutdown\n"} {
		t.Run(command, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			addr := listener.Addr().String()
			listener.Close()
			cmd := exec.Command(os.Args[0], "-test.run=^TestConsoleLogProcess$")
			cmd.Env = append(os.Environ(), "WB2A_LOG_TEST=1", "WB2A_DESKTOP=true", "WB2A_LISTEN="+addr, "WB2A_KEY_FILE=", "WB2A_ADMIN_KEY="+strings.Repeat("a", 32), "WB2A_API_KEY=api", "WB2A_BRIDGE_KEY="+strings.Repeat("b", 32), "WB2A_PUBLIC_ORIGIN=")
			pipe, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Kill()
			client := &http.Client{Timeout: 100 * time.Millisecond}
			ready := false
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
				response, err := client.Get("http://" + addr + "/livez")
				if err == nil {
					response.Body.Close()
					ready = response.StatusCode == 200
					if ready {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !ready {
				t.Fatal("console did not become ready")
			}
			if command == "" {
				pipe.Close()
			} else {
				pipe.Write([]byte(command))
				defer pipe.Close()
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("shutdown failed: %v %s", err, &output)
				}
			case <-time.After(2 * time.Second):
				cmd.Process.Kill()
				<-done
				t.Fatal("console ignored parent shutdown")
			}
			if response, err := client.Get("http://" + addr + "/livez"); err == nil {
				response.Body.Close()
				t.Fatal("HTTP still running")
			}
		})
	}
}
