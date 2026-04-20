package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PiKVM Configuration (loaded from .env)
var (
	pikvmHost        string
	pikvmUser        string
	pikvmPass        string
	baseURL          string
	tailscaleAuthKey string
	ubuntuPassword   string
)

// loadEnv populates the package-level pikvm* / tailscale* / ubuntu* globals.
//
// Lookup order (idea #3 — single config source generated from Vault):
//
//  1. config.json (next to the binary, then cwd) — preferred, JSON schema
//     written by automation/scripts/env-from-vault.sh.
//  2. .env (next to the binary, then cwd) — back-compat with anything
//     hand-written before idea #3.
//
// The function name is kept for backward compatibility with main()/tests.
func loadEnv() error {
	if err := loadFromConfigJSON(); err == nil {
		baseURL = "https://" + pikvmHost + "/api"
		return nil
	}
	if err := loadFromDotenv(); err != nil {
		return err
	}
	baseURL = "https://" + pikvmHost + "/api"
	return nil
}

// resolveConfigPath returns the first existing path matching name in the
// usual locations (next to the binary, then current directory). Empty string
// if nothing was found.
func resolveConfigPath(name string) string {
	if execPath, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(execPath), name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if _, err := os.Stat(name); err == nil {
		return name
	}
	return ""
}

