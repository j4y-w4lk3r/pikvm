package api

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Action is one ATX/MSD operation surfaced in the TUI's [O] Operations menu.
type Action struct {
	Name   string
	Desc   string
	APICmd string // e.g. "/switch/atx/power?port=%d&action=on" or "SPECIAL:disconnect_drive"
	Method string // HTTP method
}

// DefaultActions is the built-in list shown in the [O] Operations menu on
// PiKVM hosts with a KVM switch (multi-port extenders).
var DefaultActions = []Action{
	{"Power ON", "Turn power on", "/switch/atx/power?port=%d&action=on", "POST"},
	{"Power OFF", "Turn power off", "/switch/atx/power?port=%d&action=off", "POST"},
	{"Power Click", "Power button short press", "/switch/atx/click?port=%d&button=power", "POST"},
	{"Power Long Press", "Force shutdown", "/switch/atx/click?port=%d&button=power_long", "POST"},
	{"Reset Click", "Reset button short press", "/switch/atx/click?port=%d&button=reset", "POST"},
	{"Reset Long Press", "Reset button long press", "/switch/atx/click?port=%d&button=reset_long", "POST"},
	{"Disconnect Drive", "Disconnect virtual USB drive", "SPECIAL:disconnect_drive", "POST"},
}

// DirectActions is the operations list for PiKVM V3-style hosts with a
// single built-in ATX header and no KVM switch (/api/switch reports no units).
var DirectActions = []Action{
	{"Power ON", "Turn power on", "/atx/power?action=on", "POST"},
	{"Power OFF", "Turn power off", "/atx/power?action=off", "POST"},
	{"Power Click", "Power button short press", "/atx/click?button=power", "POST"},
	{"Power Long Press", "Force shutdown", "/atx/click?button=power_long", "POST"},
	{"Reset Click", "Reset button short press", "/atx/click?button=reset", "POST"},
	{"Reset Long Press", "Reset button long press", "/atx/power?action=reset_hard", "POST"},
	{"Disconnect Drive", "Disconnect virtual USB drive", "SPECIAL:disconnect_drive", "POST"},
}

// ActionsForDirect returns the appropriate action list for the host topology.
func ActionsForDirect(direct bool) []Action {
	if direct {
		return DirectActions
	}
	return DefaultActions
}

// PowerOnAction returns the Power ON action for the current host topology.
func PowerOnAction() Action {
	return ActionsForDirect(IsDirectATX())[0]
}

// HostLabel returns a user-facing target label for status messages.
func HostLabel(port int) string {
	if IsDirectATX() {
		return "host"
	}
	return FormatPort(port)
}

// ActionTarget returns the target string used in ExecuteAction success text.
func ActionTarget(act Action, port int) string {
	if !strings.Contains(act.APICmd, "%d") {
		return "host"
	}
	return FormatPort(port)
}

// ExecuteAction runs act against the given port. Returns a human-readable
// status string suitable for display in the TUI result area.
func ExecuteAction(act Action, port int) string {
	if strings.HasPrefix(act.APICmd, "SPECIAL:") {
		specialCmd := strings.TrimPrefix(act.APICmd, "SPECIAL:")
		switch specialCmd {
		case "disconnect_drive":
			if err := ConnectMSD(false); err != nil {
				return fmt.Sprintf("\uf057 Failed to disconnect drive: %v", err)
			}
			return "\uf00c Virtual USB drive disconnected from server!"
		default:
			return fmt.Sprintf("\uf057 Unknown special command: %s", specialCmd)
		}
	}

	endpoint := act.APICmd
	if strings.Contains(endpoint, "%d") {
		endpoint = fmt.Sprintf(endpoint, port)
	}
	resp, err := Do(act.Method, endpoint, nil, 0)
	if err != nil {
		return fmt.Sprintf("\uf057 Error: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("\uf057 Error reading response: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Sprintf("✓ Response: %s", string(body))
	}
	if ok, exists := result["ok"].(bool); exists && ok {
		return fmt.Sprintf("\uf00c Success: %s executed on %s", act.Name, ActionTarget(act, port))
	}
	return fmt.Sprintf("\uf071 Response: %s", string(body))
}
