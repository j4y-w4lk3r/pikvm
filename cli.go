// Command-line interface (roadmap idea #27).
//
// Two modes:
//
//   1. Pretty (default): human-readable colored output, suitable for ad-hoc use.
//   2. --json:           every successful command emits a single JSON line of
//                        the form {"ok":true,"command":"...","result":{...}};
//                        every failure emits {"ok":false,"command":"...","error":"..."}.
//                        Designed for piping into jq, scripts, or CI.
//
// The fuzzy pickers (`pikvm pick port|iso|script`) shell out to `fzf` so you
// can drive PiKVM keyboard-only without remembering port numbers or ISO
// filenames.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// ----------------------------------------------------------------------------
// Output helpers — consistent JSON shape so scripts can rely on it.
// ----------------------------------------------------------------------------

type cliResponse struct {
	OK      bool        `json:"ok"`
	Command string      `json:"command"`
	Result  interface{} `json:"result,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func emit(jsonMode bool, command string, result interface{}, err error) {
	if jsonMode {
		resp := cliResponse{OK: err == nil, Command: command}
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.Result = result
		}
		out, _ := json.Marshal(resp)
		fmt.Println(string(out))
	} else if err != nil {
		fmt.Fprintf(os.Stderr, "\uf057 %s: %v\n", command, err)
	}
}

func die(jsonMode bool, command string, err error) {
	emit(jsonMode, command, nil, err)
	os.Exit(1)
}

// ----------------------------------------------------------------------------
// Argument parsing
// ----------------------------------------------------------------------------

// parseFlags strips the global `--json` flag (anywhere in args) and returns
// (jsonMode, remainingArgs).
func parseFlags(args []string) (bool, []string) {
	jsonMode := false
	remaining := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--json", "-j":
			jsonMode = true
		default:
			remaining = append(remaining, a)
		}
	}
	return jsonMode, remaining
}

// parsePort accepts either a linear port ("5") or extender.port form ("2.3").
// Returns the linear 0-based port. Requires a live /api/switch fetch only
// when ext.port form is used (so simple integer args don't pay the round-trip).
func parsePort(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	if strings.Contains(s, ".") {
		parts := strings.SplitN(s, ".", 2)
		ext, err1 := strconv.Atoi(parts[0])
		pno, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || ext < 1 || pno < 1 {
			return 0, fmt.Errorf("invalid port %q (use linear like '5' or extender.port like '2.3')", s)
		}
		state := fetchSwitchState()
		if ext > state.Extenders || pno > state.PortsPerExt {
			return 0, fmt.Errorf("port %s out of range (have %d ext × %d ports)", s, state.Extenders, state.PortsPerExt)
		}
		return (ext-1)*state.PortsPerExt + (pno - 1), nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid port %q", s)
	}
	return n, nil
}

// ----------------------------------------------------------------------------
// CLI dispatch entry point — replaces the old handleCLI in pikvm.go
// ----------------------------------------------------------------------------

func runCLI() {
	jsonMode, args := parseFlags(os.Args[1:])
	if len(args) == 0 {
		showHelp()
		return
	}

	switch args[0] {
	case "help", "--help", "-h":
		showHelp()

	// Read-only inspection
	case "info":
		cliInfo(jsonMode)
	case "switch":
		cliSwitch(jsonMode, args[1:])
	case "iso":
		cliIso(jsonMode, args[1:])

	// Mutations
	case "power":
		cliPower(jsonMode, args[1:])
	case "reset":
		cliReset(jsonMode, args[1:])

	// Fuzzy pickers (fzf)
	case "pick":
		cliPick(jsonMode, args[1:])

	// Back-compat aliases (old syntax)
	case "on", "off", "click", "long", "reset-long":
		cliLegacyAction(jsonMode, args[0], args[1:])

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", args[0])
		showHelp()
		os.Exit(2)
	}
}

// ----------------------------------------------------------------------------
// Inspection commands
// ----------------------------------------------------------------------------

func cliInfo(jsonMode bool) {
	state, ok := fetchInfoState()
	if !ok {
		die(jsonMode, "info", fmt.Errorf("could not fetch /api/info"))
	}
	if jsonMode {
		emit(true, "info", state, nil)
		return
	}
	fmt.Printf("hostname:  %s\n", state.Hostname)
	fmt.Printf("platform:  PiKVM %s\n", state.Platform)
	fmt.Printf("kvmd:      v%s\n", state.KvmdVersion)
	fmt.Printf("uptime:    %s\n", formatUptime(state))
	fmt.Printf("cpu:       %d%%   mem: %.0f%%   temp: %.1f°C\n", state.CPUPercent, state.MemPercent, state.CPUTempC)
}

func cliSwitch(jsonMode bool, args []string) {
	if len(args) > 0 && args[0] == "set" {
		if len(args) < 2 {
			die(jsonMode, "switch set", fmt.Errorf("usage: pikvm switch set <port>"))
		}
		port, err := parsePort(args[1])
		if err != nil {
			die(jsonMode, "switch set", err)
		}
		if err := setSwitchPort(port); err != nil {
			die(jsonMode, "switch set", err)
		}
		state := fetchSwitchState()
		ext := port/state.PortsPerExt + 1
		pno := port%state.PortsPerExt + 1
		result := map[string]interface{}{"port": port, "id": fmt.Sprintf("%d.%d", ext, pno)}
		if jsonMode {
			emit(true, "switch_set", result, nil)
		} else {
			fmt.Printf("\uf00c switched to port %d.%d (linear %d)\n", ext, pno, port)
		}
		return
	}

	state := fetchSwitchState()
	if jsonMode {
		emit(true, "switch", state, nil)
		return
	}
	fmt.Printf("extenders:    %d × %d ports = %d total\n", state.Extenders, state.PortsPerExt, state.TotalPorts)
	fmt.Printf("active port:  %d  (%d.%d)\n", state.ActivePort, state.ActivePort/state.PortsPerExt+1, state.ActivePort%state.PortsPerExt+1)
	for i := 0; i < state.TotalPorts; i++ {
		ext := i/state.PortsPerExt + 1
		pno := i%state.PortsPerExt + 1
		flags := ""
		if i < len(state.PowerLeds) && state.PowerLeds[i] {
			flags += " on"
		}
		if i < len(state.VideoLinks) && state.VideoLinks[i] {
			flags += " video"
		}
		if i < len(state.UsbLinks) && state.UsbLinks[i] {
			flags += " usb"
		}
		marker := "  "
		if i == state.ActivePort {
			marker = "* "
		}
		fmt.Printf("%s%d.%d  port %d %s\n", marker, ext, pno, i, flags)
	}
}

func cliIso(jsonMode bool, args []string) {
	if len(args) > 0 && args[0] != "list" {
		die(jsonMode, "iso", fmt.Errorf("unknown iso subcommand %q (only 'list' supported here; use the TUI or web for upload)", args[0]))
	}
	entries, err := fetchAvailableISOEntries()
	if err != nil {
		die(jsonMode, "iso_list", err)
	}
	if jsonMode {
		emit(true, "iso_list", entries, nil)
		return
	}
	for _, e := range entries {
		if e.LocalPath != "" {
			fmt.Printf("  %s   %s\n", e.Display, e.LocalPath)
		} else {
			fmt.Printf("  %s\n", e.Display)
		}
	}
}

// ----------------------------------------------------------------------------
// Action commands
// ----------------------------------------------------------------------------

func cliPower(jsonMode bool, args []string) {
	if len(args) == 0 {
		die(jsonMode, "power", fmt.Errorf("usage: pikvm power on|off|click|long [port]"))
	}
	var actName string
	switch args[0] {
	case "on":
		actName = "Power ON"
	case "off":
		actName = "Power OFF"
	case "click":
		actName = "Power Click"
	case "long":
		actName = "Power Long Press"
	default:
		die(jsonMode, "power", fmt.Errorf("unknown power subcommand %q", args[0]))
	}
	runActionByName(jsonMode, "power_"+args[0], actName, args[1:])
}

func cliReset(jsonMode bool, args []string) {
	if len(args) == 0 {
		die(jsonMode, "reset", fmt.Errorf("usage: pikvm reset click|long [port]"))
	}
	var actName string
	switch args[0] {
	case "click":
		actName = "Reset Click"
	case "long":
		actName = "Reset Long Press"
	default:
		die(jsonMode, "reset", fmt.Errorf("unknown reset subcommand %q", args[0]))
	}
	runActionByName(jsonMode, "reset_"+args[0], actName, args[1:])
}

// runActionByName resolves an action by display name and runs it.
func runActionByName(jsonMode bool, command, name string, args []string) {
	port := 0
	if len(args) > 0 {
		p, err := parsePort(args[0])
		if err != nil {
			die(jsonMode, command, err)
		}
		port = p
	}
	for _, a := range actions {
		if a.name == name {
			result := executeAction(a, port)
			ok := strings.Contains(result, "\uf00c") || strings.Contains(result, "Success")
			if jsonMode {
				if ok {
					emit(true, command, map[string]interface{}{"port": port, "action": name}, nil)
				} else {
					emit(false, command, nil, fmt.Errorf("%s", stripIcons(result)))
				}
			} else {
				fmt.Println(result)
			}
			return
		}
	}
	die(jsonMode, command, fmt.Errorf("internal: action %q not registered", name))
}

func cliLegacyAction(jsonMode bool, command string, args []string) {
	mapping := map[string]string{
		"on":         "Power ON",
		"off":        "Power OFF",
		"click":      "Power Click",
		"long":       "Power Long Press",
		"reset-long": "Reset Long Press",
	}
	name := mapping[command]
	if name == "" {
		die(jsonMode, command, fmt.Errorf("unknown legacy command %q", command))
	}
	runActionByName(jsonMode, command, name, args)
}

// stripIcons removes nerd-font glyphs and leading whitespace so the JSON
// 'error' field stays readable when emitted from script-friendly output.
func stripIcons(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0xE000 && r <= 0xF8FF { // Private Use Area (most NF glyphs)
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// ----------------------------------------------------------------------------
// Fuzzy pickers (fzf)
// ----------------------------------------------------------------------------

func cliPick(jsonMode bool, args []string) {
	if len(args) == 0 {
		die(jsonMode, "pick", fmt.Errorf("usage: pikvm pick port|iso|script"))
	}
	if _, err := exec.LookPath("fzf"); err != nil {
		die(jsonMode, "pick", fmt.Errorf("fzf not found in PATH (install: brew install fzf)"))
	}
	switch args[0] {
	case "port":
		cliPickPort(jsonMode)
	case "iso":
		cliPickISO(jsonMode)
	case "script":
		cliPickScript(jsonMode)
	default:
		die(jsonMode, "pick", fmt.Errorf("unknown pick target %q (use port|iso|script)", args[0]))
	}
}

// fzfPick pipes lines into fzf and returns the selected line (without
// trailing newline) plus error if user cancelled or fzf failed.
func fzfPick(prompt string, lines []string) (string, error) {
	if len(lines) == 0 {
		return "", fmt.Errorf("nothing to pick from")
	}
	cmd := exec.Command("fzf", "--prompt="+prompt+"> ", "--height=40%", "--reverse", "--no-mouse")
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		// fzf exits non-zero on cancel; surface it as a clean cancel.
		return "", fmt.Errorf("cancelled or fzf failed: %v", err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func cliPickPort(jsonMode bool) {
	state := fetchSwitchState()
	if state.TotalPorts == 0 {
		die(jsonMode, "pick_port", fmt.Errorf("no ports detected"))
	}
	lines := make([]string, 0, state.TotalPorts)
	for i := 0; i < state.TotalPorts; i++ {
		ext := i/state.PortsPerExt + 1
		pno := i%state.PortsPerExt + 1
		flags := []string{}
		if i < len(state.PowerLeds) && state.PowerLeds[i] {
			flags = append(flags, "on")
		}
		if i < len(state.VideoLinks) && state.VideoLinks[i] {
			flags = append(flags, "video")
		}
		if i == state.ActivePort {
			flags = append(flags, "*ACTIVE*")
		}
		lines = append(lines, fmt.Sprintf("%d.%d  (linear %d)  %s", ext, pno, i, strings.Join(flags, " ")))
	}
	picked, err := fzfPick("port", lines)
	if err != nil {
		die(jsonMode, "pick_port", err)
	}
	parts := strings.Fields(picked)
	if len(parts) < 1 {
		die(jsonMode, "pick_port", fmt.Errorf("could not parse selection: %q", picked))
	}
	port, err := parsePort(parts[0])
	if err != nil {
		die(jsonMode, "pick_port", err)
	}
	if err := setSwitchPort(port); err != nil {
		die(jsonMode, "pick_port", err)
	}
	emit(jsonMode, "pick_port", map[string]interface{}{"port": port, "id": parts[0]}, nil)
	if !jsonMode {
		fmt.Printf("\uf00c switched to %s\n", parts[0])
	}
}

func cliPickISO(jsonMode bool) {
	entries, err := fetchAvailableISOEntries()
	if err != nil {
		die(jsonMode, "pick_iso", err)
	}
	if len(entries) == 0 {
		die(jsonMode, "pick_iso", fmt.Errorf("no ISOs found (place .iso files in ./iso/ or upload via PiKVM web UI)"))
	}
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = e.Display
	}
	picked, err := fzfPick("iso", lines)
	if err != nil {
		die(jsonMode, "pick_iso", err)
	}
	var chosen *isoEntry
	for i := range entries {
		if entries[i].Display == picked {
			chosen = &entries[i]
			break
		}
	}
	if chosen == nil {
		die(jsonMode, "pick_iso", fmt.Errorf("could not match selection: %q", picked))
	}
	if chosen.LocalPath != "" {
		die(jsonMode, "pick_iso", fmt.Errorf("local file %q must be uploaded first; use the TUI or ./pikvm.sh --iso --upload", chosen.LocalPath))
	}
	state := fetchSwitchState()
	port := state.ActivePort
	out := bootFromISOEntry(port, *chosen)
	if jsonMode {
		emit(true, "pick_iso", map[string]interface{}{"iso": chosen.Name, "port": port, "log": stripIcons(out)}, nil)
	} else {
		fmt.Println(out)
	}
}

func cliPickScript(jsonMode bool) {
	if len(customScripts) == 0 {
		die(jsonMode, "pick_script", fmt.Errorf("no scripts registered"))
	}
	lines := make([]string, len(customScripts))
	for i, s := range customScripts {
		lines[i] = fmt.Sprintf("%s — %s", s.name, s.desc)
	}
	picked, err := fzfPick("script", lines)
	if err != nil {
		die(jsonMode, "pick_script", err)
	}
	var chosenIdx int = -1
	for i, l := range lines {
		if l == picked {
			chosenIdx = i
			break
		}
	}
	if chosenIdx < 0 {
		die(jsonMode, "pick_script", fmt.Errorf("could not match selection: %q", picked))
	}
	s := customScripts[chosenIdx]
	switch s.name {
	case "Boot from ISO":
		die(jsonMode, "pick_script", fmt.Errorf("'Boot from ISO' needs a sub-pick; use 'pikvm pick iso' instead"))
	case "Boot to BIOS":
		die(jsonMode, "pick_script", fmt.Errorf("'Boot to BIOS' needs a key choice; run from the TUI"))
	}
	state := fetchSwitchState()
	out := s.script(state.ActivePort)
	if jsonMode {
		emit(true, "pick_script", map[string]interface{}{"script": s.name, "port": state.ActivePort, "log": stripIcons(out)}, nil)
	} else {
		fmt.Println(out)
	}
}

// ----------------------------------------------------------------------------
// Help text
// ----------------------------------------------------------------------------

func showHelp() {
	help := `
PiKVM Control - Go + Bubble Tea Edition

Usage:
  pikvm                              Launch interactive TUI
  pikvm [--json] <command> ...       Run a CLI command (add --json for script-friendly output)

Inspection:
  pikvm switch                       Show switch topology + active port
  pikvm switch set <port>            Switch active port (e.g. '5' or '2.3')
  pikvm info                         Show /api/info (kvmd version, uptime, health)
  pikvm iso list                     List ISOs (PiKVM-side + local ./iso/)

Power / reset (port = linear int or 'ext.port' like '2.3'; default = current active):
  pikvm power on   [port]            Turn power ON
  pikvm power off  [port]            Turn power OFF
  pikvm power click [port]           Power button short press
  pikvm power long  [port]           Power button long press (force shutdown)
  pikvm reset click [port]           Reset button short press
  pikvm reset long  [port]           Reset button long press

Fuzzy pickers (need 'fzf' in PATH):
  pikvm pick port                    fzf over ports → set active
  pikvm pick iso                     fzf over ISOs  → mount + boot
  pikvm pick script                  fzf over custom scripts → run

JSON mode:
  Add --json (or -j) anywhere in args. Every command emits a single line:
      {"ok":true,"command":"...","result":{...}}
  or  {"ok":false,"command":"...","error":"..."}

Examples:
  pikvm                              # TUI
  pikvm switch                       # show all ports + signals
  pikvm switch set 2.3               # switch to extender 2 / port 3
  pikvm power on 2.3                 # power on that port
  pikvm pick iso                     # fzf-pick an ISO and boot it
  pikvm --json switch | jq .         # scriptable JSON

Legacy (back-compat):
  pikvm on|off|click|long|reset|reset-long [port]   same as 'pikvm power ...'
`
	fmt.Println(strings.TrimSpace(help))
}

