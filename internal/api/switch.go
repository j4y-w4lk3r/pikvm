package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// lastPortsPerExt remembers the most recent ports-per-extender value learned
// from /api/switch (or pushed via WS). It lets callers who only have a
// linear port index format it as "ext.port" (e.g. 7 → "2.4") without
// threading topology through every function signature.
//
// Seeded to 1 so early-session calls still produce something sensible.
// Updated atomically from both FetchSwitchState and the WS decodeSwitchEvent
// path.
var lastPortsPerExt atomic.Int32

func init() { lastPortsPerExt.Store(1) }

// LastPortsPerExt returns the most recent ports-per-extender value. Always
// >= 1 so port arithmetic never divides by zero.
func LastPortsPerExt() int {
	v := int(lastPortsPerExt.Load())
	if v < 1 {
		return 1
	}
	return v
}

// FormatPort renders a linear 0-based port as the user-facing "ext.port"
// form (1-based), e.g. 7 → "2.4". Uses the most recently observed
// portsPerExt; falls back to "port <N+1>" if we've never seen one.
func FormatPort(linear int) string {
	ppe := LastPortsPerExt()
	return fmt.Sprintf("%d.%d", linear/ppe+1, linear%ppe+1)
}

// PortInfo is one entry in SwitchState.Available.
type PortInfo struct {
	ID     int
	Active bool
}

// SwitchState describes the PiKVM switch topology and current active port.
// Linear port indexing is used everywhere internally:
//
//	linearPort = (extender-1) * portsPerExt + (port-1)   // both 1-based
//	            = unit * portsPerExt + channel           // both 0-based
type SwitchState struct {
	Extenders   int        `json:"extenders"`     // number of extender units (e.g. 2)
	PortsPerExt int        `json:"ports_per_ext"` // ports per extender (typically 4)
	TotalPorts  int        `json:"total_ports"`   // Extenders * PortsPerExt
	ActivePort  int        `json:"active_port"`   // currently-active linear port (0-based)
	Available   []PortInfo `json:"-"`             // every existing linear port (TUI-internal)

	// Per-port live status (length == TotalPorts when populated).
	// PiKVM reports these in /api/switch and pushes updates over /api/ws.
	VideoLinks []bool `json:"video_links"`
	UsbLinks   []bool `json:"usb_links"`
	PowerLeds  []bool `json:"power_leds"`
	HddLeds    []bool `json:"hdd_leds"`
}

// FetchSwitchState queries /api/switch once and returns the topology. On
// error falls back to a single-extender / single-port stub so callers can
// still render something and try again later.
func FetchSwitchState() SwitchState {
	state := SwitchState{
		Extenders: 1, PortsPerExt: 1, TotalPorts: 1,
		Available: []PortInfo{{ID: 0, Active: true}},
	}

	resp, err := Do("GET", "/switch", nil, 3*time.Second)
	if err != nil {
		return state
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var parsed struct {
		OK     bool `json:"ok"`
		Result struct {
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
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || !parsed.OK {
		return state
	}

	units := len(parsed.Result.Model.Units)
	total := len(parsed.Result.Model.Ports)
	if units == 0 || total == 0 {
		return state
	}

	state.Extenders = units
	state.TotalPorts = total
	state.PortsPerExt = total / units
	if state.PortsPerExt == 0 {
		state.PortsPerExt = 1
	}
	state.ActivePort = parsed.Result.Summary.ActivePort
	state.Available = state.Available[:0]
	for i := 0; i < total; i++ {
		state.Available = append(state.Available, PortInfo{ID: i, Active: true})
	}
	state.VideoLinks = parsed.Result.Video.Links
	state.UsbLinks = parsed.Result.Usb.Links
	state.PowerLeds = parsed.Result.Atx.Leds.Power
	state.HddLeds = parsed.Result.Atx.Leds.Hdd

	// Remember portsPerExt for FormatPort / ExecuteAction's success message.
	lastPortsPerExt.Store(int32(state.PortsPerExt))
	return state
}

// SetSwitchPort points PiKVM's active port so video/HID follow the selection.
func SetSwitchPort(port int) error {
	resp, err := Do("POST", fmt.Sprintf("/switch/set_active?port=%d", port), nil, 5*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("switch/set_active returned %d", resp.StatusCode)
	}
	return nil
}

// IsPortAvailable returns true when port appears in ports with Active==true.
func IsPortAvailable(port int, ports []PortInfo) bool {
	for _, p := range ports {
		if p.ID == port && p.Active {
			return true
		}
	}
	return false
}
