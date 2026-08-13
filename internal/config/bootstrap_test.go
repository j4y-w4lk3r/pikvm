package config

import (
	"testing"

	"pikvm/internal/onepassword"
	"pikvm/internal/tailscale"
)

func TestMergeBootstrapOnline(t *testing.T) {
	creds := []onepassword.HostCreds{
		{Name: "pikvm1", User: "j4y", Pass: "a", Vault: "lab"},
		{Name: "pikvm2", User: "j4y", Pass: "b", Vault: "lab"},
	}
	peers := []tailscale.PiKVMPeer{
		{ConfigName: "pikvm1", Hostname: "j4ypikvm0", IPv4: "100.64.0.2", Online: true},
		{ConfigName: "pikvm2", Hostname: "pikvm2", IPv4: "100.64.0.7", Online: true},
	}
	res := mergeBootstrap(creds, peers, nil)
	if len(res.Hosts) != 2 {
		t.Fatalf("len(hosts)=%d want 2", len(res.Hosts))
	}
	h1 := res.Hosts["pikvm1"]
	if h1.Host != "100.64.0.2" || h1.TailscaleName != "j4ypikvm0" || h1.User != "j4y" {
		t.Fatalf("pikvm1 = %+v", h1)
	}
}

func TestMergeBootstrapOfflineUsesCache(t *testing.T) {
	creds := []onepassword.HostCreds{{Name: "pikvm1", User: "j4y", Pass: "a", Vault: "lab"}}
	existing := map[string]HostConfig{
		"pikvm1": {Host: "100.64.0.2", TailscaleName: "j4ypikvm0", User: "old", Pass: "old"},
	}
	res := mergeBootstrap(creds, nil, existing)
	if len(res.Hosts) != 1 {
		t.Fatal("expected cached offline host")
	}
	if res.Entries[0].Status != "offline" {
		t.Fatalf("status = %q", res.Entries[0].Status)
	}
}
