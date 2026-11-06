package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Passwords are not stored on disk any more: the sync writes hosts without
// "pass", and a config.json without "pass" loads; the password comes from
// PIKVM_PASS (or OpenBao) when the host is used.
func TestPersistHostsDoesNotWritePass(t *testing.T) {
	dir := t.TempDir()
	configFilePath = filepath.Join(dir, "config.json")
	t.Cleanup(func() { configFilePath = ""; Hosts = nil })
	Hosts = map[string]HostConfig{
		"pikvm1": {Host: "100.64.0.99", User: "admin", Pass: "secret-in-memory", TailscaleName: "pikvm1"},
	}
	if err := persistHostsToConfig(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configFilePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-in-memory") || strings.Contains(string(data), `"pass"`) {
		t.Fatalf("config.json still contains a password:\n%s", data)
	}
}

func TestLoadWithoutPassUsesEnvAtUseHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pikvm", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{"schema_version": 2, "hosts": map[string]any{
		"pikvm1": map[string]any{"host": "100.64.0.99", "user": "admin"},
	}}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("PIKVM_PASS", "from-env")
	t.Setenv("PIKVM_HOST_NAME", "pikvm1")
	t.Cleanup(func() { configFilePath = ""; Hosts = nil; Pass = "" })
	if err := Load(); err != nil {
		t.Fatalf("Load without pass: %v", err)
	}
	if err := UseHost("pikvm1"); err != nil {
		t.Fatalf("UseHost: %v", err)
	}
	if Pass != "from-env" {
		t.Fatalf("Pass = %q, want the PIKVM_PASS value", Pass)
	}
}
