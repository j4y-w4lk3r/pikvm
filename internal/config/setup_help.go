package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadError is returned when 1Password bootstrap and local config files both fail.
type LoadError struct {
	BootstrapErr error
	LocalErr     error
}

func (e *LoadError) Error() string {
	var b strings.Builder
	b.WriteString("could not load PiKVM configuration")
	if e.BootstrapErr != nil {
		b.WriteString("\n\nOpenBao + Tailscale bootstrap:\n  ")
		b.WriteString(e.BootstrapErr.Error())
	}
	if e.LocalErr != nil {
		b.WriteString("\n\nLocal config fallback:\n  ")
		b.WriteString(e.LocalErr.Error())
	}
	return b.String()
}

// SetupHelp returns setup instructions for the OpenBao login.
func SetupHelp() string {
	tokenHint := "present"
	if _, err := os.Stat(filepath.Join(configDir(), "openbao-pikvm.token")); err != nil {
		tokenHint = "missing — " + filepath.Join(configDir(), "openbao-pikvm.token")
	}
	return fmt.Sprintf(`%s

PiKVM loads credentials from OpenBao — no 1Password session needed when:

  1. ~/.config/j4y/openbao.addr names the Tailscale OpenBao address
  2. ~/.config/j4y/openbao-pikvm.token can read secret/pikvm/pikvmN
  3. tailscale is running on this machine

Current token file: %s

  tailscale status | grep -i pikvm
  pikvm hosts sync

Discover log: %s`,
		bootstrapSummaryLine(), tokenHint, DiscoverLogPath())
}

func configDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "j4y")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "j4y")
	}
	return filepath.Join(home, ".config", "j4y")
}

func bootstrapSummaryLine() string {
	if LastDiscoverSummary.Error != "" {
		return "Last bootstrap: " + LastDiscoverSummary.Error
	}
	return "Last bootstrap: (none yet)"
}

func newLoadError(bootstrap, local error) error {
	if bootstrap == nil && local == nil {
		return fmt.Errorf("no PiKVM hosts configured")
	}
	return &LoadError{BootstrapErr: bootstrap, LocalErr: local}
}
