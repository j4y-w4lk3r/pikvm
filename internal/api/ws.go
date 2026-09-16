// PiKVM WebSocket client.
//
// PiKVM exposes wss://<host>/api/ws?stream=0|1 which streams JSON event objects
// of the shape:
//
//	{"event_type": "<name>", "event": { ... }}
//
// stream=1 also multiplexes HDMI JPEG frames on the same socket. The TUI never
// displays video, so we connect with stream=0 (events only). Recording keeps
// its own stream=1 socket in record.go to hold the capture pipeline open.
//
// On connect the server emits one of each event type to bootstrap state, then
// pushes deltas as things change (active port, ATX power LEDs, MSD progress,
// connected client count, etc.). Unchanged payloads are not forwarded into
// Bubble Tea so the UI thread is not redrawing identical frames.

package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/coder/websocket"

	"pikvm/internal/config"
)

// ----------------------------------------------------------------------------
// Manual reconnect plumbing for the 'r' key.
// ----------------------------------------------------------------------------

var (
	wsCurrentConn     atomic.Pointer[websocket.Conn]
	wsReconnectSignal = make(chan struct{}, 1)
)

// RequestWSReconnect is safe to call from any goroutine. It is a no-op if no
// connection is currently open or if a reconnect is already pending.
func RequestWSReconnect() {
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

// ConnectedMsg is sent once after a successful (re)connect.
type ConnectedMsg struct{}

// DisconnectedMsg is sent when the WS connection drops; the goroutine will
// retry on its own, so this is informational only.
type DisconnectedMsg struct{ Err error }

// SwitchMsg carries an updated switch topology + active port.
type SwitchMsg struct{ State SwitchState }

// AtxMsg carries the global ATX state (busy + power/hdd LEDs).
type AtxMsg struct {
	Busy     bool
	PowerLed bool
	HddLed   bool
}

// MsdMsg carries the mass-storage drive state (free space, in-flight upload,
// connected/not).
type MsdMsg struct {
	Online     bool
	Busy       bool
	Connected  bool
	FreeBytes  int64
	TotalBytes int64
	Uploading  bool
	UploadName string
	UploadPct  float64
}

// ClientsMsg carries the count of WS clients currently connected to PiKVM.
type ClientsMsg struct{ Count int }

// ----------------------------------------------------------------------------
// StartWebSocket runs the WS read loop in a goroutine, with auto-reconnect.
// ----------------------------------------------------------------------------

func StartWebSocket(ctx context.Context, prog *tea.Program) {
	go func() {
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
			prog.Send(DisconnectedMsg{Err: err})

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

func runWSOnce(ctx context.Context, prog *tea.Program) error {
	url := fmt.Sprintf("wss://%s/api/ws?stream=0", config.Host)

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	headers := http.Header{}
	headers.Set("X-KVMD-User", config.User)
	headers.Set("X-KVMD-Passwd", config.Pass)

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
	conn.SetReadLimit(1 * 1024 * 1024)

	wsCurrentConn.Store(conn)
	defer wsCurrentConn.Store(nil)

	resetWSDedup()
	prog.Send(ConnectedMsg{})

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
// Event dispatch / decoders
// ----------------------------------------------------------------------------

func dispatchEvent(prog *tea.Program, data []byte) {
	if len(data) == 0 || data[0] != '{' {
		return
	}
	var head struct {
		EventType string          `json:"event_type"`
		Event     json.RawMessage `json:"event"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return
	}
	switch head.EventType {
	case "switch":
		if msg, ok := decodeSwitchEvent(head.Event); ok && switchChanged(msg.State) {
			prog.Send(msg)
		}
	case "atx":
		if msg, ok := decodeAtxEvent(head.Event); ok && atxChanged(msg) {
			prog.Send(msg)
		}
	case "msd":
		if msg, ok := decodeMsdEvent(head.Event); ok && msdChanged(msg) {
			prog.Send(msg)
		}
	case "clients":
		if msg, ok := decodeClientsEvent(head.Event); ok && clientsChanged(msg.Count) {
			prog.Send(msg)
		}
	}
}

func decodeSwitchEvent(raw json.RawMessage) (SwitchMsg, bool) {
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
		return SwitchMsg{}, false
	}

	units := len(parsed.Model.Units)
	total := len(parsed.Model.Ports)
	if units == 0 || total == 0 {
		return SwitchMsg{}, false
	}

	state := SwitchState{
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
		state.Available = append(state.Available, PortInfo{ID: i, Active: true})
	}
	lastPortsPerExt.Store(int32(state.PortsPerExt))
	return SwitchMsg{State: state}, true
}

func decodeAtxEvent(raw json.RawMessage) (AtxMsg, bool) {
	var parsed struct {
		Busy bool `json:"busy"`
		Leds struct {
			Power bool `json:"power"`
			Hdd   bool `json:"hdd"`
		} `json:"leds"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return AtxMsg{}, false
	}
	return AtxMsg{Busy: parsed.Busy, PowerLed: parsed.Leds.Power, HddLed: parsed.Leds.Hdd}, true
}

// lastMsdImageSet tracks the most recent set of ISO filenames PiKVM reported
// in storage.images. When this set changes between two MSD events, we
// invalidate the ISO cache (idea #2).
var lastMsdImageSet string

func decodeMsdEvent(raw json.RawMessage) (MsdMsg, bool) {
	var parsed struct {
		Online  bool `json:"online"`
		Busy    bool `json:"busy"`
		Storage struct {
			Images map[string]json.RawMessage `json:"images"`
			Parts  map[string]struct {
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
		return MsdMsg{}, false
	}

	if len(parsed.Storage.Images) > 0 {
		names := make([]string, 0, len(parsed.Storage.Images))
		for k := range parsed.Storage.Images {
			names = append(names, k)
		}
		sort.Strings(names)
		fingerprint := strings.Join(names, "\x00")
		if fingerprint != lastMsdImageSet {
			lastMsdImageSet = fingerprint
			InvalidateISOCache()
		}
	}

	msg := MsdMsg{
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

func decodeClientsEvent(raw json.RawMessage) (ClientsMsg, bool) {
	var parsed struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ClientsMsg{}, false
	}
	return ClientsMsg{Count: parsed.Count}, true
}

// ----------------------------------------------------------------------------
// Dedup: skip Bubble Tea messages when the parsed payload did not change.
// ----------------------------------------------------------------------------

type wsDedupState struct {
	mu          sync.Mutex
	switchSet   bool
	lastSwitch  SwitchState
	atxSet      bool
	lastAtx     AtxMsg
	msdSet      bool
	lastMsd     MsdMsg
	clientsSet  bool
	lastClients int
}

var wsDedup wsDedupState

// ResetLiveDedup clears WS + /api/info "last seen" snapshots. Call on host
// switch so the next event from the new box is never compared to the old one.
func ResetLiveDedup() {
	resetWSDedup()
	resetInfoDedup()
}

func resetWSDedup() {
	wsDedup.mu.Lock()
	defer wsDedup.mu.Unlock()
	wsDedup.switchSet = false
	wsDedup.lastSwitch = SwitchState{}
	wsDedup.atxSet = false
	wsDedup.lastAtx = AtxMsg{}
	wsDedup.msdSet = false
	wsDedup.lastMsd = MsdMsg{}
	wsDedup.clientsSet = false
	wsDedup.lastClients = 0
}

func switchStateEqual(a, b SwitchState) bool {
	return a.DirectATX == b.DirectATX &&
		a.Extenders == b.Extenders &&
		a.PortsPerExt == b.PortsPerExt &&
		a.TotalPorts == b.TotalPorts &&
		a.ActivePort == b.ActivePort &&
		slices.Equal(a.VideoLinks, b.VideoLinks) &&
		slices.Equal(a.UsbLinks, b.UsbLinks) &&
		slices.Equal(a.PowerLeds, b.PowerLeds) &&
		slices.Equal(a.HddLeds, b.HddLeds)
}

func switchChanged(s SwitchState) bool {
	wsDedup.mu.Lock()
	defer wsDedup.mu.Unlock()
	if wsDedup.switchSet && switchStateEqual(wsDedup.lastSwitch, s) {
		return false
	}
	wsDedup.lastSwitch = s
	wsDedup.switchSet = true
	return true
}

func atxChanged(msg AtxMsg) bool {
	wsDedup.mu.Lock()
	defer wsDedup.mu.Unlock()
	if wsDedup.atxSet && wsDedup.lastAtx == msg {
		return false
	}
	wsDedup.lastAtx = msg
	wsDedup.atxSet = true
	return true
}

func msdChanged(msg MsdMsg) bool {
	wsDedup.mu.Lock()
	defer wsDedup.mu.Unlock()
	if wsDedup.msdSet && wsDedup.lastMsd == msg {
		return false
	}
	wsDedup.lastMsd = msg
	wsDedup.msdSet = true
	return true
}

func clientsChanged(n int) bool {
	wsDedup.mu.Lock()
	defer wsDedup.mu.Unlock()
	if wsDedup.clientsSet && wsDedup.lastClients == n {
		return false
	}
	wsDedup.lastClients = n
	wsDedup.clientsSet = true
	return true
}
