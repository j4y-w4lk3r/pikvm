// Live video-snapshot preview for the grid view (roadmap idea #8).
//
// PiKVM has a single HDMI capture chip; the ATX switch multiplexes which
// port's video feed is routed to it. GET /api/streamer/snapshot therefore
// always returns a JPEG of the currently-active port — we do NOT attempt to
// cycle through ports (that would flip the live video output on every
// connected monitor every 10 seconds, which is disruptive). The preview
// panel is strictly "what PiKVM is showing right now".
//
// Rendering is delegated to `chafa` (brew install chafa), which auto-
// detects the terminal and emits the best available format (Kitty graphics,
// iTerm2 inline, sixel, or colored unicode blocks as a fallback). If chafa
// isn't installed, the poller produces an empty Rendered string and the
// view falls back to a one-line install hint.

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Dimensions in terminal CELLS (not pixels) that chafa will fit the image
// to. ~2:1 aspect cells → 48×12 ≈ 16:9 image, which matches HDMI.
const (
	snapshotInterval = 10 * time.Second
	snapshotCellW    = 48
	snapshotCellH    = 12
)

// snapshotState holds the most-recent snapshot + chafa-rendered output so
// the TUI View never has to run chafa on the render-hot path.
type snapshotState struct {
	Rendered  string    // chafa output ("" if chafa not installed)
	FetchedAt time.Time // when PiKVM returned the JPEG
	Err       string    // non-empty = last fetch/render failed
	ChafaSeen bool      // true if chafa was resolved at least once
}

// wsSnapshotMsg carries a new snapshot into the Bubble Tea event loop.
type wsSnapshotMsg struct{ state snapshotState }

// Per-process pause flag. Grid view's 'v' key toggles this; when paused,
// the poller skips fetches but stays alive so 'v' can resume instantly.
var snapshotPaused atomic.Bool

func setSnapshotPaused(b bool) { snapshotPaused.Store(b) }
func isSnapshotPaused() bool   { return snapshotPaused.Load() }

// startSnapshotPoller runs the snapshot fetch+render in its own goroutine.
// First fetch fires immediately; then every snapshotInterval.
//
// Unlike the WebSocket and info pollers, this one is lazy — if chafa isn't
// installed we STILL do one fetch (so the View can show a clear "install
// chafa" hint with the size of a would-be preview), but subsequent fetches
// are skipped to save bandwidth.
func startSnapshotPoller(ctx context.Context, prog *tea.Program) {
	_, chafaErr := exec.LookPath("chafa")
	chafaAvailable := chafaErr == nil

	go func() {
		fire := func() {
			if isSnapshotPaused() {
				return
			}
			state := fetchAndRenderSnapshot(chafaAvailable)
			prog.Send(wsSnapshotMsg{state: state})
		}
		fire()
		if !chafaAvailable {
			// One fetch is enough to tell the user what's missing; no point
			// in repeated polling when we can't even render.
			return
		}
		ticker := time.NewTicker(snapshotInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fire()
			}
		}
	}()
}

// fetchAndRenderSnapshot makes one round-trip + render. Safe to call from
// any goroutine.
func fetchAndRenderSnapshot(chafaAvailable bool) snapshotState {
	state := snapshotState{FetchedAt: time.Now(), ChafaSeen: chafaAvailable}
	if !chafaAvailable {
		return state
	}
	jpeg, err := fetchSnapshotJPEG()
	if err != nil {
		state.Err = err.Error()
		return state
	}
	rendered, err := renderJPEGWithChafa(jpeg, snapshotCellW, snapshotCellH)
	if err != nil {
		state.Err = err.Error()
		return state
	}
	state.Rendered = rendered
	return state
}

// fetchSnapshotJPEG calls /api/streamer/snapshot and returns the raw JPEG
// bytes of the currently-active port. Uses the shared HTTP client (idea #4).
func fetchSnapshotJPEG() ([]byte, error) {
	resp, err := pikvmDo("GET", "/streamer/snapshot", nil, 10*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// renderJPEGWithChafa shells out to `chafa` to turn JPEG bytes into
// terminal-ready ANSI art. chafa auto-detects the terminal (Kitty graphics,
// iTerm2 inline, sixel, unicode blocks). Returns the rendered string.
//
// Caller should have already confirmed chafa is in PATH — this function
// treats a missing binary as an error.
func renderJPEGWithChafa(jpeg []byte, cols, rows int) (string, error) {
	cmd := exec.Command("chafa",
		"--size", fmt.Sprintf("%dx%d", cols, rows),
		"--format", "symbols",
		"--symbols", "block",
		"--colors", "full",
		"--animate", "off",
		"-",
	)
	cmd.Stdin = bytes.NewReader(jpeg)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("chafa: %w (stderr: %s)", err, errBuf.String())
	}
	return out.String(), nil
}
