package main

import (
	"testing"
)

// TestSnapshotFetch confirms fetchSnapshotJPEG returns a non-empty JPEG
// from the live PiKVM. Skips cleanly when config is missing.
func TestSnapshotFetch(t *testing.T) {
	if err := loadEnv(); err != nil {
		t.Skipf("config missing, skipping: %v", err)
	}

	jpeg, err := fetchSnapshotJPEG()
	if err != nil {
		t.Fatalf("fetchSnapshotJPEG: %v", err)
	}
	if len(jpeg) < 1024 {
		t.Fatalf("snapshot suspiciously small: %d bytes", len(jpeg))
	}

	// JPEG files start with 0xFF 0xD8 0xFF
	if jpeg[0] != 0xFF || jpeg[1] != 0xD8 || jpeg[2] != 0xFF {
		t.Errorf("expected JPEG magic bytes, got % X", jpeg[:3])
	}
	t.Logf("snapshot OK: %d bytes", len(jpeg))
}
