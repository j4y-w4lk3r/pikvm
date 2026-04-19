package main

import (
	"bufio"
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

// Load configuration from .env file
func loadEnv() error {
	execPath, err := os.Executable()
	if err != nil {
		return err
	}
	envPath := filepath.Join(filepath.Dir(execPath), ".env")

	// If running from source (go run), try current directory
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		envPath = ".env"
	}

	file, err := os.Open(envPath)
	if err != nil {
		return fmt.Errorf("cannot open .env file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse KEY=VALUE
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

	if pikvmHost == "" || pikvmUser == "" || pikvmPass == "" {
		return fmt.Errorf("missing required config in .env: PIKVM_HOST, PIKVM_USER, or PIKVM_PASS")
	}

	baseURL = "https://" + pikvmHost + "/api"
	return scanner.Err()
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
	{"Boot to BIOS (F7)", "Power on + spam F7 for BIOS entry", bootToBIOS},
	{"Boot to BIOS (Del)", "Power on + spam Del for BIOS entry", bootToBIOSDel},
	{"Boot to BIOS (F2)", "Power on + spam F2 for BIOS entry", bootToBIOSF2},
	{"Boot to BIOS (F3)", "Power on + spam F3 for BIOS entry", bootToBIOSF3},
	{"Type from text.txt", "Type contents of text.txt file", testKeyboard},
	{"View video stream", "Open PiKVM KVM in browser (video + keyboard/mouse)", viewVideoStream},
	{"View video (mpv)", "Watch HDMI stream in mpv (needs websocat + mpv)", viewVideoMpv},
}

type portInfo struct {
	id     int
	active bool
}

// isoEntry is one option in the Boot-from-ISO list (on PiKVM or local file).
type isoEntry struct {
	Display   string // shown in list (e.g. "debian.iso (local)")
	Name      string // filename used for PiKVM set_params
	LocalPath string // non-empty if local file (upload first)
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
}

func initialModel() model {
	fmt.Printf("🔌 Connecting to PiKVM at: %s\n", pikvmHost)
	fmt.Printf("🔍 Fetching switch state...\n")
	state := fetchSwitchState()
	return model{
		cursor:         0,
		port:           state.ActivePort,
		extenders:      state.Extenders,
		portsPerExt:    state.PortsPerExt,
		totalPorts:     state.TotalPorts,
		activePort:     state.ActivePort,
		availablePorts: state.Available,
		portsDetected:  true,
		inScripts:      false,
	}
}

// extenderOf returns the 1-based extender for a linear port.
func (m model) extenderOf(linear int) int { return linear/m.portsPerExt + 1 }

// portOf returns the 1-based port (within the extender) for a linear port.
func (m model) portOf(linear int) int { return linear%m.portsPerExt + 1 }

// linearPort builds a linear port index from 1-based extender + 1-based port.
func (m model) linearPort(ext, port int) int { return (ext-1)*m.portsPerExt + (port - 1) }

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

// switchState describes the PiKVM switch topology and current active port.
// Linear port indexing is used everywhere internally:
//
//	linearPort = (extender-1) * portsPerExt + (port-1)   // both 1-based
//	            = unit * portsPerExt + channel           // both 0-based
type switchState struct {
	Extenders   int        // number of extender units (e.g. 2)
	PortsPerExt int        // ports per extender (typically 4)
	TotalPorts  int        // Extenders * PortsPerExt
	ActivePort  int        // currently-active linear port (0-based)
	Available   []portInfo // every existing linear port
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

	url := baseURL + "/switch"
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	client := &http.Client{Transport: tr, Timeout: 3 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return state
	}
	req.SetBasicAuth(pikvmUser, pikvmPass)
	resp, err := client.Do(req)
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
	return state
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit

		case "esc":
			if m.selectingISO {
				m.selectingISO = false
				m.isoCursor = 0
				m.result = "ISO selection cancelled"
			} else if m.focusMode != "" {
				m.focusMode = ""
			} else {
				m.quitting = true
				return m, tea.Quit
			}

		case "e":
			if !m.selectingISO {
				m.focusMode = "extender"
			}

		case "p":
			if !m.selectingISO {
				m.focusMode = "port"
			}

		case "o":
			if !m.selectingISO {
				m.focusMode = "ops"
				m.cursor = 0
			}

		case "c":
			if !m.selectingISO {
				m.focusMode = "scripts"
			}

		case "up", "k":
			if m.selectingISO {
				if m.isoCursor > 0 {
					m.isoCursor--
				}
			} else if m.focusMode == "ops" && m.cursor > 0 {
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
			} else if m.focusMode == "" && m.cursor < len(actions)-1 {
				m.cursor++
			} else if m.focusMode == "" && m.cursor == len(actions)-1 {
				m.inScripts = true
				m.cursor = 0
			} else if m.focusMode == "" && m.inScripts && m.cursor < len(customScripts)-1 {
				m.cursor++
			}
			// scripts focus: no cursor, use 1-9 to execute

		case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
			if m.selectingISO {
				// no digit handling in ISO list
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
						script := customScripts[idx]
						if script.name == "Boot from ISO" {
							m.result = "\uf0c1 Fetching ISOs (PiKVM + local iso/)..."
							entries, err := fetchAvailableISOEntries()
							if err != nil {
								m.result = fmt.Sprintf("\uf057 Failed to fetch ISOs: %v", err)
							} else if len(entries) == 0 {
								m.result = "\uf06a No ISOs found. Put .iso files in ./iso/ or upload: ./pikvm.sh --iso --upload /path/to/file.iso"
							} else {
								m.selectingISO = true
								m.availableISOEntries = entries
								m.isoCursor = 0
								m.result = ""
							}
						} else {
							m.result = script.script(m.port)
						}
					}
				}
			}
			// when focusMode == "", number keys do nothing (user must press e/o/c first)

		case "r":
			if !m.selectingISO {
				state := fetchSwitchState()
				m.extenders = state.Extenders
				m.portsPerExt = state.PortsPerExt
				m.totalPorts = state.TotalPorts
				m.activePort = state.ActivePort
				m.availablePorts = state.Available
				m.result = fmt.Sprintf("\uf00c Refreshed: %d extender(s) × %d port(s)  │  PiKVM active: %d.%d",
					state.Extenders, state.PortsPerExt,
					state.ActivePort/state.PortsPerExt+1, state.ActivePort%state.PortsPerExt+1)
			}

		case "enter", " ":
			if m.selectingISO {
				if m.isoCursor < len(m.availableISOEntries) {
					entry := m.availableISOEntries[m.isoCursor]
					m.selectingISO = false
					m.result = bootFromISOEntry(m.port, entry)
				}
			} else if m.focusMode == "ops" {
				result := executeAction(actions[m.cursor], m.port)
				m.result = result
			} else if m.focusMode == "" && m.inScripts {
				selectedScript := customScripts[m.cursor]
				if selectedScript.name == "Boot from ISO" {
					m.result = "\uf0c1 Fetching ISOs (PiKVM + local iso/)..."
					entries, err := fetchAvailableISOEntries()
					if err != nil {
						m.result = fmt.Sprintf("\uf057 Failed to fetch ISOs: %v", err)
					} else if len(entries) == 0 {
						m.result = "\uf06a No ISOs found. Put .iso files in ./iso/ or upload: ./pikvm.sh --iso --upload /path/to/file.iso"
					} else {
						m.selectingISO = true
						m.availableISOEntries = entries
						m.isoCursor = 0
						m.result = ""
					}
				} else {
					m.result = selectedScript.script(m.port)
				}
			} else if m.focusMode == "" && !m.inScripts {
				result := executeAction(actions[m.cursor], m.port)
				m.result = result
			}
		}
	}

	return m, nil
}

