package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pikvm/internal/api"
)

func cliSnapshot(jsonMode bool, args []string) {
	if len(args) == 0 {
		die(jsonMode, "snapshot", fmt.Errorf("usage: pikvm snapshot [port] [--out FILE] | pikvm snapshot watch [port] [--dir DIR] [--interval SEC]"))
	}
	switch args[0] {
	case "watch":
		cliSnapshotWatch(jsonMode, args[1:])
	default:
		cliSnapshotOnce(jsonMode, args)
	}
}

func cliSnapshotOnce(jsonMode bool, args []string) {
	portArg := ""
	out := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--out", "-o":
			if i+1 >= len(args) {
				die(jsonMode, "snapshot", fmt.Errorf("--out requires a path"))
			}
			out = args[i+1]
			i++
		default:
			if strings.HasPrefix(args[i], "-") {
				die(jsonMode, "snapshot", fmt.Errorf("unknown flag %q", args[i]))
			}
			portArg = args[i]
		}
	}
	port, err := resolvePort(portArg)
	if err != nil {
		die(jsonMode, "snapshot", err)
	}
	if out == "" {
		out = fmt.Sprintf("snapshot-%s.jpg", time.Now().Format("20060102-150405"))
	}
	n, err := api.SaveSnapshot(port, 85, out)
	if err != nil {
		die(jsonMode, "snapshot", err)
	}
	if jsonMode {
		emit(true, "snapshot", map[string]interface{}{"path": out, "bytes": n, "port": port}, nil)
		return
	}
	fmt.Printf("\uf00c Snapshot: %s (%d bytes)\n", out, n)
}

func cliSnapshotWatch(jsonMode bool, args []string) {
	portArg := ""
	dir := "."
	interval := 3.0
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dir", "-d":
			if i+1 >= len(args) {
				die(jsonMode, "snapshot_watch", fmt.Errorf("--dir requires a path"))
			}
			dir = args[i+1]
			i++
		case "--interval", "-i":
			if i+1 >= len(args) {
				die(jsonMode, "snapshot_watch", fmt.Errorf("--interval requires seconds"))
			}
			var err error
			interval, err = strconv.ParseFloat(args[i+1], 64)
			if err != nil || interval <= 0 {
				die(jsonMode, "snapshot_watch", fmt.Errorf("invalid interval"))
			}
			i++
		default:
			if strings.HasPrefix(args[i], "-") {
				die(jsonMode, "snapshot_watch", fmt.Errorf("unknown flag %q", args[i]))
			}
			portArg = args[i]
		}
	}
	port, err := resolvePort(portArg)
	if err != nil {
		die(jsonMode, "snapshot_watch", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		die(jsonMode, "snapshot_watch", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !jsonMode {
		fmt.Fprintf(os.Stderr, "snapshot watch: port=%d dir=%s every %.1fs (Ctrl+C to stop)\n", port, dir, interval)
	}
	n := 0
	for {
		select {
		case <-ctx.Done():
			if jsonMode {
				emit(true, "snapshot_watch", map[string]interface{}{"count": n, "dir": dir}, nil)
			} else {
				fmt.Fprintf(os.Stderr, "\n\uf00c %d snapshots in %s\n", n, dir)
			}
			return
		default:
		}
		name := fmt.Sprintf("%04d-%s.jpg", n, time.Now().Format("150405"))
		path := filepath.Join(dir, name)
		bytes, err := api.SaveSnapshot(port, 85, path)
		if err != nil {
			api.SessionLog("snapshot_error", map[string]interface{}{"error": err.Error(), "path": path})
			fmt.Fprintf(os.Stderr, "snapshot %d: %v\n", n, err)
		} else if !jsonMode {
			fmt.Fprintf(os.Stderr, "  #%d %s (%d bytes)\n", n, path, bytes)
		}
		n++
		select {
		case <-ctx.Done():
			continue
		case <-time.After(time.Duration(interval * float64(time.Second))):
		}
	}
}

func resolvePort(portArg string) (int, error) {
	if portArg == "" {
		sw := api.FetchSwitchState()
		if sw.ActivePort >= 0 {
			return sw.ActivePort, nil
		}
		return 0, nil
	}
	return parsePort(portArg)
}
