package tui

import "github.com/charmbracelet/lipgloss"

// Theme greens — pastel for content, lime for the outer frame.
const (
	colorPastelGreen = "#9bf09d"
	colorLimeFrame   = "#b8ff57" // slightly yellower / brighter than pastel
)

// Pastel green theme shared by every view.
var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorPastelGreen)).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(colorPastelGreen)).
			Padding(0, 1)

	// appFrameStyle wraps the entire TUI in a thin lime outline.
	appFrameStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(colorLimeFrame)).
			Padding(1, 2)

	portInfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorPastelGreen))

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorPastelGreen))

	unselectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#BBBBBB"))

	successStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorPastelGreen))

	warningStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFD700"))

	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF5F5F"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))
)

// wrapAppFrame draws the lime border around screen content. Once Bubble Tea has
// reported the terminal size, the frame occupies the complete pane. Lipgloss's
// Width/Height include padding but exclude the border, so subtract only the
// two border cells when forcing the outer dimensions.
func (m Model) wrapAppFrame(content string) string {
	if m.termWidth <= frameOverhead || m.termHeight <= frameOverheadV {
		return appFrameStyle.Render(content)
	}

	innerW := m.contentWidth()
	content = fitContentLines(content, innerW)
	return appFrameStyle.
		Width(m.termWidth - 2).
		Height(m.termHeight - 2).
		Render(content)
}

// Per-port status glyphs from JetBrainsMono Nerd Font (Font Awesome).
const (
	iconVideo = "\uf26c"
	iconUsb   = "\uf287"
	iconPower = "\uf0e7"
	iconDim   = "·"
)
