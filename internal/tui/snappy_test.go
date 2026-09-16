package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"pikvm/internal/api"
	"pikvm/internal/state"
)

func TestInitialModelDoesNotFetchSwitch(t *testing.T) {
	m := InitialModel()
	if m.portsDetected {
		t.Fatal("startup model must not wait on /api/switch")
	}
	if m.portsPerExt < 1 {
		t.Fatal("portsPerExt must be >= 1 before topology arrives")
	}
	if m.viewCache == nil {
		t.Fatal("expected a shared view cache")
	}
	out := m.View()
	if !strings.Contains(out, "Connecting") {
		t.Fatalf("expected connecting banner:\n%s", out)
	}
}

func TestInitFetchesSwitchInBackground(t *testing.T) {
	cmd := InitialModel().Init()
	if cmd == nil {
		t.Fatal("Init should return window-size + fetch-switch commands")
	}
}

func TestApplyLiveSwitchSeedsModel(t *testing.T) {
	m := InitialModel()
	next, cmd := m.Update(api.SwitchMsg{State: api.SwitchState{
		Extenders:   2,
		PortsPerExt: 4,
		TotalPorts:  8,
		ActivePort:  3,
		PowerLeds:   make([]bool, 8),
		DirectATX:   false,
	}})
	if cmd != nil {
		t.Fatal("switch apply should not start a command")
	}
	got := next.(Model)
	if !got.portsDetected {
		t.Fatal("expected portsDetected after switch fetch")
	}
	if got.port != 3 || got.extenders != 2 || got.totalPorts != 8 {
		t.Fatalf("port=%d ext=%d total=%d", got.port, got.extenders, got.totalPorts)
	}
	if !got.firstSwitchSeen {
		t.Fatal("first switch snapshot should mark firstSwitchSeen")
	}
	out := got.View()
	if strings.Contains(out, "Connecting") {
		t.Fatalf("connecting banner should be gone after topology:\n%s", out)
	}
	if !strings.Contains(out, "[E] Extender:") {
		t.Fatalf("expected extender row after topology:\n%s", out)
	}
}

func TestApplyLiveSwitchDirectATX(t *testing.T) {
	m := InitialModel()
	next, _ := m.Update(api.SwitchMsg{State: api.SwitchState{
		DirectATX:   true,
		Extenders:   1,
		PortsPerExt: 1,
		TotalPorts:  1,
		PowerLeds:   []bool{true},
		HddLeds:     []bool{false},
	}})
	got := next.(Model)
	if !got.directATX || !got.atxPower {
		t.Fatalf("directATX=%v atxPower=%v", got.directATX, got.atxPower)
	}
	if !strings.Contains(got.View(), "direct (no KVM switch)") {
		t.Fatalf("expected direct-ATX row:\n%s", got.View())
	}
}

func TestViewCacheReusesIdenticalFrame(t *testing.T) {
	m := Model{
		portsDetected: true,
		focusMode:     "ops",
		extenders:     1,
		portsPerExt:   4,
		totalPorts:    4,
		termWidth:     80,
		termHeight:    30,
		powerLeds:     make([]bool, 4),
		viewCache:     &viewCache{},
	}
	a := m.View()
	key0 := m.viewCache.key
	if key0 == "" || m.viewCache.out != a {
		t.Fatal("view cache was not populated")
	}
	b := m.View()
	if b != a {
		t.Fatal("identical model should reuse the framed string")
	}
	m.cursor = 1
	if m.viewKey() == key0 {
		t.Fatal("cursor change should change the view fingerprint")
	}
	_ = m.View()
	if m.viewCache.key == key0 {
		t.Fatal("cursor change should miss the cache and store a new key")
	}
}

func TestNoopKeyKeepsCursor(t *testing.T) {
	m := InitialModel()
	m.portsDetected = true
	m.focusMode = "ops"
	m.cursor = 0
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if cmd != nil {
		t.Fatal("k at top of list should not run a command")
	}
	got := next.(Model)
	if got.cursor != 0 {
		t.Fatalf("cursor=%d, want 0", got.cursor)
	}
}

func TestViewPowerBackendLookupUsesModelState(t *testing.T) {
	m := Model{
		portsDetected: true,
		extenders:     1,
		portsPerExt:   4,
		totalPorts:    4,
		port:          0,
		activePort:    0,
		powerLeds:     make([]bool, 4),
		state: state.State{Ports: map[string]state.PortProfile{
			"1.1": {
				Power: &state.PowerBackend{
					Type:  "http",
					OnURL: "http://relay.invalid/on",
				},
			},
		}},
	}
	if !m.portUsesHTTPPower(0) {
		t.Fatal("expected in-memory HTTP power profile")
	}
	if state, suffix := m.opVisualState("Power ON"); state != "normal" || suffix != "" {
		t.Fatalf("opVisualState = (%q, %q), want normal without suffix", state, suffix)
	}
}

func TestOperationStartsAsCommand(t *testing.T) {
	m := Model{
		portsDetected: true,
		focusMode:     "ops",
		portsPerExt:   4,
		totalPorts:    4,
		powerLeds:     make([]bool, 4),
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)
	if cmd == nil {
		t.Fatal("operation must return a background command")
	}
	if !got.operationBusy {
		t.Fatal("operation should be marked busy while command runs")
	}
	if !strings.Contains(got.result, "Running") {
		t.Fatalf("expected immediate running status, got %q", got.result)
	}
}

func TestPortSwitchStartsAsCommand(t *testing.T) {
	m := Model{
		portsDetected: true,
		focusMode:     "port",
		portsPerExt:   4,
		totalPorts:    4,
		availablePorts: []api.PortInfo{
			{ID: 0, Active: true},
			{ID: 1, Active: true},
		},
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	got := next.(Model)
	if cmd == nil {
		t.Fatal("port switch must return a background command")
	}
	if !got.portSwitching || got.port != 1 {
		t.Fatalf("portSwitching=%v port=%d, want true/1", got.portSwitching, got.port)
	}
}

func BenchmarkMainViewCursorMove(b *testing.B) {
	m := Model{
		portsDetected: true,
		focusMode:     "ops",
		extenders:     2,
		portsPerExt:   4,
		totalPorts:    8,
		termWidth:     120,
		termHeight:    40,
		powerLeds:     make([]bool, 8),
		viewCache:     &viewCache{},
		state:         state.State{Ports: map[string]state.PortProfile{}},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.cursor = i % len(m.actions())
		_ = m.View()
	}
}
