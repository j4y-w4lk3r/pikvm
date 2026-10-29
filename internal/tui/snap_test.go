package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"pikvm/internal/preview"
	"pikvm/internal/tour"
)

func keyRunes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestTourNamePromptFlow(t *testing.T) {
	m := InitialModel()
	m.termWidth, m.termHeight = 120, 50

	next, _ := m.handleKey(keyRunes("S"))
	m = next.(Model)
	if !m.tourNaming || !strings.HasPrefix(m.tourNameBuf, "tour-") {
		t.Fatalf("S should open the name prompt with a default, got naming=%v buf=%q", m.tourNaming, m.tourNameBuf)
	}
	if !strings.Contains(m.View(), "Start a tour") {
		t.Fatal("name prompt not rendered")
	}

	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = next.(Model)
	for _, r := range "BIOS" {
		next, _ = m.handleKey(keyRunes(string(r)))
		m = next.(Model)
	}
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	m = next.(Model)
	next, _ = m.handleKey(keyRunes("1605/$"))
	m = next.(Model)
	if m.tourNameBuf != "BIOS-1605" {
		t.Fatalf("name buffer = %q, want BIOS-1605 (unsafe runes dropped)", m.tourNameBuf)
	}

	// q inside the prompt is text, not quit.
	next, _ = m.handleKey(keyRunes("q"))
	m = next.(Model)
	if m.quitting {
		t.Fatal("q in the name prompt must not quit")
	}

	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.tourNaming {
		t.Fatal("ESC should close the prompt")
	}
}

func TestSnapDuringTourRequestsManualCapture(t *testing.T) {
	m := InitialModel()
	run := &tourRun{name: "bios-1605", manual: make(chan struct{}, 1), events: make(chan tour.Event, 1), cancel: func() {}}
	m.tour = run
	next, cmd := m.handleKey(keyRunes("s"))
	m = next.(Model)
	if cmd != nil {
		t.Fatal("s during a tour should not start a separate snapshot")
	}
	select {
	case <-run.manual:
	default:
		t.Fatal("s during a tour did not request a manual capture")
	}
}

func TestQuitDuringTourAsksOnce(t *testing.T) {
	m := InitialModel()
	m.tour = &tourRun{name: "bios-1605", manual: make(chan struct{}, 1), cancel: func() {}}
	next, cmd := m.handleKey(keyRunes("q"))
	m = next.(Model)
	if m.quitting || cmd != nil {
		t.Fatal("first q during a tour should only warn")
	}
	next, _ = m.handleKey(keyRunes("q"))
	if !next.(Model).quitting {
		t.Fatal("second q should quit")
	}
}

func TestPreviewBlockRenders(t *testing.T) {
	m := InitialModel()
	m.termWidth, m.termHeight = 140, 50
	m.previewMode = preview.Blocks
	m.preview = &preview.Image{Cols: 4, Rows: 2, Lines: []string{"AAAA", "BBBB"}, Mode: preview.Blocks}
	m.previewCaption = "snap #01"
	out := m.View()
	if !strings.Contains(out, "snap #01") || !strings.Contains(out, "AAAA") {
		t.Fatal("preview not rendered in main view")
	}
	next, _ := m.handleKey(keyRunes("v"))
	m = next.(Model)
	if strings.Contains(m.View(), "AAAA") {
		t.Fatal("v should hide the preview")
	}
}
