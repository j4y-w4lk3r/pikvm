package api

import (
	"fmt"
	"io"
	"net/http"
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
