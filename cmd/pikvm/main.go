// Command pikvm is a terminal UI + CLI for controlling a PiKVM ATX switch.
//
// Run `pikvm` with no arguments to launch the interactive TUI. Pass any
// argument to dispatch into the CLI (see `pikvm help`).
package main

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"pikvm/internal/api"
	"pikvm/internal/cli"
	"pikvm/internal/config"
	"pikvm/internal/tui"
)

func main() {
	if err := config.Load(); err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		fmt.Println("Make sure config.json or .env exists in the same directory as the binary")
		os.Exit(1)
	}

	// CLI mode: any args = one-shot command, skip the TUI entirely.
	if len(os.Args) > 1 {
		cli.Run()
		return
	}

	// TUI mode with live WebSocket + /api/info pollers.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := tea.NewProgram(tui.InitialModel(), tea.WithAltScreen())
	api.StartWebSocket(ctx, p)
	api.StartInfoPoller(ctx, p)

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
