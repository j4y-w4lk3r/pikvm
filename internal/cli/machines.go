// Package cli — high-level "ports as machines" commands (roadmap idea #11).
//
// Where the rest of cli.go is per-port primitives (switch / power / iso /
// reset), this file is the *machine-oriented* surface: think in names like
// "j4yn0" or "vault" instead of port numbers, and run multi-step boot or
// SSH workflows in a single command.
//
//	pikvm boot     <port-or-name> [iso]   — switch + mount ISO + spam BIOS key + power on
//	pikvm prepare  <port-or-name>         — switch + power-cycle + spam BIOS key (no ISO)
//	pikvm ssh      <port-or-name> [args]  — exec ssh against profile's tailscale_name + ssh_user
//	pikvm machines                        — friendly list of every saved profile
//
// All four resolve their first argument through canonicalPortID(), so they
// accept linear ints ("5"), ext.port ("2.3"), and profile names ("j4yn0").
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"pikvm/internal/api"
	"pikvm/internal/config"
	"pikvm/internal/scripts"
	"pikvm/internal/state"
)

// ----------------------------------------------------------------------------
// boot
// ----------------------------------------------------------------------------

// cliBoot runs the full ISO boot dance: switch → select ISO → mount drive →
// power on → spam BIOS key. The BIOS key comes from (in priority order):
//
//  1. --bios-key <key>             (one-off override)
//  2. profile.BIOSKey               (per-port default from state.json)
//  3. "F7"                          (fallback)
//
// The ISO comes from arg[1] if given, otherwise profile.DefaultISO.
func cliBoot(jsonMode bool, args []string) {
	if len(args) == 0 {
		die(jsonMode, "boot", fmt.Errorf(
			"usage: pikvm boot <port-or-name> [iso] [--bios-key <key>]"))
	}

	// Pull --bios-key out of args (rest are positional).
	biosOverride := ""
	positional := []string{}
	for i := 0; i < len(args); i++ {
		if args[i] == "--bios-key" || args[i] == "--bios" {
			if i+1 >= len(args) {
				die(jsonMode, "boot", fmt.Errorf("--bios-key needs a value"))
			}
			biosOverride = args[i+1]
			i++
			continue
		}
		positional = append(positional, args[i])
	}
	if len(positional) == 0 {
		die(jsonMode, "boot", fmt.Errorf("usage: pikvm boot <port-or-name> [iso]"))
	}

	portArg := positional[0]
	port, err := parsePort(portArg)
	if err != nil {
		die(jsonMode, "boot", err)
	}
	extID, err := canonicalPortID(portArg)
	if err != nil {
		die(jsonMode, "boot", err)
	}

	st := state.Load()
	profile := state.GetProfile(st, extID)

	iso := ""
	if len(positional) >= 2 {
		iso = positional[1]
	} else if profile.DefaultISO != "" {
		iso = profile.DefaultISO
	}
	if iso == "" {
		hint := portArg
		if profile.Name != "" {
			hint = profile.Name
		}
		die(jsonMode, "boot", fmt.Errorf(
			"no ISO specified and profile has no default_iso (try: pikvm boot %s ubuntu-25.10, or: pikvm profile set %s default_iso=ubuntu-25.10.iso)",
			hint, hint))
	}

	biosKey := biosOverride
	if biosKey == "" {
		biosKey = profile.BIOSKey
	}
	if biosKey == "" {
		biosKey = "F7"
	}

	entry, err := resolveISOName(iso)
	if err != nil {
		die(jsonMode, "boot", err)
	}
	// LocalPath set = the ISO is on this Mac, not yet uploaded to PiKVM.
	// CLI doesn't have the TUI's progress-bar UI for uploads, so we punt
	// to the user instead of blocking for ~minutes silently.
	if entry.LocalPath != "" {
		die(jsonMode, "boot", fmt.Errorf(
			"ISO %q is only on local disk — upload it via the TUI (Boot from ISO → pick local file → progress bar) or pre-upload then re-run",
			entry.Name))
	}

	if err := api.SetSwitchPort(port); err != nil {
		die(jsonMode, "boot", fmt.Errorf("set port: %w", err))
	}

	msg := scripts.BootFromSpecificISOWithKey(port, entry.Name, biosKey)

	emit(jsonMode, "boot", map[string]interface{}{
		"port":     extID,
		"port_id":  port,
		"iso":      entry.Name,
		"bios_key": biosKey,
		"machine":  profile.Name,
	}, nil)
	if !jsonMode {
		fmt.Println(msg)
	}
}

