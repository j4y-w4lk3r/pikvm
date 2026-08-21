package api

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// SnapshotJPEG captures one JPEG frame from the PiKVM streamer for port.
func SnapshotJPEG(port int, quality int) ([]byte, error) {
	if quality <= 0 {
		quality = 85
	}
	_ = FetchSwitchState()
	if err := SetSwitchPort(port); err != nil {
		return nil, fmt.Errorf("switch port: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	stop := startStreamKeeper(ctx)
	defer stop()

	select {
	case <-time.After(streamWarmup()):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	qs := url.Values{}
	qs.Set("save", "1")
	qs.Set("preview_quality", strconv.Itoa(quality))
	endpoint := "/streamer/snapshot?" + qs.Encode()

	resp, err := Do("GET", endpoint, nil, 60*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("snapshot HTTP %d (%d bytes)", resp.StatusCode, len(body))
	}
	if len(body) < 500 {
		return nil, fmt.Errorf("snapshot too small (%d bytes)", len(body))
	}
	return body, nil
}

// SaveSnapshot writes one JPEG to path (creates parent dirs).
func SaveSnapshot(port int, quality int, path string) (int, error) {
	data, err := SnapshotJPEG(port, quality)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return 0, err
	}
	SessionLog("snapshot", map[string]interface{}{
		"path": path, "bytes": len(data), "port": port,
	})
	return len(data), nil
}
