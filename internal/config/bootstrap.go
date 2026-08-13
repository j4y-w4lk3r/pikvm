package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pikvm/internal/onepassword"
	"pikvm/internal/tailscale"
)

// DiscoverEntry is one host row written to the discovery log (no secrets).
type DiscoverEntry struct {
	Name          string `json:"name"`
	TailscaleName string `json:"tailscale_name,omitempty"`
	IP            string `json:"ip,omitempty"`
	Online        bool   `json:"online"`
	User          string `json:"user,omitempty"`
	Vault         string `json:"vault,omitempty"`
	Status        string `json:"status"` // ok, offline, skipped
	Note          string `json:"note,omitempty"`
}

// LastDiscoverSummary is populated by the most recent bootstrap/sync pass.
var LastDiscoverSummary struct {
	At      time.Time
	Entries []DiscoverEntry
	Source  string
	Error   string
}

var bootstrapFromOP = defaultBootstrapFromOP

func defaultBootstrapFromOP() (BootstrapResult, error) {
	if os.Getenv("PIKVM_SKIP_OP") == "1" {
		return BootstrapResult{}, fmt.Errorf("1password bootstrap disabled (PIKVM_SKIP_OP=1)")
	}
	if !onepassword.Available() {
		return BootstrapResult{}, fmt.Errorf("op CLI not found in PATH")
	}
	creds, err := onepassword.ListPiKVMHosts()
	if err != nil {
		return BootstrapResult{}, err
	}
	status, err := tailscale.ParseStatusPeers(nil)
	if err != nil {
		return BootstrapResult{}, err
	}
	peers := tailscale.PiKVMPeersDetailed(status, true)
	return mergeBootstrap(creds, peers, loadExistingHostsSnapshot()), nil
}

// BootstrapResult is the output of a 1Password + tailscale bootstrap pass.
type BootstrapResult struct {
	Hosts   map[string]HostConfig
	Entries []DiscoverEntry
}

func mergeBootstrap(creds []onepassword.HostCreds, peers []tailscale.PiKVMPeer, existing map[string]HostConfig) BootstrapResult {
	peerByName := make(map[string]tailscale.PiKVMPeer, len(peers))
	for _, p := range peers {
		peerByName[p.ConfigName] = p
	}

	hosts := make(map[string]HostConfig, len(creds))
	var entries []DiscoverEntry

	for _, c := range creds {
		entry := DiscoverEntry{
			Name:   c.Name,
			User:   c.User,
			Vault:  c.Vault,
			Status: "ok",
		}
		p, ok := peerByName[c.Name]
		if ok {
			entry.TailscaleName = p.Hostname
			entry.IP = p.IPv4
			entry.Online = p.Online
			hosts[c.Name] = HostConfig{
				Host:          p.IPv4,
				User:          c.User,
				Pass:          c.Pass,
				TailscaleName: p.Hostname,
			}
			entries = append(entries, entry)
			continue
		}

		if old, hasOld := existing[c.Name]; hasOld && old.Host != "" {
			entry.TailscaleName = old.TailscaleName
			entry.IP = old.Host
			entry.Online = false
			entry.Status = "offline"
			entry.Note = "using cached IP (not online on tailnet)"
			hosts[c.Name] = HostConfig{
				Host:          old.Host,
				User:          c.User,
				Pass:          c.Pass,
				TailscaleName: old.TailscaleName,
			}
			entries = append(entries, entry)
			continue
		}

		entry.Status = "skipped"
		entry.Note = "no online tailnet peer and no cached IP"
		entries = append(entries, entry)
	}

	return BootstrapResult{Hosts: hosts, Entries: entries}
}

func loadExistingHostsSnapshot() map[string]HostConfig {
	path := configFilePath
	if path == "" {
		path = resolvePath("config.json")
	}
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw struct {
		Hosts            map[string]HostConfig `json:"hosts"`
		TailscaleAuthKey string                `json:"TAILSCALE_AUTH_KEY"`
		UbuntuPassword   string                `json:"UBUNTU_PASSWORD"`
	}
	if err := json.Unmarshal(data, &raw); err != nil || len(raw.Hosts) == 0 {
		return nil
	}
	if TailscaleAuthKey == "" {
		TailscaleAuthKey = raw.TailscaleAuthKey
	}
	if UbuntuPassword == "" {
		UbuntuPassword = raw.UbuntuPassword
	}
	out := make(map[string]HostConfig, len(raw.Hosts))
	for k, v := range raw.Hosts {
		out[k] = v
	}
	return out
}

// RefreshFromOnePassword rebuilds hosts from 1Password + tailscale and
// rewrites config.json. Called from Load() and `pikvm hosts sync`.
func RefreshFromOnePassword() error {
	return tryBootstrapFromOnePassword()
}

func tryBootstrapFromOnePassword() error {
	res, err := bootstrapFromOP()
	LastDiscoverSummary.At = time.Now()
	LastDiscoverSummary.Source = "1password+tailscale"
	LastDiscoverSummary.Error = ""
	LastDiscoverSummary.Entries = res.Entries

	if err != nil {
		LastDiscoverSummary.Error = err.Error()
		writeDiscoverLog(LastDiscoverSummary)
		return err
	}
	if len(res.Hosts) == 0 {
		err = fmt.Errorf("1password bootstrap found no usable hosts")
		LastDiscoverSummary.Error = err.Error()
		writeDiscoverLog(LastDiscoverSummary)
		return err
	}

	Hosts = res.Hosts
	SchemaVersion = 2
	ensureConfigFilePath()
	if err := persistHostsToConfig(); err != nil {
		LastDiscoverSummary.Error = "persist: " + err.Error()
	}
	writeDiscoverLog(LastDiscoverSummary)
	return nil
}

func ensureConfigFilePath() {
	if configFilePath != "" {
		return
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		configFilePath = filepath.Join(xdg, "pikvm", "config.json")
	} else if home, err := os.UserHomeDir(); err == nil {
		configFilePath = filepath.Join(home, ".config", "pikvm", "config.json")
	}
	if configFilePath != "" {
		_ = os.MkdirAll(filepath.Dir(configFilePath), 0o700)
	}
}

func discoverLogPath() string {
	if configFilePath != "" {
		return filepath.Join(filepath.Dir(configFilePath), "discover.log")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "pikvm", "discover.log")
	}
	return "discover.log"
}

func writeDiscoverLog(summary struct {
	At      time.Time
	Entries []DiscoverEntry
	Source  string
	Error   string
}) {
	path := discoverLogPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o700)

	var b strings.Builder
	b.WriteString(summary.At.Format(time.RFC3339))
	b.WriteString("  source=")
	b.WriteString(summary.Source)
	if summary.Error != "" {
		b.WriteString("  error=")
		b.WriteString(summary.Error)
	}
	b.WriteByte('\n')
	for _, e := range summary.Entries {
		fmt.Fprintf(&b, "  %-8s  tailscale=%-12s  ip=%-15s  online=%-5v  user=%-8s  vault=%s  status=%s",
			e.Name, e.TailscaleName, e.IP, e.Online, e.User, e.Vault, e.Status)
		if e.Note != "" {
			b.WriteString("  # ")
			b.WriteString(e.Note)
		}
		b.WriteByte('\n')
	}
	b.WriteByte('\n')

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(b.String())
}

// DiscoverLogPath returns the path to the append-only discovery log.
func DiscoverLogPath() string { return discoverLogPath() }
