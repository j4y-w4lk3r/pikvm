package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestUseTwoColumnsThreshold(t *testing.T) {
	wide := Model{termWidth: 120}
	if !wide.useTwoColumns() {
		t.Fatal("expected two columns at width 120")
	}
	narrow := Model{termWidth: 70}
	if narrow.useTwoColumns() {
		t.Fatal("expected stacked layout at width 70")
	}
}

func TestRenderMainStacksOnNarrowTerminal(t *testing.T) {
	m := Model{
		termWidth:   64,
		extenders:   2,
		portsPerExt: 4,
		totalPorts:  8,
		port:        0,
		activePort:  0,
		powerLeds:   make([]bool, 8),
	}
	out := m.renderMain()
	opsIdx := strings.Index(out, "[O] Operations:")
	scriptsIdx := strings.Index(out, "[C] Custom Scripts:")
	if opsIdx < 0 || scriptsIdx < 0 {
		t.Fatal("missing expected sections")
	}
	if scriptsIdx < opsIdx {
		t.Error("scripts should appear below operations in narrow layout")
	}
}

func TestFrameRespectsTerminalWidth(t *testing.T) {
	m := Model{termWidth: 50}
	out := m.wrapAppFrame("x")
	if lipgloss.Width(out) > 50 {
		t.Fatalf("frame wider than terminal: got %d want <=50\n%s", lipgloss.Width(out), out)
	}
}

func TestPackStyledLinesWraps(t *testing.T) {
	parts := []string{
		helpStyle.Render("aaa"),
		helpStyle.Render("bbbbbbbb"),
		helpStyle.Render("cc"),
	}
	lines := packStyledLines(parts, 12, " | ")
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines, got %d: %v", len(lines), lines)
	}
}