// ----------------------------------------------------------------------------
// prepare
// ----------------------------------------------------------------------------

// cliPrepare is "boot to BIOS" with profile-aware key selection — switch +
// power cycle + spam BIOS key. No ISO, no MSD activity. Useful when the
// machine just needs a manual bios menu visit.
func cliPrepare(jsonMode bool, args []string) {
	if len(args) == 0 {
		die(jsonMode, "prepare", fmt.Errorf("usage: pikvm prepare <port-or-name> [--bios-key <key>]"))
	}
	biosOverride := ""
	positional := []string{}
	for i := 0; i < len(args); i++ {
		if args[i] == "--bios-key" || args[i] == "--bios" {
			if i+1 >= len(args) {
				die(jsonMode, "prepare", fmt.Errorf("--bios-key needs a value"))
			}
			biosOverride = args[i+1]
			i++
			continue
		}
		positional = append(positional, args[i])
	}
	if len(positional) == 0 {
		die(jsonMode, "prepare", fmt.Errorf("usage: pikvm prepare <port-or-name>"))
	}

	portArg := positional[0]
	port, err := parsePort(portArg)
	if err != nil {
		die(jsonMode, "prepare", err)
	}
	extID, err := canonicalPortID(portArg)
	if err != nil {
		die(jsonMode, "prepare", err)
	}

	st := state.Load()
	profile := state.GetProfile(st, extID)

	biosKey := biosOverride
	if biosKey == "" {
		biosKey = profile.BIOSKey
	}
	if biosKey == "" {
		biosKey = "F7"
	}
	label := lookupBIOSLabel(biosKey)

	if err := api.SetSwitchPort(port); err != nil {
		die(jsonMode, "prepare", err)
	}
	msg := scripts.BootToBIOSWithKey(port, scripts.BIOSKeyOption{Label: label, Key: biosKey})

	emit(jsonMode, "prepare", map[string]interface{}{
		"port":     extID,
		"port_id":  port,
		"bios_key": biosKey,
		"machine":  profile.Name,
	}, nil)
	if !jsonMode {
		fmt.Println(msg)
	}
}

// ----------------------------------------------------------------------------
// ssh
// ----------------------------------------------------------------------------

// cliSSH opens an SSH session to the machine on a given port, using
// profile.TailscaleName as the host and profile.SSHUser as the login name
// (both optional but at least the host needs to resolve). Replaces this
// process with ssh on success so the user gets a real interactive session.
//
// Supports the cross-host shorthand `pikvm ssh garage:vault` (roadmap
// idea #25): `garage:` prefix activates the named PiKVM host *for this
// command only* before resolving "vault" against that host's profiles.
// Equivalent to `pikvm --host garage ssh vault`.
func cliSSH(jsonMode bool, args []string) {
	if len(args) == 0 {
		die(jsonMode, "ssh", fmt.Errorf("usage: pikvm ssh <[host:]port-or-name> [extra ssh args]"))
	}
	portArg := args[0]
	extra := args[1:]

	// Cross-host shorthand: "garage:vault" → switch to host "garage"
	// then resolve "vault". A bare ":" prefix would be ambiguous so we
	// only treat the first colon as a separator when the chunk before it
	// is a configured host name (otherwise we leave portArg alone — IPv6
	// SSH targets like "[::1]" stay intact for the rare power user).
	if idx := strings.Index(portArg, ":"); idx > 0 {
		hostPart, rest := portArg[:idx], portArg[idx+1:]
		if _, ok := config.Hosts[hostPart]; ok && rest != "" {
			if err := config.UseHost(hostPart); err != nil {
				die(jsonMode, "ssh", err)
			}
			portArg = rest
		}
	}

	extID, err := canonicalPortID(portArg)
	if err != nil {
		die(jsonMode, "ssh", err)
	}
	st := state.Load()
	profile := state.GetProfile(st, extID)

	target := profile.TailscaleName
	if target == "" {
		target = profile.Name // fall back to the profile name (often == DNS name)
	}
	if target == "" {
		die(jsonMode, "ssh", fmt.Errorf(
			"no tailscale_name on profile %s — set one with: pikvm profile set %s tailscale_name=<name>",
			extID, portArg))
	}

	sshArgs := []string{}
	if profile.SSHUser != "" {
		sshArgs = append(sshArgs, "-l", profile.SSHUser)
	}
	sshArgs = append(sshArgs, target)
	sshArgs = append(sshArgs, extra...)

	if jsonMode {
		out, _ := json.Marshal(response{
			OK:      true,
			Command: "ssh",
			Result: map[string]interface{}{
				"command":        "ssh",
				"args":           sshArgs,
				"target":         target,
				"ssh_user":       profile.SSHUser,
				"machine":        profile.Name,
				"port":           extID,
				"would_exec":     true,
				"hint":           "json mode does not exec ssh; run again without --json to drop into the session",
			},
		})
		fmt.Println(string(out))
		return
	}

	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		die(false, "ssh", fmt.Errorf("ssh not in PATH: %w", err))
	}
	// syscall.Exec replaces the current process with ssh, preserving stdio
	// + signals. From the user's POV they typed `pikvm ssh j4yn0` and got
	// dropped straight into a remote shell.
	if err := syscall.Exec(sshPath, append([]string{"ssh"}, sshArgs...), os.Environ()); err != nil {
		die(false, "ssh", err)
	}
}

