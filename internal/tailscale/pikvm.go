package tailscale

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	pikvmPeerRe   = regexp.MustCompile(`(?i)^pikvm(\d+)$`)
	j4yPikvmPeerRe = regexp.MustCompile(`(?i)^j4ypikvm(\d+)$`)
)

// IsPiKVMPeer reports whether hostname looks like a PiKVM tailnet node.
func IsPiKVMPeer(hostname string) bool {
	h := normalizeHostname(hostname)
	return pikvmPeerRe.MatchString(h) || j4yPikvmPeerRe.MatchString(h)
}

// ConfigNameFromPeer maps a tailnet hostname to a friendly config host key.
//
//   - pikvm2      → pikvm2
//   - j4ypikvm0   → pikvm1  (j4ypikvm uses zero-based numbering)
//   - j4ypikvm2   → pikvm3
func ConfigNameFromPeer(hostname string) (string, bool) {
	h := normalizeHostname(hostname)
	if m := pikvmPeerRe.FindStringSubmatch(h); len(m) == 2 {
		return "pikvm" + m[1], true
	}
	if m := j4yPikvmPeerRe.FindStringSubmatch(h); len(m) == 2 {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return "", false
		}
		return "pikvm" + strconv.Itoa(n+1), true
	}
	return "", false
}

// PiKVMPeers returns every PiKVM-looking peer in stable sorted order
// (by config name, then hostname). When requireOnline is true, offline
// peers are skipped.
func PiKVMPeers(peers map[string]string) []PiKVMPeer {
	return PiKVMPeersFromMap(peers, nil)
}

// PiKVMPeersDetailed maps tailnet peers to pikvmN config slots.
func PiKVMPeersDetailed(status []StatusPeer, requireOnline bool) []PiKVMPeer {
	ipMap := make(map[string]string, len(status))
	online := make(map[string]bool, len(status))
	for _, p := range status {
		key := strings.ToLower(p.Hostname)
		ipMap[key] = p.IPv4
		online[key] = p.Online
	}
	return PiKVMPeersFromMap(ipMap, online, requireOnline)
}

func PiKVMPeersFromMap(peers map[string]string, online map[string]bool, requireOnline ...bool) []PiKVMPeer {
	skipOffline := len(requireOnline) > 0 && requireOnline[0]
	found := make(map[string]PiKVMPeer)
	for hostname, ip := range peers {
		if skipOffline && online != nil && !online[strings.ToLower(hostname)] {
			continue
		}
		cfgName, ok := ConfigNameFromPeer(hostname)
		if !ok {
			continue
		}
		p := PiKVMPeer{
			ConfigName: cfgName,
			Hostname:   hostname,
			IPv4:       ip,
			Online:     online == nil || online[strings.ToLower(hostname)],
		}
		if existing, dup := found[cfgName]; !dup || hostname < existing.Hostname {
			found[cfgName] = p
		}
	}
	out := make([]PiKVMPeer, 0, len(found))
	for _, p := range found {
		out = append(out, p)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if strings.Compare(out[j].ConfigName, out[i].ConfigName) < 0 {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// PiKVMPeer is one discovered PiKVM node on the tailnet.
type PiKVMPeer struct {
	ConfigName string
	Hostname   string
	IPv4       string
	Online     bool
}
