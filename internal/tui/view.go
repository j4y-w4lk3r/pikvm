package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"pikvm/internal/api"
	"pikvm/internal/config"
	"pikvm/internal/scripts"
	"pikvm/internal/state"
)

// View is Bubble Tea's render function. Branches on the various modal
// states; the main two-column layout is the default.
func (m Model) View() string {
	var content string
	switch {
	case m.quitting:
		content = successStyle.Render("\n\uf00c Goodbye!\n\n")
	case m.showHelp:
		content = m.renderHelpView()
	case m.gridView:
		content = m.renderGridView()
	case m.selectingBIOSKey:
		content = m.renderBIOSPicker()
	case m.selectingISO:
		content = m.renderISOPicker()
	default:
		content = m.renderMain()
	}
	return m.wrapAppFrame(content)
}

// renderBIOSPicker (idea #6) — two-column key chooser with per-port saved default.
func (m Model) renderBIOSPicker() string {
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

	if m.contentWidth() < 72 {
		for idx, opt := range scripts.BIOSKeyOptions {
			marker := " "
			if opt.Key == savedKey {
				marker = "★"
			}
			cell := fmt.Sprintf(" %s [%d] %-4s  %s", marker, idx+1, opt.Label, scripts.BIOSKeyHint(opt.Label))
			s.WriteString("  " + unselectedStyle.Render(cell) + "\n")
		}
	} else {
		half := (len(scripts.BIOSKeyOptions) + 1) / 2
		for row := 0; row < half; row++ {
			line := "  "
			for col := 0; col < 2; col++ {
				idx := col*half + row
				if idx >= len(scripts.BIOSKeyOptions) {
					continue
				}
				opt := scripts.BIOSKeyOptions[idx]
				marker := " "
				if opt.Key == savedKey {
					marker = "★"
				}
				cell := fmt.Sprintf(" %s [%d] %-4s  %s", marker, idx+1, opt.Label, scripts.BIOSKeyHint(opt.Label))
				line += lipgloss.NewStyle().Width(36).Render(unselectedStyle.Render(cell))
			}
			s.WriteString(line + "\n")
		}
	}
	s.WriteString("\n")
	s.WriteString("  " + helpStyle.Render(fmt.Sprintf("1-%d: pick (saves to profile)   ESC: cancel", len(scripts.BIOSKeyOptions))) + "\n\n")
	return s.String()
}

// renderISOPicker — modal list of available ISOs (PiKVM + local).
func (m Model) renderISOPicker() string {
	var s strings.Builder
	s.WriteString("\n")
	s.WriteString(headerStyle.Render(" \uf0c1 Select ISO to Boot ") + "\n\n")

	info := fmt.Sprintf("\uf0e4 Port: %d  │  ISOs available: %d", m.port+1, len(m.availableISOEntries))
	s.WriteString("  " + portInfoStyle.Render(info) + "\n\n")

	for i, entry := range m.availableISOEntries {
		if i == m.isoCursor {
			s.WriteString("  " + selectedStyle.Render(fmt.Sprintf("  \uf0a1 %s", entry.Display)) + "\n")
		} else {
			s.WriteString("  " + unselectedStyle.Render(fmt.Sprintf("  \uf15b %s", entry.Display)) + "\n")
		}
	}
	s.WriteString("\n")
	s.WriteString("  " + helpStyle.Render("↑/↓/k/j: Navigate  │  \uf04b Enter: Boot  │  ESC: Cancel") + "\n\n")
	return s.String()
}

