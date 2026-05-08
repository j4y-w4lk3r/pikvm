// TUI bindings for roadmap idea #20 — event hooks.
//
// Live PiKVM events stream into Update() via WebSocket messages; this
// file converts those into hook dispatches so user-defined scripts in
// ~/.config/pikvm/hooks.d/ can react to them. All dispatches are
// asynchronous (hooks.Dispatch spawns goroutines) so the TUI never
// blocks on a slow hook.
package tui

import (
	"strconv"

	"pikvm/internal/api"
	"pikvm/internal/config"
	"pikvm/internal/hooks"
	"pikvm/internal/state"
)

// fireHook is the TUI-side wrapper around hooks.Dispatch. Every event
// gets the current host's connection details for free so hook scripts
// can include them without having to load config.json themselves.
func fireHook(event string, kv map[string]string) {
	if kv == nil {
		kv = map[string]string{}
	}
	kv["host_name"] = config.HostName
	kv["host"] = config.Host
	kv["user"] = config.User
	hooks.Dispatch(event, kv)
}

// firePortChanged compares old vs new active port and fires
// `port-changed` once per actual transition. Profile names (if known)
// are looked up at dispatch time so hooks can switch on machine name.
func firePortChanged(prev, next int, portsPerExt int) {
	if prev == next {
		return
	}
	st := state.Load()
	prevID := state.PortExtID(prev, portsPerExt)
	nextID := state.PortExtID(next, portsPerExt)
	prevProfile := state.GetProfile(st, prevID)
	nextProfile := state.GetProfile(st, nextID)
	fireHook("port-changed", map[string]string{
		"port":      nextID,
		"port_id":   strconv.Itoa(next),
		"prev_port": prevID,
		"name":      nextProfile.Name,
		"prev_name": prevProfile.Name,
	})
}

// firePowerTransitions emits per-port power-on / power-off events for
// every port whose state flipped between the old and new SwitchMsg.
// Sized-mismatch slices fall through silently — first-event seeding does
// not fire spurious events.
func firePowerTransitions(prev, next []bool, portsPerExt int) {
	if len(prev) == 0 || len(prev) != len(next) {
		return
	}
	st := state.Load()
	for i := range prev {
		if prev[i] == next[i] {
			continue
		}
		event := "power-off"
		if next[i] {
			event = "power-on"
		}
		id := state.PortExtID(i, portsPerExt)
		fireHook(event, map[string]string{
			"port":    id,
			"port_id": strconv.Itoa(i),
			"name":    state.GetProfile(st, id).Name,
		})
	}
}

// fireMsdTransitions emits msd-mounted / msd-unmounted (drive
// physically attached/detached to the booted machine), plus
// iso-upload-finished when an upload completes.
func fireMsdTransitions(prev, next api.MsdMsg) {
	if prev.Connected != next.Connected {
		ev := "msd-unmounted"
		if next.Connected {
			ev = "msd-mounted"
		}
		fireHook(ev, map[string]string{
			"online":    boolStr(next.Online),
			"connected": boolStr(next.Connected),
		})
	}
	if prev.Uploading && !next.Uploading {
		fireHook("iso-upload-finished", map[string]string{
			"name":      prev.UploadName,
			"connected": boolStr(next.Connected),
		})
	}
}

// fireClientsChanged emits a clients-changed event when the connected
// browser/TUI/CLI count changes. Useful for "someone else just opened
// the web UI" notifications.
func fireClientsChanged(prev, next int) {
	if prev == next {
		return
	}
	fireHook("clients-changed", map[string]string{
		"count":      strconv.Itoa(next),
		"prev_count": strconv.Itoa(prev),
	})
}

// LogPath re-exports the hooks log path for `pikvm hooks logs`.
func LogPath() string { return hooks.LogPath() }

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
