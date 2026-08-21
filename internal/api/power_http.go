package api

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PowerBackend describes how to power-cycle a machine on a KVM port when PiKVM
// ATX is not wired (e.g. Mac mini via ESP32 relay).
type PowerBackend struct {
	Type        string `json:"type,omitempty"` // "http" | "" (ATX via PiKVM)
	OnURL       string `json:"on_url,omitempty"`
	OffURL      string `json:"off_url,omitempty"`
	ClickURL    string `json:"click_url,omitempty"`
	LongURL     string `json:"long_url,omitempty"`
	HoldURL     string `json:"hold_url,omitempty"` // optional: sustained press, e.g. http://esp/hold?sec=12
	HoldSec     int    `json:"hold_recovery_sec,omitempty"` // default hold duration for Recovery boot
	Method      string `json:"method,omitempty"`
	CooldownSec int    `json:"cooldown_sec,omitempty"`
}

func (p *PowerBackend) UsesHTTP() bool {
	return p != nil && strings.EqualFold(p.Type, "http") && (p.OnURL != "" || p.OffURL != "" || p.ClickURL != "" || p.LongURL != "")
}

func (p *PowerBackend) urlForAction(action string) (string, error) {
	if p == nil {
		return "", fmt.Errorf("no power backend configured")
	}
	switch action {
	case "on":
		if p.OnURL != "" {
			return p.OnURL, nil
		}
		if p.ClickURL != "" {
			return p.ClickURL, nil
		}
	case "off":
		if p.OffURL != "" {
			return p.OffURL, nil
		}
		if p.LongURL != "" {
			return p.LongURL, nil
		}
	case "click":
		if p.ClickURL != "" {
			return p.ClickURL, nil
		}
		if p.OnURL != "" {
			return p.OnURL, nil
		}
	case "long":
		if p.LongURL != "" {
			return p.LongURL, nil
		}
		if p.OffURL != "" {
			return p.OffURL, nil
		}
	}
	return "", fmt.Errorf("no URL for power action %q", action)
}

// ExecuteHTTPPower triggers an HTTP power endpoint (ESP32 relay, smart plug, …).
func ExecuteHTTPPower(p *PowerBackend, action string) string {
	url, err := p.urlForAction(action)
	if err != nil {
		return fmt.Sprintf("\uf057 %v", err)
	}
	method := strings.ToUpper(strings.TrimSpace(p.Method))
	if method == "" {
		method = http.MethodGet
	}
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return fmt.Sprintf("\uf057 %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("\uf057 HTTP power %s: %v", action, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Sprintf("\uf00c Power %s via HTTP (%s)", action, msg)
	}
	return fmt.Sprintf("\uf057 HTTP power %s: %s (%s)", action, resp.Status, strings.TrimSpace(string(body)))
}

// ExecuteHTTPPowerHold triggers a sustained power-button hold for Apple Silicon Recovery boot.
// Uses HoldURL if set (supports {sec} placeholder), otherwise LongURL with extended timeout.
func ExecuteHTTPPowerHold(p *PowerBackend, holdSec int) string {
	if p == nil {
		return "\uf057 no power backend"
	}
	if holdSec <= 0 {
		holdSec = p.HoldSec
	}
	if holdSec <= 0 {
		holdSec = 12 // Apple Silicon: ~10s until "Loading startup options"
	}
	rawURL := strings.TrimSpace(p.HoldURL)
	if rawURL == "" {
		rawURL = strings.TrimSpace(p.LongURL)
	}
	if rawURL == "" {
		return "\uf057 no hold_url or long_url configured"
	}
	target := strings.ReplaceAll(rawURL, "{sec}", strconv.Itoa(holdSec))

	method := strings.ToUpper(strings.TrimSpace(p.Method))
	if method == "" {
		method = http.MethodGet
	}
	// Relay may stay closed for holdSec — allow extra margin for HTTP round-trip.
	client := &http.Client{Timeout: time.Duration(holdSec+20) * time.Second}
	req, err := http.NewRequest(method, target, nil)
	if err != nil {
		return fmt.Sprintf("\uf057 %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("\uf057 HTTP power hold %ds: %v", holdSec, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Sprintf("\uf00c Power hold %ds via HTTP (%s)", holdSec, msg)
	}
	return fmt.Sprintf("\uf057 HTTP power hold: %s (%s)", resp.Status, strings.TrimSpace(string(body)))
}
