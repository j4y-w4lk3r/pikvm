package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pikvm/internal/api"
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

// uploadISOCmd returns a tea.Cmd that performs the HTTP upload off the main
// goroutine. Bubble Tea will Send the returned tea.Msg to Update() when it
// finishes, at which point we kick off the boot sequence.
func uploadISOCmd(entry api.IsoEntry, port int) tea.Cmd {
	return func() tea.Msg {
		err := api.UploadISO(entry.LocalPath, entry.Name, nil)
		return uploadDoneMsg{name: entry.Name, port: port, err: err}
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
		if msg.State.TotalPorts > 0 {
			followActive := m.port == m.activePort || m.port >= msg.State.TotalPorts
			prevActive := m.activePort
			prevPower := m.powerLeds
			m.extenders = msg.State.Extenders
			m.portsPerExt = msg.State.PortsPerExt
			m.totalPorts = msg.State.TotalPorts
			m.activePort = msg.State.ActivePort
			m.availablePorts = msg.State.Available
			m.videoLinks = msg.State.VideoLinks
			m.usbLinks = msg.State.UsbLinks
			m.powerLeds = msg.State.PowerLeds
			m.hddLeds = msg.State.HddLeds
			if followActive {
				// PiKVM can return -1 when no port is currently routed.
				// activePort keeps the raw value (so the status bar can
				// say "no active port"), but the cursor must stay in
				// [0, totalPorts) so slice indexing never panics.
				m.port = msg.State.ActivePort
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
		return m, nil

	case api.AtxMsg:
		m.wsLastEvent = time.Now()
		m.atxBusy = msg.Busy
		m.atxPower = msg.PowerLed
		m.atxHdd = msg.HddLed
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
			m.result = fmt.Sprintf("\uf057 Upload failed: %v", msg.err)
			return m, nil
		}
		// Upload complete → chain into the boot sequence (mount + F7 spam).
		m.result = scripts.BootFromSpecificISO(msg.port, msg.name)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// handleKey is the large key-dispatch switch extracted out of Update() for
// readability. Returns the new model + (usually nil) command.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		case m.gridView:
			m.gridView = false
		case m.focusMode != "":
			m.focusMode = ""
		default:
			m.quitting = true
			return m, tea.Quit
		}

	case "e":
		if !m.selectingISO && !m.selectingBIOSKey && !m.gridView {
			m.focusMode = "extender"
		}
	case "p":
		if !m.selectingISO && !m.selectingBIOSKey && !m.gridView {
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

	case "g":
		if !m.selectingISO && !m.selectingBIOSKey {
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
			m.cursor = len(api.DefaultActions) - 1
		}

	case "left", "h":
		if m.gridView {
			subPort := m.gridCursor % m.portsPerExt
			if subPort > 0 {
				m.gridCursor--
			}
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
		} else if m.focusMode == "ops" && m.cursor < len(api.DefaultActions)-1 {
			m.cursor++
		} else if m.focusMode == "scripts" && m.cursor < len(scripts.Default)-1 {
			m.cursor++
		} else if m.focusMode == "" && m.cursor < len(api.DefaultActions)-1 {
			m.cursor++
		} else if m.focusMode == "" && m.cursor == len(api.DefaultActions)-1 {
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
			target := m.gridCursor
			if target >= 0 && target < m.totalPorts {
				m.port = target
				if err := m.trySetActive(target); err != nil {
					m.result = fmt.Sprintf("\uf071 set_active: %v", err)
				} else {
					curExt := m.extenderOf(target)
					curPort := m.portOf(target)
					label := fmt.Sprintf("%d.%d", curExt, curPort)
					if p, ok := m.state.Ports[state.PortExtID(target, m.portsPerExt)]; ok && p.Name != "" {
						label += " (" + p.Name + ")"
					}
					m.result = "\uf00c switched to " + label
				}
			}
			m.gridView = false
			return m, nil
		}
		if m.selectingISO {
			if m.isoCursor < len(m.availableISOEntries) {
				entry := m.availableISOEntries[m.isoCursor]
				m.selectingISO = false
				if entry.LocalPath != "" {
					// Local ISO — kick off an async in-process upload
					// (idea #17). Status bar shows live progress via WS
					// msd events. BootFromSpecificISO runs when the
					// uploadDoneMsg lands back in Update().
					m.result = fmt.Sprintf("\uf0c1 Uploading %s — live progress in status bar below…", entry.Name)
					return m, uploadISOCmd(entry, m.port)
				}
				// Already on PiKVM — mount + boot synchronously.
				m.result = scripts.BootFromSpecificISO(m.port, entry.Name)
			}
		} else if m.focusMode == "ops" {
			m.result = api.ExecuteAction(api.DefaultActions[m.cursor], m.port)
		} else if m.focusMode == "scripts" {
			m.launchScript(m.cursor)
		} else if m.focusMode == "" && m.inScripts {
			m.launchScript(m.cursor)
		} else if m.focusMode == "" && !m.inScripts {
			m.result = api.ExecuteAction(api.DefaultActions[m.cursor], m.port)
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
		m.result = scripts.BootToBIOSWithKey(m.port, opt)
		return m, nil
	}
	if m.focusMode == "extender" {
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
		if err := m.trySetActive(newPort); err != nil {
			m.result = fmt.Sprintf("\uf00c Extender %d selected (port %d.%d). \uf071 set_active: %v", digit, digit, curSubPort, err)
		} else {
			m.result = fmt.Sprintf("\uf00c Extender %d selected → active port %d.%d", digit, digit, curSubPort)
		}
		return m, nil
	}
	if m.focusMode == "port" {
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
		if err := m.trySetActive(newPort); err != nil {
			m.result = fmt.Sprintf("\uf00c Port %d.%d selected. \uf071 set_active: %v", curExt, digit, err)
		} else {
			m.result = fmt.Sprintf("\uf00c Port %d.%d → now active on PiKVM", curExt, digit)
		}
		return m, nil
	}
	if m.focusMode == "ops" {
		digit := int(msg.String()[0] - '0')
		if digit >= 1 && digit <= 7 {
			m.cursor = digit - 1
		}
		return m, nil
	}
	if m.focusMode == "scripts" {
		digit := int(msg.String()[0] - '0')
		if digit >= 1 && digit <= 9 {
			idx := digit - 1
			if idx < len(scripts.Default) {
				m.cursor = idx
				m.launchScript(idx)
			}
		}
		return m, nil
	}
	return m, nil
}
