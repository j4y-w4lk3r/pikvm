// Periodic /api/info poller (roadmap idea #10).
//
// PiKVM exposes /api/info with the host's kvmd version, kernel info, hostname,
// uptime, and basic health metrics (CPU/memory/temperature). This data does
// NOT push over /api/ws, so we poll every InfoPollInterval.

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// InfoPollInterval is the gap between /api/info fetches.
const InfoPollInterval = 30 * time.Second

// InfoState is the slice of /api/info we care about for the status bar.
type InfoState struct {
	Hostname    string  `json:"hostname"`
	KvmdVersion string  `json:"kvmd_version"`
	Platform    string  `json:"platform"`
	UptimeTotal int64   `json:"uptime_total"`
	UptimeDays  int     `json:"uptime_days"`
	UptimeHours int     `json:"uptime_hours"`
	UptimeMins  int     `json:"uptime_mins"`
	CPUPercent  int     `json:"cpu_percent"`
	MemPercent  float64 `json:"mem_percent"`
	CPUTempC    float64 `json:"cpu_temp_c"`
}

// InfoMsg is the Bubble Tea message produced by the poller.
type InfoMsg struct{ State InfoState }

// StartInfoPoller runs the /api/info fetcher in its own goroutine. First
// fetch fires immediately, then every InfoPollInterval.
func StartInfoPoller(ctx context.Context, prog *tea.Program) {
	go func() {
		fire := func() {
			if state, ok := FetchInfoState(); ok {
				prog.Send(InfoMsg{State: state})
			}
		}
		fire()
		ticker := time.NewTicker(InfoPollInterval)
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

// FetchInfoState calls /api/info once and returns the parsed slice we care
// about. Returns ok=false on any error so the caller can skip the Send.
func FetchInfoState() (InfoState, bool) {
	resp, err := Do("GET", "/info", nil, 5*time.Second)
	if err != nil {
		return InfoState{}, false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return InfoState{}, false
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
		return InfoState{}, false
	}

	return InfoState{
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

// FormatUptime turns the parts into a compact "8d23h" / "3h27m" / "47s" form.
func FormatUptime(s InfoState) string {
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
