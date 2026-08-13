package config

import (
	"fmt"
	"strings"

	"pikvm/internal/onepassword"
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
		b.WriteString("\n\n1Password + Tailscale bootstrap:\n  ")
		b.WriteString(e.BootstrapErr.Error())
	}
	if e.LocalErr != nil {
		b.WriteString("\n\nLocal config fallback:\n  ")
		b.WriteString(e.LocalErr.Error())
	}
	return b.String()
}

// SetupHelp returns platform-agnostic setup instructions (1Password first).
func SetupHelp() string {
	opHint := "installed"
	if !onepassword.Available() {
		opHint = "not found — install: yay -S 1password-cli  (Arch) / brew install 1password-cli"
	}
	return fmt.Sprintf(`%s

PiKVM loads credentials automatically — no manual config.json needed when:

  1. 1Password CLI (op) is installed and signed in
  2. tailscale is running on this machine
  3. Login items named pikvm1, pikvm2, … exist (web username / web password)

Current op status: %s

── Headless server setup (NAS, VPS, etc.) ──

  # Arch Linux
  yay -S 1password-cli tailscale

  # Sign in to 1Password (interactive once; caches session in ~/.config/op/)
  op signin

  # Or use a service account on fully headless boxes:
  # export OP_SERVICE_ACCOUNT_TOKEN='ops_...'   # vault access must include pikvm items

  # Confirm PiKVM items are visible
  op item list | grep -i pikvm

  # Confirm tailnet peers
  tailscale status | grep -i pikvm

  # Bootstrap config now (writes ~/.config/pikvm/config.json)
  pikvm hosts sync

── Manual fallback (offline / no op) ──

  mkdir -p ~/.config/pikvm
  # copy config.json from another machine, or create by hand — see: pikvm help

Discover log: %s`,
		bootstrapSummaryLine(), opHint, DiscoverLogPath())
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
