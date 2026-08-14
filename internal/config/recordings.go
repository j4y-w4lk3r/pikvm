package config

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ResolveRecordingsDir returns a writable directory for PiKVM screen clips.
// Priority: config.json recordings_dir → $PIKVM_RECORDINGS_DIR → /mnt/nas/…
// (when /mnt/nas is an NFS mount) → NAS-local bu path → ~/.config/pikvm/recordings.
func ResolveRecordingsDir() (string, error) {
	if RecordingsDir != "" {
		return ensureWritableDir(RecordingsDir)
	}
	if env := os.Getenv("PIKVM_RECORDINGS_DIR"); env != "" {
		return ensureWritableDir(env)
	}

	candidates := recordingsCandidates()
	home, err := os.UserHomeDir()
	if err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "pikvm", "recordings"))
	}
	for _, dir := range candidates {
		if path, err := ensureWritableDir(dir); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no writable recordings directory (set recordings_dir in config.json or PIKVM_RECORDINGS_DIR)")
}

func recordingsCandidates() []string {
	var out []string
	if isMountPoint("/mnt/nas") {
		out = append(out, "/mnt/nas/pikvm-recordings")
	}
	out = append(out, "/home/j4y/px/bu/pikvm-recordings")
	return out
}

func isMountPoint(path string) bool {
	var st, pst syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return false
	}
	parent := filepath.Dir(path)
	if err := syscall.Stat(parent, &pst); err != nil {
		return false
	}
	return st.Dev != pst.Dev
}

func ensureWritableDir(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
 probe := filepath.Join(dir, ".pikvm-write-test")
	f, err := os.Create(probe)
	if err != nil {
		return "", err
	}
	_ = f.Close()
	_ = os.Remove(probe)
	return dir, nil
}

// RecordingFilename builds a host-timestamped MP4 name, e.g. pikvm2_2026-08-14_123045.mp4.
func RecordingFilename(hostName string) string {
	name := hostName
	if name == "" {
		name = "pikvm"
	}
	return fmt.Sprintf("%s_%s.mp4", name, time.Now().Format("2006-01-02_150405"))
}
