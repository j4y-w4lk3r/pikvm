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
	Frames   int
}

// RecordVideo captures duration seconds of the PiKVM HDMI H.264 stream into
// outputPath (MP4). port is passed to SetSwitchPort when a KVM switch exists.
//
// PiKVM V3 hosts often encode at ~1 fps. We capture raw H.264 for the full
// wall-clock window, then mux with ffmpeg using the observed frame rate so the
// MP4 plays for the requested duration (not a sub-second clip).
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

	_ = FetchSwitchState()
	_ = SetSwitchPort(port)

	recCtx, cancel := context.WithTimeout(ctx, duration+45*time.Second)
	defer cancel()

	stopKeeper := startStreamKeeper(recCtx)
	defer stopKeeper()

	warmup := streamWarmup()
	select {
	case <-time.After(warmup):
	case <-recCtx.Done():
		return RecordResult{}, recCtx.Err()
	}

	conn, err := dialMediaWS(recCtx)
	if err != nil {
		return RecordResult{}, err
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "record done") }()

	rawPath := outputPath + ".h264.tmp"
	rawFile, err := os.Create(rawPath)
	if err != nil {
		return RecordResult{}, fmt.Errorf("temp h264: %w", err)
	}

	captureStart := time.Now()
	deadline := captureStart.Add(duration)
	frames := 0
	var pumpErr error
	for time.Now().Before(deadline) {
		readCtx, readCancel := context.WithDeadline(recCtx, deadline)
		chunk, err := readVideoChunk(readCtx, conn)
		readCancel()
		if err != nil {
			if frames > 0 {
				break
			}
			pumpErr = fmt.Errorf("waiting for video: %w", err)
			break
		}
		if len(chunk) == 0 {
			continue
		}
		if _, err := rawFile.Write(chunk); err != nil {
			pumpErr = err
			break
		}
		frames++
	}
	_ = rawFile.Close()
	defer os.Remove(rawPath)

	if pumpErr != nil {
		return RecordResult{}, fmt.Errorf("video capture: %w", pumpErr)
	}
	if frames == 0 {
		return RecordResult{}, fmt.Errorf("no video frames from PiKVM (is HDMI connected and the host displaying?)")
	}

	rawInfo, err := os.Stat(rawPath)
	if err != nil {
		return RecordResult{}, err
	}
	if rawInfo.Size() < 10_000 {
		return RecordResult{}, fmt.Errorf("recording too small (%d bytes) — no usable HDMI video (check host power and display output)", rawInfo.Size())
	}

	inputFPS := captureInputFPS(frames, duration)
	if err := encodeCapturedH264(recCtx, rawPath, outputPath, duration, inputFPS); err != nil {
		return RecordResult{}, err
	}

	outInfo, err := os.Stat(outputPath)
	if err != nil {
		return RecordResult{}, fmt.Errorf("stat output: %w", err)
	}
	if outInfo.Size() < 10_000 {
		_ = os.Remove(outputPath)
		return RecordResult{}, fmt.Errorf("encoded recording too small (%d bytes, %d frames, input_fps=%.3f)", outInfo.Size(), frames, inputFPS)
	}

	playDuration, err := ProbeMP4Duration(outputPath)
	if err != nil {
		return RecordResult{}, fmt.Errorf("probe output: %w", err)
	}
	minDuration := duration - time.Second
	if playDuration < minDuration {
		_ = os.Remove(outputPath)
		return RecordResult{}, fmt.Errorf("recording duration %v, want ~%v (%d frames, input_fps=%.3f)", playDuration, duration, frames, inputFPS)
	}

	return RecordResult{
		Path:     outputPath,
		Duration: duration,
		Bytes:    outInfo.Size(),
		Frames:   frames,
	}, nil
}

func captureInputFPS(frames int, duration time.Duration) float64 {
	if frames <= 0 || duration <= 0 {
		return 1
	}
	// Spread captured frames across the full wall-clock window so playback
	// lasts the requested duration (PiKVM V3 may only deliver ~1 fps).
	fps := float64(frames) / duration.Seconds()
	if fps < 0.05 {
		fps = 0.05
	}
	return fps
}

func encodeCapturedH264(ctx context.Context, rawPath, outputPath string, duration time.Duration, inputFPS float64) error {
	// PiKVM V3 delivers ~1 fps with sparse timestamps in the raw Annex-B
	// bitstream. Re-encoding with libx264 drops duplicate I-frames; muxing
	// with -c:v copy keeps every captured frame but needs -r before -i so
	// ffmpeg assigns timestamps across the full wall-clock window.
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-hide_banner", "-loglevel", "error",
		"-r", fmt.Sprintf("%.3f", inputFPS),
		"-f", "h264",
		"-i", rawPath,
		"-c:v", "copy",
		"-t", fmt.Sprintf("%.3f", duration.Seconds()),
		"-movflags", "+faststart",
		outputPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg mux: %w (%s)", err, string(out))
	}
	return nil
}

// streamWarmup is how long to wait after the stream keeper connects before
// opening the H.264 WebSocket. PiKVM V3 direct-ATX hosts encode at low FPS
// and need a longer spin-up than switch extenders.
func streamWarmup() time.Duration {
	if IsDirectATX() {
		return 3 * time.Second
	}
	return 2 * time.Second
}

// readVideoChunk returns one WebSocket message of H.264 data. PiKVM may send
// binary frames directly or JSON-wrapped chunks depending on firmware.
func readVideoChunk(ctx context.Context, conn *websocket.Conn) ([]byte, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	return data, nil
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

// ProbeMP4Duration returns the container duration reported by ffprobe.
func ProbeMP4Duration(path string) (time.Duration, error) {
	out, err := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	).Output()
	if err != nil {
		return 0, err
	}
	var sec float64
	if _, err := fmt.Sscanf(string(out), "%f", &sec); err != nil {
		return 0, err
	}
	return time.Duration(sec * float64(time.Second)), nil
}
