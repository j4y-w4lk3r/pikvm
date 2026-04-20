package state

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

	s := Load()
	if len(s.Ports) != 0 {
		t.Fatalf("expected empty ports on fresh state, got %d", len(s.Ports))
	}

	s = SetProfile(s, "2.3", PortProfile{
		Name:    "j4yn0",
		BIOSKey: "F7",
		Tags:    []string{"nas", "prod"},
	})
	if err := Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(tmp, "pikvm", "state.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("state.json not created: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected mode 0600, got %o", info.Mode().Perm())
	}

	s2 := Load()
	p := GetProfile(s2, "2.3")
	if p.Name != "j4yn0" || p.BIOSKey != "F7" {
		t.Errorf("roundtrip mismatch: %+v", p)
	}
	if len(p.Tags) != 2 || p.Tags[0] != "nas" || p.Tags[1] != "prod" {
		t.Errorf("tags roundtrip wrong: %v", p.Tags)
	}

	if id, ok := ResolveName(s2, "J4YN0"); !ok || id != "2.3" {
		t.Errorf("case-insensitive name resolution failed: %q ok=%v", id, ok)
	}
	if _, ok := ResolveName(s2, "doesnotexist"); ok {
		t.Error("unknown name should not resolve")
	}

	s2 = SetProfile(s2, "2.3", PortProfile{})
	if _, present := s2.Ports["2.3"]; present {
		t.Error("empty profile should be removed, got entry still present")
	}
}

func TestPortExtIDRoundtrip(t *testing.T) {
	for linear := 0; linear < 12; linear++ {
		id := PortExtID(linear, 4)
		back, ok := ParseExtID(id, 4)
		if !ok || back != linear {
			t.Errorf("roundtrip failed: linear=%d -> %q -> (%d, ok=%v)", linear, id, back, ok)
		}
	}
}
