package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// frameOverhead is border (2) + horizontal padding (4) consumed by wrapAppFrame.
const frameOverhead = 6

// twoColumnMin is the minimum inner content width for side-by-side layout.
const twoColumnMin = 88

// defaultContentWidth is used before the first WindowSizeMsg (tests, first frame).
const defaultContentWidth = 100

// contentWidth returns the usable inner width inside the lime frame.
func (m Model) contentWidth() int {
	w := m.termWidth - frameOverhead
	if w <= 0 {
		return defaultContentWidth
	}
	if w < 32 {
		return 32
	}
	return w
}

func (m Model) useTwoColumns() bool {
	return m.contentWidth() >= twoColumnMin
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