func loadFromConfigJSON() error {
	path := resolveConfigPath("config.json")
	if path == "" {
		return fmt.Errorf("config.json not found")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var cfg struct {
		SchemaVersion    int    `json:"schema_version"`
		PIKVMHost        string `json:"PIKVM_HOST"`
		PIKVMUser        string `json:"PIKVM_USER"`
		PIKVMPass        string `json:"PIKVM_PASS"`
		TailscaleAuthKey string `json:"TAILSCALE_AUTH_KEY"`
		UbuntuPassword   string `json:"UBUNTU_PASSWORD"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.PIKVMHost == "" || cfg.PIKVMUser == "" || cfg.PIKVMPass == "" {
		return fmt.Errorf("%s missing required PIKVM_HOST/USER/PASS", path)
	}
	pikvmHost = cfg.PIKVMHost
	pikvmUser = cfg.PIKVMUser
	pikvmPass = cfg.PIKVMPass
	tailscaleAuthKey = cfg.TailscaleAuthKey
	ubuntuPassword = cfg.UbuntuPassword
	return nil
}

func loadFromDotenv() error {
	path := resolveConfigPath(".env")
	if path == "" {
		return fmt.Errorf("neither config.json nor .env found")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "PIKVM_HOST":
			pikvmHost = value
		case "PIKVM_USER":
			pikvmUser = value
		case "PIKVM_PASS":
			pikvmPass = value
		case "TAILSCALE_AUTH_KEY":
			tailscaleAuthKey = value
		case "UBUNTU_PASSWORD":
			ubuntuPassword = value
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if pikvmHost == "" || pikvmUser == "" || pikvmPass == "" {
		return fmt.Errorf("%s missing required PIKVM_HOST/USER/PASS", path)
	}
	return nil
}

// Styles - Pastel green theme
var (
	// Header style
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#9bf09d")).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#9bf09d")).
			Padding(0, 1)

	// Port info style
	portInfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9bf09d"))

	// Selected item style - pastel green highlighting
	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#9bf09d"))

	// Unselected item style
	unselectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#BBBBBB"))

	// Action name style
	actionNameStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA"))

	// Description style
	descStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Italic(true)

	// Success style
	successStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#9bf09d"))

	// Warning style
	warningStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFD700"))

	// Error style
	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF5F5F"))

	// Help style
	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))

	// Divider style
	dividerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#333333"))
)

type action struct {
	name   string
	desc   string
	apiCmd string
	method string
}

type customScript struct {
	name   string
	desc   string
	script func(int) string
}

var actions = []action{
	{"Power ON", "Turn power on", "/switch/atx/power?port=%d&action=on", "POST"},
	{"Power OFF", "Turn power off", "/switch/atx/power?port=%d&action=off", "POST"},
	{"Power Click", "Power button short press", "/switch/atx/click?port=%d&button=power", "POST"},
	{"Power Long Press", "Force shutdown", "/switch/atx/click?port=%d&button=power_long", "POST"},
	{"Reset Click", "Reset button short press", "/switch/atx/click?port=%d&button=reset", "POST"},
	{"Reset Long Press", "Reset button long press", "/switch/atx/click?port=%d&button=reset_long", "POST"},
	{"Disconnect Drive", "Disconnect virtual USB drive", "SPECIAL:disconnect_drive", "POST"},
}

var customScripts = []customScript{
	{"Boot from ISO", "Select and boot from any available ISO", bootFromISO},
	{"Setup Ubuntu Server", "Install SSH + Tailscale automation", setupUbuntuServer},
	{"Boot to BIOS", "Pick a BIOS key (F7/Del/F2/F12/Esc/...) and spam it", bootToBIOSPlaceholder},
	{"Type from text.txt", "Type contents of text.txt file", testKeyboard},
	{"View video stream", "Open PiKVM KVM in browser (video + keyboard/mouse)", viewVideoStream},
	{"View video (mpv)", "Watch HDMI stream in mpv (needs websocat + mpv)", viewVideoMpv},
}

// bootToBIOSPlaceholder is wired into customScripts but never actually called:
// the script-launch handlers detect the "Boot to BIOS" name and pop the
// modal key picker instead (same pattern as bootFromISO).
func bootToBIOSPlaceholder(port int) string { return "" }

// launchScript runs (or pops the relevant modal for) the customScript at idx.
// It centralizes the "what to do when a script is selected" logic so the
// digit and Enter handlers in both focus-modes don't duplicate the switch.
func (m *model) launchScript(idx int) {
	if idx < 0 || idx >= len(customScripts) {
		return
	}
	s := customScripts[idx]
	switch s.name {
	case "Boot from ISO":
		m.result = "\uf0c1 Fetching ISOs (PiKVM + local iso/)..."
		entries, err := fetchAvailableISOEntries()
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
		m.result = s.script(m.port)
	}
}

type portInfo struct {
	id     int
	active bool
}

// isoEntry is one option in the Boot-from-ISO list (on PiKVM or local file).
type isoEntry struct {
	Display   string `json:"display"`              // shown in list (e.g. "debian.iso (local)")
	Name      string `json:"name"`                 // filename used for PiKVM set_params
	LocalPath string `json:"local_path,omitempty"` // non-empty if local file (upload first)
}

// focusMode: which menu is active for number-key input.
//
//	""         = none
//	"extender" = picking extender 1..Extenders
//	"port"     = picking port 1..PortsPerExt on the current extender
//	"ops"      = operations
//	"scripts"  = custom scripts
type model struct {
	cursor              int
	port                int        // linear port (0-based), e.g. 5 = extender 2 / port 2
	extenders           int        // number of extender units (e.g. 2)
	portsPerExt         int        // ports per extender (e.g. 4)
	totalPorts          int        // extenders * portsPerExt
	activePort          int        // last known PiKVM-side active port (linear, 0-based)
	result              string
	quitting            bool
	availablePorts      []portInfo
	portsDetected       bool
	focusMode           string
	inScripts           bool      // true if cursor is in custom scripts (used when navigating with arrows)
	selectingISO        bool      // true if selecting ISO from list
	availableISOEntries []isoEntry
	isoCursor           int
	selectingBIOSKey    bool      // true if picking a BIOS key (idea #6)

	// --- live state from the WebSocket subscription (Phase 1, idea #1) ---
	wsConnected bool      // true while the /api/ws subscription is healthy
	wsLastError string    // last reconnect error (shown if disconnected)
	wsLastEvent time.Time // when we last received any event (heartbeat indicator)
	wsClients   int       // count of WS clients currently attached to PiKVM
	atxPower    bool      // global ATX power LED (set up for Phase 2 idea 9)
	atxHdd      bool      // global ATX HDD LED
	atxBusy     bool      // ATX is busy executing a click

	// per-port live signals indexed by linear port (length == totalPorts)
	videoLinks []bool
	usbLinks   []bool
	powerLeds  []bool
	hddLeds    []bool

	// /api/info snapshot for the status bar (idea #10)
	info infoState

	// Per-port profiles from ~/.config/pikvm/state.json (idea #5). Loaded
	// once at startup; saveState() is called whenever the TUI learns a new
	// preference it wants to persist (e.g. after the user picks a BIOS key).
	state pikvmState
	msdOnline   bool      // mass-storage device is online
	msdBusy     bool      // MSD is currently writing
	msdConnect  bool      // MSD is connected to the server
	msdFree     int64     // free bytes on MSD storage
	msdTotal    int64     // total bytes on MSD storage
	msdUpload   bool      // an upload is in progress
	msdUpName   string    // name of the file currently uploading
	msdUpPct    float64   // upload progress 0..100
}

func initialModel() model {
	// Silent startup — the status bar at the bottom already shows
	// user@host and live connection state, so extra "Connecting..." prints
	// would just litter the user's scrollback above the TUI.
	state := fetchSwitchState()
	return model{
		cursor:         0,
		port:           state.ActivePort,
		extenders:      state.Extenders,
		portsPerExt:    state.PortsPerExt,
		totalPorts:     state.TotalPorts,
		activePort:     state.ActivePort,
		availablePorts: state.Available,
		videoLinks:     state.VideoLinks,
		usbLinks:       state.UsbLinks,
		powerLeds:      state.PowerLeds,
		hddLeds:        state.HddLeds,
		portsDetected:  true,
		inScripts:      false,
		state:          loadState(),
	}
}

// portName returns the user-defined profile name for a linear port, or the
// canonical "X.Y" form when no name is saved. Pure display helper.
func (m model) portName(linear int) string {
	id := portExtID(linear, m.portsPerExt)
	if p, ok := m.state.Ports[id]; ok && p.Name != "" {
		return p.Name
	}
	return id
}

// portExtIDOf is like portExtID but uses the model's own topology.
func (m model) portExtIDOf(linear int) string { return portExtID(linear, m.portsPerExt) }

// savedBIOSKey returns the saved BIOS key label for the given port, or ""
// if none is saved.
func (m model) savedBIOSKey(linear int) string {
	p := m.state.Ports[m.portExtIDOf(linear)]
	return p.BIOSKey
}

// extenderOf returns the 1-based extender for a linear port.
func (m model) extenderOf(linear int) int { return linear/m.portsPerExt + 1 }

// portOf returns the 1-based port (within the extender) for a linear port.
func (m model) portOf(linear int) int { return linear%m.portsPerExt + 1 }

// linearPort builds a linear port index from 1-based extender + 1-based port.
func (m model) linearPort(ext, port int) int { return (ext-1)*m.portsPerExt + (port - 1) }

// opVisualState describes how an operation should be rendered given the live
// state of the currently-selected port (idea #9):
//
//	"dimmed": redundant or ineffective right now (Power ON when port is
//	          already on, Reset Click when port is off, Disconnect Drive
//	          when nothing is mounted) — rendered very dim grey.
//	"normal": no opinion, render in default unselected color.
//
// suffix is an optional parenthetical hint shown after the action name (e.g.
// "Power OFF (already off)") so the user understands at a glance WHY it's
// dimmed without having to know the convention.
//
// Cursor focus styling in the View always wins — the user can still run a
// dimmed op if they want; the dim is a hint, not enforcement.
//
// Note: an earlier iteration also returned "primary" to bold-highlight the
// most likely next action (e.g. Power ON when off). That looked too much
// like permanent cursor focus and was removed by user request.
func (m model) opVisualState(name string) (state, suffix string) {
	portKnown := m.port < len(m.powerLeds)
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

// trySetActive switches the PiKVM active port and updates m.activePort on success.
// Failures are non-fatal (still update m.port locally) but recorded in m.result via caller.
func (m *model) trySetActive(linear int) error {
	if err := setSwitchPort(linear); err != nil {
		return err
	}
	m.activePort = linear
	return nil
}

func (m model) Init() tea.Cmd {
	return nil
}

// ----------------------------------------------------------------------------
// Shared HTTP client (roadmap idea #4).
//
// Previously every API call constructed its own *http.Transport + tls.Config
// + *http.Client. That meant a fresh TCP + TLS handshake per request — about
// 10-30 ms of wall-clock waste each time, plus the risk of leaking idle
// connections.
//
// Now there is exactly one client process-wide, lazily initialized. The
// transport keeps a small idle-connection pool so back-to-back calls reuse
// the underlying TCP+TLS session. Per-call timeouts are imposed through
// context (not a client-level Timeout, which would clobber every caller).
// ----------------------------------------------------------------------------

var (
	pikvmHTTPOnce   sync.Once
	pikvmHTTPClient *http.Client
)

func httpClient() *http.Client {
	pikvmHTTPOnce.Do(func() {
		pikvmHTTPClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
				MaxIdleConns:        8,
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     90 * time.Second,
				ForceAttemptHTTP2:   false, // PiKVM (kvmd) is HTTP/1.1 only
			},
			// No client-level Timeout: each pikvmDo call sets its own via ctx.
		}
	})
	return pikvmHTTPClient
}

// newPikvmRequest builds an authenticated, context-bound request to
// baseURL+endpoint. Caller is responsible for sending it via runPikvmRequest
// AND for calling the returned cancel func (typically deferred until after
// the response body is closed). Use this when you need to set extra headers
// before sending; otherwise prefer pikvmDo.
func newPikvmRequest(method, endpoint string, body io.Reader, timeout time.Duration) (*http.Request, context.CancelFunc, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	req, err := http.NewRequestWithContext(ctx, method, baseURL+endpoint, body)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	req.SetBasicAuth(pikvmUser, pikvmPass)
	return req, cancel, nil
}

// runPikvmRequest sends a request built by newPikvmRequest and wires the
// supplied cancel func into resp.Body so closing the body also frees the
// per-call context.
func runPikvmRequest(req *http.Request, cancel context.CancelFunc) (*http.Response, error) {
	resp, err := httpClient().Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = &cancelBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// pikvmDo is the common case: build + send. Caller MUST close resp.Body.
// Pass timeout <= 0 to use the 10s default.
func pikvmDo(method, endpoint string, body io.Reader, timeout time.Duration) (*http.Response, error) {
	req, cancel, err := newPikvmRequest(method, endpoint, body, timeout)
	if err != nil {
		return nil, err
	}
	return runPikvmRequest(req, cancel)
}

// cancelBody calls the request's context cancel func when the body is
// closed. Without this, the per-call timeout context would leak until GC.
type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelBody) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// switchState describes the PiKVM switch topology and current active port.
// Linear port indexing is used everywhere internally:
//
//	linearPort = (extender-1) * portsPerExt + (port-1)   // both 1-based
//	            = unit * portsPerExt + channel           // both 0-based
type switchState struct {
	Extenders   int        `json:"extenders"`     // number of extender units (e.g. 2)
	PortsPerExt int        `json:"ports_per_ext"` // ports per extender (typically 4)
	TotalPorts  int        `json:"total_ports"`   // Extenders * PortsPerExt
	ActivePort  int        `json:"active_port"`   // currently-active linear port (0-based)
	Available   []portInfo `json:"-"`             // every existing linear port (TUI-internal)

	// Per-port live status (length == TotalPorts when populated).
	// PiKVM reports these in /api/switch and pushes updates over /api/ws.
	VideoLinks []bool `json:"video_links"` // true if the port has a live HDMI signal
	UsbLinks   []bool `json:"usb_links"`   // true if the port has the USB cable up
	PowerLeds  []bool `json:"power_leds"`  // true if the port's ATX power LED is on
	HddLeds    []bool `json:"hdd_leds"`    // true if the port's ATX HDD LED blinked recently
}

// fetchSwitchState queries /api/switch once and returns the topology.
// This replaces the old brute-force detectPorts() that did 16 HEAD requests.
// On error we fall back to a single-extender / single-port assumption so the
// TUI still launches and shows a useful message via Refresh.
func fetchSwitchState() switchState {
	state := switchState{
		Extenders: 1, PortsPerExt: 1, TotalPorts: 1,
		Available: []portInfo{{id: 0, active: true}},
	}

	resp, err := pikvmDo("GET", "/switch", nil, 3*time.Second)
	if err != nil {
		return state
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var parsed struct {
		OK     bool `json:"ok"`
		Result struct {
			Model struct {
				Ports []struct {
					ID      string `json:"id"`
					Channel int    `json:"channel"`
					Unit    int    `json:"unit"`
				} `json:"ports"`
				Units []json.RawMessage `json:"units"`
			} `json:"model"`
			Summary struct {
				ActivePort int    `json:"active_port"`
				ActiveID   string `json:"active_id"`
				Synced     bool   `json:"synced"`
			} `json:"summary"`
			Atx struct {
				Leds struct {
					Power []bool `json:"power"`
					Hdd   []bool `json:"hdd"`
				} `json:"leds"`
			} `json:"atx"`
			Video struct {
				Links []bool `json:"links"`
			} `json:"video"`
			Usb struct {
				Links []bool `json:"links"`
			} `json:"usb"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || !parsed.OK {
		return state
	}

	units := len(parsed.Result.Model.Units)
	total := len(parsed.Result.Model.Ports)
	if units == 0 || total == 0 {
		return state
	}

	state.Extenders = units
	state.TotalPorts = total
	state.PortsPerExt = total / units
	if state.PortsPerExt == 0 {
		state.PortsPerExt = 1
	}
	state.ActivePort = parsed.Result.Summary.ActivePort
	state.Available = state.Available[:0]
	for i := 0; i < total; i++ {
		state.Available = append(state.Available, portInfo{id: i, active: true})
	}
	state.VideoLinks = parsed.Result.Video.Links
	state.UsbLinks = parsed.Result.Usb.Links
	state.PowerLeds = parsed.Result.Atx.Leds.Power
	state.HddLeds = parsed.Result.Atx.Leds.Hdd
	return state
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	// ---- Live PiKVM events from /api/ws (Phase 1, idea #1) -----------------
	case wsConnectedMsg:
		m.wsConnected = true
		m.wsLastError = ""
		m.wsLastEvent = time.Now()
		return m, nil

	case wsDisconnectedMsg:
		m.wsConnected = false
		if msg.err != nil {
			m.wsLastError = msg.err.Error()
		}
		return m, nil

	case wsSwitchMsg:
		// Topology / active port pushed by PiKVM. Only overwrite if the
		// payload looks valid (non-zero ports).
		m.wsLastEvent = time.Now()
		if msg.state.TotalPorts > 0 {
			// If the user's current selection was just tracking PiKVM's old
			// active port (the common case: "I haven't manually picked yet"),
			// keep following along. Otherwise leave m.port alone so a remote
			// change doesn't yank the cursor away from what they're focused on.
			followActive := m.port == m.activePort || m.port >= msg.state.TotalPorts

			m.extenders = msg.state.Extenders
			m.portsPerExt = msg.state.PortsPerExt
			m.totalPorts = msg.state.TotalPorts
			m.activePort = msg.state.ActivePort
			m.availablePorts = msg.state.Available
			m.videoLinks = msg.state.VideoLinks
			m.usbLinks = msg.state.UsbLinks
			m.powerLeds = msg.state.PowerLeds
			m.hddLeds = msg.state.HddLeds

			if followActive {
				m.port = msg.state.ActivePort
			}
		}
		return m, nil

	case wsAtxMsg:
		m.wsLastEvent = time.Now()
		m.atxBusy = msg.Busy
		m.atxPower = msg.PowerLed
		m.atxHdd = msg.HddLed
		return m, nil

	case wsMsdMsg:
		m.wsLastEvent = time.Now()
		m.msdOnline = msg.Online
		m.msdBusy = msg.Busy
		m.msdConnect = msg.Connected
		m.msdFree = msg.FreeBytes
		m.msdTotal = msg.TotalBytes
		m.msdUpload = msg.Uploading
		m.msdUpName = msg.UploadName
		m.msdUpPct = msg.UploadPct
		return m, nil

	case wsClientsMsg:
		m.wsLastEvent = time.Now()
		m.wsClients = msg.Count
		return m, nil

	case wsInfoMsg:
		// Periodic /api/info snapshot. Doesn't bump wsLastEvent — that
		// indicator is specifically about the WS subscription health.
		m.info = msg.state
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit

		case "esc":
			switch {
			case m.selectingISO:
				m.selectingISO = false
				m.isoCursor = 0
				m.result = "ISO selection cancelled"
			case m.selectingBIOSKey:
				m.selectingBIOSKey = false
				m.result = "BIOS key selection cancelled"
			case m.focusMode != "":
				m.focusMode = ""
			default:
				m.quitting = true
				return m, tea.Quit
			}

		case "e":
			if !m.selectingISO && !m.selectingBIOSKey {
				m.focusMode = "extender"
			}

		case "p":
			if !m.selectingISO && !m.selectingBIOSKey {
				m.focusMode = "port"
			}

		case "o":
			if !m.selectingISO && !m.selectingBIOSKey {
				m.focusMode = "ops"
				m.cursor = 0
			}

		case "c":
			if !m.selectingISO && !m.selectingBIOSKey {
				m.focusMode = "scripts"
				m.cursor = 0
			}

		case "up", "k":
			if m.selectingISO {
				if m.isoCursor > 0 {
					m.isoCursor--
				}
			} else if m.focusMode == "ops" && m.cursor > 0 {
				m.cursor--
			} else if m.focusMode == "scripts" && m.cursor > 0 {
				m.cursor--
			} else if m.focusMode == "" && m.cursor > 0 {
				m.cursor--
			} else if m.focusMode == "" && m.inScripts {
				m.inScripts = false
				m.cursor = len(actions) - 1
			}

		case "down", "j":
			if m.selectingISO {
				if m.isoCursor < len(m.availableISOEntries)-1 {
					m.isoCursor++
				}
			} else if m.focusMode == "ops" && m.cursor < len(actions)-1 {
				m.cursor++
			} else if m.focusMode == "scripts" && m.cursor < len(customScripts)-1 {
				m.cursor++
			} else if m.focusMode == "" && m.cursor < len(actions)-1 {
				m.cursor++
			} else if m.focusMode == "" && m.cursor == len(actions)-1 {
				m.inScripts = true
				m.cursor = 0
			} else if m.focusMode == "" && m.inScripts && m.cursor < len(customScripts)-1 {
				m.cursor++
			}

		case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
			if m.selectingISO {
				// no digit handling in ISO list
			} else if m.selectingBIOSKey {
				digit := int(msg.String()[0] - '0')
				if digit < 1 || digit > len(biosKeyOptions) {
					m.result = fmt.Sprintf("\uf071 Pick 1-%d (or ESC)", len(biosKeyOptions))
					break
				}
				opt := biosKeyOptions[digit-1]
				m.selectingBIOSKey = false

				// Remember the user's choice per port so next time we can
				// pre-indicate the saved default (idea #5).
				id := m.portExtIDOf(m.port)
				prof := m.state.Ports[id]
				if prof.BIOSKey != opt.key {
					prof.BIOSKey = opt.key
					m.state = setPortProfile(m.state, id, prof)
					if err := saveState(m.state); err != nil {
						m.result = fmt.Sprintf("\uf071 saved BIOS key %s, but could not persist: %v", opt.label, err)
						break
					}
				}
				m.result = bootToBIOSWithKey(m.port, opt)
			} else if m.focusMode == "extender" {
				digit := int(msg.String()[0] - '0')
				if digit < 1 || digit > m.extenders {
					m.result = fmt.Sprintf("\uf071 Only %d extender(s) available (press 1-%d)", m.extenders, m.extenders)
					break
				}
				curSubPort := m.portOf(m.port)
				newPort := m.linearPort(digit, curSubPort)
				if !isPortAvailable(newPort, m.availablePorts) {
					m.result = fmt.Sprintf("\uf071 Port %d.%d not available", digit, curSubPort)
					break
				}
				m.port = newPort
				if err := m.trySetActive(newPort); err != nil {
					m.result = fmt.Sprintf("\uf00c Extender %d selected (port %d.%d). \uf071 set_active: %v", digit, digit, curSubPort, err)
				} else {
					m.result = fmt.Sprintf("\uf00c Extender %d selected → active port %d.%d", digit, digit, curSubPort)
				}
			} else if m.focusMode == "port" {
				digit := int(msg.String()[0] - '0')
				if digit < 1 || digit > m.portsPerExt {
					m.result = fmt.Sprintf("\uf071 Only ports 1-%d on extender %d (press 1-%d)", m.portsPerExt, m.extenderOf(m.port), m.portsPerExt)
					break
				}
				curExt := m.extenderOf(m.port)
				newPort := m.linearPort(curExt, digit)
				if !isPortAvailable(newPort, m.availablePorts) {
					m.result = fmt.Sprintf("\uf071 Port %d.%d not available", curExt, digit)
					break
				}
				m.port = newPort
				if err := m.trySetActive(newPort); err != nil {
					m.result = fmt.Sprintf("\uf00c Port %d.%d selected. \uf071 set_active: %v", curExt, digit, err)
				} else {
					m.result = fmt.Sprintf("\uf00c Port %d.%d → now active on PiKVM", curExt, digit)
				}
			} else if m.focusMode == "ops" {
				digit := int(msg.String()[0] - '0')
				if digit >= 1 && digit <= 7 {
					m.cursor = digit - 1
				}
			} else if m.focusMode == "scripts" {
				digit := int(msg.String()[0] - '0')
				if digit >= 1 && digit <= 9 {
					idx := digit - 1
					if idx < len(customScripts) {
						m.cursor = idx
						m.launchScript(idx)
					}
				}
			}
			// when focusMode == "", number keys do nothing (user must press e/o/c first)

		case "r":
			if !m.selectingISO {
				// Force a fresh /api/ws subscription. The reader goroutine
				// will close the current conn, dial again, and PiKVM will
				// re-emit the full state burst (switch/atx/msd/...) which our
				// reducers fold into the model.
				requestWSReconnect()
				m.wsConnected = false
				m.wsLastError = ""
				m.result = "\uf021 Reconnecting WebSocket..."
			}

		case "enter", " ":
			if m.selectingISO {
				if m.isoCursor < len(m.availableISOEntries) {
					entry := m.availableISOEntries[m.isoCursor]
					m.selectingISO = false
					m.result = bootFromISOEntry(m.port, entry)
				}
			} else if m.focusMode == "ops" {
				m.result = executeAction(actions[m.cursor], m.port)
			} else if m.focusMode == "scripts" {
				m.launchScript(m.cursor)
			} else if m.focusMode == "" && m.inScripts {
				m.launchScript(m.cursor)
			} else if m.focusMode == "" && !m.inScripts {
				m.result = executeAction(actions[m.cursor], m.port)
			}
		}
	}

	return m, nil
}

