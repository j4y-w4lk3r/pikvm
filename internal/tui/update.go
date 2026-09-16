package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pikvm/internal/api"
	"pikvm/internal/config"
	"pikvm/internal/scripts"
	"pikvm/internal/state"
)

// ----------------------------------------------------------------------------
// Async ISO upload (idea #17) — replaces the old osascript / gnome-terminal
// popup. We kick off api.UploadISO in a goroutine via tea.Cmd; when it
// returns, an uploadDoneMsg arrives in Update() and we chain straight into
// BootFromSpecificISO. Live progress is already shown by the status bar via
// WS msd events, so there's no local progress plumbing.
// ----------------------------------------------------------------------------

type uploadDoneMsg struct {
	name string
	port int
	err  error
}

type actionDoneMsg struct {
	result string
}

type setPortDoneMsg struct {
	target int
	label  string
	err    error
}

type isoListDoneMsg struct {
	entries []api.IsoEntry
	err     error
}

type scriptDoneMsg struct {
	result string
}

// uploadISOCmd returns a tea.Cmd that performs the HTTP upload off the main
// goroutine. Bubble Tea will Send the returned tea.Msg to Update() when it
// finishes, at which point we kick off the boot sequence.
func uploadISOCmd(entry api.IsoEntry, port int) tea.Cmd {
	return func() tea.Msg {
		err := api.UploadISO(entry.LocalPath, entry.Name, nil)
		return uploadDoneMsg{name: entry.Name, port: port, err: err}
	}
}

func actionCmd(action api.Action, port int) tea.Cmd {
	return func() tea.Msg {
		return actionDoneMsg{result: api.ExecuteAction(action, port)}
	}
}

func setPortCmd(target int, label string) tea.Cmd {
	return func() tea.Msg {
		return setPortDoneMsg{target: target, label: label, err: api.SetSwitchPort(target)}
	}
}

func fetchISOsCmd() tea.Cmd {
	return func() tea.Msg {
		entries, err := api.FetchAvailableISOEntries()
		return isoListDoneMsg{entries: entries, err: err}
	}
}

func scriptCmd(run func() string) tea.Cmd {
	return func() tea.Msg {
		return scriptDoneMsg{result: run()}
	}
}

