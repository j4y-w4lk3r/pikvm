package tui

import (
	"fmt"
	"strings"

	"pikvm/internal/config"
)

type helpSection struct {
	title string
	lines []string
}

// helpSections returns the keyboard-shortcut reference shown by ?.
func helpSections() []helpSection {
	sections := []helpSection{
		{
			title: "Navigation",
			lines: []string{
				"e          Focus extender row, then 1-9 to pick",
				"p          Focus port row, then 1-9 to pick",
				"o          Focus operations, then 1-7 or Enter",
				"c          Focus custom scripts, then 1-9 or Enter",
				"g          Toggle port grid view",
				"↑/↓ j/k    Move cursor in lists / grid",
				"Enter      Run selected operation or script",
				"ESC        Back out of section (or quit from main)",
			},
		},
		{
			title: "Power & status",
			lines: []string{
				"1-7        Quick-select operation when [O] is focused",
				"r          Reconnect WebSocket to PiKVM",
				"Status bar shows host, kvmd version, MSD, uptime, ws state",
			},
		},
		{
			title: "Grid view",
			lines: []string{
				"arrows/hjkl  Move between port cells",
				"Enter        Switch PiKVM active port to cell",
				"g / ESC      Return to main view",
			},
		},
	}
	if multiHostEnabled() {
		hostLines := []string{
			"h          Focus [H] Host row, then press a number:",
		}
		for i, name := range config.HostNames() {
			h := config.Hosts[name]
			line := fmt.Sprintf("  [%d] %-12s  %s", i+1, name, h.Host)
			if name == config.HostName {
				line += "  ← active"
			}
			hostLines = append(hostLines, line)
		}
		hostLines = append(hostLines,
			"Header shows PiKVM - <ip> of the active host",
			"CLI: pikvm --host <name>  or  pikvm hosts list",
		)
		sections = append([]helpSection{{
			title: "Switch PiKVM",
			lines: hostLines,
		}}, sections...)
	} else {
		sections = append([]helpSection{{
			title: "Switch PiKVM",
			lines: []string{
				"Only one PiKVM configured — add more in ~/.config/pikvm/config.json",
				"Schema v2: { \"hosts\": { \"pikvm1\": {...}, \"pikvm2\": {...} } }",
				"Then press h, then 1/2/… to switch inside the TUI",
			},
		}}, sections...)
	}
	sections = append(sections, helpSection{
		title: "General",
		lines: []string{
			"?          Toggle this help screen",
			"q          Quit",
			"Port icons:  video  usb  power  (under each [P] port)",
		},
	})
	return sections
}

func (m Model) renderHelpView() string {
	var s strings.Builder
	s.WriteString("\n")
	s.WriteString(headerStyle.Render(" \uf059 Help ") + "\n\n")

	for _, sec := range helpSections() {
		s.WriteString("  " + selectedStyle.Render(sec.title) + "\n")
		for _, line := range sec.lines {
			s.WriteString("  " + unselectedStyle.Render(line) + "\n")
		}
		s.WriteString("\n")
	}

	hostLine := fmt.Sprintf("Connected: %s@%s", config.User, config.Host)
	if config.HostName != "" {
		hostLine += fmt.Sprintf("  (%s)", config.HostName)
	}
	s.WriteString("  " + portInfoStyle.Render(hostLine) + "\n\n")
	s.WriteString("  " + helpStyle.Render("Press ? or ESC to close") + "\n\n")
	return s.String()
}