func (m model) View() string {
	if m.quitting {
		return successStyle.Render("\n\uf00c Goodbye!\n\n")
	}

	// BIOS-key picker (idea #6) — shown when "Boot to BIOS" was selected.
	// If the target port has a saved default (idea #5), the matching row is
	// marked with "★" so the user can see it at a glance.
	if m.selectingBIOSKey {
		var s strings.Builder
		s.WriteString("\n")
		s.WriteString(headerStyle.Render(" \uf132 Choose BIOS key to spam "))
		s.WriteString("\n\n")

		curExt := m.extenderOf(m.port)
		curSubPort := m.portOf(m.port)
		target := fmt.Sprintf("%d.%d", curExt, curSubPort)
		if p, ok := m.state.Ports[m.portExtIDOf(m.port)]; ok && p.Name != "" {
			target = fmt.Sprintf("%s (%s)", target, p.Name)
		}
		s.WriteString("  " + portInfoStyle.Render(
			fmt.Sprintf("\uf0e4 Target port: %s   (60 presses over 30s, in parallel with Power ON)",
				target)) + "\n")
		savedKey := m.savedBIOSKey(m.port)
		if savedKey != "" {
			s.WriteString("  " + helpStyle.Render(fmt.Sprintf("saved default: %s (★) — pick any key to change it", savedKey)) + "\n")
		}
		s.WriteString("\n")

		// Render two columns of options to keep things tidy.
		half := (len(biosKeyOptions) + 1) / 2
		for row := 0; row < half; row++ {
			line := "  "
			for col := 0; col < 2; col++ {
				idx := col*half + row
				if idx >= len(biosKeyOptions) {
					continue
				}
				opt := biosKeyOptions[idx]
				marker := " "
				if opt.key == savedKey {
					marker = "★"
				}
				cell := fmt.Sprintf(" %s [%d] %-4s  %s", marker, idx+1, opt.label, biosKeyHint(opt.label))
				line += lipgloss.NewStyle().Width(36).Render(unselectedStyle.Render(cell))
			}
			s.WriteString(line + "\n")
		}
		s.WriteString("\n")

		help := helpStyle.Render(fmt.Sprintf("1-%d: pick (saves to profile)   ESC: cancel", len(biosKeyOptions)))
		s.WriteString("  " + help + "\n\n")
		return s.String()
	}

	// ISO Selection Mode
	if m.selectingISO {
		var s strings.Builder
		s.WriteString("\n")

		// Header
		header := headerStyle.Render(" \uf0c1 Select ISO to Boot ")
		s.WriteString(header + "\n\n")

		// Port info (display 1-16)
		portInfo := fmt.Sprintf("\uf0e4 Port: %d  │  ISOs available: %d", m.port+1, len(m.availableISOEntries))
		s.WriteString("  " + portInfoStyle.Render(portInfo) + "\n\n")

		// ISO list
		for i, entry := range m.availableISOEntries {
			if i == m.isoCursor {
				line := fmt.Sprintf("  \uf0a1 %s", entry.Display)
				s.WriteString("  " + selectedStyle.Render(line) + "\n")
			} else {
				line := fmt.Sprintf("  \uf15b %s", entry.Display)
				s.WriteString("  " + unselectedStyle.Render(line) + "\n")
			}
		}
		s.WriteString("\n")

		// Help
		help := helpStyle.Render("↑/↓/k/j: Navigate  │  \uf04b Enter: Boot  │  ESC: Cancel")
		s.WriteString("  " + help + "\n\n")

		return s.String()
	}

	// ---- Two columns: left = ports + operations, right = custom scripts ----
	const leftWidth = 52

	// Left column: header, [E] Extenders/ports, current port, [O] Operations
	var left strings.Builder
	left.WriteString("\n")
	left.WriteString(headerStyle.Render(" \uf0e7 PiKVM ATX Power Control "))
	left.WriteString("\n\n")

	// [E] Extender row: [1] [2]
	curExt := m.extenderOf(m.port)
	curSubPort := m.portOf(m.port)

	var extRow strings.Builder
	if m.focusMode == "extender" {
		extRow.WriteString(selectedStyle.Render("[E] Extender:"))
	} else {
		extRow.WriteString(unselectedStyle.Render("[E] Extender:"))
	}
	for i := 1; i <= m.extenders; i++ {
		extRow.WriteString(" ")
		label := fmt.Sprintf("[%d]", i)
		switch {
		case i == curExt && m.focusMode == "extender":
			extRow.WriteString(selectedStyle.Render(label))
		case i == curExt:
			extRow.WriteString(successStyle.Render(label))
		default:
			extRow.WriteString(unselectedStyle.Render(label))
		}
	}
	left.WriteString("  " + extRow.String() + "\n")

	// [P] Port row: [1]    [2]    [3]    [4]
	// Each port box is widened to a 7-char cell so the 3-glyph status row
	// below aligns visually under it.
	const portCellW = 7

	var portRow strings.Builder
	if m.focusMode == "port" {
		portRow.WriteString(selectedStyle.Render("[P] Port:    "))
	} else {
		portRow.WriteString(unselectedStyle.Render("[P] Port:    "))
	}
	for i := 1; i <= m.portsPerExt; i++ {
		portRow.WriteString(" ")
		label := fmt.Sprintf("[%d]", i)
		var styled string
		switch {
		case i == curSubPort && m.focusMode == "port":
			styled = selectedStyle.Render(label)
		case i == curSubPort:
			styled = successStyle.Render(label)
		default:
			styled = unselectedStyle.Render(label)
		}
		portRow.WriteString(lipgloss.NewStyle().Width(portCellW).Render(styled))
	}
	left.WriteString("  " + portRow.String() + "\n")

	// Status row aligned under the port boxes:
	//      <video> <usb> <power>      <-- 5 visible glyphs incl. spaces, 5 cols
	// Each glyph is colored: green when lit on the active port, pastel-green
	// when lit on an inactive port, dim · when off.
	var statusRow strings.Builder
	statusRow.WriteString(strings.Repeat(" ", len("[P] Port:    "))) // align under boxes
	for i := 1; i <= m.portsPerExt; i++ {
		statusRow.WriteString(" ")
		linear := m.linearPort(curExt, i)
		statusRow.WriteString(lipgloss.NewStyle().Width(portCellW).Render(m.portStatusGlyphs(linear)))
	}
	left.WriteString("  " + statusRow.String() + "\n")

	// Active-port summary; include the port's profile name if one is saved
	// (idea #5) so "2.4" becomes "2.4 (j4yn0)".
	activeExt := m.activePort/m.portsPerExt + 1
	activePortNum := m.activePort%m.portsPerExt + 1
	syncIcon := successStyle.Render("\uf058")
	if m.activePort != m.port {
		activeLabel := fmt.Sprintf("%d.%d", activeExt, activePortNum)
		if p, ok := m.state.Ports[portExtID(m.activePort, m.portsPerExt)]; ok && p.Name != "" {
			activeLabel = fmt.Sprintf("%s (%s)", activeLabel, p.Name)
		}
		syncIcon = warningStyle.Render("\uf06a") + " (PiKVM is on " + activeLabel + ")"
	}
	selLabel := fmt.Sprintf("%d.%d", curExt, curSubPort)
	if p, ok := m.state.Ports[portExtID(m.port, m.portsPerExt)]; ok && p.Name != "" {
		selLabel = fmt.Sprintf("%s (%s)", selLabel, p.Name)
	}
	summary := fmt.Sprintf("\uf0e4 Selected: %s  ", selLabel)
	legend := helpStyle.Render(fmt.Sprintf("  legend: %s video  %s usb  %s power", iconVideo, iconUsb, iconPower))
	left.WriteString("  " + portInfoStyle.Render(summary) + syncIcon + "\n")
	left.WriteString(legend + "\n")
	left.WriteString("\n")

	// [O] Operations — styling per row reflects live applicability (idea #9):
	//   primary  = suggested next action (e.g. Power ON when port is off)
	//   dimmed   = redundant or ineffective right now (Power ON when on)
	//   normal   = no live signal / not state-dependent
	// Cursor focus highlighting always wins so the user can still run a
	// dimmed op explicitly.
	if m.focusMode == "ops" {
		left.WriteString("  " + selectedStyle.Render("[O] Operations (1-7 select, Enter run):") + "\n")
	} else {
		left.WriteString("  " + unselectedStyle.Render("[O] Operations:") + "\n")
	}
	for i, act := range actions {
		_, suffix := m.opVisualState(act.name)
		line := fmt.Sprintf("  [%d] %s%s", i+1, act.name, suffix)
		var rendered string
		if m.focusMode == "ops" && m.cursor == i {
			rendered = selectedStyle.Render(line)
		} else {
			// All non-focused rows render identically — the parenthetical
			// suffix (e.g. " (already off)", " (port off)") is the only hint.
			rendered = unselectedStyle.Render(line)
		}
		left.WriteString("  " + rendered + "\n")
	}

	leftCol := lipgloss.NewStyle().Width(leftWidth).Render(left.String())

	// Right column: [C] Custom Scripts — j/k navigate, 1-9 jump, Enter run
	var right strings.Builder
	right.WriteString("\n\n\n")
	if m.focusMode == "scripts" {
		right.WriteString("  " + selectedStyle.Render("[C] Custom Scripts (j/k, 1-9, Enter):") + "\n\n")
	} else {
		right.WriteString("  " + unselectedStyle.Render("[C] Custom Scripts:") + "\n\n")
	}
	for i, script := range customScripts {
		line := fmt.Sprintf("  [%d] %s", i+1, script.name)
		highlighted := (m.focusMode == "scripts" && m.cursor == i) ||
			(m.focusMode == "" && m.inScripts && m.cursor == i)
		if highlighted {
			right.WriteString("  " + selectedStyle.Render(line) + "\n")
		} else {
			right.WriteString("  " + unselectedStyle.Render(line) + "\n")
		}
	}

	rightCol := lipgloss.NewStyle().Width(48).Render(right.String())

	// Join side by side
	main := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, "   ", rightCol)

	// Help and result below
	var bottom strings.Builder

	// Continuous status bar (idea #10): user@host, platform, fw, topology,
	// MSD, uptime, cpu/temp, clients, ATX-busy badge, WS health dot.
	bottom.WriteString("\n  " + m.renderStatusBar() + "\n")

	bottom.WriteString("\n  ")
	bottom.WriteString(helpStyle.Render("e: Extender  p: Port  o: Operations  c: Scripts  1-9/Enter  ESC: back  r: Reconnect  q: Quit"))
	bottom.WriteString("\n")
	if m.result != "" {
		bottom.WriteString("\n  ")
		if strings.Contains(m.result, "\uf00c") || strings.Contains(m.result, "Success") {
			bottom.WriteString(successStyle.Render(m.result))
		} else if strings.Contains(m.result, "\uf071") || strings.Contains(m.result, "Warning") {
			bottom.WriteString(warningStyle.Render(m.result))
		} else if strings.Contains(m.result, "\uf057") || strings.Contains(m.result, "Error") {
			bottom.WriteString(errorStyle.Render(m.result))
		} else {
			bottom.WriteString(m.result)
		}
		bottom.WriteString("\n")
	}
	bottom.WriteString("\n")

	return main + bottom.String()
}

