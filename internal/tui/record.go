package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pikvm/internal/api"
	"pikvm/internal/config"
)

type recordDoneMsg struct {
	result string
}

const defaultRecordSeconds = 10

func recordClipCmd(port int, duration time.Duration, outputPath string) tea.Cmd {
	return func() tea.Msg {
		out := outputPath
		if out == "" {
			dir, err := config.ResolveRecordingsDir()
			if err != nil {
				return recordDoneMsg{result: fmt.Sprintf("\uf057 %v", err)}
			}
			out = filepath.Join(dir, config.RecordingFilename(config.HostName))
		}
		res, err := api.RecordVideo(context.Background(), duration, out, port)
		if err != nil {
			return recordDoneMsg{result: fmt.Sprintf("\uf057 Record failed: %v", err)}
		}
		return recordDoneMsg{result: formatRecordSuccess(res)}
	}
}

func formatRecordSuccess(res api.RecordResult) string {
	preview := previewPath(res.Path)
	msg := fmt.Sprintf("\uf00c Saved %s (%.0fs, %s)", res.Path, res.Duration.Seconds(), formatBytes(res.Bytes))
	if preview != "" && preview != res.Path {
		msg += fmt.Sprintf("\n  Preview on desktop: %s", preview)
	}
	return msg
}

// previewPath maps NAS-local paths to the desktop NFS mount when applicable.
func previewPath(saved string) string {
	const nasLocal = "/home/j4y/px/bu/pikvm-recordings"
	const desktopNFS = "/mnt/nas/pikvm-recordings"
	if filepath.Base(saved) == saved {
		return saved
	}
	if dir := filepath.Dir(saved); dir == nasLocal || dir == nasLocal+"/" {
		return filepath.Join(desktopNFS, filepath.Base(saved))
	}
	return saved
}

func formatBytes(n int64) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func (m Model) startRecordClip(port int) (tea.Model, tea.Cmd) {
	m.result = fmt.Sprintf("\uf0c7 Recording %ds clip from %s…", defaultRecordSeconds, config.HostName)
	return m, recordClipCmd(port, defaultRecordSeconds*time.Second, "")
}

func scriptIsRecord(name string) bool {
	return name == "Record 10s clip"
}
