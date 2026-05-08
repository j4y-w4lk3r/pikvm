// Package cli — multi-host federation commands (roadmap idea #25).
//
// Subcommands:
//
//	pikvm hosts                    pikvm hosts list (alias)
//	pikvm hosts list               every configured host + which is default + which is active
//	pikvm hosts show [name]        connection details for one host (or active host if omitted)
//	pikvm hosts use <name>         set this host as the new default (rewrites config.json)
//
// Read paths only require config.json present; mutating paths (`use`)
// rewrite the file in place preserving formatting and other top-level
// fields. Adding/removing hosts isn't wired here yet — edit config.json
// by hand for now (or run `make config-edit` if you wire that target).
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	case "use":
		if len(args) < 2 {
			die(jsonMode, "hosts use", fmt.Errorf("usage: pikvm hosts use <name>"))
		}
		cliHostsUse(jsonMode, args[1])
	default:
		die(jsonMode, "hosts", fmt.Errorf("unknown subcommand %q (try: list / show / use)", sub))
	}
}

// cliHostsList prints every configured host in a table marking the
// default and the currently-active one. Active is usually == default but
// can differ when --host or PIKVM_HOST_NAME is in play.
func cliHostsList(jsonMode bool) {
	if jsonMode {
		rows := []map[string]interface{}{}
		for _, name := range config.HostNames() {
			h := config.Hosts[name]
			rows = append(rows, map[string]interface{}{
				"name":    name,
				"host":    h.Host,
				"user":    h.User,
				"default": name == config.DefaultHost,
				"active":  name == config.HostName,
			})
		}
		out, _ := json.Marshal(response{
			OK: true, Command: "hosts list",
			Result: map[string]interface{}{
				"hosts":          rows,
				"default":        config.DefaultHost,
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
	fmt.Printf("%-12s %-22s %-12s %s\n", "NAME", "HOST", "USER", "FLAGS")
	for _, name := range config.HostNames() {
		h := config.Hosts[name]
		flags := []string{}
		if name == config.HostName {
			flags = append(flags, "active")
		}
		if name == config.DefaultHost {
			flags = append(flags, "default")
		}
		fmt.Printf("%-12s %-22s %-12s %s\n", name, h.Host, h.User, strings.Join(flags, ","))
	}
	if config.SchemaVersion == 1 {
		fmt.Println()
		fmt.Println("(schema v1 — single-host legacy. To add more hosts, migrate config.json to v2:")
		fmt.Println(" {\"schema_version\":2,\"default\":\"...\",\"hosts\":{\"name\":{\"host\":\"...\",\"user\":\"...\",\"pass\":\"...\"}}})")
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
				"name":    name,
				"host":    h.Host,
				"user":    h.User,
				"default": name == config.DefaultHost,
				"active":  name == config.HostName,
				// pass deliberately omitted — JSON consumers should never
				// log secrets accidentally.
			},
		})
		fmt.Println(string(out))
		return
	}
	fmt.Printf("name:    %s\n", name)
	fmt.Printf("host:    %s\n", h.Host)
	fmt.Printf("user:    %s\n", h.User)
	fmt.Printf("default: %v\n", name == config.DefaultHost)
	fmt.Printf("active:  %v\n", name == config.HostName)
}

// cliHostsUse rewrites config.json's `default` field to point at the
// chosen host. Only works on schema v2 — v1 has no concept of multiple
// hosts to default between.
func cliHostsUse(jsonMode bool, name string) {
	if _, ok := config.Hosts[name]; !ok {
		die(jsonMode, "hosts use", fmt.Errorf("no host named %q (configured: %s)",
			name, strings.Join(config.HostNames(), ", ")))
	}
	if config.SchemaVersion < 2 {
		die(jsonMode, "hosts use", fmt.Errorf(
			"schema v1 single-host config has nothing to switch — migrate to v2 to use multiple hosts"))
	}

	path := findConfigPath()
	if path == "" {
		die(jsonMode, "hosts use", fmt.Errorf(
			"can't locate config.json to update (try editing it by hand and setting \"default\":\"%s\")", name))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		die(jsonMode, "hosts use", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		die(jsonMode, "hosts use", fmt.Errorf("parse %s: %w", path, err))
	}
	raw["default"] = name
	if _, ok := raw["schema_version"]; !ok {
		raw["schema_version"] = 2
	}
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		die(jsonMode, "hosts use", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		die(jsonMode, "hosts use", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		die(jsonMode, "hosts use", err)
	}

	// Reflect the change in this process's view of the world too — that
	// way `pikvm hosts use foo && pikvm switch` does what you'd expect.
	if err := config.UseHost(name); err != nil {
		die(jsonMode, "hosts use", err)
	}
	config.DefaultHost = name

	emit(jsonMode, "hosts use", map[string]interface{}{
		"default": name, "active": name, "config_file": path,
	}, nil)
	if !jsonMode {
		fmt.Printf("\uf00c default host now %s (saved to %s)\n", name, path)
	}
}

// findConfigPath returns the absolute path to whichever config.json
// loadJSON() picked up (we re-run the same XDG → home → binary-dir →
// cwd search to keep a single source of truth).
func findConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		if p := filepath.Join(xdg, "pikvm", "config.json"); fileExists(p) {
			return p
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if p := filepath.Join(home, ".config", "pikvm", "config.json"); fileExists(p) {
			return p
		}
	}
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), "config.json"); fileExists(p) {
			return p
		}
	}
	if fileExists("config.json") {
		abs, _ := filepath.Abs("config.json")
		return abs
	}
	return ""
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
