package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestUseTwoColumnsThreshold(t *testing.T) {
	wide := Model{termWidth: 120, termHeight: 40}
	if !wide.useTwoColumns() {
		t.Fatal("expected two columns at width 120")
	}
	narrow := Model{termWidth: 70, termHeight: 40}
	if narrow.useTwoColumns() {
		t.Fatal("expected stacked layout at width 70")
	}
	// Typical tmux horizontal split (~80-90 cols outer).
	split := Model{termWidth: 86, termHeight: 40}
	if split.useTwoColumns() {
		t.Fatal("expected stacked layout in ~86-col tmux split")
	}
}

func TestContentWidthFillsPane(t *testing.T) {
	pane := Model{termWidth: 86}
	if got := pane.contentWidth(); got != 80 {
		t.Fatalf("contentWidth = %d, want 80 (pane fill)", got)
	}
	ultra := Model{termWidth: 300}
	if got := ultra.contentWidth(); got != 294 {
		t.Fatalf("contentWidth = %d, want 294 (full pane)", got)
	}
}

func TestLayoutCompact(t *testing.T) {
	compact := Model{termHeight: 20}
	if !compact.layoutCompact() {
		t.Fatal("expected compact layout below compactHeightThreshold")
	}
	tall := Model{termHeight: 40}
	if tall.layoutCompact() {
		t.Fatal("expected normal layout with plenty of height")
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

func TestFrameFillsTerminal(t *testing.T) {
	m := Model{termWidth: 50, termHeight: 20}
	out := m.wrapAppFrame("x")
	if got := lipgloss.Width(out); got != 50 {
		t.Fatalf("frame width = %d, want 50\n%s", got, out)
	}
	if got := lipgloss.Height(out); got != 20 {
		t.Fatalf("frame height = %d, want 20\n%s", got, out)
	}
}

func TestFrameFillsUltraWideTerminal(t *testing.T) {
	m := Model{
		termWidth:   200,
		termHeight:  40,
		extenders:   1,
		portsPerExt: 4,
		totalPorts:  4,
		port:        0,
		activePort:  0,
		powerLeds:   []bool{false, false, false, false},
	}
	out := m.View()
	if got := lipgloss.Width(out); got != 200 {
		t.Fatalf("frame width = %d, want 200", got)
	}
	if got := lipgloss.Height(out); got != 40 {
		t.Fatalf("frame height = %d, want 40", got)
	}
}

func TestViewFillsTmuxPaneWithoutOverflow(t *testing.T) {
	for _, tw := range []int{50, 70, 86, 94, 120} {
		t.Run(fmt.Sprintf("width%d", tw), func(t *testing.T) {
			m := Model{
				termWidth: tw, termHeight: 30,
				extenders: 2, portsPerExt: 4, totalPorts: 8,
				port: 1, activePort: 1,
				powerLeds:   make([]bool, 8),
				wsConnected: true,
			}
			out := m.View()
			if lipgloss.Width(out) != tw {
				t.Fatalf("rendered width %d does not fill pane %d:\n%s", lipgloss.Width(out), tw, out)
			}
		})
	}
}

func TestFitContentLinesClipsLongLines(t *testing.T) {
	long := strings.Repeat("x", 40)
	got := fitContentLines(long, 20)
	if ansi.StringWidth(got) > 20 {
		t.Fatalf("line not clipped: width=%d", ansi.StringWidth(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis truncation, got %q", got)
	}
}

func TestHelpWrapsInMediumPane(t *testing.T) {
	m := Model{termWidth: 94, termHeight: 30, extenders: 2, portsPerExt: 4}
	lines := m.renderHelpLines()
	if len(lines) < 2 {
		t.Fatalf("expected wrapped help lines in 94-col pane, got %d line(s)", len(lines))
	}
	for _, line := range lines {
		if lipgloss.Width(line) > m.contentWidth() {
			t.Fatalf("help line wider than content: %d > %d\n%s", lipgloss.Width(line), m.contentWidth(), line)
		}
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
