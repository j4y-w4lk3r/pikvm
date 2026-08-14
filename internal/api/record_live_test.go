//go:build live

package api

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/coder/websocket"

	"pikvm/internal/config"
)

func TestLiveRecordVideoPikvm2(t *testing.T) {
	if err := config.Load(); err != nil {
		t.Skip(err)
	}
	if err := config.UseHost("pikvm2"); err != nil {
		t.Skip(err)
	}

	out := t.TempDir() + "/test.mp4"
	res, err := RecordVideo(context.Background(), 5*time.Second, out, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("path=%s bytes=%d direct_atx=%v", res.Path, res.Bytes, IsDirectATX())
	if res.Bytes < 10_000 {
		t.Fatalf("expected substantial video data, got %d bytes", res.Bytes)
	}
}

func TestLiveRecordRawH264(t *testing.T) {
	if err := config.Load(); err != nil {
		t.Skip(err)
	}
	if err := config.UseHost("pikvm2"); err != nil {
		t.Skip(err)
	}
	_ = FetchSwitchState()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stop := startStreamKeeper(ctx)
	defer stop()
	time.Sleep(streamWarmup())

	conn, err := dialMediaWS(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	out := t.TempDir() + "/clip.h264"
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var total int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		chunk, err := readVideoChunk(ctx, conn)
		if err != nil {
			break
		}
		n, _ := f.Write(chunk)
		total += n
	}
	t.Logf("raw h264 bytes=%d path=%s direct_atx=%v", total, out, IsDirectATX())
	if total < 10_000 {
		t.Fatalf("expected raw h264 data")
	}
}

func TestLiveSnapshotAfterKeeper(t *testing.T) {
	if err := config.Load(); err != nil {
		t.Skip(err)
	}
	if err := config.UseHost("pikvm2"); err != nil {
		t.Skip(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stop := startStreamKeeper(ctx)
	defer stop()

	for _, wait := range []time.Duration{1 * time.Second, 2 * time.Second, 3 * time.Second, 5 * time.Second} {
		time.Sleep(wait)
		resp, err := Do("GET", "/streamer/snapshot?save=1", nil, 10*time.Second)
		if err != nil {
			t.Logf("wait=%s do err: %v", wait, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Logf("wait=%s status=%d bytes=%d", wait, resp.StatusCode, len(body))
		if resp.StatusCode == 200 && len(body) > 1000 {
			return
		}
	}
	t.Fatal("snapshot never became available")
}

func TestLiveMediaWSBytes(t *testing.T) {
	if err := config.Load(); err != nil {
		t.Skip(err)
	}
	if err := config.UseHost("pikvm2"); err != nil {
		t.Skip(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stop := startStreamKeeper(ctx)
	defer stop()
	time.Sleep(2 * time.Second)

	conn, err := dialMediaWS(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()

	var total int
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Logf("read err after %d bytes: %v", total, err)
			break
		}
		total += len(data)
		if len(data) > 0 {
			t.Logf("chunk %d total %d", len(data), total)
		}
	}
	t.Logf("TOTAL bytes=%d direct_atx=%v", total, IsDirectATX())
	if total < 1000 {
		t.Fatalf("expected h264 data")
	}
}
