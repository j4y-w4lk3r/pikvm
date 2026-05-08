// Package config loads PiKVM credentials + settings from disk.
//
// As of schema_version 2 a single user can have multiple PiKVMs configured
// (roadmap idea #25 — multi-host federation). Each host gets a name; one
// is marked as the default. Picking a non-default host is a single CLI
// flag (--host <name>), env var (PIKVM_HOST_NAME), or TUI selection.
//
// File format (canonical, schema v2):
//
//	{
//	  "schema_version": 2,
//	  "default": "lab",
//	  "hosts": {
//	    "lab":    {"host":"100.64.0.10","user":"admin","pass":"..."},
//	    "garage": {"host":"100.64.0.11","user":"admin","pass":"..."}
//	  },
//	  "TAILSCALE_AUTH_KEY": "...",   // shared across hosts
//	  "UBUNTU_PASSWORD":     "..."
//	}
//
// The legacy single-host schema (v1, with PIKVM_HOST/USER/PASS at top
// level) and the .env fallback are still accepted: Load() converts them
// into a single-entry hosts map at runtime, so consumers always see the
// same shape via Hosts / HostName / Host / User / Pass.
//
// Callers read the package-level vars (Host, User, Pass, ...) after Load()
// returns. They are deliberately simple exported strings so the TUI / API
// / scripts packages all write `config.Host` / `config.User` / ... in
// place of the old package-main globals.
package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// HostConfig is one PiKVM. Multiple of these live in a config.json under
// the `hosts` map (keyed by friendly name). The active one populates the
// package-level Host/User/Pass vars after Load().
type HostConfig struct {
	Host string `json:"host"`
	User string `json:"user"`
	Pass string `json:"pass"`
}

var (
	// Active host's connection details. Equivalent to Hosts[HostName].*
	// — duplicated as plain strings to keep call sites tiny.
	Host    string
	User    string
	Pass    string
	BaseURL string // "https://<Host>/api" — recomputed on every UseHost()

	// Settings shared across hosts.
	TailscaleAuthKey string
	UbuntuPassword   string

	// HostName is the active host's friendly name (e.g. "lab"). Empty
	// only when no config has been loaded.
	HostName string

	// Hosts is every PiKVM the user has configured. Always has at least
	// one entry once Load() succeeds.
	Hosts map[string]HostConfig

	// DefaultHost is the host name picked when no --host flag, env var,
	// or runtime override is in play.
	DefaultHost string

	// SchemaVersion of the loaded config. 1 = legacy single-host, 2 = v2.
	// Useful for tooling that wants to suggest a migration.
	SchemaVersion int

	// Loaded is true once Load() has populated Host/User/Pass.
	Loaded bool

	// searched records every path resolvePath() looked at, in order, so
	// the "config not found" error can show users exactly where to drop a
	// file. Reset on each Load() call.
	searched []string
)

// SearchedPaths returns every config path the last Load() call attempted.
func SearchedPaths() []string { return append([]string{}, searched...) }