// Per-port status glyphs from JetBrainsMono Nerd Font (Font Awesome).
// Render these only when the corresponding link/LED is true; otherwise show
// a dim middle-dot to keep column alignment.
const (
	iconVideo = "\uf26c" // nf-fa-television (HDMI signal present)
	iconUsb   = "\uf287" // nf-fa-usb        (USB cable up to the target)
	iconPower = "\uf0e7" // nf-fa-bolt       (ATX power LED on)
	iconHdd   = "\uf0a0" // nf-fa-hdd_o      (ATX HDD activity LED)
	iconDim   = "·"
)

// portStatusGlyphs returns a 3-glyph status string for one linear port:
// video / usb / power. The active port (the one PiKVM video/USB are routed
// through) gets its glyphs rendered in success-green; non-active but lit
// glyphs are rendered in pastel green (portInfoStyle); off glyphs are dim.
func (m model) portStatusGlyphs(linear int) string {
	on := func(arr []bool) bool {
		return linear < len(arr) && arr[linear]
	}
	style := func(lit bool, glyph string) string {
		if !lit {
			return helpStyle.Render(iconDim)
		}
		if linear == m.activePort {
			return successStyle.Render(glyph)
		}
		return portInfoStyle.Render(glyph)
	}
	return style(on(m.videoLinks), iconVideo) + " " +
		style(on(m.usbLinks), iconUsb) + " " +
		style(on(m.powerLeds), iconPower)
}

