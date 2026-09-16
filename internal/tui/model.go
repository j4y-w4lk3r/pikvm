// Package tui is the Bubble Tea model/view/update layer. It imports:
//
//	api      - for live state + sending requests
//	state    - for per-port profiles + ext.port helpers
//	scripts  - for the custom-script registry and modal helpers
package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pikvm/internal/api"
	"pikvm/internal/scripts"
	"pikvm/internal/state"
)

// focusMode: which menu is active for number-key input.
//
//	""         = none
//	"extender" = picking extender 1..Extenders
//	"port"     = picking port 1..PortsPerExt on the current extender
//	"ops"      = operations
//	"scripts"  = custom scripts
type Model struct {
	cursor              int
	port                int // linear (0-based)
	extenders           int
	portsPerExt         int
	totalPorts          int
	activePort          int
	result              string
	quitting            bool
	availablePorts      []api.PortInfo
	portsDetected       bool
	focusMode           string
	inScripts           bool
	selectingISO        bool
	availableISOEntries []api.IsoEntry
	isoCursor           int
	selectingBIOSKey    bool
	showHelp            bool

	// Grid-view (idea #7)
	gridView   bool
	gridCursor int

	// Live WS state (idea #1)
	wsConnected bool
	wsLastError string
	wsLastEvent time.Time
	wsClients   int
	atxPower    bool
	atxHdd      bool
	atxBusy     bool

	// Per-port live signals (length == totalPorts)
	videoLinks []bool
	usbLinks   []bool
	powerLeds  []bool
	hddLeds    []bool

	// /api/info snapshot (idea #10)
	info api.InfoState

	// Per-port profiles (idea #5)
	state state.State

	// MSD status (idea #10)
	msdOnline  bool
	msdBusy    bool
	msdConnect bool
	msdFree    int64
	msdTotal   int64
	msdUpload  bool
	msdUpName  string
	msdUpPct   float64

	// Terminal size — drives responsive layout (tmux splits, small panes).
	termWidth  int
	termHeight int

	// Multi-host federation (roadmap #25): async switch in flight.
	hostSwitching bool

	// In-flight commands. Network and long-running work must never execute in
	// Update(), because it would stop Bubble Tea from processing key messages.
	operationBusy bool
	portSwitching bool

	// directATX is true for PiKVM V3-style hosts with built-in ATX only (no KVM switch).
	directATX bool

	// Shared across Model copies so View() can reuse the last framed
	// string when nothing visual changed (no-op keys, duplicate paints).
	viewCache *viewCache

	// Hooks event suppression on first event (idea #20). The first
	// SwitchMsg / MsdMsg / ClientsMsg after startup populates the model
	// from "zero" values — we don't want that to fire spurious "port
	// changed from 0 to 5" hooks. These flags flip to true after the
	// first message of each kind so subsequent transitions dispatch.
	firstSwitchSeen  bool
	firstMsdSeen     bool
	firstClientsSeen bool
}

// InitialModel builds the starting TUI model without touching the network.
// Switch topology is fetched in Init() via fetchSwitchCmd so the first
// frame can paint immediately. portsPerExt is 1 so extender/port helpers
// never divide by zero before the fetch lands.
//
// PiKVM's /api/switch can return summary.active_id == -1 (no port
// currently routed). We keep that as-is in m.activePort (so the status
// bar can show "no active port") but clamp the user-cursor m.port to 0
// — the cursor is what the user is hovering over, and indexing slices
// by it must never panic.
func InitialModel() Model {
	return Model{
		extenders:      1,
		portsPerExt:    1,
		totalPorts:     1,
		availablePorts: []api.PortInfo{{ID: 0, Active: true}},
		powerLeds:      []bool{false},
		hddLeds:        []bool{false},
		state:          state.Load(),
		viewCache:      &viewCache{},
	}
}

func (m Model) actions() []api.Action {
	return api.ActionsForDirect(m.directATX)
}

// Init is Bubble Tea's startup hook. Window size plus a background
// /api/switch fetch; the WebSocket and info poller are started in main().
func (m Model) Init() tea.Cmd {
	return tea.Batch(tea.WindowSize(), fetchSwitchCmd())
}

func fetchSwitchCmd() tea.Cmd {
	return func() tea.Msg {
		return api.SwitchMsg{State: api.FetchSwitchState()}
	}
}

// ---- helpers on Model ------------------------------------------------------

