package config

import (
	"errors"
	"strings"
	"testing"
)

func TestLoadErrorIncludesBootstrap(t *testing.T) {
	err := newLoadError(errors.New("op CLI not found in PATH"), errors.New("neither config.json nor .env found"))
	msg := err.Error()
	if !strings.Contains(msg, "1Password") || !strings.Contains(msg, "op CLI not found") {
		t.Fatalf("unexpected error: %s", msg)
	}
}

func TestSetupHelpMentionsOp(t *testing.T) {
	if !strings.Contains(SetupHelp(), "1password-cli") {
		t.Fatal("SetupHelp should mention 1password-cli install")
	}
}
