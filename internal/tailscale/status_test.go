package tailscale

import "testing"

const sampleStatusJSON = `{
  "Peer": {
    "node1": {
      "HostName": "j4ypikvm0",
      "TailscaleIPs": ["100.64.0.2", "fd7a:115c:a1e0::2"]
    },
    "node2": {
      "HostName": "j4ypikvm2",
      "TailscaleIPs": ["100.64.0.6"]
    },
    "node3": {
      "HostName": "pikvm2",
      "TailscaleIPs": ["100.64.0.7"]
    },
    "node4": {
      "HostName": "nas",
      "TailscaleIPs": ["100.64.0.5"]
    }
  }
}`

func TestParseStatusJSON(t *testing.T) {
	peers, err := ParseStatusJSON([]byte(sampleStatusJSON))
	if err != nil {
		t.Fatal(err)
	}
	if got := peers["j4ypikvm0"]; got != "100.64.0.2" {
		t.Fatalf("j4ypikvm0 IP = %q, want 100.64.0.2", got)
	}
	if got := peers["pikvm2"]; got != "100.64.0.7" {
		t.Fatalf("pikvm2 IP = %q, want 100.64.0.7", got)
	}
}

func TestResolveIPv4(t *testing.T) {
	peers, err := ParseStatusJSON([]byte(sampleStatusJSON))
	if err != nil {
		t.Fatal(err)
	}
	ip, ok := ResolveIPv4("pikvm2.tail.d0j0.dev.", peers)
	if !ok || ip != "100.64.0.7" {
		t.Fatalf("ResolveIPv4 fqdn = %q ok=%v, want 100.64.0.7 true", ip, ok)
	}
}

func TestConfigNameFromPeer(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"j4ypikvm0", "pikvm1", true},
		{"j4ypikvm2", "pikvm3", true},
		{"pikvm2", "pikvm2", true},
		{"nas", "", false},
	}
	for _, c := range cases {
		got, ok := ConfigNameFromPeer(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("ConfigNameFromPeer(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestPiKVMPeers(t *testing.T) {
	peers, err := ParseStatusJSON([]byte(sampleStatusJSON))
	if err != nil {
		t.Fatal(err)
	}
	found := PiKVMPeers(peers)
	if len(found) != 3 {
		t.Fatalf("len(PiKVMPeers) = %d, want 3", len(found))
	}
	if found[0].ConfigName != "pikvm1" || found[0].IPv4 != "100.64.0.2" {
		t.Fatalf("first peer = %+v, want pikvm1/100.64.0.2", found[0])
	}
}