// ----------------------------------------------------------------------------
// machines
// ----------------------------------------------------------------------------

// cliMachines is `pikvm profile list` with a friendlier columnar output.
// Always lists every profile in `state.json` regardless of whether the port
// is currently part of the live PiKVM topology.
func cliMachines(jsonMode bool) {
	st := state.Load()

	if jsonMode {
		rows := make([]map[string]interface{}, 0, len(st.Ports))
		for _, id := range state.SortedPortIDs(st) {
			p := st.Ports[id]
			rows = append(rows, map[string]interface{}{
				"port":           id,
				"name":           p.Name,
				"bios_key":       p.BIOSKey,
				"default_iso":    p.DefaultISO,
				"tailscale_name": p.TailscaleName,
				"ssh_user":       p.SSHUser,
				"tags":           p.Tags,
				"notes":          p.Notes,
			})
		}
		out, _ := json.Marshal(response{OK: true, Command: "machines", Result: rows})
		fmt.Println(string(out))
		return
	}

	if len(st.Ports) == 0 {
		fmt.Println("(no machines / profiles yet)")
		fmt.Println("Add one with: pikvm profile set <port> name=<machine> bios_key=<F7|Delete|...> tailscale_name=<dns> ssh_user=<user>")
		return
	}

	fmt.Printf("%-12s %-6s %-10s %-22s %-22s %-10s %s\n",
		"NAME", "PORT", "BIOS_KEY", "DEFAULT_ISO", "TAILSCALE_NAME", "SSH_USER", "TAGS")
	for _, id := range state.SortedPortIDs(st) {
		p := st.Ports[id]
		fmt.Printf("%-12s %-6s %-10s %-22s %-22s %-10s %s\n",
			truncate(p.Name, 12),
			id,
			truncate(p.BIOSKey, 10),
			truncate(p.DefaultISO, 22),
			truncate(p.TailscaleName, 22),
			truncate(p.SSHUser, 10),
			strings.Join(p.Tags, ","),
		)
	}
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

// resolveISOName matches user input ("ubuntu-25.10") against the available
// ISO list. Tries exact match first, then case-insensitive prefix, then
// substring. All errors include the available list so the user can fix typos.
func resolveISOName(needle string) (api.IsoEntry, error) {
	entries, err := api.FetchAvailableISOEntries()
	if err != nil {
		return api.IsoEntry{}, fmt.Errorf("fetch ISOs: %w", err)
	}
	if len(entries) == 0 {
		return api.IsoEntry{}, fmt.Errorf("no ISOs available on PiKVM or in ./iso/")
	}
	for _, e := range entries {
		if e.Name == needle {
			return e, nil
		}
	}
	lneedle := strings.ToLower(needle)
	for _, e := range entries {
		if strings.HasPrefix(strings.ToLower(e.Name), lneedle) {
			return e, nil
		}
	}
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Name), lneedle) {
			return e, nil
		}
	}
	available := []string{}
	for _, e := range entries {
		available = append(available, e.Name)
	}
	return api.IsoEntry{}, fmt.Errorf("no ISO matches %q — available: %s",
		needle, strings.Join(available, ", "))
}

// lookupBIOSLabel returns the short UI label ("F7", "Del") for a key code
// ("F7", "Delete"). Falls back to the code itself for unknown keys.
func lookupBIOSLabel(key string) string {
	for _, opt := range scripts.BIOSKeyOptions {
		if opt.Key == key {
			return opt.Label
		}
	}
	return key
}

func truncate(s string, n int) string {
	if n < 1 || len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