// Update is Bubble Tea's reducer. We dispatch on message type (WS / info /
// key events) and return a new model + an optional Cmd.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	// ---- Live PiKVM events from /api/ws (idea #1) --------------------------
	// These message handlers also fire hook events (idea #20) on every
	// state transition. Hook dispatch is async via goroutines so it
	// never blocks the TUI event loop, even if a hook hangs.
	case api.ConnectedMsg:
		wasConnected := m.wsConnected
		m.wsConnected = true
		m.wsLastError = ""
		m.wsLastEvent = time.Now()
		if !wasConnected {
			fireHook("host-connected", nil)
		}
		return m, nil

	case api.DisconnectedMsg:
		wasConnected := m.wsConnected
		m.wsConnected = false
		if msg.Err != nil {
			m.wsLastError = msg.Err.Error()
		}
		if wasConnected {
			env := map[string]string{}
			if msg.Err != nil {
				env["error"] = msg.Err.Error()
			}
			fireHook("host-disconnected", env)
		}
		return m, nil

	case api.SwitchMsg:
		m.wsLastEvent = time.Now()
		m.applyLiveSwitch(msg.State)
		return m, nil

	case api.AtxMsg:
		m.wsLastEvent = time.Now()
		m.atxBusy = msg.Busy
		m.atxPower = msg.PowerLed
		m.atxHdd = msg.HddLed
		if m.directATX {
			m.powerLeds = []bool{msg.PowerLed}
			m.hddLeds = []bool{msg.HddLed}
		}
		return m, nil

	case api.MsdMsg:
		m.wsLastEvent = time.Now()
		prev := api.MsdMsg{
			Online: m.msdOnline, Busy: m.msdBusy, Connected: m.msdConnect,
			FreeBytes: m.msdFree, TotalBytes: m.msdTotal,
			Uploading: m.msdUpload, UploadName: m.msdUpName, UploadPct: m.msdUpPct,
		}
		m.msdOnline = msg.Online
		m.msdBusy = msg.Busy
		m.msdConnect = msg.Connected
		m.msdFree = msg.FreeBytes
		m.msdTotal = msg.TotalBytes
		m.msdUpload = msg.Uploading
		m.msdUpName = msg.UploadName
		m.msdUpPct = msg.UploadPct
		if m.firstMsdSeen {
			fireMsdTransitions(prev, msg)
		}
		m.firstMsdSeen = true
		return m, nil

	case api.ClientsMsg:
		m.wsLastEvent = time.Now()
		prev := m.wsClients
		m.wsClients = msg.Count
		if m.firstClientsSeen {
			fireClientsChanged(prev, msg.Count)
		}
		m.firstClientsSeen = true
		return m, nil

	case api.InfoMsg:
		m.info = msg.State
		return m, nil

	case uploadDoneMsg:
		if msg.err != nil {
			m.operationBusy = false
			m.result = fmt.Sprintf("\uf057 Upload failed: %v", msg.err)
			return m, nil
		}
		// Upload complete → chain into the boot sequence off the UI thread.
		m.operationBusy = true
		m.result = fmt.Sprintf("\uf021 Booting %s…", msg.name)
		return m, scriptCmd(func() string {
			return scripts.BootFromSpecificISO(msg.port, msg.name)
		})

	case actionDoneMsg:
		m.operationBusy = false
		m.result = msg.result
		return m, nil

	case setPortDoneMsg:
		m.portSwitching = false
		if msg.err != nil {
			m.result = fmt.Sprintf("\uf071 Selected %s. set_active: %v", msg.label, msg.err)
			return m, nil
		}
		m.activePort = msg.target
		m.result = "\uf00c switched to " + msg.label
		return m, nil

	case isoListDoneMsg:
		m.operationBusy = false
		if msg.err != nil {
			m.result = fmt.Sprintf("\uf057 Failed to fetch ISOs: %v", msg.err)
			return m, nil
		}
		if len(msg.entries) == 0 {
			m.result = "\uf06a No ISOs found. Put .iso files in ./iso/ or upload: ./pikvm.sh --iso --upload /path/to/file.iso"
			return m, nil
		}
		m.selectingISO = true
		m.availableISOEntries = msg.entries
		m.isoCursor = 0
		m.result = ""
		return m, nil

	case scriptDoneMsg:
		m.operationBusy = false
		m.result = msg.result
		return m, nil

	case recordDoneMsg:
		m.operationBusy = false
		m.result = msg.result
		return m, nil

	case hostSwitchDoneMsg:
		if msg.err != nil {
			m.hostSwitching = false
			m.focusMode = ""
			if msg.prevName != "" {
				_ = config.UseHost(msg.prevName)
				api.RequestWSReconnect()
			}
			m.result = fmt.Sprintf("\uf057 Host switch failed: %v", msg.err)
			return m, nil
		}
		if msg.name == msg.prevName {
			m.hostSwitching = false
			m.focusMode = ""
			m.result = fmt.Sprintf("\uf00c Already on %s", hostDisplayLabel())
			return m, nil
		}
		m.applyHostSwitch(msg)
		m.result = fmt.Sprintf("\uf00c Switched to %s (%s)", hostDisplayLabel(), config.Host)
		return m, nil

	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// applyLiveSwitch copies switch topology from a WS event or the startup
