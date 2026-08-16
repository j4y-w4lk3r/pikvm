package api

import (
	"testing"

	"pikvm/internal/state"
)

func TestActionNameToHTTP(t *testing.T) {
	cases := []struct {
		name   string
		action string
		ok     bool
	}{
		{"Power Click", "click", true},
		{"Power Long Press", "long", true},
		{"Reset Click", "", false},
	}
	for _, c := range cases {
		got, ok := ActionNameToHTTP(c.name)
		if ok != c.ok || (c.ok && got != c.action) {
			t.Fatalf("ActionNameToHTTP(%q) = %q, %v; want %q, %v", c.name, got, ok, c.action, c.ok)
		}
	}
}

func TestPowerBackendFromProfile(t *testing.T) {
	pb := PowerBackendFromProfile(state.PortProfile{
		Power: &state.PowerBackend{Type: "http", ClickURL: "http://x/short"},
	})
	if pb == nil || !pb.UsesHTTP() {
		t.Fatal("expected HTTP backend")
	}
	if PowerBackendFromProfile(state.PortProfile{}) != nil {
		t.Fatal("empty profile should have no backend")
	}
}
