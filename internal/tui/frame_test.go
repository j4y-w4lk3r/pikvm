package tui

import (
	"strings"
	"testing"
)

func TestWrapAppFrameDrawsLimeBorder(t *testing.T) {
	out := Model{}.wrapAppFrame("hello")
	// Rounded border corners from lipgloss.
	for _, corner := range []string{"╭", "╮", "╰", "╯"} {
		if !strings.Contains(out, corner) {
			t.Errorf("frame missing corner %q:\n%s", corner, out)
		}
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("frame should preserve inner content:\n%s", out)
	}
}

func TestViewWrapsMainScreen(t *testing.T) {
	m := Model{
		extenders:   1,
		portsPerExt: 4,
		totalPorts:  4,
		port:        0,
		activePort:  0,
		powerLeds:   []bool{false, false, false, false},
	}
	out := m.View()
	if !strings.Contains(out, "╭") || !strings.Contains(out, "PiKVM -") {
		t.Fatalf("View() should render framed main screen:\n%s", out)
	}
}
