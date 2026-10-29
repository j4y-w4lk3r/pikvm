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

// SnapshotStream keeps the PiKVM streamer awake so repeated frames are cheap.
// Open it once (port switch + warmup), call Grab as often as needed, Close
// when done. Tours poll through one stream instead of paying the warmup per
// frame.
type SnapshotStream struct {
	ctx        context.Context
	cancel     context.CancelFunc
	stopKeeper context.CancelFunc
	quality    int
}

// OpenSnapshotStream switches to port and waits for the streamer to produce
// frames. The stream lives until Close or until ctx ends.
func OpenSnapshotStream(ctx context.Context, port int, quality int) (*SnapshotStream, error) {
	if quality <= 0 {
		quality = 85
	}
	_ = FetchSwitchState()
	if err := SetSwitchPort(port); err != nil {
		return nil, fmt.Errorf("switch port: %w", err)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	s := &SnapshotStream{ctx: streamCtx, cancel: cancel, quality: quality}
	s.stopKeeper = startStreamKeeper(streamCtx)
	select {
	case <-time.After(streamWarmup()):
	case <-streamCtx.Done():
		s.Close()
		return nil, streamCtx.Err()
	}
	return s, nil
}

// Grab fetches the current frame as JPEG.
func (s *SnapshotStream) Grab() ([]byte, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	qs := url.Values{}
	qs.Set("save", "1")
	qs.Set("preview_quality", strconv.Itoa(s.quality))
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

// Close stops the stream keeper.
func (s *SnapshotStream) Close() {
	if s.stopKeeper != nil {
		s.stopKeeper()
	}
	s.cancel()
}

// SnapshotJPEG captures one JPEG frame from the PiKVM streamer for port.
func SnapshotJPEG(port int, quality int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := OpenSnapshotStream(ctx, port, quality)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	return s.Grab()
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