// HTTP fetch onto the model.
func (m *Model) applyLiveSwitch(sw api.SwitchState) {
	if sw.TotalPorts <= 0 {
		return
	}
	followActive := !m.portsDetected || m.port == m.activePort || m.port >= sw.TotalPorts
	prevActive := m.activePort
	prevPower := m.powerLeds
	m.extenders = sw.Extenders
	m.portsPerExt = sw.PortsPerExt
	if m.portsPerExt < 1 {
		m.portsPerExt = 1
	}
	m.totalPorts = sw.TotalPorts
	m.activePort = sw.ActivePort
	m.availablePorts = sw.Available
	m.videoLinks = sw.VideoLinks
	m.usbLinks = sw.UsbLinks
	m.powerLeds = sw.PowerLeds
	m.hddLeds = sw.HddLeds
	m.directATX = sw.DirectATX
	m.portsDetected = true
	if sw.DirectATX {
		m.atxPower = len(sw.PowerLeds) > 0 && sw.PowerLeds[0]
		m.atxHdd = len(sw.HddLeds) > 0 && sw.HddLeds[0]
	}
	if followActive {
		// PiKVM can return -1 when no port is currently routed.
		// activePort keeps the raw value (so the status bar can
		// say "no active port"), but the cursor must stay in
		// [0, totalPorts) so slice indexing never panics.
		m.port = sw.ActivePort
		if m.port < 0 {
			m.port = 0
		}
	}
	// Fire transition events only after the first event has
	// seeded the model so we don't dispatch a port-changed at
	// startup ("0 → real value" isn't a real transition).
	if m.firstSwitchSeen {
		firePortChanged(prevActive, m.activePort, m.portsPerExt)
		firePowerTransitions(prevPower, m.powerLeds, m.portsPerExt)
	}
	m.firstSwitchSeen = true
}

