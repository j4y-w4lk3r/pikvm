// Package state persists per-port profiles and any other user-editable TUI
// state in ~/.config/pikvm/state.json (or $XDG_CONFIG_HOME/pikvm/state.json).
//
// Schema:
//
//	{
//	  "schema_version": 1,
//	  "ports": {
//	    "1.1": { "name": "j4yn0", "bios_key": "F7", "tags": ["nas"] },
//	    "2.4": { "name": "vault", "bios_key": "Delete" }
//	  }
//	}
//
// Keys in `ports` are the 1-based ext.port form ("2.4"), matching how PiKVM's
// own /api/switch exposes `summary.active_id`. Easy to hand-edit and stable
// across PiKVM reboots.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// PowerBackend describes non-ATX power control (e.g. ESP32 HTTP relay for Mac mini).
type PowerBackend struct {
	Type        string `json:"type,omitempty"` // "http"
	OnURL       string `json:"on_url,omitempty"`
	OffURL      string `json:"off_url,omitempty"`
	ClickURL    string `json:"click_url,omitempty"`
	LongURL     string `json:"long_url,omitempty"`
	Method      string `json:"method,omitempty"`
	CooldownSec int    `json:"cooldown_sec,omitempty"`
}

func (p *PowerBackend) IsEmpty() bool {
	if p == nil {
		return true
	}
	return p.Type == "" && p.OnURL == "" && p.OffURL == "" && p.ClickURL == "" && p.LongURL == ""
}

// PortProfile holds everything a user might want to associate with a port.
// All fields are optional — an empty profile is the same as no profile.
type PortProfile struct {
	Name          string        `json:"name,omitempty"`
	BIOSKey       string        `json:"bios_key,omitempty"` // matches api.BIOSKeyOption.Key ("F7", "Delete", ...)
	DefaultISO    string        `json:"default_iso,omitempty"`
	Tags          []string      `json:"tags,omitempty"`
	TailscaleName string        `json:"tailscale_name,omitempty"`
	SSHUser       string        `json:"ssh_user,omitempty"`
	Notes         string        `json:"notes,omitempty"`
	Power         *PowerBackend `json:"power,omitempty"`
}

// State is the persisted root document.
type State struct {
	SchemaVersion int                    `json:"schema_version"`
	Ports         map[string]PortProfile `json:"ports"` // key: "ext.port" e.g. "2.4"
}

// filePath returns the absolute path to state.json respecting XDG_CONFIG_HOME.
func filePath() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "pikvm", "state.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "pikvm-state.json")
	}
	return filepath.Join(home, ".config", "pikvm", "state.json")
}

var mu sync.Mutex

// Load reads state.json. Missing file -> empty state (not an error).
// Malformed JSON -> empty state + warning on stderr (file untouched until fixed).
func Load() State {
	empty := State{SchemaVersion: 1, Ports: map[string]PortProfile{}}

	mu.Lock()
	defer mu.Unlock()

	path := filePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return empty
		}
		fmt.Fprintf(os.Stderr, "warning: could not read %s: %v (continuing with empty state)\n", path, err)
		return empty
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s is not valid JSON: %v (continuing with empty state; the file will not be overwritten until you fix it)\n", path, err)
		return empty
	}
	if s.Ports == nil {
		s.Ports = map[string]PortProfile{}
	}
	if s.SchemaVersion == 0 {
		s.SchemaVersion = 1
	}
	return s
}

// Save writes state.json atomically (tmpfile + rename, mode 0600).
func Save(s State) error {
	mu.Lock()
	defer mu.Unlock()

	path := filePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}
	if s.SchemaVersion == 0 {
		s.SchemaVersion = 1
	}
	if s.Ports == nil {
		s.Ports = map[string]PortProfile{}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}

// GetProfile returns the profile for the given 1-based ext.port id.
func GetProfile(s State, extPort string) PortProfile {
	if s.Ports == nil {
		return PortProfile{}
	}
	return s.Ports[extPort]
}

// SetProfile writes the profile under the given ext.port id, creating the
// Ports map if needed. Empty profiles are removed so no dead keys accumulate.
func SetProfile(s State, extPort string, p PortProfile) State {
	if s.Ports == nil {
		s.Ports = map[string]PortProfile{}
	}
	if p.IsEmpty() {
		delete(s.Ports, extPort)
	} else {
		s.Ports[extPort] = p
	}
	return s
}

// IsEmpty returns true when every field on p is its zero value.
func (p PortProfile) IsEmpty() bool {
	return p.Name == "" && p.BIOSKey == "" && p.DefaultISO == "" &&
		len(p.Tags) == 0 && p.TailscaleName == "" && p.SSHUser == "" && p.Notes == "" &&
		p.Power.IsEmpty()
}

// ResolveName maps a profile name (e.g. "j4yn0") to its ext.port id (e.g.
// "2.4"). Case-insensitive. Returns ok=false if no profile matches.
func ResolveName(s State, name string) (string, bool) {
	lower := strings.ToLower(name)
	for id, p := range s.Ports {
		if strings.ToLower(p.Name) == lower {
			return id, true
		}
	}
	return "", false
}

// PortExtID formats a 1-based ext.port id from a linear 0-based port given
// the topology (ports per extender).
func PortExtID(linear, portsPerExt int) string {
	if portsPerExt <= 0 {
		portsPerExt = 1
	}
	ext := linear/portsPerExt + 1
	pno := linear%portsPerExt + 1
	return fmt.Sprintf("%d.%d", ext, pno)
}

// ParseExtID parses a "2.3" form into a linear 0-based port given topology.
// Returns (linear, true) on success; (0, false) on bad format or out-of-range.
func ParseExtID(id string, portsPerExt int) (int, bool) {
	parts := strings.SplitN(id, ".", 2)
	if len(parts) != 2 {
		return 0, false
	}
	var ext, pno int
	if _, err := fmt.Sscanf(parts[0]+" "+parts[1], "%d %d", &ext, &pno); err != nil {
		return 0, false
	}
	if ext < 1 || pno < 1 || portsPerExt <= 0 {
		return 0, false
	}
	return (ext-1)*portsPerExt + (pno - 1), true
}

// SortedPortIDs returns the ext.port keys sorted naturally (1.1, 1.2, ...,
// 2.1, 2.2, ...).
func SortedPortIDs(s State) []string {
	ids := make([]string, 0, len(s.Ports))
	for id := range s.Ports {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a1, a2 := parseTwoInts(ids[i])
		b1, b2 := parseTwoInts(ids[j])
		if a1 != b1 {
			return a1 < b1
		}
		return a2 < b2
	})
	return ids
}

func parseTwoInts(id string) (int, int) {
	var a, b int
	_, _ = fmt.Sscanf(id, "%d.%d", &a, &b)
	return a, b
}
