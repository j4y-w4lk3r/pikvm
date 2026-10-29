package tour

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FrameSource yields JPEG frames. api.SnapshotStream satisfies it.
type FrameSource interface {
	Grab() ([]byte, error)
}

// Options describe one tour.
type Options struct {
	Name        string        // directory name (sanitized by the caller or here)
	Note        string        // free text, e.g. "before BIOS 2503 update"
	Host        string        // PiKVM host name (pikvm2)
	HostAddr    string        // PiKVM address (100.64.0.7)
	Port        int           // KVM switch port (0 on direct-ATX hosts)
	Dir         string        // tour directory; TourDir(...) when empty
	Interval    time.Duration // poll interval (default 1s)
	MinChange   float64       // detector threshold (default DefaultMinChange)
	ToolVersion string        // pikvm CLI version
	KvmdVersion string        // PiKVM kvmd version, if known
}

// Frame is one saved screenshot.
type Frame struct {
	File   string    `json:"file"`
	Time   time.Time `json:"time"`
	Change float64   `json:"change"`
	Manual bool      `json:"manual,omitempty"`
}

// Meta is written to tour.json after every saved frame and on stop.
type Meta struct {
	Name         string    `json:"name"`
	Note         string    `json:"note,omitempty"`
	Host         string    `json:"host"`
	HostAddr     string    `json:"host_addr,omitempty"`
	Port         int       `json:"port"`
	Started      time.Time `json:"started"`
	Ended        time.Time `json:"ended,omitempty"`
	Tool         string    `json:"tool"`
	ToolVersion  string    `json:"tool_version"`
	KvmdVersion  string    `json:"kvmd_version,omitempty"`
	IntervalSec  float64   `json:"interval_sec"`
	MinChange    float64   `json:"min_change"`
	Frames       []Frame   `json:"frames"`
	ContactSheet string    `json:"contact_sheet,omitempty"`
	Index        string    `json:"index,omitempty"`
}

// Event reports tour progress to the TUI / CLI.
type Event struct {
	Kind   string  // "saved", "error", "done"
	Index  int     // 1-based frame number for "saved"
	Path   string  // saved frame path, or tour dir for "done"
	Change float64 // for "saved"
	Manual bool
	Err    error
	Meta   *Meta // for "done"
}

// Run polls src until ctx ends, saving one frame per distinct screen into
// opts.Dir. A value on manual forces a capture of the current screen.
// Events are sent without blocking forever: a full channel drops "saved"
// notifications rather than stalling capture. "done" is always delivered.
func Run(ctx context.Context, opts Options, src FrameSource, manual <-chan struct{}, events chan<- Event) (*Meta, error) {
	if opts.Interval <= 0 {
		opts.Interval = time.Second
	}
	if opts.Dir == "" {
		return nil, fmt.Errorf("tour: Dir is required")
	}
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return nil, err
	}
	meta := &Meta{
		Name: opts.Name, Note: opts.Note, Host: opts.Host, HostAddr: opts.HostAddr,
		Port: opts.Port, Started: time.Now(), Tool: "pikvm", ToolVersion: opts.ToolVersion,
		KvmdVersion: opts.KvmdVersion, IntervalSec: opts.Interval.Seconds(),
		Frames: []Frame{},
	}
	det := NewDetector(opts.MinChange)
	meta.MinChange = det.MinChange
	if err := writeMeta(opts.Dir, meta); err != nil {
		return nil, err
	}

	send := func(ev Event) {
		if events == nil {
			return
		}
		select {
		case events <- ev:
		default:
		}
	}

	save := func(data []byte, change float64, isManual bool) {
		n := len(meta.Frames) + 1
		name := fmt.Sprintf("%03d.jpg", n)
		path := filepath.Join(opts.Dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			send(Event{Kind: "error", Err: err})
			return
		}
		meta.Frames = append(meta.Frames, Frame{File: name, Time: time.Now(), Change: change, Manual: isManual})
		if err := writeMeta(opts.Dir, meta); err != nil {
			send(Event{Kind: "error", Err: err})
		}
		send(Event{Kind: "saved", Index: n, Path: path, Change: change, Manual: isManual})
	}

	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			err := finish(opts.Dir, meta)
			if events != nil {
				events <- Event{Kind: "done", Path: opts.Dir, Meta: meta, Err: err}
			}
			return meta, err

		case <-manual:
			data, err := src.Grab()
			if err != nil {
				send(Event{Kind: "error", Err: err})
				continue
			}
			fp, _, err := FingerprintJPEG(data)
			if err != nil {
				send(Event{Kind: "error", Err: err})
				continue
			}
			save(data, det.Force(fp), true)

		case <-ticker.C:
			data, err := src.Grab()
			if err != nil {
				if ctx.Err() == nil {
					send(Event{Kind: "error", Err: err})
				}
				continue
			}
			fp, _, err := FingerprintJPEG(data)
			if err != nil {
				send(Event{Kind: "error", Err: err})
				continue
			}
			if ok, change := det.Observe(fp); ok {
				save(data, change, false)
			}
		}
	}
}

// finish writes the contact sheet, index.html and the final tour.json.
func finish(dir string, meta *Meta) error {
	meta.Ended = time.Now()
	var firstErr error
	if len(meta.Frames) > 0 {
		if err := WriteContactSheet(dir, meta, filepath.Join(dir, "contact-sheet.jpg")); err != nil {
			firstErr = err
		} else {
			meta.ContactSheet = "contact-sheet.jpg"
		}
	}
	if err := WriteIndexHTML(dir, meta); err != nil && firstErr == nil {
		firstErr = err
	} else if err == nil {
		meta.Index = "index.html"
	}
	if err := writeMeta(dir, meta); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func writeMeta(dir string, meta *Meta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".tour.json.tmp")
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "tour.json"))
}
