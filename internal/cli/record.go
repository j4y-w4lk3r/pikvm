package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pikvm/internal/api"
	"pikvm/internal/config"
)

func cliRecord(jsonMode bool, args []string) {
	duration := 10 * time.Second
	output := ""
	port := 0
	rest := args

	for len(rest) > 0 {
		switch rest[0] {
		case "--duration", "-d":
			if len(rest) < 2 {
				die(jsonMode, "record", fmt.Errorf("usage: pikvm record [--duration SEC] [--output PATH] [port]"))
			}
			sec, err := strconv.Atoi(rest[1])
			if err != nil || sec <= 0 {
				die(jsonMode, "record", fmt.Errorf("invalid duration %q", rest[1]))
			}
			duration = time.Duration(sec) * time.Second
			rest = rest[2:]
		case "--output", "-o":
			if len(rest) < 2 {
				die(jsonMode, "record", fmt.Errorf("usage: pikvm record [--duration SEC] [--output PATH] [port]"))
			}
			output = rest[1]
			rest = rest[2:]
		default:
			if strings.HasPrefix(rest[0], "-") {
				die(jsonMode, "record", fmt.Errorf("unknown flag %q", rest[0]))
			}
			p, err := parsePort(rest[0])
			if err != nil {
				die(jsonMode, "record", err)
			}
			port = p
			rest = rest[1:]
		}
	}

	if output == "" {
		dir, err := config.ResolveRecordingsDir()
		if err != nil {
			die(jsonMode, "record", err)
		}
		output = filepath.Join(dir, config.RecordingFilename(config.HostName))
	}

	res, err := api.RecordVideo(context.Background(), duration, output, port)
	if err != nil {
		die(jsonMode, "record", err)
	}

	if jsonMode {
		emit(true, "record", map[string]interface{}{
			"path":         res.Path,
			"duration_sec": res.Duration.Seconds(),
			"bytes":        res.Bytes,
			"preview_path": recordPreviewPath(res.Path),
		}, nil)
		return
	}

	fmt.Println(formatRecordCLI(res))
}

func formatRecordCLI(res api.RecordResult) string {
	msg := fmt.Sprintf("\uf00c Saved %s (%.0fs, %s)", res.Path, res.Duration.Seconds(), formatRecordBytes(res.Bytes))
	if preview := recordPreviewPath(res.Path); preview != "" && preview != res.Path {
		msg += fmt.Sprintf("\n  Preview on desktop: %s", preview)
	}
	return msg
}

func recordPreviewPath(saved string) string {
	const nasLocal = "/home/j4y/px/bu/pikvm-recordings"
	const desktopNFS = "/mnt/nas/pikvm-recordings"
	if filepath.Dir(saved) == nasLocal {
		return filepath.Join(desktopNFS, filepath.Base(saved))
	}
	return saved
}

func formatRecordBytes(n int64) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
