package tui

import "testing"

// TestOpVisualState exhaustively walks the (port-power-on, port-known,
// msd-connected) decision matrix for opVisualState (idea #9).
func TestOpVisualState(t *testing.T) {
	cases := []struct {
		name       string
		op         string
		port       int
		powerLeds  []bool
		msdConnect bool
		wantState  string
		wantSuffix string
	}{
		{"PowerON_off", "Power ON", 0, []bool{false, false}, false, "normal", ""},
		{"PowerON_on", "Power ON", 0, []bool{true, false}, false, "dimmed", " (already on)"},
		{"PowerON_unknown", "Power ON", 5, []bool{false}, false, "normal", ""},

		{"PowerOFF_on", "Power OFF", 0, []bool{true}, false, "normal", ""},
		{"PowerOFF_off", "Power OFF", 0, []bool{false}, false, "dimmed", " (already off)"},
		{"PowerOFF_unknown", "Power OFF", 5, []bool{false}, false, "normal", ""},

		{"Click_off", "Power Click", 0, []bool{false}, false, "dimmed", " (port off)"},
		{"Click_on", "Power Click", 0, []bool{true}, false, "normal", ""},
		{"LongPress_off", "Power Long Press", 0, []bool{false}, false, "dimmed", " (port off)"},
		{"ResetClick_off", "Reset Click", 0, []bool{false}, false, "dimmed", " (port off)"},
		{"ResetLong_off", "Reset Long Press", 0, []bool{false}, false, "dimmed", " (port off)"},
		{"Click_unknown", "Power Click", 5, []bool{false}, false, "normal", ""},

		{"Disconnect_attached", "Disconnect Drive", 0, []bool{false}, true, "normal", ""},
		{"Disconnect_detached", "Disconnect Drive", 0, []bool{false}, false, "dimmed", " (no drive attached)"},

		{"UnknownOp", "Make Coffee", 0, []bool{true}, true, "normal", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := Model{
				port:       c.port,
				powerLeds:  c.powerLeds,
				msdConnect: c.msdConnect,
			}
			gotState, gotSuffix := m.opVisualState(c.op)
			if gotState != c.wantState || gotSuffix != c.wantSuffix {
				t.Errorf("opVisualState(%q) port=%d powerLeds=%v msdConnect=%v\n  got:  (%q, %q)\n  want: (%q, %q)",
					c.op, c.port, c.powerLeds, c.msdConnect,
					gotState, gotSuffix, c.wantState, c.wantSuffix)
			}
		})
	}
}
