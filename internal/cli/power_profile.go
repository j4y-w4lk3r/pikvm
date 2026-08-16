package cli

import (
	"fmt"
	"strings"

	"pikvm/internal/api"
	"pikvm/internal/state"
)

func profilePowerBackend(p state.PortProfile) *api.PowerBackend {
	return api.PowerBackendFromProfile(p)
}

func powerBackendForPort(port int) (*api.PowerBackend, string, state.PortProfile) {
	pb, extID := api.PowerBackendForPort(port)
	if pb != nil {
		st := state.Load()
		return pb, extID, state.GetProfile(st, extID)
	}
	extID = ""
	if port >= 0 {
		sw := api.FetchSwitchState()
		extID = state.PortExtID(port, sw.PortsPerExt)
		st := state.Load()
		return nil, extID, state.GetProfile(st, extID)
	}
	return nil, extID, state.PortProfile{}
}

func runPowerAction(jsonMode bool, command, action string, args []string) {
	port := 0
	if len(args) > 0 {
		p, err := parsePort(args[0])
		if err != nil {
			die(jsonMode, command, err)
		}
		port = p
	}

	pb, extID, profile := powerBackendForPort(port)
	if pb != nil {
		result := api.ExecuteHTTPPower(pb, action)
		api.SessionLog("power_"+action, map[string]interface{}{
			"port":    extID,
			"machine": profile.Name,
			"backend": "http",
		})
		ok := strings.Contains(result, "\uf00c") || strings.Contains(result, "Success")
		if jsonMode {
			if ok {
				emit(true, command, map[string]interface{}{
					"action":  action,
					"port":    extID,
					"machine": profile.Name,
					"backend": "http",
				}, nil)
			} else {
				emit(false, command, nil, fmt.Errorf("%s", stripIcons(result)))
			}
		} else {
			fmt.Println(result)
		}
		return
	}

	var actName string
	switch action {
	case "on":
		actName = "Power ON"
	case "off":
		actName = "Power OFF"
	case "click":
		actName = "Power Click"
	case "long":
		actName = "Power Long Press"
	default:
		die(jsonMode, command, fmt.Errorf("unknown power action %q", action))
	}
	runActionByName(jsonMode, command, actName, args)
}

func ensureProfilePower(p *state.PortProfile) {
	if p.Power == nil {
		p.Power = &state.PowerBackend{}
	}
}

func applyProfilePowerKey(p *state.PortProfile, k, v string) error {
	ensureProfilePower(p)
	switch k {
	case "power_type":
		p.Power.Type = v
	case "power_on_url":
		p.Power.OnURL = v
	case "power_off_url":
		p.Power.OffURL = v
	case "power_click_url":
		p.Power.ClickURL = v
	case "power_long_url":
		p.Power.LongURL = v
	case "power_method":
		p.Power.Method = v
	case "power_cooldown_sec":
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
			return fmt.Errorf("power_cooldown_sec: %w", err)
		}
		p.Power.CooldownSec = n
	default:
		return fmt.Errorf("unknown key %q", k)
	}
	if p.Power.IsEmpty() {
		p.Power = nil
	}
	return nil
}

func formatProfilePower(p state.PortProfile) []string {
	if p.Power == nil || p.Power.IsEmpty() {
		return nil
	}
	var bits []string
	if p.Power.Type != "" {
		bits = append(bits, "power_type="+p.Power.Type)
	}
	if p.Power.OnURL != "" {
		bits = append(bits, "power_on_url="+p.Power.OnURL)
	}
	if p.Power.OffURL != "" {
		bits = append(bits, "power_off_url="+p.Power.OffURL)
	}
	if p.Power.ClickURL != "" {
		bits = append(bits, "power_click_url="+p.Power.ClickURL)
	}
	if p.Power.LongURL != "" {
		bits = append(bits, "power_long_url="+p.Power.LongURL)
	}
	return bits
}