// HostNames returns every configured host name in stable alphabetical
// order (the default host is *not* moved to the front; sorting matters
// more for `pikvm hosts list`).
func HostNames() []string {
	names := make([]string, 0, len(Hosts))
	for n := range Hosts {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Load populates the package-level vars by reading config.json (preferred)
// or .env (fallback). Sets Loaded = true on success so other packages can
// branch on it without re-checking error state.
//
// Override the active host (vs. the file's `default`) before Load returns
// by setting $PIKVM_HOST_NAME — useful for `--host` CLI dispatch where the
// flag is parsed earlier than Load is called.
func Load() error {
	searched = searched[:0]
	Hosts = nil
	HostName = ""
	DefaultHost = ""
	SchemaVersion = 0

	if err := loadJSON(); err != nil {
		// Fall through to .env so existing single-host setups keep
		// working. Only error out when neither file is usable.
		if envErr := loadDotenv(); envErr != nil {
			Loaded = false
			return envErr
		}
	}

	if err := UseHost(pickInitialHost()); err != nil {
		Loaded = false
		return err
	}
	Loaded = true
	return nil
}

// pickInitialHost returns the host name to activate at Load() time:
//
//  1. $PIKVM_HOST_NAME if set + valid
//  2. The file's `default` field
//  3. The lone host name when there's only one
//  4. The alphabetically-first host name as a last resort
func pickInitialHost() string {
	if env := os.Getenv("PIKVM_HOST_NAME"); env != "" {
		if _, ok := Hosts[env]; ok {
			return env
		}
	}
	if DefaultHost != "" {
		if _, ok := Hosts[DefaultHost]; ok {
			return DefaultHost
		}
	}
	if len(Hosts) == 1 {
		for n := range Hosts {
			return n
		}
	}
	if names := HostNames(); len(names) > 0 {
		return names[0]
	}
	return ""
}

// UseHost activates a configured host by name, repopulating the
// Host/User/Pass/BaseURL vars. Returns an error when the name doesn't
// match any configured host.
//
// Callers (CLI dispatcher, TUI host picker, future hot-swap) call this
// instead of mutating the package vars directly so BaseURL stays in sync.
func UseHost(name string) error {
	if name == "" {
		return fmt.Errorf("no host configured")
	}
	h, ok := Hosts[name]
	if !ok {
		return fmt.Errorf("no host named %q (configured: %s)",
			name, strings.Join(HostNames(), ", "))
	}
	HostName = name
	Host = h.Host
	User = h.User
	Pass = h.Pass
	BaseURL = "https://" + h.Host + "/api"
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

// loadJSON reads config.json supporting both v1 (legacy single-host) and
// v2 (multi-host) schemas. v1 is auto-wrapped into a single-entry hosts
// map named "default" so callers always see Hosts populated.
func loadJSON() error {
	path := resolvePath("config.json")
	if path == "" {
		return fmt.Errorf("config.json not found")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	// Decode loosely so we can sniff the schema version and route.
	var raw struct {
		SchemaVersion    int                   `json:"schema_version"`
		Default          string                `json:"default"`
		Hosts            map[string]HostConfig `json:"hosts"`
		PIKVMHost        string                `json:"PIKVM_HOST"`
		PIKVMUser        string                `json:"PIKVM_USER"`
		PIKVMPass        string                `json:"PIKVM_PASS"`
		TailscaleAuthKey string                `json:"TAILSCALE_AUTH_KEY"`
		UbuntuPassword   string                `json:"UBUNTU_PASSWORD"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	TailscaleAuthKey = raw.TailscaleAuthKey
	UbuntuPassword = raw.UbuntuPassword

	switch {
	case len(raw.Hosts) > 0:
		// v2 multi-host.
		SchemaVersion = 2
		if raw.SchemaVersion != 0 {
			SchemaVersion = raw.SchemaVersion
		}
		Hosts = raw.Hosts
		DefaultHost = raw.Default
		// Validate every host.
		for name, h := range Hosts {
			if h.Host == "" || h.User == "" || h.Pass == "" {
				return fmt.Errorf("%s: host %q missing host/user/pass", path, name)
			}
		}
		// If `default` wasn't set or is bogus, fall back to the lone
		// host (or alphabetically-first).
		if DefaultHost == "" || Hosts[DefaultHost].Host == "" {
			if names := HostNames(); len(names) > 0 {
				DefaultHost = names[0]
			}
		}
	case raw.PIKVMHost != "":
		// v1 legacy: top-level PIKVM_HOST/USER/PASS. Wrap into a
		// single-entry map under name "default".
		SchemaVersion = 1
		if raw.PIKVMUser == "" || raw.PIKVMPass == "" {
			return fmt.Errorf("%s: missing PIKVM_USER or PIKVM_PASS (legacy schema)", path)
		}
		Hosts = map[string]HostConfig{
			"default": {Host: raw.PIKVMHost, User: raw.PIKVMUser, Pass: raw.PIKVMPass},
		}
		DefaultHost = "default"
	default:
		return fmt.Errorf("%s: no PIKVM_HOST or hosts entry found", path)
	}
	return nil
}

// loadDotenv keeps the very-old .env path working — single host only,
// wrapped into the same Hosts map shape as v1 JSON for downstream code.
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

	var h, u, p string
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
			h = value
		case "PIKVM_USER":
			u = value
		case "PIKVM_PASS":
			p = value
		case "TAILSCALE_AUTH_KEY":
			TailscaleAuthKey = value
		case "UBUNTU_PASSWORD":
			UbuntuPassword = value
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if h == "" || u == "" || p == "" {
		return fmt.Errorf("%s missing required PIKVM_HOST/USER/PASS", path)
	}
	SchemaVersion = 1
	Hosts = map[string]HostConfig{
		"default": {Host: h, User: u, Pass: p},
	}
	DefaultHost = "default"
	return nil
}
