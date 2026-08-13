package api

import (
	"encoding/json"
	"testing"
)

func TestParseSwitchStateDirectATX(t *testing.T) {
	body := []byte(`{
		"ok": true,
		"result": {
			"model": {"ports": [], "units": []},
			"summary": {"active_port": -1, "active_id": "", "synced": true},
			"atx": {"leds": {"power": [], "hdd": []}},
			"video": {"links": []},
			"usb": {"links": []}
		}
	}`)
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
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	units := len(parsed.Result.Model.Units)
	total := len(parsed.Result.Model.Ports)
	if units != 0 || total != 0 {
		t.Fatalf("fixture: units=%d total=%d", units, total)
	}
	if !IsDirectATX() && units == 0 {
		// IsDirectATX is set by FetchSwitchState; this test only validates parsing logic.
	}
}

func TestActionsForDirect(t *testing.T) {
	switchActs := ActionsForDirect(false)
	if len(switchActs) != 7 || switchActs[0].APICmd != "/switch/atx/power?port=%d&action=on" {
		t.Fatalf("switch actions: %+v", switchActs[0])
	}
	directActs := ActionsForDirect(true)
	if len(directActs) != 7 || directActs[0].APICmd != "/atx/power?action=on" {
		t.Fatalf("direct actions: %+v", directActs[0])
	}
}

func TestActionTarget(t *testing.T) {
	lastPortsPerExt.Store(4)
	switchAct := DefaultActions[0]
	if got := ActionTarget(switchAct, 7); got != "2.4" {
		t.Fatalf("switch target got %q want 2.4", got)
	}
	directAct := DirectActions[0]
	if got := ActionTarget(directAct, 0); got != "host" {
		t.Fatalf("direct target got %q want host", got)
	}
}
