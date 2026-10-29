package tour

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// page draws a fake 1920x1080 BIOS page: dark background, a tab bar whose
// active tab depends on tab, ten setting rows, and a highlight bar on row hl.
func page(tab, hl int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{20, 20, 40, 255}}, image.Point{}, draw.Src)
	fill := func(r image.Rectangle, c color.RGBA) {
		draw.Draw(img, r, &image.Uniform{c}, image.Point{}, draw.Src)
	}
	// Tab bar: the active tab is bright.
	for t := 0; t < 6; t++ {
		c := color.RGBA{60, 60, 80, 255}
		if t == tab {
			c = color.RGBA{230, 230, 230, 255}
		}
		fill(image.Rect(40+t*300, 20, 300+t*300, 90), c)
	}
	// Setting rows: label and value blocks laid out pseudo-randomly per tab,
	// like real BIOS pages listing different settings.
	seed := uint32(tab*7919 + 17)
	next := func(n int) int {
		seed = seed*1664525 + 1013904223
		return int(seed>>16) % n
	}
	for row := 0; row < 10; row++ {
		y := 150 + row*80
		w := 200 + next(9)*80
		fill(image.Rect(80, y, 80+w, y+40), color.RGBA{200, 200, 200, 255})
		if next(3) != 0 {
			vx := 1000 + next(7)*90
			fill(image.Rect(vx, y, vx+260, y+40), color.RGBA{150, 220, 150, 255})
		}
	}
	// Highlight bar on the selected row (thin, as in ASUS Advanced Mode).
	y := 150 + hl*80
	fill(image.Rect(60, y-4, 1860, y+44), color.RGBA{90, 140, 220, 255})
	return img
}

func fp(img image.Image) Fingerprint { return FingerprintImage(img) }

func TestDiffIdenticalIsZero(t *testing.T) {
	a := fp(page(0, 0))
	if d := Diff(a, a); d != 0 {
		t.Fatalf("Diff(a,a) = %v, want 0", d)
	}
}

func TestHighlightMoveIsNotANewPage(t *testing.T) {
	for _, to := range []int{1, 5, 9} {
		frac, rows := DiffStats(fp(page(0, 0)), fp(page(0, to)))
		if frac >= DefaultMinChange && rows >= DefaultMinRows {
			t.Errorf("highlight 0->%d: frac %.3f rows %d counts as a new page", to, frac, rows)
		}
	}
}

func TestEveryTabIsANewPage(t *testing.T) {
	for a := 0; a < 6; a++ {
		for b := a + 1; b < 6; b++ {
			frac, rows := DiffStats(fp(page(a, 0)), fp(page(b, 0)))
			if frac < DefaultMinChange || rows < DefaultMinRows {
				t.Errorf("tab %d vs %d: frac %.3f rows %d not a new page", a, b, frac, rows)
			}
		}
	}
}

func TestDetectorOnePerPage(t *testing.T) {
	d := NewDetector(0)
	// Each page is observed twice (settle), revisits and highlight moves
	// must not add frames.
	seq := []image.Image{
		page(0, 0), page(0, 0), // tab 0 -> save
		page(0, 3), page(0, 3), // highlight moved -> skip
		page(1, 0), page(1, 0), // tab 1 -> save
		page(2, 0),             // passing through, never settles -> skip
		page(3, 0), page(3, 0), // tab 3 -> save
		page(0, 0), page(0, 0), // back to tab 0 -> already saved
	}
	var saved int
	for _, img := range seq {
		if ok, _ := d.Observe(fp(img)); ok {
			saved++
		}
	}
	if saved != 3 {
		t.Fatalf("saved %d frames, want 3", saved)
	}
}

func TestDetectorForce(t *testing.T) {
	d := NewDetector(0)
	a := fp(page(0, 0))
	d.Observe(a)
	d.Observe(a)
	d.Force(a)
	if d.Saved() != 2 {
		t.Fatalf("Saved() = %d, want 2 (auto + forced)", d.Saved())
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"bios-1605":         "bios-1605",
		"BIOS 2503 after":   "bios-2503-after",
		"../etc/passwd":     "etc-passwd",
		"  weird$*name!  ":  "weirdname",
		"pre/post.update_1": "pre-post.update_1",
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := SanitizeName("$$$"); !strings.HasPrefix(got, "tour-") {
		t.Errorf("SanitizeName(\"$$$\") = %q, want default tour-…", got)
	}
}

func TestTourDirNeverReuses(t *testing.T) {
	root := t.TempDir()
	first := TourDir(root, "pikvm2", "bios-1605")
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}
	second := TourDir(root, "pikvm2", "bios-1605")
	if second == first || !strings.HasSuffix(second, "bios-1605-2") {
		t.Fatalf("second TourDir = %q, want …/bios-1605-2", second)
	}
}

func TestNextSnapPathNumbers(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 29, 19, 0, 0, 0, time.UTC)
	p1, n1, err := NextSnapPath(root, "pikvm2", now)
	if err != nil || n1 != 1 || !strings.HasSuffix(p1, "pikvm2/snaps/2026-10-29/01.jpg") {
		t.Fatalf("first snap = %q #%d (%v)", p1, n1, err)
	}
	if err := os.WriteFile(p1, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, n2, _ := NextSnapPath(root, "pikvm2", now)
	if n2 != 2 {
		t.Fatalf("second snap #%d, want 2", n2)
	}
}

// fakeSource replays a fixed list of frames, then repeats the last one.
type fakeSource struct {
	mu     sync.Mutex
	frames [][]byte
	i      int
}

func (f *fakeSource) Grab() ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fr := f.frames[f.i]
	if f.i < len(f.frames)-1 {
		f.i++
	}
	return fr, nil
}

func encode(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestRunWritesFramesSheetAndIndex(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bios-test")
	src := &fakeSource{frames: [][]byte{
		encode(t, page(0, 0)), encode(t, page(0, 0)),
		encode(t, page(1, 2)), encode(t, page(1, 2)),
		encode(t, page(1, 5)), encode(t, page(1, 5)),
	}}
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan Event, 16)
	manual := make(chan struct{}, 1)

	done := make(chan *Meta, 1)
	go func() {
		meta, err := Run(ctx, Options{Name: "bios-test", Host: "pikvm2", Dir: dir, Interval: 5 * time.Millisecond, ToolVersion: "test"}, src, manual, events)
		if err != nil {
			t.Errorf("Run: %v", err)
		}
		done <- meta
	}()

	// Wait for two auto-saved pages, then one manual capture.
	waitSaved := func(want int) {
		deadline := time.After(5 * time.Second)
		for {
			select {
			case ev := <-events:
				if ev.Kind == "saved" && ev.Index == want {
					return
				}
			case <-deadline:
				t.Fatalf("timed out waiting for frame %d", want)
			}
		}
	}
	waitSaved(2)
	manual <- struct{}{}
	waitSaved(3)
	cancel()
	meta := <-done

	if len(meta.Frames) != 3 || !meta.Frames[2].Manual {
		t.Fatalf("frames = %+v, want 2 auto + 1 manual", meta.Frames)
	}
	for _, f := range []string{"001.jpg", "002.jpg", "003.jpg", "contact-sheet.jpg", "index.html", "tour.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "tour.json"))
	var onDisk Meta
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk.Ended.IsZero() || onDisk.ContactSheet != "contact-sheet.jpg" || onDisk.Index != "index.html" {
		t.Fatalf("tour.json not finalized: %+v", onDisk)
	}
}
