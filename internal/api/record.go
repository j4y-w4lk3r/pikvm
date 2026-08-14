package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/coder/websocket"

	"pikvm/internal/config"
)

// RecordResult describes a finished screen recording.
type RecordResult struct {
	Path     string
	Duration time.Duration
	Bytes    int64
}

// RecordVideo captures duration seconds of the PiKVM HDMI H.264 stream into
// outputPath (MP4). port is passed to SetSwitchPort when a KVM switch exists.
func RecordVideo(ctx context.Context, duration time.Duration, outputPath string, port int) (RecordResult, error) {
	if duration <= 0 {
		duration = 10 * time.Second
	}
	if outputPath == "" {
		return RecordResult{}, fmt.Errorf("output path required")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return RecordResult{}, fmt.Errorf("ffmpeg not found in PATH (install the ffmpeg package)")
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return RecordResult{}, fmt.Errorf("create output dir: %w", err)
	}

	_ = SetSwitchPort(port)

	recCtx, cancel := context.WithTimeout(ctx, duration+20*time.Second)
	defer cancel()

	stopKeeper := startStreamKeeper(recCtx)
	defer stopKeeper()

	// Give the streamer a moment to spin up after the keeper connects.
	select {
	case <-time.After(750 * time.Millisecond):
	case <-recCtx.Done():
		return RecordResult{}, recCtx.Err()
	}

	conn, err := dialMediaWS(recCtx)
	if err != nil {
		return RecordResult{}, err
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "record done") }()

	cmd := exec.CommandContext(recCtx, "ffmpeg",
		"-y",
		"-hide_banner", "-loglevel", "error",
		"-f", "h264",
		"-framerate", "30",
		"-probesize", "10M",
		"-analyzeduration", "5M",
		"-fflags", "nobuffer",
		"-flags", "low_delay",
		"-i", "pipe:0",
		"-t", fmt.Sprintf("%.3f", duration.Seconds()),
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "23",
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		outputPath,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return RecordResult{}, fmt.Errorf("ffmpeg stdin: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return RecordResult{}, fmt.Errorf("ffmpeg start: %w", err)
	}

	deadline := time.Now().Add(duration)
	pumpDone := make(chan struct{})
	go func() {
		defer close(pumpDone)
		defer func() { _ = stdin.Close() }()
		for time.Now().Before(deadline) {
			readCtx, readCancel := context.WithDeadline(recCtx, deadline)
			_, data, err := conn.Read(readCtx)
			readCancel()
			if err != nil {
				return
			}
			if _, err := stdin.Write(data); err != nil {
				return
			}
		}
	}()

	<-pumpDone
	if err := cmd.Wait(); err != nil {
		return RecordResult{}, fmt.Errorf("ffmpeg encode: %w", err)
	}

	fi, err := os.Stat(outputPath)
	if err != nil {
		return RecordResult{}, fmt.Errorf("stat output: %w", err)
	}
	if fi.Size() == 0 {
		return RecordResult{}, fmt.Errorf("recording is empty (no video data from PiKVM?)")
	}

	return RecordResult{
		Path:     outputPath,
		Duration: duration,
		Bytes:    fi.Size(),
	}, nil
}

func dialMediaWS(ctx context.Context) (*websocket.Conn, error) {
	httpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	headers := http.Header{}
	headers.Set("X-KVMD-User", config.User)
	headers.Set("X-KVMD-Passwd", config.Pass)

	mediaURL := fmt.Sprintf("wss://%s/api/media/ws?video=h264", config.Host)
	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dialCancel()

	conn, _, err := websocket.Dial(dialCtx, mediaURL, &websocket.DialOptions{
		HTTPClient: httpClient,
		HTTPHeader: headers,
	})
	if err != nil {
		return nil, fmt.Errorf("video stream: %w", err)
	}
	conn.SetReadLimit(16 * 1024 * 1024)
	return conn, nil
}

// startStreamKeeper keeps /api/ws?stream=1 open so the HDMI streamer stays up.
func startStreamKeeper(ctx context.Context) context.CancelFunc {
	keeperCtx, cancel := context.WithCancel(ctx)
	go func() {
		httpClient := &http.Client{
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		}
		headers := http.Header{}
		headers.Set("X-KVMD-User", config.User)
		headers.Set("X-KVMD-Passwd", config.Pass)

		url := fmt.Sprintf("wss://%s/api/ws?stream=1", config.Host)
		conn, _, err := websocket.Dial(keeperCtx, url, &websocket.DialOptions{
			HTTPClient: httpClient,
			HTTPHeader: headers,
		})
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "keeper done") }()
		for {
			if _, _, err := conn.Read(keeperCtx); err != nil {
				return
			}
		}
	}()
	return cancel
}
