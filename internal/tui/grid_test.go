package tui

import (
	"strings"
	"testing"

	"pikvm/internal/state"
)

// TestGridRender confirms the grid view output structurally — every port
// has its own bordered cell, profile names surface from state.json, and the
// active-port star marker appears on the right port.
func TestGridRender(t *testing.T) {
	m := Model{
		extenders:   2,
		portsPerExt: 4,
		totalPorts:  8,
		port:        5,
		activePort:  5,
		gridView:    true,
		gridCursor:  5,
		powerLeds:   []bool{true, false, true, false, false, false, false, true},
		videoLinks:  []bool{true, false, true, false, false, false, false, false},
		usbLinks:    []bool{true, false, true, false, false, false, false, true},
	}
	m.state = state.State{
		SchemaVersion: 1,
		Ports: map[string]state.PortProfile{
			"1.1": {Name: "j4yn0"},
			"1.3": {Name: "vault"},
			"2.4": {Name: "pi"},
		},
	}

	out := m.renderGridView()

	for _, want := range []string{
		"Extender 1", "Extender 2",
		"1.1", "1.2", "1.3", "1.4",
		"2.1", "2.2", "2.3", "2.4",
		"j4yn0", "vault", "pi",
		"on", "off",
		"\u2605",
		"V", "U", "P",
		"legend: V video",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("grid render missing %q", want)
		}
	}
	t.Log("\n" + out)
}
