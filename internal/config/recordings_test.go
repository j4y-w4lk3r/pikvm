package config

import (
	"os"
	"strings"
	"testing"
)

func TestRecordingFilename(t *testing.T) {
	name := RecordingFilename("pikvm2")
	if !strings.HasPrefix(name, "pikvm2_") || !strings.HasSuffix(name, ".mp4") {
		t.Fatalf("got %q", name)
	}
}

func TestResolveRecordingsDirExplicit(t *testing.T) {
	dir := t.TempDir()
	prev := RecordingsDir
	RecordingsDir = dir
	t.Cleanup(func() { RecordingsDir = prev })

	got, err := ResolveRecordingsDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Fatalf("got %q want %q", got, dir)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("dir missing: %v", err)
	}
}

func TestResolveRecordingsDirEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIKVM_RECORDINGS_DIR", dir)
	prev := RecordingsDir
	RecordingsDir = ""
	t.Cleanup(func() { RecordingsDir = prev })

	got, err := ResolveRecordingsDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Fatalf("got %q want %q", got, dir)
	}
}
