package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pikvm/internal/api"
	"pikvm/internal/config"
	"pikvm/internal/tour"
)

// pikvm tour <name> [--note TEXT] [--port N] [--interval SEC] [--min-change F]
// pikvm tour list
//
// Saves one screenshot per new screen while you click through the PiKVM web
// UI (e.g. a BIOS). Ctrl+C finishes the tour and writes contact-sheet.jpg,
// index.html and tour.json into ~/Pictures/pikvm/<host>/tours/<name>/.
func cliTour(jsonMode bool, args []string) {
	if len(args) > 0 && (args[0] == "list" || args[0] == "ls") {
		cliTourList(jsonMode)
		return
	}
	name, note, portArg := "", "", ""
	interval := 1.0
	minChange := 0.0
	for i := 0; i < len(args); i++ {
		need := func(flag string) string {
			if i+1 >= len(args) {
				die(jsonMode, "tour", fmt.Errorf("%s requires a value", flag))
			}
			i++
			return args[i]
		}
		switch args[i] {
		case "--note", "-n":
			note = need(args[i])
		case "--port", "-p":
			portArg = need(args[i])
		case "--interval", "-i":
			v, err := strconv.ParseFloat(need(args[i]), 64)
			if err != nil || v <= 0 {
				die(jsonMode, "tour", fmt.Errorf("invalid interval"))
			}
			interval = v
		case "--min-change":
			v, err := strconv.ParseFloat(need(args[i]), 64)
			if err != nil || v <= 0 || v >= 1 {
				die(jsonMode, "tour", fmt.Errorf("--min-change must be between 0 and 1 (e.g. 0.06)"))
			}
			minChange = v
		default:
			if strings.HasPrefix(args[i], "-") {
				die(jsonMode, "tour", fmt.Errorf("unknown flag %q", args[i]))
			}
			if name != "" {
				die(jsonMode, "tour", fmt.Errorf("one tour name only (got %q and %q)", name, args[i]))
			}
			name = args[i]
		}
	}
	if name == "" {
		name = tour.DefaultName(time.Now())
	}
	name = tour.SanitizeName(name)

	port, err := resolvePort(portArg)
	if err != nil {
		die(jsonMode, "tour", err)
	}
	root, err := tour.PicturesRoot()
	if err != nil {
		die(jsonMode, "tour", err)
	}
	dir := tour.TourDir(root, config.HostName, name)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	stream, err := api.OpenSnapshotStream(ctx, port, 90)
	if err != nil {
		die(jsonMode, "tour", err)
	}
	defer stream.Close()
	info, _ := api.FetchInfoState()

	if !jsonMode {
		fmt.Fprintf(os.Stderr, " Tour %s on %s (port %d) → %s\n", filepath.Base(dir), config.HostName, port+1, dir)
		fmt.Fprintln(os.Stderr, "  Click through the screens in the PiKVM web UI. Ctrl+C to finish.")
	}

	events := make(chan tour.Event, 32)
	go func() {
		for ev := range events {
			if jsonMode {
				continue
			}
			switch ev.Kind {
			case "saved":
				fmt.Fprintf(os.Stderr, "  #%d %s (%.0f%% changed)\n", ev.Index, ev.Path, ev.Change*100)
			case "error":
				fmt.Fprintf(os.Stderr, "  ! %v\n", ev.Err)
			}
		}
	}()

	meta, err := tour.Run(ctx, tour.Options{
		Name: filepath.Base(dir), Note: note, Host: config.HostName, HostAddr: config.Host,
		Port: port, Dir: dir, Interval: time.Duration(interval * float64(time.Second)),
		MinChange: minChange, ToolVersion: Version, KvmdVersion: info.KvmdVersion,
	}, stream, nil, events)
	close(events)
	if meta == nil {
		die(jsonMode, "tour", err)
	}
	if jsonMode {
		emit(true, "tour", map[string]interface{}{"dir": dir, "frames": len(meta.Frames)}, err)
		return
	}
	fmt.Fprintf(os.Stderr, "\n %d screens → %s\n", len(meta.Frames), dir)
	if meta.ContactSheet != "" {
		fmt.Fprintf(os.Stderr, "  contact sheet: %s\n", filepath.Join(dir, meta.ContactSheet))
	}
	fmt.Fprintf(os.Stderr, "  index:         %s\n", filepath.Join(dir, "index.html"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: %v\n", err)
	}
}

// cliTourList prints the tours saved for the active host.
func cliTourList(jsonMode bool) {
	root, err := tour.PicturesRoot()
	if err != nil {
		die(jsonMode, "tour list", err)
	}
	host := config.HostName
	if host == "" {
		host = "pikvm"
	}
	base := filepath.Join(root, tour.SanitizeName(host), "tours")
	entries, _ := os.ReadDir(base)
	var rows []tour.Meta
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(base, e.Name(), "tour.json"))
		if err != nil {
			continue
		}
		var m tour.Meta
		if json.Unmarshal(raw, &m) == nil {
			if m.Name == "" {
				m.Name = e.Name()
			}
			rows = append(rows, m)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Started.Before(rows[j].Started) })
	if jsonMode {
		emit(true, "tour list", map[string]interface{}{"dir": base, "tours": rows}, nil)
		return
	}
	if len(rows) == 0 {
		fmt.Printf("No tours in %s\n", base)
		return
	}
	fmt.Printf("%-24s %-17s %7s  %s\n", "NAME", "STARTED", "SCREENS", "NOTE")
	for _, m := range rows {
		fmt.Printf("%-24s %-17s %7d  %s\n", m.Name, m.Started.Format("2006-01-02 15:04"), len(m.Frames), m.Note)
	}
	fmt.Printf("\n%s\n", base)
}
