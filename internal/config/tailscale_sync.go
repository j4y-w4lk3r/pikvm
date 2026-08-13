package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"pikvm/internal/tailscale"
)

// SyncResult describes what changed during a tailscale sync pass.
type SyncResult struct {
	Updated []string // host names whose IP changed
	Skipped string   // non-empty when sync could not run (e.g. tailscale down)
}

// peerLookupFunc is swappable in tests.
var peerLookupFunc = tailscale.PeerMap

// configFilePath is set when config.json is loaded successfully.
var configFilePath string

// ConfigFilePath returns the absolute path to the loaded config.json, or "".
func ConfigFilePath() string { return configFilePath }

// SyncTailscaleHosts resolves tailscale_name (or hostname-style host fields)
// to current tailnet IPv4 addresses for hosts already in config. Called
// automatically from Load(). Does not add new hosts — only pikvm1, pikvm2,
// … entries you define in config.json are kept.
func SyncTailscaleHosts() SyncResult {
	peers, err := peerLookupFunc()
	if err != nil {
		return SyncResult{Skipped: err.Error()}
	}
	return applyTailscalePeers(peers, true)
}

func applyTailscalePeers(peers map[string]string, persist bool) SyncResult {
	res := SyncResult{}
	if len(Hosts) == 0 {
		return res
	}

	for name, h := range Hosts {
		tsName := h.TailscaleName
		if tsName == "" && !isIPv4(h.Host) {
			tsName = h.Host
		}
		if tsName == "" {
			continue
		}
		ip, ok := tailscale.ResolveIPv4(tsName, peers)
		if !ok {
			continue
		}
		if ip != h.Host {
			h.Host = ip
			if h.TailscaleName == "" {
				h.TailscaleName = strings.TrimSuffix(tsName, ".")
				if i := strings.Index(h.TailscaleName, "."); i > 0 {
					h.TailscaleName = h.TailscaleName[:i]
				}
			}
			Hosts[name] = h
			res.Updated = append(res.Updated, name)
		}
	}

	sort.Strings(res.Updated)

	if persist && configFilePath != "" && len(res.Updated) > 0 {
		if err := persistHostsToConfig(); err != nil {
			res.Skipped = fmt.Sprintf("persist: %v", err)
		}
	}
	return res
}

func isIPv4(s string) bool {
	ip := net.ParseIP(strings.TrimSpace(s))
	return ip != nil && ip.To4() != nil
}

func persistHostsToConfig() error {
	if configFilePath == "" {
		ensureConfigFilePath()
	}
	if configFilePath == "" {
		return fmt.Errorf("no config path")
	}
	_ = os.MkdirAll(filepath.Dir(configFilePath), 0o700)

	raw := map[string]interface{}{"schema_version": 2}
	if data, err := os.ReadFile(configFilePath); err == nil {
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
	}
	if TailscaleAuthKey != "" {
		raw["TAILSCALE_AUTH_KEY"] = TailscaleAuthKey
	}
	if UbuntuPassword != "" {
		raw["UBUNTU_PASSWORD"] = UbuntuPassword
	}

	hostsOut := make(map[string]interface{}, len(Hosts))
	for _, name := range HostNames() {
		h := Hosts[name]
		entry := map[string]interface{}{
			"host": h.Host,
			"user": h.User,
			"pass": h.Pass,
		}
		if h.TailscaleName != "" {
			entry["tailscale_name"] = h.TailscaleName
		}
		hostsOut[name] = entry
	}
	raw["hosts"] = hostsOut
	delete(raw, "default")
	if _, ok := raw["schema_version"]; !ok {
		raw["schema_version"] = 2
	}

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	tmp := configFilePath + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, configFilePath)
}

// normalizeHosts renames legacy "default" to pikvm1 and drops unknown keys
// that are not pikvmN when a numbered pikvm host already exists.
func normalizeHosts() {
	if h, ok := Hosts["default"]; ok {
		if _, exists := Hosts["pikvm1"]; !exists {
			Hosts["pikvm1"] = h
		}
		delete(Hosts, "default")
	}
}
