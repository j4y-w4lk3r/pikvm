// Per-port profiles and other persistent user state (roadmap idea #5).
//
// Lives at ~/.config/pikvm/state.json (or $XDG_CONFIG_HOME/pikvm/state.json).
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
// own /api/switch exposes `summary.active_id`. That makes the file stable
// across PiKVM reboots and easy to edit by hand.
//
// Every consumer (TUI, CLI, and downstream ideas like #11 bare-name CLI,
// #15 provisioning, #22 AI BIOS pilot) reads and writes through this module
// so we have one source of truth.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ----------------------------------------------------------------------------
// Types
// ----------------------------------------------------------------------------

// portProfile holds everything a user might want to associate with a port.
// All fields are optional — an empty profile is the same as no profile.
type portProfile struct {
	Name          string   `json:"name,omitempty"`
	BIOSKey       string   `json:"bios_key,omitempty"` // matches biosKeyOption.Key ("F7", "Delete", ...)
	DefaultISO    string   `json:"default_iso,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	TailscaleName string   `json:"tailscale_name,omitempty"` // for idea #11 `pikvm ssh NAME`
	SSHUser       string   `json:"ssh_user,omitempty"`
	Notes         string   `json:"notes,omitempty"`
}

// pikvmState is the persisted root document.
type pikvmState struct {
	SchemaVersion int                    `json:"schema_version"`
	Ports         map[string]portProfile `json:"ports"` // key: "ext.port" e.g. "2.4"
}

// ----------------------------------------------------------------------------
// Disk layout
// ----------------------------------------------------------------------------

// stateFilePath returns the absolute path to state.json, respecting
// XDG_CONFIG_HOME. Falls back to "./pikvm-state.json" if we can't resolve a
// home directory (unusual, but we never want loading to crash).
func stateFilePath() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "pikvm", "state.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "pikvm-state.json")
	}
	return filepath.Join(home, ".config", "pikvm", "state.json")
}

// ----------------------------------------------------------------------------
// Load / save
// ----------------------------------------------------------------------------

var stateMu sync.Mutex

// loadState reads state.json. Missing file -> returns an empty state (not an
// error). Malformed JSON -> returns empty state but prints a warning to
// stderr so the user can investigate without losing the running session.
func loadState() pikvmState {
	empty := pikvmState{SchemaVersion: 1, Ports: map[string]portProfile{}}

	stateMu.Lock()
	defer stateMu.Unlock()

	path := stateFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return empty
		}
		fmt.Fprintf(os.Stderr, "warning: could not read %s: %v (continuing with empty state)\n", path, err)
		return empty
	}
	var s pikvmState
	if err := json.Unmarshal(data, &s); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s is not valid JSON: %v (continuing with empty state; the file will not be overwritten until you fix it)\n", path, err)
		return empty
	}
	if s.Ports == nil {
		s.Ports = map[string]portProfile{}
	}
	if s.SchemaVersion == 0 {
		s.SchemaVersion = 1
	}
	return s
}

// saveState writes state.json atomically (tmpfile + rename) with mode 0600.
// Creates parent dirs as needed.
func saveState(s pikvmState) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	path := stateFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}

	if s.SchemaVersion == 0 {
		s.SchemaVersion = 1
	}
	if s.Ports == nil {
		s.Ports = map[string]portProfile{}
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

// ----------------------------------------------------------------------------
// Convenience helpers
// ----------------------------------------------------------------------------

// getPortProfile returns the profile for the given 1-based ext.port id
// (e.g. "2.4"). Returns an empty profile (all zero fields) if none is saved
// — callers should treat that as "no customization".
func getPortProfile(s pikvmState, extPort string) portProfile {
	if s.Ports == nil {
		return portProfile{}
	}
	return s.Ports[extPort]
}

// setPortProfile writes the profile under the given ext.port id, creating
// the Ports map if needed. If the profile is fully empty, the entry is
// removed so we don't accumulate dead keys.
func setPortProfile(s pikvmState, extPort string, p portProfile) pikvmState {
	if s.Ports == nil {
		s.Ports = map[string]portProfile{}
	}
	if isEmptyProfile(p) {
		delete(s.Ports, extPort)
	} else {
		s.Ports[extPort] = p
	}
	return s
}

func isEmptyProfile(p portProfile) bool {
	return p.Name == "" && p.BIOSKey == "" && p.DefaultISO == "" &&
		len(p.Tags) == 0 && p.TailscaleName == "" && p.SSHUser == "" && p.Notes == ""
}

// resolvePortName maps a profile name (e.g. "j4yn0") to its ext.port id
// (e.g. "2.4"). Case-insensitive. Returns ok=false if no profile matches.
func resolvePortName(s pikvmState, name string) (string, bool) {
	lower := strings.ToLower(name)
	for id, p := range s.Ports {
		if strings.ToLower(p.Name) == lower {
			return id, true
		}
	}
	return "", false
}

// portExtID formats a 1-based ext.port id from a linear 0-based port given
// the topology (ports per extender).
func portExtID(linear, portsPerExt int) string {
	if portsPerExt <= 0 {
		portsPerExt = 1
	}
	ext := linear/portsPerExt + 1
	pno := linear%portsPerExt + 1
	return fmt.Sprintf("%d.%d", ext, pno)
}

// parseExtID parses a "2.3" form into a linear 0-based port given topology.
// Returns (linear, true) on success; (0, false) on bad format or out-of-range.
func parseExtID(id string, portsPerExt int) (int, bool) {
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

// sortedPortIDs returns the ext.port keys sorted naturally (1.1, 1.2, ...,
// 2.1, 2.2, ...). Handy for deterministic CLI output.
func sortedPortIDs(s pikvmState) []string {
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
