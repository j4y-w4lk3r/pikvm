package tui

import (
	"fmt"
	"strings"

	"pikvm/internal/state"
)

// renderGridView is the whole-screen view shown while m.gridView is true.
// See renderPortCellLines for rendering strategy notes.
func (m Model) renderGridView() string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(headerStyle.Render(" \uf0ce PiKVM Port Grid ") + "\n\n")

	for ext := 1; ext <= m.extenders; ext++ {
		sb.WriteString("  " + portInfoStyle.Render(fmt.Sprintf("Extender %d", ext)) + "\n")

		cells := make([][]string, m.portsPerExt)
		for p := 1; p <= m.portsPerExt; p++ {
			cells[p-1] = m.renderPortCellLines(m.linearPort(ext, p))
		}
		for row := 0; row < 6; row++ {
			sb.WriteString("  ")
			for i, cell := range cells {
				if i > 0 {
					sb.WriteString(" ")
				}
				sb.WriteString(cell[row])
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	legend := helpStyle.Render("legend: V video  U usb  P power  ★ PiKVM-active")
	help := helpStyle.Render("arrows/hjkl: move   Enter: switch to port   g or ESC: back   q: quit")
	sb.WriteString("  " + legend + "\n")
	sb.WriteString("  " + help + "\n")
	sb.WriteString("\n  " + m.renderStatusBar() + "\n")
	return sb.String()
}

// cellWidth is the visible character width of the interior of one grid cell.
const cellWidth = 10

// renderPortCellLines returns a grid cell as 6 fixed-width strings (top
// border, 4 content rows, bottom border). ASCII-only content so every row
// lines up regardless of terminal font. The cursor cell uses heavy box-
// drawing chars (┏ ┓ ┃ ┗ ┛) in success-green; non-cursor cells use light
// chars (┌ ┐ │ └ ┘) in dim grey. Same visible width so nothing shifts.
func (m Model) renderPortCellLines(linear int) []string {
	id := state.PortExtID(linear, m.portsPerExt)
	isActive := linear == m.activePort
	isCursor := m.gridView && linear == m.gridCursor

	row1 := id
	if isActive {
		row1 += " \u2605"
	}
	name := ""
	if p, ok := m.state.Ports[id]; ok && p.Name != "" {
		name = p.Name
	}
	if len(name) > cellWidth-2 {
		name = name[:cellWidth-2]
	}
	power := "-"
	if linear < len(m.powerLeds) {
		if m.powerLeds[linear] {
			power = "on"
		} else {
			power = "off"
		}
	}
	flag := func(arr []bool, lit string) string {
		if linear < len(arr) && arr[linear] {
			return successStyle.Render(lit)
		}
		return helpStyle.Render("\u00B7")
	}
	r1Pad := padRight(row1, cellWidth-2)
	r2Pad := padRight(name, cellWidth-2)
	r3Pad := padRight(power, cellWidth-2)
	flagStr := flag(m.videoLinks, "V") + " " + flag(m.usbLinks, "U") + " " + flag(m.powerLeds, "P")
	const visibleFlagLen = 5
	if visibleFlagLen < cellWidth-2 {
		flagStr += strings.Repeat(" ", cellWidth-2-visibleFlagLen)
	}

	tl, tr, bl, br, h, v := "\u250C", "\u2510", "\u2514", "\u2518", "\u2500", "\u2502"
	borderStyle := helpStyle
	if isCursor {
		tl, tr, bl, br, h, v = "\u250F", "\u2513", "\u2517", "\u251B", "\u2501", "\u2503"
		borderStyle = successStyle
	}
	topBorder := borderStyle.Render(tl + strings.Repeat(h, cellWidth) + tr)
	botBorder := borderStyle.Render(bl + strings.Repeat(h, cellWidth) + br)
	leftV := borderStyle.Render(v)
	rightV := borderStyle.Render(v)
	wrap := func(body string) string { return leftV + " " + body + " " + rightV }

	return []string{
		topBorder,
		wrap(r1Pad),
		wrap(r2Pad),
		wrap(r3Pad),
		wrap(flagStr),
		botBorder,
	}
}

// padRight pads s with spaces on the right so the rune count is exactly n.
// Uses rune count (not byte count) because '★' is 3 bytes of UTF-8 but 1
// display column.
func padRight(s string, n int) string {
	runes := 0
	for range s {
		runes++
	}
	if runes >= n {
		return s
	}
	return s + strings.Repeat(" ", n-runes)
}
