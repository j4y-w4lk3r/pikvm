package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"pikvm/internal/api"
	"pikvm/internal/config"
	"pikvm/internal/hooks"
)

// hostSwitchDoneMsg is returned by switchHostCmd after config.UseHost,
// a fresh /api/switch fetch, and an immediate /api/info poll.
type hostSwitchDoneMsg struct {
	name         string
	prevName     string
	prevHost     string
	sw           api.SwitchState
	info         api.InfoState
	infoOK       bool
	err          error
}

// multiHostEnabled is true when config.json v2 lists more than one PiKVM.
func multiHostEnabled() bool {
	return len(config.Hosts) > 1
}

// hostDisplayLabel returns the friendly name shown in the header.
func hostDisplayLabel() string {
	if config.HostName != "" {
		return config.HostName
	}
	return config.Host
}

// renderAppHeader is the bordered title box used across main/grid views.
func renderAppHeader() string {
	host := config.Host
	if host == "" {
		host = "unknown"
	}
	return headerStyle.Render(fmt.Sprintf(" PiKVM - %s ", host))
}

// switchHostCmd activates another configured PiKVM off the UI thread, then
// reconnects the WebSocket and refetches topology + /api/info.
func switchHostCmd(name string) tea.Cmd {
	prevName := config.HostName
	prevHost := config.Host
	return func() tea.Msg {
		if name == prevName {
			return hostSwitchDoneMsg{name: name, prevName: prevName, prevHost: prevHost}
		}
		if err := config.UseHost(name); err != nil {
			return hostSwitchDoneMsg{name: name, prevName: prevName, prevHost: prevHost, err: err}
		}
		if prevName != "" {
			hooks.Dispatch("host-disconnected", map[string]string{
				"host_name": prevName,
				"host":      prevHost,
				"user":      config.User,
				"next_host": name,
			})
		}
		api.InvalidateISOCache()
		api.RequestWSReconnect()
		sw := api.FetchSwitchState()
		info, infoOK := api.FetchInfoState()
		hooks.Dispatch("host-connected", map[string]string{
			"host_name": config.HostName,
			"host":      config.Host,
			"user":      config.User,
			"prev_host": prevHost,
		})
		return hostSwitchDoneMsg{
			name: name, prevName: prevName, prevHost: prevHost,
			sw: sw, info: info, infoOK: infoOK,
		}
	}
}

// applyHostSwitch resets live PiKVM state on the model after a successful
// host change so the TUI reflects the new box instead of stale data.
func (m *Model) applyHostSwitch(msg hostSwitchDoneMsg) {
	sw := msg.sw
	cursor := sw.ActivePort
	if cursor < 0 {
		cursor = 0
	}

	m.port = cursor
	m.extenders = sw.Extenders
	m.portsPerExt = sw.PortsPerExt
	m.totalPorts = sw.TotalPorts
	m.activePort = sw.ActivePort
	m.availablePorts = sw.Available
	m.videoLinks = sw.VideoLinks
	m.usbLinks = sw.UsbLinks
	m.powerLeds = sw.PowerLeds
	m.hddLeds = sw.HddLeds
	m.portsDetected = true
	m.directATX = sw.DirectATX

	m.cursor = 0
	m.focusMode = ""
	m.inScripts = false
	m.selectingISO = false
	m.selectingBIOSKey = false
	m.gridView = false
	m.gridCursor = cursor
	m.hostSwitching = false
	m.showHelp = false

	m.wsConnected = false
	m.wsLastError = ""
	m.wsClients = 0
	m.atxPower = len(sw.PowerLeds) > 0 && sw.PowerLeds[0]
	m.atxHdd = len(sw.HddLeds) > 0 && sw.HddLeds[0]
	m.atxBusy = false

	m.msdOnline = false
	m.msdBusy = false
	m.msdConnect = false
	m.msdFree = 0
	m.msdTotal = 0
	m.msdUpload = false
	m.msdUpName = ""
	m.msdUpPct = 0

	if msg.infoOK {
		m.info = msg.info
	} else {
		m.info = api.InfoState{}
	}

	// Suppress spurious transition hooks until the new WS stream seeds state.
	m.firstSwitchSeen = false
	m.firstMsdSeen = false
	m.firstClientsSeen = false
}
