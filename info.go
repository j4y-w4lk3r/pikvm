// Periodic /api/info poller (roadmap idea #10).
//
// PiKVM exposes /api/info with the host's kvmd version, kernel info, hostname,
// uptime, and basic health metrics (CPU/memory/temperature). Unlike /api/switch,
// this data does NOT push over /api/ws — the WS "info" event only carries
// auth.enabled. So we poll periodically.
//
// The poller runs in a single goroutine alongside startWebSocket, fires the
// initial fetch immediately, then re-fetches every infoPollInterval.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const infoPollInterval = 30 * time.Second

// infoState is the slice of /api/info we care about for the status bar.
type infoState struct {
	Hostname     string  // meta.server.host       (e.g. "j4ypi0")
	KvmdVersion  string  // system.kvmd.version    (e.g. "4.127")
	Platform     string  // hw.platform.model      (e.g. "v4plus")
	UptimeTotal  int64   // uptime.total           (seconds)
	UptimeDays   int     // uptime.parts.days
	UptimeHours  int     // uptime.parts.hours
	UptimeMins   int     // uptime.parts.minutes
	CPUPercent   int     // hw.health.cpu.percent
	MemPercent   float64 // hw.health.mem.percent
	CPUTempC     float64 // hw.health.temp.cpu     (Celsius)
}

// wsInfoMsg is the Bubble Tea message produced by the poller. Reusing the
// 'ws' prefix keeps the message-type naming consistent with WS-driven msgs
// even though /api/info itself isn't streamed.
type wsInfoMsg struct{ state infoState }

// startInfoPoller runs the /api/info fetcher in its own goroutine.
// The first fetch fires immediately so the status bar is populated within
// ~1s of TUI launch; subsequent fetches every infoPollInterval.
func startInfoPoller(ctx context.Context, prog *tea.Program) {
	go func() {
		fire := func() {
			if state, ok := fetchInfoState(ctx); ok {
				prog.Send(wsInfoMsg{state: state})
			}
		}
		fire()
		ticker := time.NewTicker(infoPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fire()
			}
		}
	}()
}

// fetchInfoState calls /api/info once and returns the parsed slice we care
// about. Returns ok=false on any error so the caller can skip the Send.
func fetchInfoState(ctx context.Context) (infoState, bool) {
	resp, err := pikvmDo("GET", "/info", nil, 5*time.Second)
	if err != nil {
		return infoState{}, false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return infoState{}, false
	}

	var parsed struct {
		OK     bool `json:"ok"`
		Result struct {
			System struct {
				Kvmd struct {
					Version string `json:"version"`
				} `json:"kvmd"`
			} `json:"system"`
			Meta struct {
				Server struct {
					Host string `json:"host"`
				} `json:"server"`
			} `json:"meta"`
			Uptime struct {
				Total int64 `json:"total"`
				Parts struct {
					Days    int `json:"days"`
					Hours   int `json:"hours"`
					Minutes int `json:"minutes"`
					Seconds int `json:"seconds"`
				} `json:"parts"`
			} `json:"uptime"`
			HW struct {
				Platform struct {
					Model string `json:"model"`
				} `json:"platform"`
				Health struct {
					CPU struct {
						Percent int `json:"percent"`
					} `json:"cpu"`
					Mem struct {
						Percent float64 `json:"percent"`
					} `json:"mem"`
					Temp struct {
						CPU float64 `json:"cpu"`
					} `json:"temp"`
				} `json:"health"`
			} `json:"hw"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || !parsed.OK {
		return infoState{}, false
	}

	return infoState{
		Hostname:    parsed.Result.Meta.Server.Host,
		KvmdVersion: parsed.Result.System.Kvmd.Version,
		Platform:    parsed.Result.HW.Platform.Model,
		UptimeTotal: parsed.Result.Uptime.Total,
		UptimeDays:  parsed.Result.Uptime.Parts.Days,
		UptimeHours: parsed.Result.Uptime.Parts.Hours,
		UptimeMins:  parsed.Result.Uptime.Parts.Minutes,
		CPUPercent:  parsed.Result.HW.Health.CPU.Percent,
		MemPercent:  parsed.Result.HW.Health.Mem.Percent,
		CPUTempC:    parsed.Result.HW.Health.Temp.CPU,
	}, true
}

// formatUptime turns the parts into a compact "8d23h" / "3h27m" / "47s" form.
func formatUptime(s infoState) string {
	if s.UptimeDays > 0 {
		return fmt.Sprintf("%dd%dh", s.UptimeDays, s.UptimeHours)
	}
	if s.UptimeHours > 0 {
		return fmt.Sprintf("%dh%dm", s.UptimeHours, s.UptimeMins)
	}
	if s.UptimeMins > 0 {
		return fmt.Sprintf("%dm", s.UptimeMins)
	}
	return fmt.Sprintf("%ds", s.UptimeTotal)
}