// renderStatusBar (roadmap idea #10) — the always-visible single-line health
// summary at the bottom of the screen. Pulls together everything we already
// stream + everything we periodically poll:
//
//	user@host  │  PiKVM model  │  kvmd version  │  N ext × M ports  │
//	MSD: ...  │  uptime ...  │  cpu N% / N°C  │  clients N  │  ws ●
//
// Sections that don't have data yet (e.g. before /api/info has returned the
// first snapshot) are skipped silently so the bar doesn't flicker on startup.
func (m model) renderStatusBar() string {
	var parts []string

	// 1) user@host (we always have this from .env / config.json)
	parts = append(parts, helpStyle.Render(fmt.Sprintf("%s@%s", pikvmUser, pikvmHost)))

	// 2) PiKVM platform (from /api/info hw.platform.model)
	if m.info.Platform != "" {
		parts = append(parts, helpStyle.Render("PiKVM "+m.info.Platform))
	}

	// 3) kvmd firmware version
	if m.info.KvmdVersion != "" {
		parts = append(parts, helpStyle.Render("kvmd v"+m.info.KvmdVersion))
	}

	// 4) topology summary (from /api/switch via WS)
	if m.totalPorts > 0 {
		parts = append(parts, helpStyle.Render(fmt.Sprintf("%d ext × %d ports", m.extenders, m.portsPerExt)))
	}

	// 5) MSD state
	if m.msdOnline {
		var msd string
		switch {
		case m.msdUpload:
			msd = fmt.Sprintf("MSD %s %.0f%%", m.msdUpName, m.msdUpPct)
		case m.msdConnect:
			msd = "MSD attached"
		default:
			msd = fmt.Sprintf("MSD idle (%s free)", humanBytes(m.msdFree))
		}
		parts = append(parts, helpStyle.Render(msd))
	}

	// 6) uptime
	if m.info.UptimeTotal > 0 {
		parts = append(parts, helpStyle.Render("up "+formatUptime(m.info)))
	}

	// 7) health (CPU% / temp). Color-code temp: warn over 70°C, error over 80°C.
	if m.info.CPUTempC > 0 {
		tempStr := fmt.Sprintf("%.0f°C", m.info.CPUTempC)
		switch {
		case m.info.CPUTempC >= 80:
			tempStr = errorStyle.Render(tempStr)
		case m.info.CPUTempC >= 70:
			tempStr = warningStyle.Render(tempStr)
		default:
			tempStr = helpStyle.Render(tempStr)
		}
		parts = append(parts, helpStyle.Render(fmt.Sprintf("cpu %d%%", m.info.CPUPercent))+helpStyle.Render(" / ")+tempStr)
	}

	// 8) clients connected to PiKVM (us + anyone else)
	if m.wsClients > 0 {
		parts = append(parts, helpStyle.Render(fmt.Sprintf("clients %d", m.wsClients)))
	}

	// 9) ATX busy badge (rare but useful while a click is mid-flight)
	if m.atxBusy {
		parts = append(parts, warningStyle.Render("ATX busy"))
	}

	// 10) WS connection indicator (always last — most prominent)
	var dot, label string
	switch {
	case m.wsConnected:
		dot = successStyle.Render("●")
		label = "ws live"
	case m.wsLastError != "":
		dot = errorStyle.Render("●")
		label = "ws reconnecting"
	default:
		dot = warningStyle.Render("●")
		label = "ws connecting"
	}
	parts = append(parts, fmt.Sprintf("%s %s", dot, helpStyle.Render(label)))

	return strings.Join(parts, helpStyle.Render("  │  "))
}

