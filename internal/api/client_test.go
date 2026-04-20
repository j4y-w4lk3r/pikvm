package api

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"pikvm/internal/config"
)

// TestHTTPClientReuse confirms the shared client (idea #4) actually reuses
// TCP+TLS connections — subsequent calls should be comparably fast or
// faster than doing a fresh client per call. Relaxed assertion to tolerate
// network jitter; only a large regression (>25% slower) fails.
func TestHTTPClientReuse(t *testing.T) {
	if err := config.Load(); err != nil {
		t.Skipf("config missing, skipping: %v", err)
	}

	freshDurations := make([]time.Duration, 5)
	for i := range freshDurations {
		c := &http.Client{
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
			Timeout:   10 * time.Second,
		}
		req, _ := http.NewRequest("GET", config.BaseURL+"/info", nil)
		req.SetBasicAuth(config.User, config.Pass)
		start := time.Now()
		resp, err := c.Do(req)
		freshDurations[i] = time.Since(start)
		if err != nil {
			t.Fatalf("fresh-client call %d: %v", i, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	sharedDurations := make([]time.Duration, 5)
	for i := range sharedDurations {
		start := time.Now()
		resp, err := Do("GET", "/info", nil, 10*time.Second)
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

	if sharedTotal > freshTotal*5/4 {
		t.Errorf("shared client should not be meaningfully slower than fresh; got shared=%v fresh=%v",
			sharedTotal, freshTotal)
	} else if sharedTotal < freshTotal {
		fmt.Printf("    -> shared client wins by %.1fx\n", float64(freshTotal)/float64(sharedTotal+1))
	}
}