func (m Model) extenderOf(linear int) int     { return linear/m.portsPerExt + 1 }
func (m Model) portOf(linear int) int         { return linear%m.portsPerExt + 1 }
func (m Model) linearPort(ext, port int) int  { return (ext-1)*m.portsPerExt + (port - 1) }
func (m Model) portExtIDOf(linear int) string { return state.PortExtID(linear, m.portsPerExt) }

func (m Model) portName(linear int) string {
	id := m.portExtIDOf(linear)
	if p, ok := m.state.Ports[id]; ok && p.Name != "" {
		return p.Name
	}
	return id
}

func (m Model) savedBIOSKey(linear int) string {
	return m.state.Ports[m.portExtIDOf(linear)].BIOSKey
}

// portUsesHTTPPower answers from the state snapshot already held by the model.
// View must never call api.PortUsesHTTPPower: that API helper may read state
// from disk and is intended for one-shot CLI/action execution.
func (m Model) portUsesHTTPPower(linear int) bool {
	if linear < 0 {
		return false
	}
	profile := m.state.Ports[m.portExtIDOf(linear)]
	backend := api.PowerBackendFromProfile(profile)
	return backend != nil && backend.UsesHTTP()
}

// trySetActive switches PiKVM's active port and updates m.activePort.
func (m *Model) trySetActive(linear int) error {
	if err := api.SetSwitchPort(linear); err != nil {
		return err
	}
	m.activePort = linear
	return nil
}

// launchScript opens a modal or returns a command for work that may perform
// I/O or sleep. The returned command runs outside Bubble Tea's update loop.
func (m *Model) launchScript(idx int) tea.Cmd {
	if idx < 0 || idx >= len(scripts.Default) {
		return nil
	}
	s := scripts.Default[idx]
	switch s.Name {
	case "Boot from ISO":
		m.result = "\uf0c1 Fetching ISOs (PiKVM + local iso/)..."
		m.operationBusy = true
		return fetchISOsCmd()
	case "Boot to BIOS":
		m.selectingBIOSKey = true
		m.result = ""
		return nil
	default:
		m.operationBusy = true
		m.result = fmt.Sprintf("\uf021 Running %s…", s.Name)
		port := m.port
		return scriptCmd(func() string { return s.Run(port) })
	}
}

// opVisualState describes how an operation should be rendered given the live
// state of the currently-selected port (idea #9). Returns a state key
// ("dimmed" or "normal") and an optional parenthetical suffix.
//
// PiKVM's /api/switch can legitimately return summary.active_id == -1
// (e.g., transiently right after a power-on, or when no port has an
// input yet), so m.port is allowed to be negative. portKnown must check
// BOTH bounds before indexing m.powerLeds — otherwise the View() pass
// panics with "index out of range [-1]" and Bubble Tea kills the program.
func (m Model) opVisualState(name string) (vs, suffix string) {
	// ESP32 / HTTP power: PiKVM switch LEDs do not reflect Mac state — always allow click/long.
	if m.portUsesHTTPPower(m.port) {
		switch name {
		case "Power Click", "Power Long Press", "Power ON", "Power OFF":
			return "normal", ""
		}
	}

	powerOn := m.atxPower
	if !m.directATX {
		portKnown := m.port >= 0 && m.port < len(m.powerLeds)
		powerOn = portKnown && m.powerLeds[m.port]
		switch name {
		case "Power ON":
			if portKnown && powerOn {
				return "dimmed", " (already on)"
			}
			return "normal", ""
		case "Power OFF":
			if portKnown && !powerOn {
				return "dimmed", " (already off)"
			}
			return "normal", ""
		case "Power Click", "Power Long Press", "Reset Click", "Reset Long Press":
			if portKnown && !powerOn {
				return "dimmed", " (port off)"
			}
			return "normal", ""
		case "Disconnect Drive":
			if !m.msdConnect {
				return "dimmed", " (no drive attached)"
			}
			return "normal", ""
		}
		return "normal", ""
	}

	switch name {
	case "Power ON":
		if powerOn {
			return "dimmed", " (already on)"
		}
		return "normal", ""
	case "Power OFF":
		if !powerOn {
			return "dimmed", " (already off)"
		}
		return "normal", ""
	case "Power Click", "Power Long Press", "Reset Click", "Reset Long Press":
		if !powerOn {
			return "dimmed", " (host off)"
		}
		return "normal", ""
	case "Disconnect Drive":
		if !m.msdConnect {
			return "dimmed", " (no drive attached)"
		}
		return "normal", ""
	}
	return "normal", ""
}