// humanBytes formats a byte count in a friendly short form (1.4G, 230M, ...).
func humanBytes(n int64) string {
	const k = 1024
	switch {
	case n >= k*k*k:
		return fmt.Sprintf("%.1fG", float64(n)/float64(k*k*k))
	case n >= k*k:
		return fmt.Sprintf("%.0fM", float64(n)/float64(k*k))
	case n >= k:
		return fmt.Sprintf("%.0fK", float64(n)/float64(k))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

func isPortAvailable(port int, ports []portInfo) bool {
	for _, p := range ports {
		if p.id == port && p.active {
			return true
		}
	}
	return false
}

// sendKey sends a keyboard key press to PiKVM
// For special keys (F7, F2, Delete, etc.) use events/send_key
// For text characters use hid/print
func sendKey(key string) error {
	var endpoint string
	if isSpecialKey(key) {
		// Function keys, Delete, etc. use send_key endpoint
		endpoint = "/hid/events/send_key?key=" + key
	} else {
		endpoint = "/hid/print?text=" + key
	}
	resp, err := pikvmDo("POST", endpoint, nil, 0)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// isSpecialKey checks if a key is a special key (F-keys, Delete, etc.)
func isSpecialKey(key string) bool {
	specialKeys := map[string]bool{
		"F1": true, "F2": true, "F3": true, "F4": true,
		"F5": true, "F6": true, "F7": true, "F8": true,
		"F9": true, "F10": true, "F11": true, "F12": true,
		"Delete": true, "Escape": true, "Enter": true,
		"Tab": true, "Backspace": true,
		"ArrowUp": true, "ArrowDown": true, "ArrowLeft": true, "ArrowRight": true,
	}
	return specialKeys[key]
}

// ----------------------------------------------------------------------------
// Boot-to-BIOS (roadmap idea #6)
//
// Generic key-spammer: power-cycles a port and pounds the chosen BIOS key
// at 2 Hz for 30s in a background goroutine, so the boot screen always
// catches it. Replaces the four hand-coded wrappers (bootToBIOS{,Del,F2,F3})
// — every key plus a few extras (F12, Esc, F8, F11, F1) is now a single-key
// picker entry shown by selectingBIOSKey mode.
// ----------------------------------------------------------------------------

type biosKeyOption struct {
	label string // shown in the picker (kept short)
	key   string // value passed to sendKey() / PiKVM HID API
}

// Order matters — these are picked by digits 1..9.
var biosKeyOptions = []biosKeyOption{
	{"F7", "F7"},     // Dell, HP boot menu
	{"Del", "Delete"}, // ASUS, MSI BIOS setup
	{"F2", "F2"},     // Lenovo, Acer BIOS setup
	{"F3", "F3"},
	{"F12", "F12"},   // Dell boot menu, generic boot order
	{"Esc", "Escape"}, // HP boot menu, some Lenovo
	{"F8", "F8"},     // older boot menu shortcut
	{"F11", "F11"},   // Asrock, some Gigabyte
	{"F1", "F1"},
}

// biosKeyHint returns a short vendor cheat-sheet shown next to each option in
// the picker. Empty for keys without a well-known mapping.
func biosKeyHint(label string) string {
	switch label {
	case "F7":
		return "(Dell, HP boot menu)"
	case "Del":
		return "(ASUS, MSI BIOS setup)"
	case "F2":
		return "(Lenovo, Acer BIOS setup)"
	case "F12":
		return "(Dell boot menu, generic)"
	case "Esc":
		return "(HP boot menu, some Lenovo)"
	case "F11":
		return "(Asrock, some Gigabyte)"
	case "F8":
		return "(older boot menu)"
	}
	return ""
}

// bootToBIOSWithKey power-cycles the port and spams the requested key for ~30s.
func bootToBIOSWithKey(port int, opt biosKeyOption) string {
	go func() {
		for i := 0; i < 60; i++ {
			sendKey(opt.key)
			time.Sleep(500 * time.Millisecond)
		}
	}()

	// Power on in parallel with the key spam — catches the boot screen
	// from frame 1 instead of racing it.
	act := action{"Power ON", "", "/switch/atx/power?port=%d&action=on", "POST"}
	executeAction(act, port)

	return fmt.Sprintf("\uf00c %s spam started! (60 presses over 30s) + Powered on port %d", opt.label, port+1)
}

// Helper function to type a sudo command with password automation
func typeSudoCommand(command string, password string) {
	// Type the command (sendText includes newline)
	sendText(command + "\n")
	time.Sleep(2 * time.Second) // Wait for sudo password prompt

	// Type password (sendText includes newline)
	sendText(password + "\n")
	time.Sleep(2 * time.Second) // Wait for command to execute
}

// Setup Ubuntu Server - installs SSH and Tailscale
func setupUbuntuServer(port int) string {
	if tailscaleAuthKey == "" {
		return "\uf057 Error: TAILSCALE_AUTH_KEY not set in .env file"
	}
	if ubuntuPassword == "" {
		return "\uf057 Error: UBUNTU_PASSWORD not set in .env file"
	}

	// Run commands with password automation
	go func() {
		time.Sleep(1 * time.Second) // Small delay before starting

		// 1. Update package lists
		typeSudoCommand("sudo apt update", ubuntuPassword)

		// 2. Install OpenSSH Server
		typeSudoCommand("sudo apt install -y openssh-server", ubuntuPassword)

		// 3. Enable SSH
		typeSudoCommand("sudo systemctl enable ssh", ubuntuPassword)

		// 4. Start SSH
		typeSudoCommand("sudo systemctl start ssh", ubuntuPassword)

		// 5. Install Tailscale (curl command, then installer will need sudo)
		sendText("curl -fsSL https://tailscale.com/install.sh | sh\n")
		time.Sleep(5 * time.Second) // Wait for installer to start and prompt for sudo

		// Installer will prompt for sudo password
		sendText(ubuntuPassword + "\n")
		time.Sleep(8 * time.Second) // Wait for Tailscale installation to complete

		// 6. Authenticate Tailscale
		tailscaleCmd := fmt.Sprintf("sudo tailscale up --authkey=%s --ssh", tailscaleAuthKey)
		typeSudoCommand(tailscaleCmd, ubuntuPassword)

		// 7. Show status (no sudo needed)
		time.Sleep(2 * time.Second)
		sendText("tailscale status\n")

		// 8. Show SSH info
		time.Sleep(3 * time.Second)
		sendText("echo 'Setup complete! SSH with: ssh $(whoami)@$(tailscale ip -4)'\n")
	}()

	var result strings.Builder
	result.WriteString("\uf121 Ubuntu Server Setup Started!\n\n")
	result.WriteString("  Automated setup with password handling:\n\n")
	result.WriteString("  1. \uf0c8 Update package lists\n")
	result.WriteString("  2. \uf233 Install OpenSSH Server\n")
	result.WriteString("  3. \uf085 Enable & start SSH\n")
	result.WriteString("  4. \uf0c1 Install Tailscale\n")
	result.WriteString("  5. \uf023 Authenticate Tailscale\n")
	result.WriteString("  6. \uf00c Show Tailscale status\n\n")
	result.WriteString("\uf06a Watch your screen - commands + passwords typed automatically!\n")
	result.WriteString("  Each sudo command will auto-enter password after 2s delay\n\n")
	result.WriteString("  After completion, SSH to server with:\n")
	result.WriteString("  ssh username@100.xxx.xxx.xxx\n")

	return result.String()
}

func testKeyboard(port int) string {
	// Read text from text.txt file
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Sprintf("\uf057 Error finding executable path: %v", err)
	}
	textPath := filepath.Join(filepath.Dir(execPath), "text.txt")

	// If running from source (go run), try current directory
	if _, err := os.Stat(textPath); os.IsNotExist(err) {
		textPath = "text.txt"
	}

	// Read the text file
	textBytes, err := os.ReadFile(textPath)
	if err != nil {
		return fmt.Sprintf("\uf057 Error reading text.txt: %v\nMake sure text.txt exists in the same directory as the binary", err)
	}

	textToType := string(textBytes)
	if len(textToType) == 0 {
		return "\uf06a text.txt is empty! Add some text to type."
	}

	// Type the text using PiKVM's HID print API (types entire string)
	go func() {
		sendText(textToType)
	}()

	return fmt.Sprintf("\uf00c Typing text from text.txt (%d characters)...\n  Content: %s\n  Check your connected device!", len(textToType), textToType)
}

// setSwitchPort sets the PiKVM switch active port so video/HID follow the selected port (POST /api/switch/set_active?port=N).
func setSwitchPort(port int) error {
	resp, err := pikvmDo("POST", fmt.Sprintf("/switch/set_active?port=%d", port), nil, 5*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("switch/set_active returned %d", resp.StatusCode)
	}
	return nil
}

// viewVideoStream opens the PiKVM KVM page in the default browser so you can watch the HDMI stream.
func viewVideoStream(port int) string {
	if err := setSwitchPort(port); err != nil {
		// Non-fatal: still open browser, user can switch port there
	}
	kvmURL := fmt.Sprintf("https://%s/", pikvmHost)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", kvmURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", kvmURL)
	default:
		cmd = exec.Command("xdg-open", kvmURL)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("\uf057 Could not open browser: %v\n  Open manually: %s", err, kvmURL)
	}
	return fmt.Sprintf("\uf00c Opening PiKVM in browser (video set to port %d).\n  Log in if prompted, then watch the stream.", port+1)
}

// viewVideoMpv opens the PiKVM HDMI stream in ffplay/mpv. On macOS we run pikvm.sh --stream in a new
// Terminal window so the pipeline gets a real TTY (avoids websocat "Invalid argument" when run from TUI).
// On Linux/Windows we run the pipeline in-process. Raw H.264 needs -probesize/-analyzeduration so the player
// doesn't fail on "unspecified size" before the first keyframe.
func viewVideoMpv(port int) string {
	if _, err := exec.LookPath("websocat"); err != nil {
		return "\uf057 websocat not found in PATH.\n  Install: brew install websocat\n  Run this program from a terminal where PATH includes Homebrew."
	}
	if _, err := exec.LookPath("ffplay"); err != nil {
		if _, err := exec.LookPath("mpv"); err != nil {
			return "\uf057 Need ffplay or mpv in PATH.\n  Install: brew install ffmpeg  (for ffplay) or brew install mpv"
		}
	}
	// Set PiKVM active port so the stream shows the same port as the TUI
	if err := setSwitchPort(port); err != nil {
		// Non-fatal: stream may still show previous port
	}

	execPath, _ := os.Executable()
	scriptDir := filepath.Dir(execPath)
	if _, err := os.Stat(filepath.Join(scriptDir, "pikvm.sh")); os.IsNotExist(err) {
		scriptDir = "."
	}
	scriptPath := filepath.Join(scriptDir, "pikvm.sh")
	absScript, _ := filepath.Abs(scriptPath)

	if runtime.GOOS == "darwin" {
		// Run in new Terminal window so websocat gets a real TTY and the same env as when you run ./pikvm.sh --stream
		runInTerminal := fmt.Sprintf("cd %s && ./pikvm.sh --stream", quotedPath(filepath.Dir(absScript)))
		cmd := exec.Command("osascript", "-e", "tell application \"Terminal\" to do script "+quotedAppleScript(runInTerminal))
		if err := cmd.Run(); err != nil {
			return fmt.Sprintf("\uf057 Could not open Terminal: %v\n  Run in terminal instead: cd %s && ./pikvm.sh --stream", err, scriptDir)
		}
		return fmt.Sprintf("\uf00c Opened stream in a new Terminal window (port %d). Close the window when done.", port+1)
	}

	// Linux/Windows: run pipeline in-process (may hit websocat EINVAL on some setups; then use pikvm.sh --stream in a terminal)
	useFfplay := false
	if _, err := exec.LookPath("ffplay"); err == nil {
		useFfplay = true
	}
	keeperURL := fmt.Sprintf("wss://%s/api/ws?stream=1", pikvmHost)
	keeper := exec.Command("websocat", "-k", keeperURL,
		"-H", "X-KVMD-User: "+pikvmUser,
		"-H", "X-KVMD-Passwd: "+pikvmPass,
	)
	keeper.Stdout = nil
	keeper.Stderr = nil
	if err := keeper.Start(); err != nil {
		return fmt.Sprintf("\uf057 Could not start stream keeper: %v", err)
	}
	defer keeper.Process.Kill()
	time.Sleep(2 * time.Second)

	mediaURL := fmt.Sprintf("wss://%s/api/media/ws?video=h264", pikvmHost)
	var script string
	if useFfplay {
		script = `websocat -b -B10000000 -k "$WS_MEDIA_URL" -H "X-KVMD-User: $WS_USER" -H "X-KVMD-Passwd: $WS_PASS" | ffplay -f h264 -framerate 30 -probesize 10M -analyzeduration 5M -fflags nobuffer -flags low_delay -i pipe:0 -window_title PiKVM`
	} else {
		script = `websocat -b -B10000000 -k "$WS_MEDIA_URL" -H "X-KVMD-User: $WS_USER" -H "X-KVMD-Passwd: $WS_PASS" | mpv --no-cache --demuxer-lavf-format=h264 --demuxer-lavf-o=probesize=10000000,analyzeduration=5000000 -`
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", script)
	} else {
		cmd = exec.Command("sh", "-c", script)
	}
	cmd.Dir = scriptDir
	cmd.Env = append(os.Environ(), "WS_MEDIA_URL="+mediaURL, "WS_USER="+pikvmUser, "WS_PASS="+pikvmPass)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Sprintf("\uf00c Stream ended. %v", err)
	}
	return "\uf00c Stream ended."
}

