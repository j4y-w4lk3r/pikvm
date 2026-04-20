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

// DefaultActions is the built-in list shown in the [O] Operations menu.
var DefaultActions = []Action{
	{"Power ON", "Turn power on", "/switch/atx/power?port=%d&action=on", "POST"},
	{"Power OFF", "Turn power off", "/switch/atx/power?port=%d&action=off", "POST"},
	{"Power Click", "Power button short press", "/switch/atx/click?port=%d&button=power", "POST"},
	{"Power Long Press", "Force shutdown", "/switch/atx/click?port=%d&button=power_long", "POST"},
	{"Reset Click", "Reset button short press", "/switch/atx/click?port=%d&button=reset", "POST"},
	{"Reset Long Press", "Reset button long press", "/switch/atx/click?port=%d&button=reset_long", "POST"},
	{"Disconnect Drive", "Disconnect virtual USB drive", "SPECIAL:disconnect_drive", "POST"},
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

	endpoint := fmt.Sprintf(act.APICmd, port)
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
		return fmt.Sprintf("\uf00c Success: %s executed on port %s", act.Name, FormatPort(port))
	}
	return fmt.Sprintf("\uf071 Response: %s", string(body))
}
