// PiKVM WebSocket client.
//
// PiKVM exposes wss://<host>/api/ws?stream=1 which streams JSON event objects
// of the shape:
//
//	{"event_type": "<name>", "event": { ... }}
//
// On connect the server emits one of each event type to bootstrap state, then
// pushes deltas as things change (active port, ATX power LEDs, MSD progress,
// connected client count, etc.).
//
// We subscribe once at startup and pump every interesting event into the
// Bubble Tea program via tea.Program.Send, where Update() can react to it.
// This replaces the old GET /api/switch polling and gives the TUI live state
// regardless of who/what changes the PiKVM (this binary, the web UI, another
// device on Tailscale, a button on the unit).

package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/coder/websocket"
)

// ----------------------------------------------------------------------------
// Manual reconnect plumbing for the 'r' key.
//
// The Bubble Tea Update() loop calls requestWSReconnect() which:
//   1. Closes the currently-open *websocket.Conn (if any), which makes the
//      reader in runWSOnce return and the outer loop reconnect.
//   2. Pokes wsReconnectSignal so the outer loop skips its backoff sleep
//      and dials immediately instead of waiting up to 30s.
// ----------------------------------------------------------------------------

var (
	wsCurrentConn     atomic.Pointer[websocket.Conn]
	wsReconnectSignal = make(chan struct{}, 1)
)

// requestWSReconnect is safe to call from any goroutine. It is a no-op if no
// connection is currently open or if a reconnect is already pending.
func requestWSReconnect() {
	if conn := wsCurrentConn.Load(); conn != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "manual reconnect")
	}
	select {
	case wsReconnectSignal <- struct{}{}:
	default:
	}
}

// ----------------------------------------------------------------------------
// Bubble Tea messages emitted by the WS goroutine.
// ----------------------------------------------------------------------------

// wsConnectedMsg is sent once after a successful (re)connect.
type wsConnectedMsg struct{}

// wsDisconnectedMsg is sent when the WS connection drops; the goroutine will
// retry on its own, so this is informational only.
type wsDisconnectedMsg struct{ err error }

// wsSwitchMsg carries an updated switch topology + active port.
// We reuse switchState (defined in pikvm.go) so the existing reducer logic
// for `r` (refresh) can be shared.
type wsSwitchMsg struct{ state switchState }

// wsAtxMsg carries the global ATX state (busy + power/hdd LEDs). Note that
// /api/ws "atx" is the single-port aggregate; per-port LED arrays come via the
// "switch" event under result.atx in the polled API. We keep both around so
// Phase 2 (idea 9 — dim Power ON if already on) has the data it needs.
type wsAtxMsg struct {
	Busy     bool
	PowerLed bool
	HddLed   bool
}

// wsMsdMsg carries the mass-storage drive state (free space, in-flight upload,
// connected/not). Used by the status bar (Phase 2 idea 10).
type wsMsdMsg struct {
	Online      bool
	Busy        bool
	Connected   bool
	FreeBytes   int64
	TotalBytes  int64
	Uploading   bool
	UploadName  string
	UploadPct   float64
}

// wsClientsMsg carries the count of WS clients currently connected to PiKVM.
// Useful as a "someone else is also looking at this" indicator.
type wsClientsMsg struct{ Count int }

// ----------------------------------------------------------------------------
// startWebSocket runs the WS read loop in a goroutine, with auto-reconnect.
//
// It returns immediately. The goroutine sends events into prog until ctx is
// cancelled (which happens automatically when the Bubble Tea program exits).
// ----------------------------------------------------------------------------

func startWebSocket(ctx context.Context, prog *tea.Program) {
	go func() {
		// Exponential backoff for reconnect attempts.
		backoff := time.Second
		const maxBackoff = 30 * time.Second

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			err := runWSOnce(ctx, prog)
			if ctx.Err() != nil {
				return
			}

			prog.Send(wsDisconnectedMsg{err: err})

			// Sleep `backoff` OR until a manual reconnect is requested,
			// whichever comes first. A manual reconnect resets backoff so the
			// next failure starts retrying quickly again.
			select {
			case <-ctx.Done():
				return
			case <-wsReconnectSignal:
				backoff = time.Second
				continue
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}()
}

// runWSOnce opens a single WS connection and pumps events until it dies.
// Returns nil on clean close, error on read/dial failure.
func runWSOnce(ctx context.Context, prog *tea.Program) error {
	url := fmt.Sprintf("wss://%s/api/ws?stream=1", pikvmHost)

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	headers := http.Header{}
	headers.Set("X-KVMD-User", pikvmUser)
	headers.Set("X-KVMD-Passwd", pikvmPass)

	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(dialCtx, url, &websocket.DialOptions{
		HTTPClient: httpClient,
		HTTPHeader: headers,
	})
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "client closed")

	// PiKVM may stream large initial bursts (full switch + edids JSON). Allow
	// generously large messages.
	conn.SetReadLimit(4 * 1024 * 1024)

	// Publish the live conn so requestWSReconnect() can force-close it.
	wsCurrentConn.Store(conn)
	defer wsCurrentConn.Store(nil)

	prog.Send(wsConnectedMsg{})

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("ws read: %w", err)
		}
		dispatchEvent(prog, data)
	}
}

