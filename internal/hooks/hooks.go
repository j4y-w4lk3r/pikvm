// Package hooks implements roadmap idea #20 — user-defined event hooks.
//
// Drop any executable file (or shell script with the executable bit set)
// into ~/.config/pikvm/hooks.d/ and the binary will run it whenever a
// matching PiKVM event fires. Filename → event matching:
//
//	port-changed.sh                    runs only on port-changed events
//	power-on.py                        runs only on power-on events
//	port-changed.d/notify-slack        any executable inside <event>.d/
//	                                   also runs for that event (cron-style
//	                                   directory of hooks for one event)
//	_all.sh                            runs on every event
//	_all.d/audit-log                   any exec under _all.d/ runs always
//
// Hooks always run **out-of-process** with a 30-second timeout so a stuck
// hook can't lock up the TUI. Multiple hooks for the same event run in
// parallel. Stdout / stderr is appended to ~/.config/pikvm/hooks.log so
// you can debug without disturbing the TUI.
//
// Each hook receives:
//
//	$PIKVM_EVENT          event name (e.g. "port-changed")
//	$PIKVM_TIMESTAMP      RFC3339 UTC time of the event
//	$PIKVM_HOST_NAME      friendly host name from config (e.g. "lab")
//	$PIKVM_HOST           IP / DNS of the active PiKVM
//	$PIKVM_USER           connection username
//	... event-specific keys, all uppercased and prefixed with PIKVM_
//
// Plus the parent process's environment so hooks can assume PATH, HOME,
// SLACK_WEBHOOK, etc. are present.
package hooks

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Default per-hook timeout. A misbehaving hook is SIGKILLed after this.
const DefaultTimeout = 30 * time.Second

// Hook is one discovered hook file (or the file member of a *.d/ dir).
// Used by `pikvm hooks list` and Dispatch() alike.
type Hook struct {
	// Event the hook subscribes to. "_all" means the hook runs on every
	// event we dispatch.
	Event string
	// Path to the executable on disk.
	Path string
	// Source explains where Find() picked this hook up from (the bare
	// file or the per-event subdirectory). Useful for `hooks list`.
	Source string
}

// Dir returns the hooks directory (~/.config/pikvm/hooks.d, respecting
// $XDG_CONFIG_HOME). Caller is responsible for creating it on demand.
func Dir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "pikvm", "hooks.d")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "pikvm", "hooks.d")
	}
	return filepath.Join(".", "pikvm-hooks.d")
}

// LogPath returns the absolute path to the hook output log file.
func LogPath() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "pikvm", "hooks.log")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "pikvm", "hooks.log")
	}
	return filepath.Join(".", "pikvm-hooks.log")
}

// List returns every discovered hook in the hooks dir, sorted by
// (event, path) for stable output.
func List() ([]Hook, error) {
	dir := Dir()
	out := []Hook{}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return out, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if e.IsDir() {
			// <event>.d/<file> — scan the subdir.
			ev := strings.TrimSuffix(e.Name(), ".d")
			subEntries, err := os.ReadDir(full)
			if err != nil {
				continue
			}
			for _, se := range subEntries {
				if se.IsDir() {
					continue
				}
				p := filepath.Join(full, se.Name())
				if !isExecutable(p) {
					continue
				}
				out = append(out, Hook{Event: ev, Path: p, Source: "dir"})
			}
			continue
		}
		// Bare file — strip the extension for the event name.
		base := e.Name()
		ev := base
		if i := strings.LastIndex(base, "."); i > 0 {
			ev = base[:i]
		}
		if !isExecutable(full) {
			continue
		}
		out = append(out, Hook{Event: ev, Path: full, Source: "file"})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Event != out[j].Event {
			return out[i].Event < out[j].Event
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// Find returns the subset of hooks that should fire for `event`. Always
// includes any `_all` hooks plus the event-specific ones.
func Find(event string) ([]Hook, error) {
	all, err := List()
	if err != nil {
		return nil, err
	}
	matched := []Hook{}
	for _, h := range all {
		if h.Event == event || h.Event == "_all" {
			matched = append(matched, h)
		}
	}
	return matched, nil
}

// Dispatch fires every hook that matches `event`, asynchronously. Returns
// immediately — caller does not need to wait for hooks to finish (and
// shouldn't, from a hot path like the TUI's Update()).
//
// kv is event-specific data; each entry becomes PIKVM_<KEY> in the hook
// child env. Common keys (set by integration sites): port, prev_port,
// host_name, host, user, power, iso, drive_connected, clients.
//
// Errors during hook execution don't propagate to the caller — they're
// written to the log file (LogPath()) where they can be inspected later.
func Dispatch(event string, kv map[string]string) {
	hooks, err := Find(event)
	if err != nil {
		writeLog(fmt.Sprintf("[%s] hooks list error: %v", event, err))
		return
	}
	if len(hooks) == 0 {
		return
	}
	for _, h := range hooks {
		go run(event, h, kv)
	}
}

// DispatchSync fires hooks synchronously and waits for them to finish.
// Used by `pikvm hooks test` so users can see output and exit codes.
// Returns the slice of hook paths that were attempted (even if they
// errored / timed out).
func DispatchSync(event string, kv map[string]string) ([]string, error) {
	hooks, err := Find(event)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(hooks))
	var wg sync.WaitGroup
	for _, h := range hooks {
		paths = append(paths, h.Path)
		wg.Add(1)
		go func(h Hook) {
			defer wg.Done()
			run(event, h, kv)
		}(h)
	}
	wg.Wait()
	return paths, nil
}

// run executes one hook with the merged env, captures its stdout+stderr,
// and writes a structured log entry. Killed if it exceeds DefaultTimeout.
func run(event string, h Hook, kv map[string]string) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()

	env := os.Environ()
	env = append(env,
		"PIKVM_EVENT="+event,
		"PIKVM_TIMESTAMP="+time.Now().UTC().Format(time.RFC3339),
	)
	for k, v := range kv {
		env = append(env, "PIKVM_"+strings.ToUpper(k)+"="+v)
	}

	cmd := exec.CommandContext(ctx, h.Path)
	cmd.Env = env

	out, err := cmd.CombinedOutput()

	var status string
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		status = "timeout"
	case err != nil:
		status = fmt.Sprintf("error: %v", err)
	default:
		status = "ok"
	}

	rel := h.Path
	if home, herr := os.UserHomeDir(); herr == nil && strings.HasPrefix(h.Path, home) {
		rel = "~" + strings.TrimPrefix(h.Path, home)
	}
	header := fmt.Sprintf("[%s] %s %s [%s]",
		time.Now().UTC().Format(time.RFC3339), event, rel, status)
	body := strings.TrimRight(string(out), "\n")
	if body == "" {
		writeLog(header)
	} else {
		writeLog(header + "\n" + indent(body, "    "))
	}
}

// writeLog appends one record to ~/.config/pikvm/hooks.log, creating the
// file (and parent dir) on demand. Errors are swallowed — the log is
// best-effort, and we don't want a missing $HOME to crash the TUI.
func writeLog(line string) {
	path := LogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = io.WriteString(f, line+"\n")
}

func isExecutable(p string) bool {
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

func indent(s, prefix string) string {
	var b strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		b.WriteString(prefix)
		b.WriteString(scanner.Text())
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}
