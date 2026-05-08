// Package config loads PiKVM credentials + settings from disk.
//
// Source-of-truth is config.json (written by
// automation/scripts/env-from-vault.sh from HashiCorp Vault). Legacy .env
// format is accepted as a fallback so hand-written setups keep working.
//
// Callers read the package-level vars (Host, User, Pass, ...) after Load()
// returns nil. They are deliberately simple exported strings rather than a
// struct so every consumer writes `config.Host` / `config.User` / ... in
// place of the old package-main globals.
package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Populated by Load(). Leaving them as package-level vars keeps the call
// sites simple; the TUI / API / scripts packages all read these and none
// of them write to them.
var (
	Host             string
	User             string
	Pass             string
	BaseURL          string // "https://<host>/api" — computed from Host
	TailscaleAuthKey string
	UbuntuPassword   string

	// Loaded is true once Load() has successfully populated Host/User/Pass.
	// CLI commands that don't need a PiKVM (help, version) check this and
	// skip the connection-required path entirely.
	Loaded bool

	// searched records every path resolvePath() looked at, in order, so
	// the "config not found" error can show users exactly where to drop a
	// file. Reset on each Load() call.
	searched []string
)

// SearchedPaths returns every config path the last Load() call attempted,
// for use in user-facing "config not found" diagnostics.
func SearchedPaths() []string { return append([]string{}, searched...) }

// Load populates the package-level vars by reading config.json (preferred)
// or .env (fallback). Returns an error only if neither file is usable.
// Sets Loaded = true on success so other packages can branch on it without
// re-checking error state.
func Load() error {
	searched = searched[:0]
	if err := loadJSON(); err == nil {
		BaseURL = "https://" + Host + "/api"
		Loaded = true
		return nil
	}
	if err := loadDotenv(); err != nil {
		Loaded = false
		return err
	}
	BaseURL = "https://" + Host + "/api"
	Loaded = true
	return nil
}

// resolvePath returns the first existing path matching name, searching
// (in order):
//
//  1. $XDG_CONFIG_HOME/pikvm/<name>     — if XDG_CONFIG_HOME is set
//  2. ~/.config/pikvm/<name>            — XDG default, works the same on
//     macOS + Linux despite Go's os.UserConfigDir() preferring
//     ~/Library/Application Support on macOS (we want git/gh-style
//     behaviour here)
//  3. <binary-dir>/<name>                — alongside the executable
//  4. ./<name>                           — current working directory
//
// Every candidate is recorded in `searched` so error messages can list
// them all to the user.
func resolvePath(name string) string {
	candidates := []string{}

	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "pikvm", name))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "pikvm", name))
	}
	if execPath, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(execPath), name))
	}
	candidates = append(candidates, name)

	for _, p := range candidates {
		if !contains(searched, p) {
			searched = append(searched, p)
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func loadJSON() error {
	path := resolvePath("config.json")
	if path == "" {
		return fmt.Errorf("config.json not found")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var cfg struct {
		SchemaVersion    int    `json:"schema_version"`
		PIKVMHost        string `json:"PIKVM_HOST"`
		PIKVMUser        string `json:"PIKVM_USER"`
		PIKVMPass        string `json:"PIKVM_PASS"`
		TailscaleAuthKey string `json:"TAILSCALE_AUTH_KEY"`
		UbuntuPassword   string `json:"UBUNTU_PASSWORD"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.PIKVMHost == "" || cfg.PIKVMUser == "" || cfg.PIKVMPass == "" {
		return fmt.Errorf("%s missing required PIKVM_HOST/USER/PASS", path)
	}
	Host = cfg.PIKVMHost
	User = cfg.PIKVMUser
	Pass = cfg.PIKVMPass
	TailscaleAuthKey = cfg.TailscaleAuthKey
	UbuntuPassword = cfg.UbuntuPassword
	return nil
}

func loadDotenv() error {
	path := resolvePath(".env")
	if path == "" {
		return fmt.Errorf("neither config.json nor .env found")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "PIKVM_HOST":
			Host = value
		case "PIKVM_USER":
			User = value
		case "PIKVM_PASS":
			Pass = value
		case "TAILSCALE_AUTH_KEY":
			TailscaleAuthKey = value
		case "UBUNTU_PASSWORD":
			UbuntuPassword = value
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if Host == "" || User == "" || Pass == "" {
		return fmt.Errorf("%s missing required PIKVM_HOST/USER/PASS", path)
	}
	return nil
}
