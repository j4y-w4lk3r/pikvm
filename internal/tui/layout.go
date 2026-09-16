package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// frameOverhead is border (2) + horizontal padding (4) consumed by wrapAppFrame.
const frameOverhead = 6

// frameOverheadV is top/bottom border (2) + vertical padding (2).
const frameOverheadV = 4

// twoColumnMin is the minimum inner width for side-by-side ops + scripts.
const twoColumnMin = 88

// Minimum inner widths for the two-column split (left ops block, right scripts).
const colLeftMin = 44
const colRightMin = 28
const colGap = 2

// compactHeightThreshold: below this, drop non-essential lines and tighten spacing.
const compactHeightThreshold = 22

// defaultContentWidth is used before the first WindowSizeMsg (tests, first frame).
const defaultContentWidth = 100

// contentWidth returns the usable inner width inside the lime frame.
func (m Model) contentWidth() int {
	w := m.termWidth - frameOverhead
	if w <= 0 {
		if m.termWidth > 0 {
			return 1
		}
		return defaultContentWidth
	}
	return w
}

// contentHeight returns usable inner height inside the lime frame.
func (m Model) contentHeight() int {
	h := m.termHeight - frameOverheadV
	if h <= 0 {
		if m.termHeight > 0 {
			return 1
		}
		return 40
	}
	return h
}

// indentedContentWidth is the width available to rows rendered with the
// conventional two-space inset used throughout the TUI.
func (m Model) indentedContentWidth() int {
	w := m.contentWidth() - 2
	if w < 1 {
		return 1
	}
	return w
}

// layoutCompact is true on very short terminals — hide legend, tighten gaps.
func (m Model) layoutCompact() bool {
	return m.termHeight > 0 && m.contentHeight() < compactHeightThreshold
}

// useTwoColumns picks side-by-side ops/scripts only when the pane is wide
// enough for both columns without clipping (tmux horizontal splits stack).
func (m Model) useTwoColumns() bool {
	w := m.contentWidth()
	return w >= twoColumnMin && w >= colLeftMin+colRightMin+colGap
}

// columnWidths splits inner width between the ops block and scripts column.
func (m Model) columnWidths() (left, right, gap int) {
	w := m.contentWidth()
	gap = colGap
	right = w * 42 / 100
	if right < colRightMin {
		right = colRightMin
	}
	left = w - right - gap
	if left < colLeftMin {
		left = colLeftMin
		right = w - left - gap
	}
	return left, right, gap
}

func (m Model) portCellWidth() int {
	switch {
	case m.contentWidth() < 52:
		return 5
	case m.contentWidth() < 68:
		return 6
	default:
		return 7
	}
}

func (m Model) gridCellWidth() int {
	switch {
	case m.contentWidth() < 44:
		return 7
	case m.contentWidth() < 64:
		return 8
	case m.contentWidth() < 88:
		return 9
	default:
		return 10
	}
}

// gridCellsPerRow returns how many port cells fit on one grid line.
func (m Model) gridCellsPerRow() int {
	cellOuter := m.gridCellWidth() + 3 // borders + one space gap
	n := m.contentWidth() / cellOuter
	if n < 1 {
		n = 1
	}
	if n > m.portsPerExt {
		n = m.portsPerExt
	}
	return n
}

// wrapPlain wraps unstyled text to width using spaces as break points.
func wrapPlain(text string, width int) string {
	if width <= 0 || utf8.RuneCountInString(text) <= width {
		return text
	}
	var out strings.Builder
	lineLen := 0
	for _, word := range strings.Fields(text) {
		wl := utf8.RuneCountInString(word)
		if lineLen > 0 && lineLen+1+wl > width {
			out.WriteByte('\n')
			lineLen = 0
		}
		if lineLen > 0 {
			out.WriteByte(' ')
			lineLen++
		}
		out.WriteString(word)
		lineLen += wl
	}
	return out.String()
}

// packStyledLines joins styled fragments into multiple lines that fit width.
func packStyledLines(parts []string, maxWidth int, sep string) []string {
	if maxWidth <= 0 || len(parts) == 0 {
		return parts
	}
	sepW := lipgloss.Width(sep)
	var lines []string
	var cur strings.Builder
	curW := 0
	for _, p := range parts {
		pw := lipgloss.Width(p)
		add := pw
		if cur.Len() > 0 {
			add += sepW
		}
		if cur.Len() > 0 && curW+add > maxWidth {
			lines = append(lines, cur.String())
			cur.Reset()
			curW = 0
		}
		if cur.Len() > 0 {
			cur.WriteString(sep)
			curW += sepW
		}
		cur.WriteString(p)
		curW += pw
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}

// fitContentLines clips every rendered line to maxW display columns so tmux
// splits and narrow kitty panes never spill past the lime frame border.
func fitContentLines(content string, maxW int) string {
	if maxW <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if ansi.StringWidth(line) > maxW {
			lines[i] = ansi.Truncate(line, maxW, "…")
		}
	}
	return strings.Join(lines, "\n")
}

// wrapStyledText word-wraps plain text to maxW, then applies style per line.
func wrapStyledText(text string, maxW int, style lipgloss.Style) []string {
	if maxW <= 0 {
		return []string{style.Render(text)}
	}
	wrapped := wrapPlain(text, maxW)
	lines := strings.Split(wrapped, "\n")
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = style.Render(line)
	}
	return out
}