// ----------------------------------------------------------------------------
// dispatchEvent parses one raw WS frame and converts it to a tea.Msg.
//
// We deliberately ignore event types we don't currently render (gpio, ocr,
// hid_keymaps, info, loop, streamer, hid). They can be added in later phases.
// ----------------------------------------------------------------------------

func dispatchEvent(prog *tea.Program, data []byte) {
	var head struct {
		EventType string          `json:"event_type"`
		Event     json.RawMessage `json:"event"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return
	}

	switch head.EventType {
	case "switch":
		if msg, ok := decodeSwitchEvent(head.Event); ok {
			prog.Send(msg)
		}
	case "atx":
		if msg, ok := decodeAtxEvent(head.Event); ok {
			prog.Send(msg)
		}
	case "msd":
		if msg, ok := decodeMsdEvent(head.Event); ok {
			prog.Send(msg)
		}
	case "clients":
		if msg, ok := decodeClientsEvent(head.Event); ok {
			prog.Send(msg)
		}
	}
}

// decodeSwitchEvent reuses the same shape as fetchSwitchState's parser, so
// downstream Update() logic can treat WS-pushed and refresh-pulled data
// identically.
func decodeSwitchEvent(raw json.RawMessage) (wsSwitchMsg, bool) {
	var parsed struct {
		Model struct {
			Ports []struct {
				ID      string `json:"id"`
				Channel int    `json:"channel"`
				Unit    int    `json:"unit"`
			} `json:"ports"`
			Units []json.RawMessage `json:"units"`
		} `json:"model"`
		Summary struct {
			ActivePort int    `json:"active_port"`
			ActiveID   string `json:"active_id"`
			Synced     bool   `json:"synced"`
		} `json:"summary"`
		Atx struct {
			Leds struct {
				Power []bool `json:"power"`
				Hdd   []bool `json:"hdd"`
			} `json:"leds"`
		} `json:"atx"`
		Video struct {
			Links []bool `json:"links"`
		} `json:"video"`
		Usb struct {
			Links []bool `json:"links"`
		} `json:"usb"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return wsSwitchMsg{}, false
	}

	units := len(parsed.Model.Units)
	total := len(parsed.Model.Ports)
	if units == 0 || total == 0 {
		return wsSwitchMsg{}, false
	}

	state := switchState{
		Extenders:   units,
		TotalPorts:  total,
		PortsPerExt: total / units,
		ActivePort:  parsed.Summary.ActivePort,
		VideoLinks:  parsed.Video.Links,
		UsbLinks:    parsed.Usb.Links,
		PowerLeds:   parsed.Atx.Leds.Power,
		HddLeds:     parsed.Atx.Leds.Hdd,
	}
	if state.PortsPerExt == 0 {
		state.PortsPerExt = 1
	}
	for i := 0; i < total; i++ {
		state.Available = append(state.Available, portInfo{id: i, active: true})
	}
	return wsSwitchMsg{state: state}, true
}

func decodeAtxEvent(raw json.RawMessage) (wsAtxMsg, bool) {
	var parsed struct {
		Busy bool `json:"busy"`
		Leds struct {
			Power bool `json:"power"`
			Hdd   bool `json:"hdd"`
		} `json:"leds"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return wsAtxMsg{}, false
	}
	return wsAtxMsg{
		Busy:     parsed.Busy,
		PowerLed: parsed.Leds.Power,
		HddLed:   parsed.Leds.Hdd,
	}, true
}

func decodeMsdEvent(raw json.RawMessage) (wsMsdMsg, bool) {
	var parsed struct {
		Online  bool `json:"online"`
		Busy    bool `json:"busy"`
		Storage struct {
			Parts map[string]struct {
				Size int64 `json:"size"`
				Free int64 `json:"free"`
			} `json:"parts"`
			Uploading *struct {
				Name    string  `json:"name"`
				Size    int64   `json:"size"`
				Written int64   `json:"written"`
				Speed   float64 `json:"speed"`
			} `json:"uploading"`
		} `json:"storage"`
		Drive struct {
			Image     *string `json:"image"`
			Connected bool    `json:"connected"`
		} `json:"drive"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return wsMsdMsg{}, false
	}

	msg := wsMsdMsg{
		Online:    parsed.Online,
		Busy:      parsed.Busy,
		Connected: parsed.Drive.Connected,
	}
	for _, p := range parsed.Storage.Parts {
		msg.FreeBytes = p.Free
		msg.TotalBytes = p.Size
		break
	}
	if u := parsed.Storage.Uploading; u != nil {
		msg.Uploading = true
		msg.UploadName = u.Name
		if u.Size > 0 {
			msg.UploadPct = 100.0 * float64(u.Written) / float64(u.Size)
		}
	}
	return msg, true
}

func decodeClientsEvent(raw json.RawMessage) (wsClientsMsg, bool) {
	var parsed struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return wsClientsMsg{}, false
	}
	return wsClientsMsg{Count: parsed.Count}, true
}
