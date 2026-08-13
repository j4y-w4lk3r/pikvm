package tui

import (
	"strings"
	"testing"

	"pikvm/internal/api"
	"pikvm/internal/config"
)

func TestRenderAppHeaderShowsHost(t *testing.T) {
	config.Host = "100.64.0.10"
	out := renderAppHeader()
	if !strings.Contains(out, "PiKVM - 100.64.0.10") {
		t.Errorf("header missing host %q:\n%s", "PiKVM - 100.64.0.10", out)
	}
}

func TestHelpViewToggle(t *testing.T) {
	m := Model{showHelp: true, termWidth: 80}
	out := m.View()
	for _, want := range []string{"Help", "Navigation", "?", "ESC to close"} {
		if !strings.Contains(out, want) {
			t.Errorf("help view missing %q", want)
		}
	}
}

func TestHostRowMultiHost(t *testing.T) {
	config.Hosts = map[string]config.HostConfig{
		"pikvm1": {Host: "100.64.0.10", User: "admin", Pass: "x"},
		"pikvm2": {Host: "100.64.0.11", User: "admin", Pass: "x"},
	}
	config.HostName = "pikvm1"
	config.User = "admin"
	config.Host = "100.64.0.10"

	m := Model{
		extenders:   1,
		portsPerExt: 4,
		totalPorts:  4,
		port:        0,
		activePort:  0,
		powerLeds:   []bool{false, false, false, false},
		focusMode:   "host",
	}
	out := m.renderMain()
	for _, want := range []string{
		"[H] Host:",
		"[1] pikvm1",
		"[2] pikvm2",
		"PiKVM - 100.64.0.10",
		"h: Host",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("main view missing %q", want)
		}
	}
}

func TestApplyHostSwitchResetsLiveState(t *testing.T) {
	m := Model{
		port:            3,
		extenders:       2,
		portsPerExt:     4,
		totalPorts:      8,
		activePort:      3,
		wsConnected:     true,
		wsClients:       2,
		msdConnect:      true,
		firstSwitchSeen: true,
		firstMsdSeen:    true,
	}
	m.applyHostSwitch(hostSwitchDoneMsg{
		name: "garage",
		sw: api.SwitchState{
			Extenders:   1,
			PortsPerExt: 4,
			TotalPorts:  4,
			ActivePort:  1,
			PowerLeds:   []bool{true, false, false, false},
		},
		infoOK: true,
		info:   api.InfoState{Platform: "v4"},
	})
	if m.port != 1 || m.extenders != 1 || m.wsConnected || m.msdConnect || m.firstSwitchSeen {
		t.Fatalf("host switch did not reset model: port=%d ext=%d ws=%v msd=%v first=%v",
			m.port, m.extenders, m.wsConnected, m.msdConnect, m.firstSwitchSeen)
	}
	if m.info.Platform != "v4" {
		t.Fatalf("expected info from new host, got %+v", m.info)
	}
}