// quotedPath returns a path safe for use in sh -c "cd ..." (single-quote path).
func quotedPath(p string) string {
	return "'" + strings.ReplaceAll(p, "'", "'\"'\"'") + "'"
}

// quotedAppleScript returns a string safe for AppleScript (backslash-escape " and \).
func quotedAppleScript(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString("\\\\")
		case '"':
			b.WriteString("\\\"")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// sendText POSTs raw text to PiKVM's /hid/print so it gets typed on the
// connected target.  Long text needs the body channel (not a query param)
// hence the per-call header tweak.
func sendText(text string) error {
	req, cancel, err := newPikvmRequest("POST", "/hid/print", strings.NewReader(text), 30*time.Second)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := runPikvmRequest(req, cancel)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// Fetch available ISOs from PiKVM
func fetchAvailableISOs() ([]string, error) {
	resp, err := pikvmDo("GET", "/msd", nil, 10*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse JSON response
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	// Extract image list
	var isos []string
	if resultData, ok := result["result"].(map[string]interface{}); ok {
		if storage, ok := resultData["storage"].(map[string]interface{}); ok {
			if images, ok := storage["images"].(map[string]interface{}); ok {
				for name := range images {
					isos = append(isos, name)
				}
			}
		}
	}

	return isos, nil
}

// getISODir returns the path to the local iso folder (next to executable or cwd).
func getISODir() string {
	execPath, err := os.Executable()
	if err != nil {
		return filepath.Join(".", "iso")
	}
	dir := filepath.Join(filepath.Dir(execPath), "iso")
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	return filepath.Join(".", "iso")
}

// ----------------------------------------------------------------------------
// ISO listing — parallel fetch + 30s TTL cache (roadmap idea #2).
//
// fetchAvailableISOEntries() is called whenever the user opens the
// "Boot from ISO" menu. It pulls from two independent sources:
//
//   1. PiKVM's mass-storage device  (HTTP GET /api/msd, ~500 ms LAN, more on
//      Tailscale) for ISOs already uploaded to the unit.
//   2. The local ./iso/ directory   (filesystem ReadDir, ~tens of ms) for
//      files staged on disk that we can upload + boot.
//
// Both queries are independent, so we run them concurrently. Results are
// memoized in isoCache for cacheTTL; the second open is instant. The cache
// is invalidated automatically whenever the WS sees the MSD storage change
// (e.g. someone uploaded an ISO from the web UI), so the cache never serves
// stale data — it just absorbs back-to-back menu opens.
//
// Per-source failures are tolerated: if PiKVM is unreachable we still return
// the local entries, and vice-versa. Only an "everything failed" call
// returns an error.
// ----------------------------------------------------------------------------

const isoCacheTTL = 30 * time.Second

type isoCacheEntry struct {
	entries   []isoEntry
	fetchedAt time.Time
}

var (
	isoCacheMu sync.Mutex
	isoCache   *isoCacheEntry
)

// invalidateISOCache forces the next fetchAvailableISOEntries call to refetch.
// Called from the WS msd reducer whenever the storage image list changes.
func invalidateISOCache() {
	isoCacheMu.Lock()
	isoCache = nil
	isoCacheMu.Unlock()
}

// fetchAvailableISOEntries returns ISOs on PiKVM plus local .iso files from
// the iso/ folder. PiKVM and local sources are scanned in parallel; results
// are cached for isoCacheTTL.
func fetchAvailableISOEntries() ([]isoEntry, error) {
	// Fast path: serve from cache if fresh.
	isoCacheMu.Lock()
	if isoCache != nil && time.Since(isoCache.fetchedAt) < isoCacheTTL {
		entries := isoCache.entries
		isoCacheMu.Unlock()
		return entries, nil
	}
	isoCacheMu.Unlock()

	var (
		wg            sync.WaitGroup
		pikvmEntries  []isoEntry
		localEntries  []isoEntry
		pikvmErr      error
		localErr      error
	)

	// Source 1: PiKVM /api/msd
	wg.Add(1)
	go func() {
		defer wg.Done()
		names, err := fetchAvailableISOs()
		if err != nil {
			pikvmErr = err
			return
		}
		for _, name := range names {
			pikvmEntries = append(pikvmEntries, isoEntry{Display: name, Name: name, LocalPath: ""})
		}
	}()

	// Source 2: local ./iso/ directory
	wg.Add(1)
	go func() {
		defer wg.Done()
		isoDir := getISODir()
		dirEntries, err := os.ReadDir(isoDir)
		if err != nil {
			// No iso/ folder is fine — not an error, just an empty list.
			if !os.IsNotExist(err) {
				localErr = err
			}
			return
		}
		for _, e := range dirEntries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(strings.ToLower(name), ".iso") {
				continue
			}
			fullPath := filepath.Join(isoDir, name)
			if abs, err := filepath.Abs(fullPath); err == nil {
				fullPath = abs
			}
			localEntries = append(localEntries, isoEntry{
				Display:   name + " (local)",
				Name:      name,
				LocalPath: fullPath,
			})
		}
	}()

	wg.Wait()

	// Combine — order: PiKVM-side first (they're already uploaded and ready
	// to mount), then local files (which need uploading first).
	entries := make([]isoEntry, 0, len(pikvmEntries)+len(localEntries))
	entries = append(entries, pikvmEntries...)
	entries = append(entries, localEntries...)

	// Only fail when both sources failed — partial success is more useful
	// than no result.
	if len(entries) == 0 && pikvmErr != nil && localErr != nil {
		return nil, fmt.Errorf("PiKVM: %v; local: %v", pikvmErr, localErr)
	}
	if len(entries) == 0 && pikvmErr != nil {
		return nil, pikvmErr
	}

	isoCacheMu.Lock()
	isoCache = &isoCacheEntry{entries: entries, fetchedAt: time.Now()}
	isoCacheMu.Unlock()

	return entries, nil
}

// bootFromISOEntry boots from the selected entry; uploads first if it's a local file.
// On macOS (and Linux with terminal) we run the upload in a new terminal window so the TUI stays clean.
func bootFromISOEntry(port int, entry isoEntry) string {
	if entry.LocalPath != "" {
		execPath, _ := os.Executable()
		scriptDir := filepath.Dir(execPath)
		if _, err := os.Stat(filepath.Join(scriptDir, "pikvm.sh")); os.IsNotExist(err) {
			scriptDir = "."
		}
		absDir, _ := filepath.Abs(scriptDir)
		uploadCmd := fmt.Sprintf("cd %s && ./pikvm.sh --iso --upload %s", quotedBash(absDir), quotedBash(entry.LocalPath))

		if runtime.GOOS == "darwin" {
			cmd := exec.Command("osascript", "-e", "tell application \"Terminal\" to do script "+quotedAppleScript(uploadCmd))
			if err := cmd.Run(); err != nil {
				return fmt.Sprintf("\uf057 Could not open Terminal: %v\n  Run in another terminal: ./pikvm.sh --iso --upload %s", err, entry.LocalPath)
			}
			return fmt.Sprintf("\uf00c Upload started in new Terminal window.\n  When it finishes, run Boot from ISO (1) again and select %s to boot.", entry.Name)
		}
		if runtime.GOOS == "linux" {
			// Try gnome-terminal or xterm so logs don't flood the TUI
			for _, termCmd := range []string{"gnome-terminal --", "xterm -e"} {
				var cmd *exec.Cmd
				if strings.HasPrefix(termCmd, "gnome-terminal") {
					cmd = exec.Command("gnome-terminal", "--", "bash", "-c", uploadCmd+"; echo; read -p 'Press Enter to close...'")
				} else {
					cmd = exec.Command("xterm", "-e", "bash", "-c", uploadCmd+"; echo; read -p 'Press Enter to close...'")
				}
				if err := cmd.Start(); err != nil {
					continue
				}
				return fmt.Sprintf("\uf00c Upload started in new window.\n  When it finishes, run Boot from ISO (1) again and select %s to boot.", entry.Name)
			}
		}
		// Fallback: run in process (logs will mix with TUI)
		cmd := exec.Command("sh", "-c", uploadCmd)
		cmd.Dir = scriptDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Sprintf("\uf057 Upload failed: %v\n  Run manually: ./pikvm.sh --iso --upload %s", err, entry.LocalPath)
		}
	}
	return bootFromSpecificISO(port, entry.Name)
}

// quotedBash returns a string safe for sh -c (single-quoted).
func quotedBash(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// Boot from ISO - placeholder; selection is done in TUI
func bootFromISO(port int) string {
	return ""
}

// Boot from specific ISO - generic function for any ISO (must already be on PiKVM).
func bootFromSpecificISO(port int, isoName string) string {
	var result strings.Builder
	result.WriteString(fmt.Sprintf("\uf121 Starting boot from: %s\n\n", isoName))

	// Step 1: Select the ISO
	result.WriteString("  \uf0a0 Step 1: Selecting ISO on PiKVM...\n")
	result.WriteString(fmt.Sprintf("    → API: POST /api/msd/set_params?image=%s\n", isoName))
	if err := selectMSDImage(isoName); err != nil {
		result.WriteString(fmt.Sprintf("  \uf057 Failed to select ISO: %v\n", err))
		return result.String()
	}
	result.WriteString("  \uf00c ISO selected in PiKVM!\n\n")

	// Step 2: Connect the virtual drive to the server
	result.WriteString("  \uf0c1 Step 2: Connecting drive to server (mounting)...\n")
	result.WriteString("    → API: POST /api/msd/set_connected?connected=1\n")
	if err := connectMSD(true); err != nil {
		result.WriteString(fmt.Sprintf("  \uf057 Failed to connect drive: %v\n", err))
		return result.String()
	}
	result.WriteString("  \uf00c Virtual USB drive connected to server!\n\n")

	// Step 3: Wait for drive recognition
	result.WriteString("  \uf252 Step 3: Waiting for drive recognition (2s)...\n")
	time.Sleep(2 * time.Second)
	result.WriteString("  \uf00c Ready!\n\n")

	// Step 4: Start F7 spam and power on
	result.WriteString("  \uf0e7 Step 4: Booting to BIOS...\n")
	go func() {
		for i := 0; i < 60; i++ {
			sendKey("F7")
			time.Sleep(500 * time.Millisecond)
		}
	}()

	// Power on in parallel
	act := action{"Power ON", "", "/switch/atx/power?port=%d&action=on", "POST"}
	executeAction(act, port)

	result.WriteString("  \uf00c F7 spam started! (60 presses over 30s)\n")
	result.WriteString(fmt.Sprintf("  \uf00c Powered on port %d\n\n", port+1))
	result.WriteString("\uf058 Boot sequence complete!\n")
	result.WriteString(fmt.Sprintf("\uf0a1 Select the USB drive from BIOS to boot: %s", isoName))

	return result.String()
}

// Boot from Ubuntu ISO - mounts the ISO and boots to BIOS (DEPRECATED - kept for compatibility)
func bootFromUbuntuISO(port int) string {
	var result strings.Builder
	result.WriteString("\uf121 Starting Ubuntu ISO boot sequence...\n\n")

	// Step 1: Select the Ubuntu ISO image
	result.WriteString("  \uf0a0 Step 1: Selecting Ubuntu ISO...\n")
	// Note: Change this to match the exact filename on your PiKVM
	isoName := "ubuntu-25.10-live-server-amd64.iso"
	if err := selectMSDImage(isoName); err != nil {
		result.WriteString(fmt.Sprintf("  \uf057 Failed to select ISO: %v\n", err))
		result.WriteString("\n  \uf06a Note: Upload the ISO via PiKVM web interface first:\n")
		result.WriteString(fmt.Sprintf("     https://%s/kvm/\n", pikvmHost))
		return result.String()
	}
	result.WriteString("  \uf00c ISO selected!\n\n")

	// Step 2: Connect the virtual drive
	result.WriteString("  \uf0c1 Step 2: Connecting virtual USB drive...\n")
	if err := connectMSD(true); err != nil {
		result.WriteString(fmt.Sprintf("  \uf057 Failed to connect drive: %v\n", err))
		return result.String()
	}
	result.WriteString("  \uf00c Drive connected!\n\n")

	// Step 3: Wait a moment for the system to recognize the drive
	result.WriteString("  \uf252 Step 3: Waiting for drive recognition (2s)...\n")
	time.Sleep(2 * time.Second)
	result.WriteString("  \uf00c Ready!\n\n")

	// Step 4: Start F7 spam and power on
	result.WriteString("  \uf0e7 Step 4: Booting to BIOS...\n")
	go func() {
		for i := 0; i < 60; i++ {
			sendKey("F7")
			time.Sleep(500 * time.Millisecond)
		}
	}()

	// Power on in parallel
	act := action{"Power ON", "", "/switch/atx/power?port=%d&action=on", "POST"}
	executeAction(act, port)

	result.WriteString("  \uf00c F7 spam started! (60 presses over 30s)\n")
	result.WriteString(fmt.Sprintf("  \uf00c Powered on port %d\n\n", port+1))
	result.WriteString("\uf058 Boot sequence complete!\n")
	result.WriteString("\uf0a1 Select the USB drive from the BIOS boot menu to boot Ubuntu installer")

	return result.String()
}

// Boot from Windows 11 ISO - mounts the ISO and boots to BIOS
func bootFromWindows11ISO(port int) string {
	var result strings.Builder
	result.WriteString("\uf17a Starting Windows 11 ISO boot sequence...\n\n")

	// Step 1: Select the Windows 11 ISO image
	result.WriteString("  \uf0a0 Step 1: Selecting Windows 11 ISO...\n")
	isoName := "Windows 11 24H2 Polish x64.iso"
	if err := selectMSDImage(isoName); err != nil {
		result.WriteString(fmt.Sprintf("  \uf057 Failed to select ISO: %v\n", err))
		result.WriteString("\n  \uf06a Check available ISOs with: ./list_isos.sh\n")
		return result.String()
	}
	result.WriteString("  \uf00c ISO selected!\n\n")

	// Step 2: Connect the virtual drive
	result.WriteString("  \uf0c1 Step 2: Connecting virtual USB drive...\n")
	if err := connectMSD(true); err != nil {
		result.WriteString(fmt.Sprintf("  \uf057 Failed to connect drive: %v\n", err))
		return result.String()
	}
	result.WriteString("  \uf00c Drive connected!\n\n")

	// Step 3: Wait a moment for the system to recognize the drive
	result.WriteString("  \uf252 Step 3: Waiting for drive recognition (2s)...\n")
	time.Sleep(2 * time.Second)
	result.WriteString("  \uf00c Ready!\n\n")

	// Step 4: Start F7 spam and power on
	result.WriteString("  \uf0e7 Step 4: Booting to BIOS...\n")
	go func() {
		for i := 0; i < 60; i++ {
			sendKey("F7")
			time.Sleep(500 * time.Millisecond)
		}
	}()

	// Power on in parallel
	act := action{"Power ON", "", "/switch/atx/power?port=%d&action=on", "POST"}
	executeAction(act, port)

	result.WriteString("  \uf00c F7 spam started! (60 presses over 30s)\n")
	result.WriteString(fmt.Sprintf("  \uf00c Powered on port %d\n\n", port+1))
	result.WriteString("\uf058 Boot sequence complete!\n")
	result.WriteString("\uf17a Select the USB drive from the BIOS boot menu to boot Windows installer")

	return result.String()
}

// selectMSDImage selects an ISO image as the active mass-storage backing.
func selectMSDImage(imageName string) error {
	resp, err := pikvmDo("POST", "/msd/set_params?image="+imageName, nil, 10*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}
	fmt.Printf("    → API Response: %s\n", string(body))
	return nil
}

// connectMSD attaches/detaches the virtual USB drive to/from the target.
func connectMSD(connect bool) error {
	connectValue := 0
	if connect {
		connectValue = 1
	}
	resp, err := pikvmDo("POST", fmt.Sprintf("/msd/set_connected?connected=%d", connectValue), nil, 10*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}
	fmt.Printf("    → API Response: %s\n", string(body))
	return nil
}

func executeAction(act action, port int) string {
	// Handle special actions that don't follow the standard API pattern
	if strings.HasPrefix(act.apiCmd, "SPECIAL:") {
		specialCmd := strings.TrimPrefix(act.apiCmd, "SPECIAL:")

		switch specialCmd {
		case "disconnect_drive":
			if err := connectMSD(false); err != nil {
				return fmt.Sprintf("\uf057 Failed to disconnect drive: %v", err)
			}
			return "\uf00c Virtual USB drive disconnected from server!"
		default:
			return fmt.Sprintf("\uf057 Unknown special command: %s", specialCmd)
		}
	}

	// Standard API call
	endpoint := fmt.Sprintf(act.apiCmd, port)
	resp, err := pikvmDo(act.method, endpoint, nil, 0)
	if err != nil {
		return fmt.Sprintf("\uf057 Error: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("\uf057 Error reading response: %v", err)
	}

	// Parse JSON response
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Sprintf("✓ Response: %s", string(body))
	}

	if ok, exists := result["ok"].(bool); exists && ok {
		return fmt.Sprintf("\uf00c Success: %s executed on port %d", act.name, port+1)
	}

	return fmt.Sprintf("\uf071 Response: %s", string(body))
}

func main() {
	// Load configuration from .env file
	if err := loadEnv(); err != nil {
		fmt.Printf("Error loading .env file: %v\n", err)
		fmt.Println("Make sure .env file exists in the same directory as the binary")
		os.Exit(1)
	}

	// CLI mode (idea #27): if any args were passed, run as a one-shot
	// command instead of launching the TUI. See cli.go for the full surface.
	if len(os.Args) > 1 {
		runCLI()
		return
	}

	// Launch TUI with a live PiKVM WebSocket subscription feeding events into
	// the Bubble Tea event loop. Cancelling wsCtx on shutdown cleanly tears
	// down the reader goroutine.
	wsCtx, wsCancel := context.WithCancel(context.Background())
	defer wsCancel()

	// WithAltScreen: save the terminal's existing content, run the TUI in a
	// separate screen buffer, restore on quit. Standard full-screen TUI
	// behavior — nothing the TUI prints leaks into shell scrollback.
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	startWebSocket(wsCtx, p)
	startInfoPoller(wsCtx, p)

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

