package api

import (
	"testing"
	"time"

	"pikvm/internal/config"
)

// TestISOCache calls FetchAvailableISOEntries three times against the live
// PiKVM and prints timing + cache behaviour. Skips if config is missing.
func TestISOCache(t *testing.T) {
	if err := config.Load(); err != nil {
		t.Skipf("config missing, skipping: %v", err)
	}

	t.Log("--- cold call (no cache)")
	start := time.Now()
	entries, err := FetchAvailableISOEntries()
	coldDur := time.Since(start)
	if err != nil {
		t.Fatalf("cold fetch: %v", err)
	}
	t.Logf("  got %d entries in %v", len(entries), coldDur)

	t.Log("--- warm call (cached)")
	start = time.Now()
	entries2, err := FetchAvailableISOEntries()
	warmDur := time.Since(start)
	if err != nil {
		t.Fatalf("warm fetch: %v", err)
	}
	t.Logf("  got %d entries in %v", len(entries2), warmDur)

	if warmDur > coldDur/10 {
		t.Errorf("cache does not appear to be hit: warm=%v, cold=%v", warmDur, coldDur)
	}
	if len(entries2) != len(entries) {
		t.Errorf("cached result differs: cold=%d, warm=%d", len(entries), len(entries2))
	}

	t.Log("--- invalidate + call again (cold again)")
	InvalidateISOCache()
	start = time.Now()
	entries3, err := FetchAvailableISOEntries()
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
