package config

import (
	"testing"

	"pikvm/internal/tailscale"
)

func TestApplyTailscalePeersUpdatesIP(t *testing.T) {
	Hosts = map[string]HostConfig{
		"pikvm1": {
			Host:          "100.64.0.99",
			User:          "admin",
			Pass:          "secret",
			TailscaleName: "j4ypikvm0",
		},
	}
	peerLookupFunc = func() (map[string]string, error) {
		return map[string]string{"j4ypikvm0": "100.64.0.2"}, nil
	}
	t.Cleanup(func() { peerLookupFunc = tailscale.PeerMap })

	res := applyTailscalePeers(map[string]string{"j4ypikvm0": "100.64.0.2"}, false)
	if len(res.Updated) != 1 || res.Updated[0] != "pikvm1" {
		t.Fatalf("Updated = %v, want [pikvm1]", res.Updated)
	}
	if Hosts["pikvm1"].Host != "100.64.0.2" {
		t.Fatalf("host IP = %q, want 100.64.0.2", Hosts["pikvm1"].Host)
	}
}

func TestApplyTailscalePeersDoesNotAutoAdd(t *testing.T) {
	Hosts = map[string]HostConfig{
		"pikvm1": {
			Host:          "100.64.0.2",
			User:          "admin",
			Pass:          "secret",
			TailscaleName: "j4ypikvm0",
		},
	}
	peers := map[string]string{
		"j4ypikvm0": "100.64.0.2",
		"pikvm2":    "100.64.0.7",
		"j4ypikvm2": "100.64.0.6",
	}
	res := applyTailscalePeers(peers, false)
	if len(Hosts) != 1 {
		t.Fatalf("len(Hosts) = %d, want 1 (no auto-discovery)", len(Hosts))
	}
	if len(res.Updated) != 0 {
		t.Fatalf("Updated = %v, want none", res.Updated)
	}
}

func TestPickInitialHostPrefersPikvm1(t *testing.T) {
	Hosts = map[string]HostConfig{
		"pikvm2": {Host: "100.64.0.7", User: "a", Pass: "b"},
		"pikvm1": {Host: "100.64.0.2", User: "a", Pass: "b"},
	}
	t.Setenv("PIKVM_HOST_NAME", "")
	if got := pickInitialHost(); got != "pikvm1" {
		t.Fatalf("pickInitialHost() = %q, want pikvm1", got)
	}
}

func TestHostNamesNumericOrder(t *testing.T) {
	Hosts = map[string]HostConfig{
		"pikvm10": {Host: "1", User: "a", Pass: "b"},
		"pikvm2":  {Host: "2", User: "a", Pass: "b"},
		"pikvm1":  {Host: "3", User: "a", Pass: "b"},
	}
	names := HostNames()
	want := []string{"pikvm1", "pikvm2", "pikvm10"}
	for i, n := range want {
		if names[i] != n {
			t.Fatalf("HostNames()[%d] = %q, want %q (full: %v)", i, names[i], n, names)
		}
	}
}

func TestNormalizeHostsRenamesDefault(t *testing.T) {
	Hosts = map[string]HostConfig{
		"default": {Host: "100.64.0.2", User: "a", Pass: "b"},
	}
	normalizeHosts()
	if _, ok := Hosts["default"]; ok {
		t.Fatal("default host should be removed")
	}
	if Hosts["pikvm1"].Host != "100.64.0.2" {
		t.Fatalf("pikvm1 = %+v", Hosts["pikvm1"])
	}
}
