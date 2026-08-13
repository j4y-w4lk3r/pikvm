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

// wrapAppFrame draws the lime border around any screen content, sized to the
// terminal when known so tmux splits don't spill past the pane edge.
func (m Model) wrapAppFrame(content string) string {
	s := appFrameStyle
	if m.termWidth > 0 {
		s = s.MaxWidth(m.termWidth)
	}
	return s.Render(content)
}

// Per-port status glyphs from JetBrainsMono Nerd Font (Font Awesome).
const (
	iconVideo = "\uf26c"
	iconUsb   = "\uf287"
	iconPower = "\uf0e7"
	iconDim   = "·"
)
