package config

import (
	"errors"
	"strings"
	"testing"
)

func TestLoadErrorIncludesBootstrap(t *testing.T) {
	err := newLoadError(errors.New("op CLI not found in PATH"), errors.New("neither config.json nor .env found"))
	msg := err.Error()
	if !strings.Contains(msg, "OpenBao") || !strings.Contains(msg, "op CLI not found") {
		t.Fatalf("unexpected error: %s", msg)
	}
}

func TestSetupHelpMentionsOpenBao(t *testing.T) {
	if !strings.Contains(SetupHelp(), "openbao-pikvm.token") {
		t.Fatal("SetupHelp should mention the OpenBao token file")
	}
}