// renderMain adapts between two-column (wide) and stacked (narrow) layouts.
func (m Model) renderMain() string {
	portCellW := m.portCellWidth()

	var left strings.Builder
	left.WriteString("\n")
	left.WriteString(renderAppHeader() + "\n\n")

	curExt := m.extenderOf(m.port)
	curSubPort := m.portOf(m.port)

	// [H] Host row — only when multiple PiKVMs are configured.
	if multiHostEnabled() {
		var hostRow strings.Builder
		if m.focusMode == "host" {
			hostRow.WriteString(selectedStyle.Render("[H] Host:    "))
		} else {
			hostRow.WriteString(unselectedStyle.Render("[H] Host:    "))
		}
		for i, name := range config.HostNames() {
			hostRow.WriteString(" ")
			label := fmt.Sprintf("[%d] %s", i+1, name)
			switch {
			case name == config.HostName && m.focusMode == "host":
				hostRow.WriteString(selectedStyle.Render(label))
			case name == config.HostName:
				hostRow.WriteString(successStyle.Render(label))
			default:
				hostRow.WriteString(unselectedStyle.Render(label))
			}
		}
		left.WriteString("  " + hostRow.String() + "\n")
		if m.hostSwitching {
			left.WriteString("  " + helpStyle.Render("\uf021 Switching PiKVM…") + "\n")
		}
		left.WriteString("\n")
	}

	// [E] Extender row
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

	// [P] Port row
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

	// Status row under the port boxes
	var statusRow strings.Builder
	statusRow.WriteString(strings.Repeat(" ", len("[P] Port:    ")))
	for i := 1; i <= m.portsPerExt; i++ {
		statusRow.WriteString(" ")
		linear := m.linearPort(curExt, i)
		statusRow.WriteString(lipgloss.NewStyle().Width(portCellW).Render(m.portStatusGlyphs(linear)))
	}
	left.WriteString("  " + statusRow.String() + "\n")

	// Active-port summary
	activeExt := m.activePort/m.portsPerExt + 1
	activePortNum := m.activePort%m.portsPerExt + 1
	syncIcon := successStyle.Render("\uf058")
	if m.activePort != m.port {
		activeLabel := fmt.Sprintf("%d.%d", activeExt, activePortNum)
		if p, ok := m.state.Ports[state.PortExtID(m.activePort, m.portsPerExt)]; ok && p.Name != "" {
			activeLabel = fmt.Sprintf("%s (%s)", activeLabel, p.Name)
		}
		if m.contentWidth() < 64 {
			syncIcon = warningStyle.Render("\uf06a")
		} else {
			syncIcon = warningStyle.Render("\uf06a") + " (PiKVM is on " + activeLabel + ")"
		}
	}
	selLabel := fmt.Sprintf("%d.%d", curExt, curSubPort)
	if p, ok := m.state.Ports[state.PortExtID(m.port, m.portsPerExt)]; ok && p.Name != "" {
		selLabel = fmt.Sprintf("%s (%s)", selLabel, p.Name)
	}
	summary := fmt.Sprintf("\uf0e4 Selected: %s  ", selLabel)
	legend := helpStyle.Render(fmt.Sprintf("  legend: %s video  %s usb  %s power", iconVideo, iconUsb, iconPower))
	left.WriteString("  " + portInfoStyle.Render(summary) + syncIcon + "\n")
	left.WriteString("  " + legend + "\n\n")

	// [O] Operations
	if m.focusMode == "ops" {
		left.WriteString("  " + selectedStyle.Render("[O] Operations (1-7 select, Enter run):") + "\n")
	} else {
		left.WriteString("  " + unselectedStyle.Render("[O] Operations:") + "\n")
	}
	for i, act := range api.DefaultActions {
		_, suffix := m.opVisualState(act.Name)
		line := fmt.Sprintf("  [%d] %s%s", i+1, act.Name, suffix)
		var rendered string
		if m.focusMode == "ops" && m.cursor == i {
			rendered = selectedStyle.Render(line)
		} else {
			rendered = unselectedStyle.Render(line)
		}
		left.WriteString("  " + rendered + "\n")
	}

	scriptsBlock := m.renderScriptsBlock(m.useTwoColumns())

	var mainBody string
	if m.useTwoColumns() {
		leftCol := lipgloss.NewStyle().Width(m.contentWidth() * 55 / 100).Render(left.String())
		rightCol := lipgloss.NewStyle().Width(m.contentWidth()*45/100 - 3).Render(scriptsBlock)
		mainBody = lipgloss.JoinHorizontal(lipgloss.Top, leftCol, "   ", rightCol)
	} else {
		mainBody = lipgloss.JoinVertical(lipgloss.Left,
			left.String(),
			"",
			scriptsBlock,
		)
	}

	// Help + result footer + status bar
	var bottom strings.Builder
	for _, line := range m.renderStatusBarLines() {
		bottom.WriteString("\n  " + line)
	}
	bottom.WriteString("\n")
	for _, line := range m.renderHelpLines() {
		bottom.WriteString("\n  " + line)
	}
	if m.result != "" {
		bottom.WriteString("\n  ")
		switch {
		case strings.Contains(m.result, "\uf00c") || strings.Contains(m.result, "Success"):
			bottom.WriteString(successStyle.Render(m.result))
		case strings.Contains(m.result, "\uf071") || strings.Contains(m.result, "Warning"):
			bottom.WriteString(warningStyle.Render(m.result))
		case strings.Contains(m.result, "\uf057") || strings.Contains(m.result, "Error"):
			bottom.WriteString(errorStyle.Render(m.result))
		default:
			bottom.WriteString(m.result)
		}
		bottom.WriteString("\n")
	}
	bottom.WriteString("\n")

	return mainBody + bottom.String()
}

