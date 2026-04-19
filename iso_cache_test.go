package main

import (
	"testing"
	"time"
)

// TestISOCache calls fetchAvailableISOEntries three times against the live
// PiKVM and prints timing + cache behaviour. It uses the real env (same
// baseURL/creds pikvm.go uses) so it needs a reachable PiKVM.
//
// Run:  go test -v -run TestISOCache ./...
func TestISOCache(t *testing.T) {
	if err := loadEnv(); err != nil {
		t.Skipf(".env missing, skipping: %v", err)
	}

	// First call: cold (no cache)
	t.Log("--- cold call (no cache)")
	start := time.Now()
	entries, err := fetchAvailableISOEntries()
	coldDur := time.Since(start)
	if err != nil {
		t.Fatalf("cold fetch: %v", err)
	}
	t.Logf("  got %d entries in %v", len(entries), coldDur)

	// Second call: should hit cache instantly
	t.Log("--- warm call (cached)")
	start = time.Now()
	entries2, err := fetchAvailableISOEntries()
	warmDur := time.Since(start)
	if err != nil {
		t.Fatalf("warm fetch: %v", err)
	}
	t.Logf("  got %d entries in %v", len(entries2), warmDur)

	if warmDur > coldDur/10 {
		t.Errorf("cache does not appear to be hit: warm=%v, cold=%v (expected warm << cold)", warmDur, coldDur)
	}
	if len(entries2) != len(entries) {
		t.Errorf("cached result differs: cold=%d entries, warm=%d entries", len(entries), len(entries2))
	}

	// Invalidate and confirm next call refetches
	t.Log("--- invalidate + call again (cold again)")
	invalidateISOCache()
	start = time.Now()
	entries3, err := fetchAvailableISOEntries()
	coldAgainDur := time.Since(start)
	if err != nil {
		t.Fatalf("invalidated fetch: %v", err)
	}
	t.Logf("  got %d entries in %v", len(entries3), coldAgainDur)

	if coldAgainDur < warmDur {
		t.Errorf("expected post-invalidate to be slower than cached: post=%v, cached=%v", coldAgainDur, warmDur)
	}

	t.Logf("=== summary ===")
	t.Logf("  cold:         %v", coldDur)
	t.Logf("  warm (cache): %v   (%.0fx faster)", warmDur, float64(coldDur)/float64(warmDur+1))
	t.Logf("  post-invalid: %v", coldAgainDur)
}