// handleKey is the large key-dispatch switch extracted out of Update() for
// readability. Returns the new model + (usually nil) command.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.showHelp {
		switch key {
		case "esc", "?":
			m.showHelp = false
		}
		return m, nil
	}

	switch key {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit

	case "?":
		m.showHelp = true
		m.focusMode = ""
		return m, nil

	case "esc":
		switch {
		case m.selectingISO:
			m.selectingISO = false
			m.isoCursor = 0
			m.result = "ISO selection cancelled"
		case m.selectingBIOSKey:
			m.selectingBIOSKey = false
			m.result = "BIOS key selection cancelled"
		case m.gridView:
			m.gridView = false
		case m.focusMode != "":
			m.focusMode = ""
		default:
			m.quitting = true
			return m, tea.Quit
		}

	case "e":
		if !m.directATX && !m.selectingISO && !m.selectingBIOSKey && !m.gridView {
			m.focusMode = "extender"
		}
	case "p":
		if !m.directATX && !m.selectingISO && !m.selectingBIOSKey && !m.gridView {
			m.focusMode = "port"
		}
	case "o":
		if !m.selectingISO && !m.selectingBIOSKey && !m.gridView {
			m.focusMode = "ops"
			m.cursor = 0
		}
	case "c":
		if !m.selectingISO && !m.selectingBIOSKey && !m.gridView {
			m.focusMode = "scripts"
			m.cursor = 0
		}
	case "h", "H":
		if m.gridView {
			subPort := m.gridCursor % m.portsPerExt
			if subPort > 0 {
				m.gridCursor--
			}
		} else if multiHostEnabled() && !m.selectingISO && !m.selectingBIOSKey && !m.hostSwitching {
			m.focusMode = "host"
		}

	case "left":
		if m.gridView {
			subPort := m.gridCursor % m.portsPerExt
			if subPort > 0 {
				m.gridCursor--
			}
		}

	case "g":
		if !m.directATX && !m.selectingISO && !m.selectingBIOSKey {
			m.gridView = !m.gridView
			if m.gridView {
				m.gridCursor = m.port
				m.focusMode = ""
			}
		}

	case "up", "k":
		if m.gridView {
			if m.gridCursor >= m.portsPerExt {
				m.gridCursor -= m.portsPerExt
			}
			break
		}
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
			m.cursor = len(m.actions()) - 1
		}

	case "right", "l":
		if m.gridView {
			subPort := m.gridCursor % m.portsPerExt
			if subPort < m.portsPerExt-1 && m.gridCursor+1 < m.totalPorts {
				m.gridCursor++
			}
		}

	case "down", "j":
		if m.gridView {
			if m.gridCursor+m.portsPerExt < m.totalPorts {
				m.gridCursor += m.portsPerExt
			}
			break
		}
		if m.selectingISO {
			if m.isoCursor < len(m.availableISOEntries)-1 {
				m.isoCursor++
			}
		} else if m.focusMode == "ops" && m.cursor < len(m.actions())-1 {
			m.cursor++
		} else if m.focusMode == "scripts" && m.cursor < len(scripts.Default)-1 {
			m.cursor++
		} else if m.focusMode == "" && m.cursor < len(m.actions())-1 {
			m.cursor++
		} else if m.focusMode == "" && m.cursor == len(m.actions())-1 {
			m.inScripts = true
			m.cursor = 0
		} else if m.focusMode == "" && m.inScripts && m.cursor < len(scripts.Default)-1 {
			m.cursor++
		}

	case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
		return m.handleDigit(msg)

	case "r":
		if !m.selectingISO {
			api.RequestWSReconnect()
			m.wsConnected = false
			m.wsLastError = ""
			m.result = "\uf021 Reconnecting WebSocket..."
		}

	case "enter", " ":
		if m.gridView {
			if m.portSwitching {
				return m, nil
			}
			target := m.gridCursor
			if target >= 0 && target < m.totalPorts {
				m.port = target
				curExt := m.extenderOf(target)
				curPort := m.portOf(target)
				label := fmt.Sprintf("%d.%d", curExt, curPort)
				if p, ok := m.state.Ports[state.PortExtID(target, m.portsPerExt)]; ok && p.Name != "" {
					label += " (" + p.Name + ")"
				}
				m.portSwitching = true
				m.result = "\uf021 Switching to " + label + "…"
				m.gridView = false
				return m, setPortCmd(target, label)
			}
			m.gridView = false
			return m, nil
		}
		if m.selectingISO {
			if m.operationBusy {
				return m, nil
			}
			if m.isoCursor < len(m.availableISOEntries) {
				entry := m.availableISOEntries[m.isoCursor]
				m.selectingISO = false
				m.operationBusy = true
				if entry.LocalPath != "" {
					// Local ISO — kick off an async in-process upload
					// (idea #17). Status bar shows live progress via WS
					// msd events. BootFromSpecificISO runs when the
					// uploadDoneMsg lands back in Update().
					m.result = fmt.Sprintf("\uf0c1 Uploading %s — live progress in status bar below…", entry.Name)
					return m, uploadISOCmd(entry, m.port)
				}
				// Already on PiKVM — mount + boot off the UI thread.
				port := m.port
				m.result = fmt.Sprintf("\uf021 Booting %s…", entry.Name)
				return m, scriptCmd(func() string {
					return scripts.BootFromSpecificISO(port, entry.Name)
				})
			}
		} else if m.focusMode == "ops" {
			if m.operationBusy {
				return m, nil
			}
			action := m.actions()[m.cursor]
			m.operationBusy = true
			m.result = fmt.Sprintf("\uf021 Running %s…", action.Name)
			return m, actionCmd(action, m.port)
		} else if m.focusMode == "scripts" {
			if m.operationBusy {
				return m, nil
			}
			if scriptIsRecord(scripts.Default[m.cursor].Name) {
				return m.startRecordClip(m.port)
			}
			return m, m.launchScript(m.cursor)
		} else if m.focusMode == "" && m.inScripts {
			if m.operationBusy {
				return m, nil
			}
			if scriptIsRecord(scripts.Default[m.cursor].Name) {
				return m.startRecordClip(m.port)
			}
			return m, m.launchScript(m.cursor)
		} else if m.focusMode == "" && !m.inScripts {
			if m.operationBusy {
				return m, nil
			}
			action := m.actions()[m.cursor]
			m.operationBusy = true
			m.result = fmt.Sprintf("\uf021 Running %s…", action.Name)
			return m, actionCmd(action, m.port)
		}
	}
	return m, nil
}

