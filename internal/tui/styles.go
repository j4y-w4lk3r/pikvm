package tui

import "github.com/charmbracelet/lipgloss"

// Pastel green theme shared by every view.
var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#9bf09d")).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#9bf09d")).
			Padding(0, 1)

	portInfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9bf09d"))

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#9bf09d"))

	unselectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#BBBBBB"))

	successStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#9bf09d"))

	warningStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFD700"))

	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF5F5F"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))
)

// Per-port status glyphs from JetBrainsMono Nerd Font (Font Awesome).
const (
	iconVideo = "\uf26c"
	iconUsb   = "\uf287"
	iconPower = "\uf0e7"
	iconDim   = "·"
)
