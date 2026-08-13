// Package cli — multi-host federation commands (roadmap idea #25).
//
// Subcommands:
//
//	pikvm hosts                    pikvm hosts list (alias)
//	pikvm hosts list               every configured host (pikvm1, pikvm2, …)
//	pikvm hosts show [name]        connection details for one host (or active if omitted)
//	pikvm hosts sync               refresh host IPs from tailscale / headscale now
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"pikvm/internal/config"
)

func cliHosts(jsonMode bool, args []string) {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "list", "ls", "l":
		cliHostsList(jsonMode)
	case "show":
		name := ""
		if len(args) >= 2 {
			name = args[1]
		}
		cliHostsShow(jsonMode, name)
	case "sync":
		cliHostsSync(jsonMode)
	case "log", "logs":
		n := 20
		if len(args) >= 2 {
			fmt.Sscanf(args[1], "%d", &n)
		}
		cliHostsLog(jsonMode, n)
	case "use":
		die(jsonMode, "hosts use", fmt.Errorf(
			"'hosts use' was removed — launch starts on pikvm1; switch with pikvm --host <name> or h+number in the TUI"))
	default:
		die(jsonMode, "hosts", fmt.Errorf("unknown subcommand %q (try: list / show / sync)", sub))
	}
}

func cliHostsList(jsonMode bool) {
	if jsonMode {
		rows := []map[string]interface{}{}
		for _, name := range config.HostNames() {
			h := config.Hosts[name]
			rows = append(rows, map[string]interface{}{
				"name":           name,
				"host":           h.Host,
				"user":           h.User,
				"tailscale_name": h.TailscaleName,
				"active":         name == config.HostName,
			})
		}
		out, _ := json.Marshal(response{
			OK: true, Command: "hosts list",
			Result: map[string]interface{}{
				"hosts":          rows,
				"active":         config.HostName,
				"schema_version": config.SchemaVersion,
			},
		})
		fmt.Println(string(out))
		return
	}
	if len(config.Hosts) == 0 {
		fmt.Println("(no hosts configured)")
		return
	}
	fmt.Printf("%-12s %-22s %-12s %-16s %s\n", "NAME", "HOST", "USER", "TAILSCALE", "FLAGS")
	for _, name := range config.HostNames() {
		h := config.Hosts[name]
		flags := []string{}
		if name == config.HostName {
			flags = append(flags, "active")
		}
		ts := h.TailscaleName
		if ts == "" {
			ts = "-"
		}
		fmt.Printf("%-12s %-22s %-12s %-16s %s\n", name, h.Host, h.User, ts, strings.Join(flags, ","))
	}
	if config.SchemaVersion == 1 {
		fmt.Println()
		fmt.Println("(legacy single-host — add pikvm2 in config.json to use multiple PiKVMs:")
		fmt.Println(` {"schema_version":2,"hosts":{"pikvm1":{...},"pikvm2":{...}}})`)
	}
}

func cliHostsShow(jsonMode bool, name string) {
	if name == "" {
		name = config.HostName
	}
	h, ok := config.Hosts[name]
	if !ok {
		die(jsonMode, "hosts show", fmt.Errorf("no host named %q (configured: %s)",
			name, strings.Join(config.HostNames(), ", ")))
	}
	if jsonMode {
		out, _ := json.Marshal(response{
			OK: true, Command: "hosts show",
			Result: map[string]interface{}{
				"name":           name,
				"host":           h.Host,
				"user":           h.User,
				"tailscale_name": h.TailscaleName,
				"active":         name == config.HostName,
			},
		})
		fmt.Println(string(out))
		return
	}
	fmt.Printf("name:    %s\n", name)
	fmt.Printf("host:    %s\n", h.Host)
	if h.TailscaleName != "" {
		fmt.Printf("tailscale_name: %s\n", h.TailscaleName)
	}
	fmt.Printf("user:    %s\n", h.User)
	fmt.Printf("active:  %v\n", name == config.HostName)
}

func cliHostsSync(jsonMode bool) {
	active := config.HostName
	if err := config.RefreshFromOnePassword(); err != nil {
		die(jsonMode, "hosts sync", err)
	}
	if active != "" {
		if err := config.UseHost(active); err != nil {
			die(jsonMode, "hosts sync", err)
		}
	}
	result := map[string]interface{}{
		"hosts":   len(config.Hosts),
		"entries": config.LastDiscoverSummary.Entries,
		"active":  config.HostName,
		"log":     config.DiscoverLogPath(),
	}
	emit(jsonMode, "hosts sync", result, nil)
	if jsonMode {
		return
	}
	for _, e := range config.LastDiscoverSummary.Entries {
		fmt.Printf("  %-8s  %s@%s  tailscale=%s  online=%v  %s\n",
			e.Name, e.User, e.IP, e.TailscaleName, e.Online, e.Status)
	}
	fmt.Printf("\n\uf00c %d host(s) synced → %s\n", len(config.Hosts), config.DiscoverLogPath())
}

func cliHostsLog(jsonMode bool, lines int) {
	path := config.DiscoverLogPath()
	data, err := os.ReadFile(path)
	if err != nil {
		die(jsonMode, "hosts log", fmt.Errorf("read %s: %w", path, err))
	}
	all := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if lines > 0 && len(all) > lines {
		all = all[len(all)-lines:]
	}
	if jsonMode {
		emit(jsonMode, "hosts log", map[string]interface{}{
			"path":  path,
			"lines": all,
		}, nil)
		return
	}
	fmt.Printf("# %s (last %d lines)\n\n", path, lines)
	for _, line := range all {
		fmt.Println(line)
	}
}
