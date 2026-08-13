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

	// Hooks event suppression on first event (idea #20). The first
	// SwitchMsg / MsdMsg / ClientsMsg after startup populates the model
	// from "zero" values — we don't want that to fire spurious "port
	// changed from 0 to 5" hooks. These flags flip to true after the
	// first message of each kind so subsequent transitions dispatch.
	firstSwitchSeen   bool
	firstMsdSeen      bool
	firstClientsSeen  bool
}

// InitialModel builds the starting TUI model.
//
// PiKVM's /api/switch can return summary.active_id == -1 (no port
// currently routed). We keep that as-is in m.activePort (so the status
// bar can show "no active port") but clamp the user-cursor m.port to 0
// — the cursor is what the user is hovering over, and indexing slices
// by it must never panic.
func InitialModel() Model {
	sw := api.FetchSwitchState()
	cursor := sw.ActivePort
	if cursor < 0 {
		cursor = 0
	}
	return Model{
		port:           cursor,
		extenders:      sw.Extenders,
		portsPerExt:    sw.PortsPerExt,
		totalPorts:     sw.TotalPorts,
		activePort:     sw.ActivePort,
		availablePorts: sw.Available,
		videoLinks:     sw.VideoLinks,
		usbLinks:       sw.UsbLinks,
		powerLeds:      sw.PowerLeds,
		hddLeds:        sw.HddLeds,
		portsDetected:  true,
		state:          state.Load(),
	}
}

// Init is Bubble Tea's startup hook. We do nothing here; external pollers
// (WebSocket, info) are started in main() and send events via prog.Send.
func (m Model) Init() tea.Cmd {
	return tea.WindowSize()
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

// trySetActive switches PiKVM's active port and updates m.activePort.
func (m *Model) trySetActive(linear int) error {
	if err := api.SetSwitchPort(linear); err != nil {
		return err
	}
	m.activePort = linear
	return nil
}

// launchScript runs (or pops the modal for) the custom script at idx.
func (m *Model) launchScript(idx int) {
	if idx < 0 || idx >= len(scripts.Default) {
		return
	}
	s := scripts.Default[idx]
	switch s.Name {
	case "Boot from ISO":
		m.result = "\uf0c1 Fetching ISOs (PiKVM + local iso/)..."
		entries, err := api.FetchAvailableISOEntries()
		if err != nil {
			m.result = fmt.Sprintf("\uf057 Failed to fetch ISOs: %v", err)
			return
		}
		if len(entries) == 0 {
			m.result = "\uf06a No ISOs found. Put .iso files in ./iso/ or upload: ./pikvm.sh --iso --upload /path/to/file.iso"
			return
		}
		m.selectingISO = true
		m.availableISOEntries = entries
		m.isoCursor = 0
		m.result = ""
	case "Boot to BIOS":
		m.selectingBIOSKey = true
		m.result = ""
	default:
		m.result = s.Run(m.port)
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
	portKnown := m.port >= 0 && m.port < len(m.powerLeds)
	portOn := portKnown && m.powerLeds[m.port]

	switch name {
	case "Power ON":
		if portKnown && portOn {
			return "dimmed", " (already on)"
		}
		return "normal", ""
	case "Power OFF":
		if portKnown && !portOn {
			return "dimmed", " (already off)"
		}
		return "normal", ""
	case "Power Click", "Power Long Press", "Reset Click", "Reset Long Press":
		if portKnown && !portOn {
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