func (m model) View() string {
	if m.quitting {
		return successStyle.Render("\n\uf00c Goodbye!\n\n")
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

	// [P] Port row: [1] [2] [3] [4]
	var portRow strings.Builder
	if m.focusMode == "port" {
		portRow.WriteString(selectedStyle.Render("[P] Port:    "))
	} else {
		portRow.WriteString(unselectedStyle.Render("[P] Port:    "))
	}
	for i := 1; i <= m.portsPerExt; i++ {
		portRow.WriteString(" ")
		label := fmt.Sprintf("[%d]", i)
		switch {
		case i == curSubPort && m.focusMode == "port":
			portRow.WriteString(selectedStyle.Render(label))
		case i == curSubPort:
			portRow.WriteString(successStyle.Render(label))
		default:
			portRow.WriteString(unselectedStyle.Render(label))
		}
	}
	left.WriteString("  " + portRow.String() + "\n")

	// Active-port summary
	activeExt := m.activePort/m.portsPerExt + 1
	activePortNum := m.activePort%m.portsPerExt + 1
	syncIcon := successStyle.Render("\uf058")
	if m.activePort != m.port {
		syncIcon = warningStyle.Render("\uf06a") + " (PiKVM is on " + fmt.Sprintf("%d.%d", activeExt, activePortNum) + ")"
	}
	summary := fmt.Sprintf("\uf0e4 Selected: %d.%d  ", curExt, curSubPort)
	left.WriteString("  " + portInfoStyle.Render(summary) + syncIcon + "\n")
	left.WriteString("\n")

	// [O] Operations
	if m.focusMode == "ops" {
		left.WriteString("  " + selectedStyle.Render("[O] Operations (1-7 select, Enter run):") + "\n")
	} else {
		left.WriteString("  " + unselectedStyle.Render("[O] Operations:") + "\n")
	}
	for i, act := range actions {
		if m.focusMode == "ops" && m.cursor == i {
			left.WriteString("  " + selectedStyle.Render(fmt.Sprintf("  [%d] %s", i+1, act.name)) + "\n")
		} else {
			left.WriteString("  " + unselectedStyle.Render(fmt.Sprintf("  [%d] %s", i+1, act.name)) + "\n")
		}
	}

	leftCol := lipgloss.NewStyle().Width(leftWidth).Render(left.String())

	// Right column: [C] Custom Scripts, 1-9 to run
	var right strings.Builder
	right.WriteString("\n\n\n")
	if m.focusMode == "scripts" {
		right.WriteString("  " + selectedStyle.Render("[C] Custom Scripts (1-9 run)") + " \uf058\n\n")
	} else {
		right.WriteString("  " + unselectedStyle.Render("[C] Custom Scripts:") + "\n\n")
	}
	for i, script := range customScripts {
		line := fmt.Sprintf("  [%d] %s", i+1, script.name)
		if m.focusMode == "scripts" {
			right.WriteString("  " + unselectedStyle.Render(line) + "\n")
		} else if m.focusMode == "" && m.inScripts && m.cursor == i {
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
	bottom.WriteString("\n  ")
	bottom.WriteString(helpStyle.Render("e: Extender  p: Port  o: Operations  c: Scripts  1-9/Enter  ESC: back  r: Refresh  q: Quit"))
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
	var url string

	// Check if it's a special key or regular text
	// Function keys, Delete, etc. use send_key endpoint
	if isSpecialKey(key) {
		url = fmt.Sprintf("%s/hid/events/send_key?key=%s", baseURL, key)
	} else {
		// Regular text uses print API
		url = fmt.Sprintf("%s/hid/print?text=%s", baseURL, key)
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return err
	}

	req.SetBasicAuth(pikvmUser, pikvmPass)

	resp, err := client.Do(req)
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

// Custom script implementations
func bootToBIOS(port int) string {
	// Start spamming F7 IMMEDIATELY in background (doesn't wait!)
	go func() {
		for i := 0; i < 60; i++ {
			sendKey("F7")
			time.Sleep(500 * time.Millisecond)
		}
	}()

	// Power on immediately (happens in parallel with F7 spam)
	act := action{"Power ON", "", "/switch/atx/power?port=%d&action=on", "POST"}
	executeAction(act, port)

	// Return immediately - don't wait!
	return fmt.Sprintf("\uf00c F7 spam started! (60 presses over 30s) + Powered on port %d", port+1)
}

func bootToBIOSDel(port int) string {
	// Start spamming Delete IMMEDIATELY
	go func() {
		for i := 0; i < 60; i++ {
			sendKey("Delete")
			time.Sleep(500 * time.Millisecond)
		}
	}()

	// Power on in parallel
	act := action{"Power ON", "", "/switch/atx/power?port=%d&action=on", "POST"}
	executeAction(act, port)

	// Return immediately
	return fmt.Sprintf("\uf00c Delete spam started! (60 presses over 30s) + Powered on port %d", port+1)
}

func bootToBIOSF2(port int) string {
	// Start spamming F2 IMMEDIATELY
	go func() {
		for i := 0; i < 60; i++ {
			sendKey("F2")
			time.Sleep(500 * time.Millisecond)
		}
	}()

	// Power on in parallel
	act := action{"Power ON", "", "/switch/atx/power?port=%d&action=on", "POST"}
	executeAction(act, port)

	// Return immediately
	return fmt.Sprintf("\uf00c F2 spam started! (60 presses over 30s) + Powered on port %d", port+1)
}

func bootToBIOSF3(port int) string {
	// Start spamming F3 IMMEDIATELY
	go func() {
		for i := 0; i < 60; i++ {
			sendKey("F3")
			time.Sleep(500 * time.Millisecond)
		}
	}()

	// Power on in parallel
	act := action{"Power ON", "", "/switch/atx/power?port=%d&action=on", "POST"}
	executeAction(act, port)

	// Return immediately
	return fmt.Sprintf("\uf00c F3 spam started! (60 presses over 30s) + Powered on port %d", port+1)
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
	url := fmt.Sprintf("%s/switch/set_active?port=%d", baseURL, port)
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(pikvmUser, pikvmPass)
	resp, err := client.Do(req)
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

// Helper function to send text using PiKVM's print API
func sendText(text string) error {
	// URL-encode the text for the query parameter
	encodedText := strings.ReplaceAll(text, " ", "+")
	encodedText = strings.ReplaceAll(encodedText, "\n", "%0A")

	url := fmt.Sprintf("%s/hid/print", baseURL)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr, Timeout: 30 * time.Second}

	// Send text as POST body (not query parameter for large text)
	req, err := http.NewRequest("POST", url, strings.NewReader(text))
	if err != nil {
		return err
	}

	req.SetBasicAuth(pikvmUser, pikvmPass)
	req.Header.Set("Content-Type", "text/plain")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// Fetch available ISOs from PiKVM
func fetchAvailableISOs() ([]string, error) {
	url := baseURL + "/msd"

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(pikvmUser, pikvmPass)

	resp, err := client.Do(req)
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

// fetchAvailableISOEntries returns ISOs on PiKVM plus local .iso files from the iso/ folder.
func fetchAvailableISOEntries() ([]isoEntry, error) {
	var entries []isoEntry
	// 1) ISOs already on PiKVM
	onPiKVM, err := fetchAvailableISOs()
	if err != nil {
		return nil, err
	}
	for _, name := range onPiKVM {
		entries = append(entries, isoEntry{Display: name, Name: name, LocalPath: ""})
	}
	// 2) Local iso/ folder (e.g. ./iso or next to binary)
	isoDir := getISODir()
	dirEntries, err := os.ReadDir(isoDir)
	if err != nil {
		return entries, nil // no local folder is ok
	}
	for _, e := range dirEntries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".iso") {
			fullPath := filepath.Join(isoDir, name)
			if abs, err := filepath.Abs(fullPath); err == nil {
				fullPath = abs
			}
			entries = append(entries, isoEntry{
				Display:   name + " (local)",
				Name:      name,
				LocalPath: fullPath,
			})
		}
	}
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

// Helper function to select an MSD image
func selectMSDImage(imageName string) error {
	// Use query parameter as per official API docs
	url := fmt.Sprintf("https://%s/api/msd/set_params?image=%s", pikvmHost, imageName)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second}

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return err
	}

	req.SetBasicAuth(pikvmUser, pikvmPass)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Log successful response for debugging
	fmt.Printf("    → API Response: %s\n", string(body))

	return nil
}

// Helper function to connect/disconnect MSD
func connectMSD(connect bool) error {
	// Use correct endpoint: /api/msd/set_connected (not set_params!)
	// As per official API docs: https://docs.pikvm.org/api/
	connectValue := 0
	if connect {
		connectValue = 1
	}
	url := fmt.Sprintf("https://%s/api/msd/set_connected?connected=%d", pikvmHost, connectValue)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second}

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return err
	}

	req.SetBasicAuth(pikvmUser, pikvmPass)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Log successful response for debugging
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
	url := baseURL + endpoint

	// Create HTTP client with SSL verification disabled
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	req, err := http.NewRequest(act.method, url, nil)
	if err != nil {
		return fmt.Sprintf("\uf057 Error: %v", err)
	}

	req.SetBasicAuth(pikvmUser, pikvmPass)

	resp, err := client.Do(req)
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

	// Check for CLI arguments
	if len(os.Args) > 1 {
		handleCLI()
		return
	}

	// Launch TUI
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func handleCLI() {
	if len(os.Args) < 2 {
		showHelp()
		return
	}

	port := 0
	if len(os.Args) > 2 {
		fmt.Sscanf(os.Args[2], "%d", &port)
	}

	command := os.Args[1]

	var selectedAction *action
	switch command {
	case "on":
		selectedAction = &actions[0]
	case "off":
		selectedAction = &actions[1]
	case "click":
		selectedAction = &actions[2]
	case "long":
		selectedAction = &actions[3]
	case "reset":
		selectedAction = &actions[4]
	case "reset-long":
		selectedAction = &actions[5]
	default:
		showHelp()
		return
	}

	fmt.Printf("⚡ Executing: %s on port %d\n", selectedAction.name, port)
	result := executeAction(*selectedAction, port)
	fmt.Println(result)
}

func showHelp() {
	help := `
PiKVM Control - Go + Bubble Tea Edition

Usage:
  pikvm              Launch interactive TUI
  pikvm [cmd] [port] Execute command on specified port (default: 0)

Commands:
  on                 Turn power ON
  off                Turn power OFF
  click              Power button short press
  long               Power button long press
  reset              Reset button short press
  reset-long         Reset button long press

Examples:
  pikvm              # Launch TUI
  pikvm on           # Power on port 0
  pikvm off 1        # Power off port 1
  pikvm long 0       # Force shutdown port 0

`
	fmt.Println(strings.TrimSpace(help))
}
