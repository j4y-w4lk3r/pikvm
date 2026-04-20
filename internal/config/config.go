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
)

// Load populates the package-level vars by reading config.json (preferred)
// or .env (fallback). Returns an error only if neither file is usable.
func Load() error {
	if err := loadJSON(); err == nil {
		BaseURL = "https://" + Host + "/api"
		return nil
	}
	if err := loadDotenv(); err != nil {
		return err
	}
	BaseURL = "https://" + Host + "/api"
	return nil
}

// resolvePath returns the first existing path matching name in the usual
// locations: next to the binary, then current directory.
func resolvePath(name string) string {
	if execPath, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(execPath), name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if _, err := os.Stat(name); err == nil {
		return name
	}
	return ""
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
