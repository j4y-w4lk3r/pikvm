// Package tailscale resolves tailnet peer names to IPv4 addresses via the
// local tailscale CLI (Headscale-compatible — same wire format as upstream).
package tailscale

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// PeerMap returns a lookup from hostname (lowercase) to the peer's primary
// IPv4 tailnet address. Keys include bare HostName values as reported by
// `tailscale status --json` (e.g. "j4ypikvm0", "pikvm2").
func PeerMap() (map[string]string, error) {
	peers, err := ParseStatusPeers(nil)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(peers))
	for _, p := range peers {
		if p.IPv4 != "" {
			m[strings.ToLower(p.Hostname)] = p.IPv4
		}
	}
	return m, nil
}

// ParseStatusPeers returns every tailnet peer with IPv4 and online state.
// If data is nil, runs `tailscale status --json`.
func ParseStatusPeers(data []byte) ([]StatusPeer, error) {
	if data == nil {
		out, err := exec.Command("tailscale", "status", "--json").Output()
		if err != nil {
			return nil, fmt.Errorf("tailscale status: %w", err)
		}
		data = out
	}
	var raw struct {
		Peer map[string]struct {
			HostName     string   `json:"HostName"`
			TailscaleIPs []string `json:"TailscaleIPs"`
			Online       bool     `json:"Online"`
		} `json:"Peer"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse tailscale status json: %w", err)
	}
	out := make([]StatusPeer, 0, len(raw.Peer))
	for _, p := range raw.Peer {
		ip := firstIPv4(p.TailscaleIPs)
		if ip == "" || p.HostName == "" {
			continue
		}
		out = append(out, StatusPeer{
			Hostname: p.HostName,
			IPv4:     ip,
			Online:   p.Online,
		})
	}
	return out, nil
}

// StatusPeer is one node from `tailscale status --json`.
type StatusPeer struct {
	Hostname string
	IPv4     string
	Online   bool
}

// ParseStatusJSON builds a hostname→IPv4 map from `tailscale status --json`
// output. Exported for unit tests.
func ParseStatusJSON(data []byte) (map[string]string, error) {
	return PeerMapFromBytes(data)
}

func PeerMapFromBytes(data []byte) (map[string]string, error) {
	peers, err := ParseStatusPeers(data)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(peers))
	for _, p := range peers {
		m[strings.ToLower(p.Hostname)] = p.IPv4
	}
	return m, nil
}

// ResolveIPv4 looks up name in peers. Accepts bare hostnames or MagicDNS
// FQDNs (suffix is stripped before lookup).
func ResolveIPv4(name string, peers map[string]string) (string, bool) {
	key := normalizeHostname(name)
	if key == "" {
		return "", false
	}
	ip, ok := peers[key]
	return ip, ok
}

func normalizeHostname(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, ".")
	// MagicDNS FQDN: pikvm2.tail.d0j0.dev → pikvm2
	if i := strings.Index(name, "."); i > 0 {
		name = name[:i]
	}
	return strings.ToLower(name)
}

func firstIPv4(ips []string) string {
	for _, s := range ips {
		if ip := net.ParseIP(s); ip != nil && ip.To4() != nil {
			return s
		}
	}
	return ""
}