// handleDigit owns the digit-key branch. Pulled out to keep handleKey small.
func (m Model) handleDigit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.selectingISO {
		return m, nil
	}
	if m.selectingBIOSKey {
		digit := int(msg.String()[0] - '0')
		if digit < 1 || digit > len(scripts.BIOSKeyOptions) {
			m.result = fmt.Sprintf("\uf071 Pick 1-%d (or ESC)", len(scripts.BIOSKeyOptions))
			return m, nil
		}
		opt := scripts.BIOSKeyOptions[digit-1]
		m.selectingBIOSKey = false

		// Remember the user's choice per port (idea #5).
		id := m.portExtIDOf(m.port)
		prof := m.state.Ports[id]
		if prof.BIOSKey != opt.Key {
			prof.BIOSKey = opt.Key
			m.state = state.SetProfile(m.state, id, prof)
			if err := state.Save(m.state); err != nil {
				m.result = fmt.Sprintf("\uf071 saved BIOS key %s, but could not persist: %v", opt.Label, err)
				return m, nil
			}
		}
		port := m.port
		m.operationBusy = true
		m.result = fmt.Sprintf("\uf021 Starting %s BIOS boot…", opt.Label)
		return m, scriptCmd(func() string {
			return scripts.BootToBIOSWithKey(port, opt)
		})
	}
	if m.focusMode == "host" {
		digit := int(msg.String()[0] - '0')
		names := config.HostNames()
		if digit < 1 || digit > len(names) {
			m.result = fmt.Sprintf("\uf071 Pick host 1-%d (or ESC)", len(names))
			return m, nil
		}
		name := names[digit-1]
		m.hostSwitching = true
		m.focusMode = ""
		m.result = fmt.Sprintf("\uf021 Switching to %s…", name)
		return m, switchHostCmd(name)
	}
	if m.focusMode == "extender" {
		if m.portSwitching {
			return m, nil
		}
		digit := int(msg.String()[0] - '0')
		if digit < 1 || digit > m.extenders {
			m.result = fmt.Sprintf("\uf071 Only %d extender(s) available (press 1-%d)", m.extenders, m.extenders)
			return m, nil
		}
		curSubPort := m.portOf(m.port)
		newPort := m.linearPort(digit, curSubPort)
		if !api.IsPortAvailable(newPort, m.availablePorts) {
			m.result = fmt.Sprintf("\uf071 Port %d.%d not available", digit, curSubPort)
			return m, nil
		}
		m.port = newPort
		label := fmt.Sprintf("%d.%d", digit, curSubPort)
		m.portSwitching = true
		m.result = "\uf021 Switching to " + label + "…"
		return m, setPortCmd(newPort, label)
	}
	if m.focusMode == "port" {
		if m.portSwitching {
			return m, nil
		}
		digit := int(msg.String()[0] - '0')
		if digit < 1 || digit > m.portsPerExt {
			m.result = fmt.Sprintf("\uf071 Only ports 1-%d on extender %d (press 1-%d)", m.portsPerExt, m.extenderOf(m.port), m.portsPerExt)
			return m, nil
		}
		curExt := m.extenderOf(m.port)
		newPort := m.linearPort(curExt, digit)
		if !api.IsPortAvailable(newPort, m.availablePorts) {
			m.result = fmt.Sprintf("\uf071 Port %d.%d not available", curExt, digit)
			return m, nil
		}
		m.port = newPort
		label := fmt.Sprintf("%d.%d", curExt, digit)
		m.portSwitching = true
		m.result = "\uf021 Switching to " + label + "…"
		return m, setPortCmd(newPort, label)
	}
	if m.focusMode == "ops" {
		digit := int(msg.String()[0] - '0')
		if digit >= 1 && digit <= 7 {
			m.cursor = digit - 1
		}
		return m, nil
	}
	if m.focusMode == "scripts" {
		if m.operationBusy {
			return m, nil
		}
		digit := int(msg.String()[0] - '0')
		if digit >= 1 && digit <= 9 {
			idx := digit - 1
			if idx < len(scripts.Default) {
				m.cursor = idx
				if scriptIsRecord(scripts.Default[idx].Name) {
					return m.startRecordClip(m.port)
				}
				return m, m.launchScript(idx)
			}
		}
		return m, nil
	}
	return m, nil
}
