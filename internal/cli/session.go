package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pikvm/internal/api"
	"pikvm/internal/config"
	"pikvm/internal/state"
)

func cliSession(jsonMode bool, args []string) {
	if len(args) == 0 {
		die(jsonMode, "session", fmt.Errorf("usage: pikvm session start|stop|status|note|worker <text>"))
	}
	switch args[0] {
	case "start":
		cliSessionStart(jsonMode, args[1:])
	case "stop":
		cliSessionStop(jsonMode)
	case "status":
		cliSessionStatus(jsonMode)
	case "note":
		if len(args) < 2 {
			die(jsonMode, "session", fmt.Errorf("usage: pikvm session note <text>"))
		}
		cliSessionNote(jsonMode, strings.Join(args[1:], " "))
	case "worker":
		cliSessionWorker(jsonMode, args[1:])
	default:
		die(jsonMode, "session", fmt.Errorf("unknown session subcommand %q", args[0]))
	}
}

func cliSessionStart(jsonMode bool, args []string) {
	name := "session"
	var portArg string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--name", "-n":
			if i+1 >= len(args) {
				die(jsonMode, "session", fmt.Errorf("--name requires a value"))
			}
			name = args[i+1]
			i++
		case "--help", "-h":
			die(jsonMode, "session", fmt.Errorf("usage: pikvm session start <port-or-name> [--name LABEL]"))
		default:
			if strings.HasPrefix(args[i], "-") {
				die(jsonMode, "session", fmt.Errorf("unknown flag %q", args[i]))
			}
			portArg = args[i]
		}
	}
	if portArg == "" {
		die(jsonMode, "session", fmt.Errorf("usage: pikvm session start <port-or-name> [--name LABEL]"))
	}

	port, err := parsePort(portArg)
	if err != nil {
		die(jsonMode, "session", err)
	}
	extID, err := canonicalPortID(portArg)
	if err != nil {
		die(jsonMode, "session", err)
	}
	st := state.Load()
	profile := state.GetProfile(st, extID)
	machine := profile.Name
	if machine == "" {
		machine = extID
	}

	if err := api.SetSwitchPort(port); err != nil {
		die(jsonMode, "session", fmt.Errorf("switch port: %w", err))
	}
	api.SessionLog("switch", map[string]interface{}{"port": extID, "machine": machine})

	dir, err := sessionDir(name, machine)
	if err != nil {
		die(jsonMode, "session", err)
	}

	s, err := api.StartSession(context.Background(), dir, name, machine, port)
	if err != nil {
		die(jsonMode, "session", err)
	}

	exe, _ := os.Executable()
	worker := exec.Command(exe, append([]string{"session", "worker", dir, strconv.Itoa(port)}, hostArgs()...)...)
	worker.Stdout = os.Stderr
	worker.Stderr = os.Stderr
	if err := worker.Start(); err != nil {
		die(jsonMode, "session", fmt.Errorf("start worker: %w", err))
	}
	_ = os.WriteFile(filepath.Join(dir, "worker.pid"), []byte(strconv.Itoa(worker.Process.Pid)), 0o644)

	if jsonMode {
		emit(true, "session_start", map[string]interface{}{
			"dir": dir, "name": name, "machine": machine, "port": extID, "worker_pid": worker.Process.Pid,
		}, nil)
		return
	}
	fmt.Printf("\uf00c Session started: %s\n", s.Dir)
	fmt.Printf("  machine: %s  port: %s  worker pid: %d\n", machine, extID, worker.Process.Pid)
	fmt.Println("  screen chunks: screen-NNN.mp4  log: actions.jsonl")
	fmt.Println("  stop with: pikvm session stop")
}

func hostArgs() []string {
	if config.HostName != "" {
		return []string{"--host", config.HostName}
	}
	return nil
}

func cliSessionWorker(jsonMode bool, args []string) {
	if len(args) < 2 {
		die(jsonMode, "session_worker", fmt.Errorf("usage: pikvm session worker <dir> <port>"))
	}
	dir := args[0]
	port, err := strconv.Atoi(args[1])
	if err != nil {
		die(jsonMode, "session_worker", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := api.RunSessionWorker(ctx, dir, port); err != nil && ctx.Err() == nil {
		die(jsonMode, "session_worker", err)
	}
}

func cliSessionStop(jsonMode bool) {
	s, err := api.StopSession()
	if err != nil {
		die(jsonMode, "session", err)
	}
	if jsonMode {
		emit(true, "session_stop", map[string]interface{}{"dir": s.Dir}, nil)
		return
	}
	fmt.Printf("\uf00c Session stopped: %s\n", s.Dir)
}

func cliSessionStatus(jsonMode bool) {
	s := api.ActiveSession()
	dir := ""
	if s != nil {
		dir = s.Dir
	} else if d, err := api.ActiveSessionDir(); err == nil {
		dir = d
	}
	if dir == "" {
		if jsonMode {
			emit(true, "session_status", map[string]interface{}{"active": false}, nil)
			return
		}
		fmt.Println("(no active session)")
		return
	}
	if s == nil {
		if jsonMode {
			emit(true, "session_status", map[string]interface{}{"active": true, "dir": dir, "recording_process": "unknown"}, nil)
			return
		}
		fmt.Printf("active session (on disk): %s\n", dir)
		fmt.Println("  (recording may be running in another pikvm process; use session stop to finalize log)")
		return
	}
	if jsonMode {
		emit(true, "session_status", map[string]interface{}{
			"active": true, "dir": s.Dir, "name": s.Name, "machine": s.Machine,
			"started_at": s.StartedAt.Format(time.RFC3339),
		}, nil)
		return
	}
	fmt.Printf("active session: %s\n", s.Dir)
	fmt.Printf("  name: %s  machine: %s  since: %s\n", s.Name, s.Machine, s.StartedAt.Format(time.RFC3339))
}

func cliSessionNote(jsonMode bool, text string) {
	if api.ActiveSession() == nil {
		if _, err := api.ActiveSessionDir(); err != nil {
			die(jsonMode, "session", fmt.Errorf("no active session"))
		}
	}
	api.SessionLog("note", map[string]interface{}{"text": text})
	if jsonMode {
		emit(true, "session_note", map[string]interface{}{"text": text}, nil)
		return
	}
	fmt.Println("\uf00c note logged")
}

func sessionDir(name, machine string) (string, error) {
	base, err := config.ResolveRecordingsDir()
	if err != nil {
		return "", err
	}
	stamp := time.Now().Format("20060102-150405")
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, machine+"-"+name)
	return filepath.Join(base, "sessions", stamp+"-"+safe), nil
}
