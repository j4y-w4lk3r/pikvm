package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

// TestHTTPClientReuse confirms the shared client (idea #4) actually reuses
// TCP+TLS connections — second call should be measurably faster than the
// first against the live PiKVM. Skips if .env / config.json is missing.
//
// Run:  go test -v -run TestHTTPClientReuse ./...
func TestHTTPClientReuse(t *testing.T) {
	if err := loadEnv(); err != nil {
		t.Skipf("config missing, skipping: %v", err)
	}

	// --- Baseline: simulate the OLD pattern (fresh client per request) -----
	freshDurations := make([]time.Duration, 5)
	for i := range freshDurations {
		c := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
			Timeout: 10 * time.Second,
		}
		req, _ := http.NewRequest("GET", baseURL+"/info", nil)
		req.SetBasicAuth(pikvmUser, pikvmPass)
		start := time.Now()
		resp, err := c.Do(req)
		freshDurations[i] = time.Since(start)
		if err != nil {
			t.Fatalf("fresh-client call %d: %v", i, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	// --- New pattern: shared client (this is what production uses now) -----
	sharedDurations := make([]time.Duration, 5)
	for i := range sharedDurations {
		start := time.Now()
		resp, err := pikvmDo("GET", "/info", nil, 10*time.Second)
		sharedDurations[i] = time.Since(start)
		if err != nil {
			t.Fatalf("shared-client call %d: %v", i, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	report := func(label string, ds []time.Duration) (total time.Duration) {
		for i, d := range ds {
			t.Logf("  %s call %d: %v", label, i+1, d)
			total += d
		}
		t.Logf("  %s TOTAL: %v", label, total)
		return total
	}

	t.Log("--- fresh client per call (the OLD way) ---")
	freshTotal := report("fresh", freshDurations)

	t.Log("--- shared client (idea #4) ---")
	sharedTotal := report("shared", sharedDurations)

	t.Logf("=== summary ===")
	t.Logf("  fresh:  %v", freshTotal)
	t.Logf("  shared: %v   (%.1fx faster)", sharedTotal, float64(freshTotal)/float64(sharedTotal+1))

	// On macOS<->Pi over Tailscale, TLS handshake is ~10-30 ms. On 5 fresh
	// calls vs 5 shared we typically see ~4-5x improvement. Be generous in
	// the assertion to avoid network-flake false-failures.
	if sharedTotal >= freshTotal {
		t.Errorf("shared client should be faster than fresh client; got shared=%v >= fresh=%v",
			sharedTotal, freshTotal)
	} else {
		fmt.Printf("    -> shared client wins by %.1fx\n", float64(freshTotal)/float64(sharedTotal+1))
	}
}