// renderScriptsBlock is the [C] Custom Scripts column/section.
func (m Model) renderScriptsBlock(wideOffset bool) string {
	var right strings.Builder
	if wideOffset {
		right.WriteString("\n\n\n")
	} else {
		right.WriteString("\n")
	}
	if m.focusMode == "scripts" {
		right.WriteString("  " + selectedStyle.Render("[C] Custom Scripts (j/k, 1-9, Enter):") + "\n\n")
	} else {
		right.WriteString("  " + unselectedStyle.Render("[C] Custom Scripts:") + "\n\n")
	}
	for i, s := range scripts.Default {
		line := fmt.Sprintf("  [%d] %s", i+1, s.Name)
		highlighted := (m.focusMode == "scripts" && m.cursor == i) ||
			(m.focusMode == "" && m.inScripts && m.cursor == i)
		if highlighted {
			right.WriteString("  " + selectedStyle.Render(line) + "\n")
		} else {
			right.WriteString("  " + unselectedStyle.Render(line) + "\n")
		}
	}
	return right.String()
}

func (m Model) renderHelpLines() []string {
	w := m.contentWidth()
	if w < 72 {
		line1 := "?:Help  e:Ext  p:Port  o:Ops  c:Scripts  g:Grid  ESC  r  q"
		if multiHostEnabled() {
			line1 = "h:Host  " + line1
		}
		return []string{helpStyle.Render(line1)}
	}
	helpKeys := "?: Help  e: Extender  p: Port  o: Ops  c: Scripts  g: Grid  1-9/Enter  ESC: back  r: Reconnect  q: Quit"
	if multiHostEnabled() {
		helpKeys = "h: Host  " + helpKeys
	}
	return packStyledLines([]string{helpStyle.Render(helpKeys)}, w, " ")
}

// portStatusGlyphs returns a 3-glyph status string for one linear port
// (video / usb / power). Used under the [P] Port row and in the grid cells.
func (m Model) portStatusGlyphs(linear int) string {
	on := func(arr []bool) bool { return linear < len(arr) && arr[linear] }
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

// renderStatusBarLines returns one or more status-bar rows that fit the pane.
func (m Model) renderStatusBarLines() []string {
	return packStyledLines(m.statusBarParts(), m.contentWidth(), helpStyle.Render("  │  "))
}

func (m Model) statusBarParts() []string {
	var parts []string

	// Status bar's identity chunk: prefer the friendly host name (config
	// schema v2) over the raw IP, so users see "lab" / "garage" instead
	// of duplicate-looking 100.64.x.x addresses.
	identity := fmt.Sprintf("%s@%s (%s)", config.User, config.Host, config.HostName)
	if config.HostName == "" {
		identity = fmt.Sprintf("%s@%s", config.User, config.Host)
	}
	parts = append(parts, helpStyle.Render(identity))

	if m.info.Platform != "" {
		parts = append(parts, helpStyle.Render("PiKVM "+m.info.Platform))
	}
	if m.info.KvmdVersion != "" {
		parts = append(parts, helpStyle.Render("kvmd v"+m.info.KvmdVersion))
	}
	if m.totalPorts > 0 {
		parts = append(parts, helpStyle.Render(fmt.Sprintf("%d ext × %d ports", m.extenders, m.portsPerExt)))
	}
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
	if m.info.UptimeTotal > 0 {
		parts = append(parts, helpStyle.Render("up "+api.FormatUptime(m.info)))
	}
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
	if m.wsClients > 0 {
		parts = append(parts, helpStyle.Render(fmt.Sprintf("clients %d", m.wsClients)))
	}
	if m.atxBusy {
		parts = append(parts, warningStyle.Render("ATX busy"))
	}

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

	return parts
}

// renderStatusBar keeps a single-line join for callers that expect it.
func (m Model) renderStatusBar() string {
	lines := m.renderStatusBarLines()
	return strings.Join(lines, "\n  ")
}

// humanBytes formats a byte count in a friendly short form.
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
