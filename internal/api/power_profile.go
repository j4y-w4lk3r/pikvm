package api

import (
	"strings"

	"pikvm/internal/state"
)

// PowerBackendFromProfile maps a state.PortProfile power block to api.PowerBackend.
func PowerBackendFromProfile(p state.PortProfile) *PowerBackend {
	if p.Power == nil || p.Power.IsEmpty() {
		return nil
	}
	return &PowerBackend{
		Type:        p.Power.Type,
		OnURL:       p.Power.OnURL,
		OffURL:      p.Power.OffURL,
		ClickURL:    p.Power.ClickURL,
		LongURL:     p.Power.LongURL,
		Method:      p.Power.Method,
		CooldownSec: p.Power.CooldownSec,
	}
}

// PowerBackendForPort returns the HTTP power backend for a linear port index, if any.
func PowerBackendForPort(port int) (*PowerBackend, string) {
	if port < 0 {
		return nil, ""
	}
	sw := FetchSwitchState()
	extID := state.PortExtID(port, sw.PortsPerExt)
	st := state.Load()
	p := state.GetProfile(st, extID)
	if pb := PowerBackendFromProfile(p); pb != nil && pb.UsesHTTP() {
		return pb, extID
	}
	return nil, extID
}

// PortUsesHTTPPower reports whether the port has an HTTP power backend (ESP32, smart plug, …).
func PortUsesHTTPPower(port int) bool {
	pb, _ := PowerBackendForPort(port)
	return pb != nil
}

// ActionNameToHTTP maps TUI/ATX action labels to HTTP power action keys.
func ActionNameToHTTP(name string) (string, bool) {
	switch name {
	case "Power ON":
		return "on", true
	case "Power OFF":
		return "off", true
	case "Power Click":
		return "click", true
	case "Power Long Press":
		return "long", true
	default:
		return "", false
	}
}

// ExecuteActionPower tries HTTP power for act on port. Returns (result, true) if handled.
func ExecuteActionPower(act Action, port int) (string, bool) {
	if action, ok := ActionNameToHTTP(act.Name); ok {
		if pb, extID := PowerBackendForPort(port); pb != nil {
			result := ExecuteHTTPPower(pb, action)
			SessionLog("power_"+action, map[string]interface{}{
				"port": extID, "backend": "http", "via": "tui",
			})
			if strings.Contains(result, "\uf00c") {
				target := extID
				if target == "" {
					target = FormatPort(port)
				}
				return strings.Replace(result, "Power "+action+" via HTTP", act.Name+" via HTTP ("+target+")", 1), true
			}
			return result, true
		}
	}
	return "", false
}
