package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"pikvm/internal/config"
)

// Session holds an active PiKVM capture session (screen chunks + action log).
type Session struct {
	Dir       string
	Name      string
	Machine   string
	Port      int
	StartedAt time.Time

	mu      sync.Mutex
	logFile *os.File
	part    int
}

func sessionStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "pikvm", "active-session.json"), nil
}

func saveSessionState(s *Session) error {
	path, err := sessionStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	row := map[string]string{"dir": s.Dir, "name": s.Name, "machine": s.Machine}
	b, _ := json.MarshalIndent(row, "", "  ")
	return os.WriteFile(path, b, 0o600)
}

func clearSessionState() {
	path, err := sessionStatePath()
	if err == nil {
		_ = os.Remove(path)
	}
}

func loadSessionStateDir() (string, error) {
	path, err := sessionStatePath()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var row struct {
		Dir string `json:"dir"`
	}
	if err := json.Unmarshal(b, &row); err != nil {
		return "", err
	}
	if row.Dir == "" {
		return "", fmt.Errorf("empty session dir in state file")
	}
	return row.Dir, nil
}

// ActiveSession is always nil; session state lives on disk for cross-process use.
func ActiveSession() *Session {
	return nil
}

// StartSession creates session dir, meta.json, and actions.jsonl.
func StartSession(_ context.Context, dir, name, machine string, port int) (*Session, error) {
	if existing, err := loadSessionStateDir(); err == nil {
		return nil, fmt.Errorf("session already active: %s — run: pikvm session stop", existing)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	logPath := filepath.Join(dir, "actions.jsonl")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}

	s := &Session{
		Dir:       dir,
		Name:      name,
		Machine:   machine,
		Port:      port,
		StartedAt: time.Now().UTC(),
		logFile:   logFile,
		part:      1,
	}

	meta := map[string]interface{}{
		"name":       name,
		"machine":    machine,
		"port":       port,
		"started_at": s.StartedAt.Format(time.RFC3339),
		"host":       config.HostName,
	}
	metaBytes, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), metaBytes, 0o644); err != nil {
		_ = logFile.Close()
		return nil, err
	}

	s.logLocked("session_start", map[string]interface{}{
		"name": name, "machine": machine, "port": port,
	})

	if err := saveSessionState(s); err != nil {
		_ = logFile.Close()
		return nil, err
	}
	return s, nil
}

// RunSessionWorker records MP4 chunks until ctx is cancelled.
func RunSessionWorker(ctx context.Context, dir string, port int) error {
	s := &Session{Dir: dir, Port: port, part: 1}
	const chunk = 10 * time.Minute
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		s.mu.Lock()
		part := s.part
		s.part++
		s.mu.Unlock()

		out := filepath.Join(dir, fmt.Sprintf("screen-%03d.mp4", part))
		SessionLog("record_start", map[string]interface{}{"path": out, "part": part})
		_, err := RecordVideo(ctx, chunk, out, port)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			SessionLog("record_error", map[string]interface{}{"error": err.Error(), "part": part})
			time.Sleep(2 * time.Second)
			continue
		}
		SessionLog("record_done", map[string]interface{}{"path": out, "part": part})
	}
}

func (s *Session) logLocked(eventType string, fields map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.logFile == nil {
		return
	}
	row := map[string]interface{}{
		"ts": time.Now().UTC().Format(time.RFC3339Nano), "event": eventType,
	}
	for k, v := range fields {
		row[k] = v
	}
	b, _ := json.Marshal(row)
	_, _ = s.logFile.Write(append(b, '\n'))
}

// StopSession finalizes the session and stops the worker process if present.
func StopSession() (*Session, error) {
	dir, err := loadSessionStateDir()
	if err != nil {
		return nil, fmt.Errorf("no active session")
	}

	pidPath := filepath.Join(dir, "worker.pid")
	if b, err := os.ReadFile(pidPath); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
		_ = os.Remove(pidPath)
	}

	clearSessionState()
	_ = appendSessionStopMarker(dir)
	return &Session{Dir: dir}, nil
}

func appendSessionStopMarker(dir string) error {
	logPath := filepath.Join(dir, "actions.jsonl")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	row := map[string]interface{}{
		"ts": time.Now().UTC().Format(time.RFC3339Nano), "event": "session_stop",
	}
	b, _ := json.Marshal(row)
	_, err = f.Write(append(b, '\n'))
	return err
}

// SessionLog appends to the active session actions.jsonl.
func SessionLog(eventType string, fields map[string]interface{}) {
	dir, err := loadSessionStateDir()
	if err != nil {
		return
	}
	logPath := filepath.Join(dir, "actions.jsonl")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	row := map[string]interface{}{
		"ts": time.Now().UTC().Format(time.RFC3339Nano), "event": eventType,
	}
	for k, v := range fields {
		row[k] = v
	}
	b, _ := json.Marshal(row)
	_, _ = f.Write(append(b, '\n'))
}

// ActiveSessionDir returns the session directory from the state file.
func ActiveSessionDir() (string, error) {
	return loadSessionStateDir()
}
