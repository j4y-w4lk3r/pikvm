package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStateRoundtrip writes a profile, reads it back, resolves by name, and
// confirms atomic-rename + empty-profile pruning. No live PiKVM needed.
func TestStateRoundtrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Empty state on fresh dir.
	s := loadState()
	if len(s.Ports) != 0 {
		t.Fatalf("expected empty ports on fresh state, got %d", len(s.Ports))
	}

	// Save a profile.
	s = setPortProfile(s, "2.3", portProfile{
		Name:    "j4yn0",
		BIOSKey: "F7",
		Tags:    []string{"nas", "prod"},
	})
	if err := saveState(s); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	// File must exist with mode 0600 and under $XDG_CONFIG_HOME/pikvm/.
	path := filepath.Join(tmp, "pikvm", "state.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("state.json not created: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected mode 0600, got %o", info.Mode().Perm())
	}

	// Reload and confirm the profile survived.
	s2 := loadState()
	p := getPortProfile(s2, "2.3")
	if p.Name != "j4yn0" || p.BIOSKey != "F7" {
		t.Errorf("roundtrip mismatch: %+v", p)
	}
	if len(p.Tags) != 2 || p.Tags[0] != "nas" || p.Tags[1] != "prod" {
		t.Errorf("tags roundtrip wrong: %v", p.Tags)
	}

	// Name resolution (case-insensitive).
	if id, ok := resolvePortName(s2, "J4YN0"); !ok || id != "2.3" {
		t.Errorf("case-insensitive name resolution failed: %q ok=%v", id, ok)
	}
	if _, ok := resolvePortName(s2, "doesnotexist"); ok {
		t.Error("unknown name should not resolve")
	}

	// Unsetting (empty profile) should prune the entry.
	s2 = setPortProfile(s2, "2.3", portProfile{})
	if _, present := s2.Ports["2.3"]; present {
		t.Error("empty profile should be removed, got entry still present")
	}
}

func TestPortExtIDRoundtrip(t *testing.T) {
	for linear := 0; linear < 12; linear++ {
		id := portExtID(linear, 4)
		back, ok := parseExtID(id, 4)
		if !ok || back != linear {
			t.Errorf("roundtrip failed: linear=%d -> %q -> (%d, ok=%v)", linear, id, back, ok)
		}
	}
}

func TestIsLikelyName(t *testing.T) {
	cases := []struct {
		in   string
		name bool
	}{
		{"5", false},
		{"2.3", false},
		{"j4yn0", true}, // contains letters even though it has digits
		{"web03", true},
		{"my-host", true},
		{"my_host", true},
		{"", false},
	}
	for _, c := range cases {
		if got := isLikelyName(c.in); got != c.name {
			t.Errorf("isLikelyName(%q) = %v, want %v", c.in, got, c.name)
		}
	}
}
