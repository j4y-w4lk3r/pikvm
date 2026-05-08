// Package cli — `pikvm hooks ...` subcommands for managing the event-hook
// system (roadmap idea #20).
//
//	pikvm hooks                        alias for `hooks list`
//	pikvm hooks list                   show every hook on disk + which event it serves
//	pikvm hooks dir                    print the hooks directory path
//	pikvm hooks logs [N]               print the last N (default 50) log lines
//	pikvm hooks test <event> [k=v...]  fire a synthetic event synchronously,
//	                                   so the hook's stdout/stderr land on
//	                                   YOUR terminal in addition to the log
//
// Configuration lives in $XDG_CONFIG_HOME/pikvm/hooks.d/ (or
// ~/.config/pikvm/hooks.d). See the hooks package docstring for filename
// conventions and the env vars passed to each invocation.
package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"pikvm/internal/hooks"
)

func cliHooksCmd(jsonMode bool, args []string) {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "list", "ls", "l":
		cliHooksList(jsonMode)
	case "dir":
		cliHooksDir(jsonMode)
	case "logs", "log":
		n := 50
		if len(args) >= 2 {
			if v, err := strconv.Atoi(args[1]); err == nil && v > 0 {
				n = v
			}
		}
		cliHooksLogs(jsonMode, n)
	case "test":
		if len(args) < 2 {
			die(jsonMode, "hooks test", fmt.Errorf("usage: pikvm hooks test <event> [key=value ...]"))
		}
		cliHooksTest(jsonMode, args[1], args[2:])
	default:
		die(jsonMode, "hooks", fmt.Errorf("unknown subcommand %q (try: list / dir / logs / test)", sub))
	}
}

func cliHooksList(jsonMode bool) {
	all, err := hooks.List()
	if err != nil {
		die(jsonMode, "hooks list", err)
	}
	if jsonMode {
		out, _ := json.Marshal(response{
			OK: true, Command: "hooks list",
			Result: map[string]interface{}{
				"dir":   hooks.Dir(),
				"hooks": all,
			},
		})
		fmt.Println(string(out))
		return
	}
	fmt.Printf("hooks directory: %s\n\n", hooks.Dir())
	if len(all) == 0 {
		fmt.Println("(no hooks configured)")
		fmt.Println()
		fmt.Println("Drop an executable file in the directory above. The filename's stem")
		fmt.Println("is the event name; the extension is ignored. Examples:")
		fmt.Println("  port-changed.sh        runs on every active-port switch")
		fmt.Println("  power-on.py            runs whenever any port turns on")
		fmt.Println("  _all.sh                runs on every event (catch-all)")
		fmt.Println()
		fmt.Println("Available events: host-connected, host-disconnected, port-changed,")
		fmt.Println("                  power-on, power-off, msd-mounted, msd-unmounted,")
		fmt.Println("                  iso-upload-finished, clients-changed")
		fmt.Println()
		fmt.Println("Test one before saving any state changes with: pikvm hooks test <event>")
		return
	}
	fmt.Printf("%-22s %-6s %s\n", "EVENT", "TYPE", "PATH")
	for _, h := range all {
		fmt.Printf("%-22s %-6s %s\n", h.Event, h.Source, h.Path)
	}
}

func cliHooksDir(jsonMode bool) {
	dir := hooks.Dir()
	if jsonMode {
		emit(true, "hooks dir", map[string]string{"dir": dir, "log": hooks.LogPath()}, nil)
		return
	}
	fmt.Println(dir)
}

func cliHooksLogs(jsonMode bool, n int) {
	path := hooks.LogPath()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			if jsonMode {
				emit(true, "hooks logs", []string{}, nil)
				return
			}
			fmt.Printf("(no log yet — %s does not exist)\n", path)
			return
		}
		die(jsonMode, "hooks logs", err)
	}
	defer f.Close()

	// Quick-and-dirty tail: read all lines into a ring buffer of length n.
	buf := make([]string, 0, n)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for scanner.Scan() {
		if len(buf) == n {
			buf = buf[1:]
		}
		buf = append(buf, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		die(jsonMode, "hooks logs", err)
	}
	if jsonMode {
		emit(true, "hooks logs", buf, nil)
		return
	}
	for _, l := range buf {
		fmt.Println(l)
	}
}

func cliHooksTest(jsonMode bool, event string, kvArgs []string) {
	kv := map[string]string{}
	for _, raw := range kvArgs {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 {
			die(jsonMode, "hooks test", fmt.Errorf("expected key=value, got %q", raw))
		}
		kv[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	paths, err := hooks.DispatchSync(event, kv)
	if err != nil {
		die(jsonMode, "hooks test", err)
	}
	if jsonMode {
		emit(true, "hooks test", map[string]interface{}{
			"event": event, "kv": kv, "ran": paths, "log": hooks.LogPath(),
		}, nil)
		return
	}
	if len(paths) == 0 {
		fmt.Printf("(no hooks registered for event %q — see: pikvm hooks list)\n", event)
		return
	}
	fmt.Printf("\uf00c fired %d hook(s) for %q — output captured to %s\n",
		len(paths), event, hooks.LogPath())
	fmt.Println("    last log entries:")
	cliHooksLogs(false, 20)
}
