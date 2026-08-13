// Command pikvm is a terminal UI + CLI for controlling a PiKVM ATX switch.
//
// Run `pikvm` with no arguments to launch the interactive TUI. Pass any
// argument to dispatch into the CLI (see `pikvm help`).
//
// Build-time vars (version, commit, date) are injected by GoReleaser via
// `-X main.<var>=...` ldflags. Local `go build` leaves them at "dev".
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

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cfgErr := config.Load()

	// CLI mode: any args = one-shot command, skip the TUI entirely.
	// Help / version always work; subcommands that need a PiKVM check
	// config.Loaded themselves and emit a friendly error if missing.
	if len(os.Args) > 1 {
		cli.Version = version
		cli.Commit = commit
		cli.Date = date
		cli.ConfigErr = cfgErr
		cli.Run()
		return
	}

	// TUI mode requires a working config — no point starting Bubble Tea
	// just to show "neither config.json nor .env found" inside the alt
	// screen.
	if cfgErr != nil {
		printConfigError(cfgErr)
		os.Exit(1)
	}

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

func printConfigError(err error) {
	fmt.Fprintf(os.Stderr, "pikvm: %v\n\n", err)
	if len(config.SearchedPaths()) > 0 {
		fmt.Fprintln(os.Stderr, "Searched local paths (in order):")
		for _, p := range config.SearchedPaths() {
			fmt.Fprintf(os.Stderr, "  - %s\n", p)
		}
		fmt.Fprintln(os.Stderr, "")
	}
	fmt.Fprintln(os.Stderr, config.SetupHelp())
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Or run 'pikvm help' / 'pikvm --version' for commands that don't need a PiKVM.")
}
