// Package cli is the one-shot command-line interface. It parses os.Args,
// dispatches to the right subcommand, and emits either human-readable
// output (default) or {"ok":..., "command":..., ...} JSON (--json / -j).
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"pikvm/internal/api"
	"pikvm/internal/scripts"
	"pikvm/internal/state"
)

// ----------------------------------------------------------------------------
// Output helpers
// ----------------------------------------------------------------------------

type response struct {
	OK      bool        `json:"ok"`
	Command string      `json:"command"`
	Result  interface{} `json:"result,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func emit(jsonMode bool, command string, result interface{}, err error) {
	if jsonMode {
		resp := response{OK: err == nil, Command: command}
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

// isLikelyName returns true for strings that look like profile names rather
// than numbers (contains any letter / '_' / '-').
func isLikelyName(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r == '-' {
			return true
		}
	}
	return false
}

// parsePort accepts linear ("5"), ext.port ("2.3"), or profile name ("j4yn0").
func parsePort(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	if isLikelyName(s) {
		st := state.Load()
		if id, ok := state.ResolveName(st, s); ok {
			return parsePort(id)
		}
		return 0, fmt.Errorf("no profile named %q (try: pikvm profile list)", s)
	}
	if strings.Contains(s, ".") {
		parts := strings.SplitN(s, ".", 2)
		ext, err1 := strconv.Atoi(parts[0])
		pno, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || ext < 1 || pno < 1 {
			return 0, fmt.Errorf("invalid port %q (use linear like '5', extender.port like '2.3', or a profile name)", s)
		}
		sw := api.FetchSwitchState()
		if ext > sw.Extenders || pno > sw.PortsPerExt {
			return 0, fmt.Errorf("port %s out of range (have %d ext × %d ports)", s, sw.Extenders, sw.PortsPerExt)
		}
		return (ext-1)*sw.PortsPerExt + (pno - 1), nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		st := state.Load()
		if id, ok := state.ResolveName(st, s); ok {
			return parsePort(id)
		}
		return 0, fmt.Errorf("invalid port %q", s)
	}
	return n, nil
}

// canonicalPortID converts user input into a stable "ext.port" key.
func canonicalPortID(s string) (string, error) {
	if isLikelyName(s) {
		st := state.Load()
		if id, ok := state.ResolveName(st, s); ok {
			return id, nil
		}
		return "", fmt.Errorf("no profile named %q", s)
	}
	if strings.Contains(s, ".") {
		return s, nil
	}
	linear, err := strconv.Atoi(s)
	if err != nil || linear < 0 {
		return "", fmt.Errorf("invalid port %q", s)
	}
	sw := api.FetchSwitchState()
	return state.PortExtID(linear, sw.PortsPerExt), nil
}

// ----------------------------------------------------------------------------
// Entry point
// ----------------------------------------------------------------------------

// Run parses os.Args and runs the requested subcommand. Designed to be
// called from main() only when os.Args[1:] is non-empty.
func Run() {
	jsonMode, args := parseFlags(os.Args[1:])
	if len(args) == 0 {
		showHelp()
		return
	}
	switch args[0] {
	case "help", "--help", "-h":
		showHelp()
	case "info":
		cliInfo(jsonMode)
	case "switch":
		cliSwitch(jsonMode, args[1:])
	case "iso":
		cliIso(jsonMode, args[1:])
	case "power":
		cliPower(jsonMode, args[1:])
	case "reset":
		cliReset(jsonMode, args[1:])
	case "pick":
		cliPick(jsonMode, args[1:])
	case "profile":
		cliProfile(jsonMode, args[1:])
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
	st, ok := api.FetchInfoState()
	if !ok {
		die(jsonMode, "info", fmt.Errorf("could not fetch /api/info"))
	}
	if jsonMode {
		emit(true, "info", st, nil)
		return
	}
	fmt.Printf("hostname:  %s\n", st.Hostname)
	fmt.Printf("platform:  PiKVM %s\n", st.Platform)
	fmt.Printf("kvmd:      v%s\n", st.KvmdVersion)
	fmt.Printf("uptime:    %s\n", api.FormatUptime(st))
	fmt.Printf("cpu:       %d%%   mem: %.0f%%   temp: %.1f°C\n", st.CPUPercent, st.MemPercent, st.CPUTempC)
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
		if err := api.SetSwitchPort(port); err != nil {
			die(jsonMode, "switch set", err)
		}
		sw := api.FetchSwitchState()
		ext := port/sw.PortsPerExt + 1
		pno := port%sw.PortsPerExt + 1
		result := map[string]interface{}{"port": port, "id": fmt.Sprintf("%d.%d", ext, pno)}
		if jsonMode {
			emit(true, "switch_set", result, nil)
		} else {
			fmt.Printf("\uf00c switched to port %d.%d (linear %d)\n", ext, pno, port)
		}
		return
	}
	sw := api.FetchSwitchState()
	if jsonMode {
		emit(true, "switch", sw, nil)
		return
	}
	fmt.Printf("extenders:    %d × %d ports = %d total\n", sw.Extenders, sw.PortsPerExt, sw.TotalPorts)
	fmt.Printf("active port:  %d  (%d.%d)\n", sw.ActivePort, sw.ActivePort/sw.PortsPerExt+1, sw.ActivePort%sw.PortsPerExt+1)
	for i := 0; i < sw.TotalPorts; i++ {
		ext := i/sw.PortsPerExt + 1
		pno := i%sw.PortsPerExt + 1
		flags := ""
		if i < len(sw.PowerLeds) && sw.PowerLeds[i] {
			flags += " on"
		}
		if i < len(sw.VideoLinks) && sw.VideoLinks[i] {
			flags += " video"
		}
		if i < len(sw.UsbLinks) && sw.UsbLinks[i] {
			flags += " usb"
		}
		marker := "  "
		if i == sw.ActivePort {
			marker = "* "
		}
		fmt.Printf("%s%d.%d  port %d %s\n", marker, ext, pno, i, flags)
	}
}

func cliIso(jsonMode bool, args []string) {
	if len(args) > 0 && args[0] != "list" {
		die(jsonMode, "iso", fmt.Errorf("unknown iso subcommand %q (only 'list' supported here; use the TUI or web for upload)", args[0]))
	}
	entries, err := api.FetchAvailableISOEntries()
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
// Actions
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

func runActionByName(jsonMode bool, command, name string, args []string) {
	port := 0
	if len(args) > 0 {
		p, err := parsePort(args[0])
		if err != nil {
			die(jsonMode, command, err)
		}
		port = p
	}
	for _, a := range api.DefaultActions {
		if a.Name == name {
			result := api.ExecuteAction(a, port)
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

func stripIcons(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0xE000 && r <= 0xF8FF {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// ----------------------------------------------------------------------------
// Profiles
// ----------------------------------------------------------------------------

func cliProfile(jsonMode bool, args []string) {
	if len(args) == 0 {
		die(jsonMode, "profile", fmt.Errorf("usage: pikvm profile list | get <port> | set <port> k=v [k=v ...] | unset <port>"))
	}
	switch args[0] {
	case "list":
		cliProfileList(jsonMode)
	case "get":
		if len(args) < 2 {
			die(jsonMode, "profile_get", fmt.Errorf("usage: pikvm profile get <port>"))
		}
		cliProfileGet(jsonMode, args[1])
	case "set":
		if len(args) < 3 {
			die(jsonMode, "profile_set", fmt.Errorf("usage: pikvm profile set <port> key=value [key=value ...]\n  keys: name, bios_key, default_iso, tags (comma-sep), tailscale_name, ssh_user, notes"))
		}
		cliProfileSet(jsonMode, args[1], args[2:])
	case "unset":
		if len(args) < 2 {
			die(jsonMode, "profile_unset", fmt.Errorf("usage: pikvm profile unset <port>"))
		}
		cliProfileUnset(jsonMode, args[1])
	default:
		die(jsonMode, "profile", fmt.Errorf("unknown profile subcommand %q", args[0]))
	}
}

func cliProfileList(jsonMode bool) {
	st := state.Load()
	if jsonMode {
		emit(true, "profile_list", st, nil)
		return
	}
	if len(st.Ports) == 0 {
		fmt.Println("(no port profiles saved — try: pikvm profile set 2.3 name=myhost bios_key=F7)")
		return
	}
	for _, id := range state.SortedPortIDs(st) {
		p := st.Ports[id]
		bits := []string{}
		if p.Name != "" {
			bits = append(bits, "name="+p.Name)
		}
		if p.BIOSKey != "" {
			bits = append(bits, "bios_key="+p.BIOSKey)
		}
		if p.DefaultISO != "" {
			bits = append(bits, "default_iso="+p.DefaultISO)
		}
		if len(p.Tags) > 0 {
			bits = append(bits, "tags="+strings.Join(p.Tags, ","))
		}
		if p.TailscaleName != "" {
			bits = append(bits, "tailscale_name="+p.TailscaleName)
		}
		if p.SSHUser != "" {
			bits = append(bits, "ssh_user="+p.SSHUser)
		}
		if p.Notes != "" {
			bits = append(bits, "notes="+p.Notes)
		}
		fmt.Printf("  %-5s  %s\n", id, strings.Join(bits, "  "))
	}
}

func cliProfileGet(jsonMode bool, portArg string) {
	id, err := canonicalPortID(portArg)
	if err != nil {
		die(jsonMode, "profile_get", err)
	}
	st := state.Load()
	p := state.GetProfile(st, id)
	if jsonMode {
		emit(true, "profile_get", map[string]interface{}{"id": id, "profile": p}, nil)
		return
	}
	if p.IsEmpty() {
		fmt.Printf("(no profile for %s)\n", id)
		return
	}
	fmt.Printf("  id:             %s\n", id)
	if p.Name != "" {
		fmt.Printf("  name:           %s\n", p.Name)
	}
	if p.BIOSKey != "" {
		fmt.Printf("  bios_key:       %s\n", p.BIOSKey)
	}
	if p.DefaultISO != "" {
		fmt.Printf("  default_iso:    %s\n", p.DefaultISO)
	}
	if len(p.Tags) > 0 {
		fmt.Printf("  tags:           %s\n", strings.Join(p.Tags, ", "))
	}
	if p.TailscaleName != "" {
		fmt.Printf("  tailscale_name: %s\n", p.TailscaleName)
	}
	if p.SSHUser != "" {
		fmt.Printf("  ssh_user:       %s\n", p.SSHUser)
	}
	if p.Notes != "" {
		fmt.Printf("  notes:          %s\n", p.Notes)
	}
}

func cliProfileSet(jsonMode bool, portArg string, kvs []string) {
	id, err := canonicalPortID(portArg)
	if err != nil {
		die(jsonMode, "profile_set", err)
	}
	st := state.Load()
	p := state.GetProfile(st, id)

	for _, kv := range kvs {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			die(jsonMode, "profile_set", fmt.Errorf("expected key=value, got %q", kv))
		}
		k, v := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		switch k {
		case "name":
			p.Name = v
		case "bios_key":
			p.BIOSKey = v
		case "default_iso":
			p.DefaultISO = v
		case "tags":
			if v == "" {
				p.Tags = nil
			} else {
				p.Tags = strings.Split(v, ",")
				for i := range p.Tags {
					p.Tags[i] = strings.TrimSpace(p.Tags[i])
				}
			}
		case "tailscale_name":
			p.TailscaleName = v
		case "ssh_user":
			p.SSHUser = v
		case "notes":
			p.Notes = v
		default:
			die(jsonMode, "profile_set", fmt.Errorf("unknown key %q (valid: name, bios_key, default_iso, tags, tailscale_name, ssh_user, notes)", k))
		}
	}
	st = state.SetProfile(st, id, p)
	if err := state.Save(st); err != nil {
		die(jsonMode, "profile_set", err)
	}
	if jsonMode {
		emit(true, "profile_set", map[string]interface{}{"id": id, "profile": p}, nil)
	} else {
		fmt.Printf("\uf00c saved profile for %s\n", id)
	}
}

func cliProfileUnset(jsonMode bool, portArg string) {
	id, err := canonicalPortID(portArg)
	if err != nil {
		die(jsonMode, "profile_unset", err)
	}
	st := state.Load()
	delete(st.Ports, id)
	if err := state.Save(st); err != nil {
		die(jsonMode, "profile_unset", err)
	}
	if jsonMode {
		emit(true, "profile_unset", map[string]interface{}{"id": id}, nil)
	} else {
		fmt.Printf("\uf00c removed profile for %s\n", id)
	}
}

// ----------------------------------------------------------------------------
// fzf pickers
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

func fzfPick(prompt string, lines []string) (string, error) {
	if len(lines) == 0 {
		return "", fmt.Errorf("nothing to pick from")
	}
	cmd := exec.Command("fzf", "--prompt="+prompt+"> ", "--height=40%", "--reverse", "--no-mouse")
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("cancelled or fzf failed: %v", err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func cliPickPort(jsonMode bool) {
	sw := api.FetchSwitchState()
	if sw.TotalPorts == 0 {
		die(jsonMode, "pick_port", fmt.Errorf("no ports detected"))
	}
	lines := make([]string, 0, sw.TotalPorts)
	for i := 0; i < sw.TotalPorts; i++ {
		ext := i/sw.PortsPerExt + 1
		pno := i%sw.PortsPerExt + 1
		flags := []string{}
		if i < len(sw.PowerLeds) && sw.PowerLeds[i] {
			flags = append(flags, "on")
		}
		if i < len(sw.VideoLinks) && sw.VideoLinks[i] {
			flags = append(flags, "video")
		}
		if i == sw.ActivePort {
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
	if err := api.SetSwitchPort(port); err != nil {
		die(jsonMode, "pick_port", err)
	}
	emit(jsonMode, "pick_port", map[string]interface{}{"port": port, "id": parts[0]}, nil)
	if !jsonMode {
		fmt.Printf("\uf00c switched to %s\n", parts[0])
	}
}

func cliPickISO(jsonMode bool) {
	entries, err := api.FetchAvailableISOEntries()
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
	var chosen *api.IsoEntry
	for i := range entries {
		if entries[i].Display == picked {
			chosen = &entries[i]
			break
		}
	}
	if chosen == nil {
		die(jsonMode, "pick_iso", fmt.Errorf("could not match selection: %q", picked))
	}
	sw := api.FetchSwitchState()
	port := sw.ActivePort

	// Local ISO → do the upload in-process first (idea #17). This is a
	// blocking CLI call, so we just wait for it. The TUI uses tea.Cmd for
	// async progress; the CLI prints "uploading..." then blocks.
	if chosen.LocalPath != "" {
		if !jsonMode {
			fmt.Printf("\uf0c1 Uploading %s → PiKVM (this may take a while on Tailscale)...\n", chosen.Name)
		}
		if err := api.UploadISO(chosen.LocalPath, chosen.Name, nil); err != nil {
			die(jsonMode, "pick_iso", fmt.Errorf("upload: %w", err))
		}
		if !jsonMode {
			fmt.Printf("\uf00c Upload complete.\n")
		}
	}

	out := scripts.BootFromSpecificISO(port, chosen.Name)
	if jsonMode {
		emit(true, "pick_iso", map[string]interface{}{"iso": chosen.Name, "port": port, "log": stripIcons(out)}, nil)
	} else {
		fmt.Println(out)
	}
}

func cliPickScript(jsonMode bool) {
	if len(scripts.Default) == 0 {
		die(jsonMode, "pick_script", fmt.Errorf("no scripts registered"))
	}
	lines := make([]string, len(scripts.Default))
	for i, s := range scripts.Default {
		lines[i] = fmt.Sprintf("%s — %s", s.Name, s.Desc)
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
	s := scripts.Default[chosenIdx]
	switch s.Name {
	case "Boot from ISO":
		die(jsonMode, "pick_script", fmt.Errorf("'Boot from ISO' needs a sub-pick; use 'pikvm pick iso' instead"))
	case "Boot to BIOS":
		die(jsonMode, "pick_script", fmt.Errorf("'Boot to BIOS' needs a key choice; run from the TUI"))
	}
	sw := api.FetchSwitchState()
	out := s.Run(sw.ActivePort)
	if jsonMode {
		emit(true, "pick_script", map[string]interface{}{"script": s.Name, "port": sw.ActivePort, "log": stripIcons(out)}, nil)
	} else {
		fmt.Println(out)
	}
}

// ----------------------------------------------------------------------------
// Help
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

Power / reset (port = linear int '5', ext.port '2.3', OR profile name 'j4yn0'):
  pikvm power on   [port]            Turn power ON
  pikvm power off  [port]            Turn power OFF
  pikvm power click [port]           Power button short press
  pikvm power long  [port]           Power button long press (force shutdown)
  pikvm reset click [port]           Reset button short press
  pikvm reset long  [port]           Reset button long press

Per-port profiles (~/.config/pikvm/state.json):
  pikvm profile list                 Show all saved profiles
  pikvm profile get <port>           Show profile for one port
  pikvm profile set <port> k=v ...   Set fields (name, bios_key, default_iso,
                                                 tags, tailscale_name, ssh_user, notes)
  pikvm profile unset <port>         Remove a profile entirely

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
  pikvm profile set 2.3 name=j4yn0 bios_key=F7
  pikvm power on j4yn0               # name-based (from profile)
  pikvm pick iso                     # fzf-pick an ISO and boot it
  pikvm --json switch | jq .         # scriptable JSON

Legacy (back-compat):
  pikvm on|off|click|long|reset|reset-long [port]   same as 'pikvm power ...'
`
	fmt.Println(strings.TrimSpace(help))
}
