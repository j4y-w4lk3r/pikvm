package preview

import (
	"image"
	"image/color"
	"image/draw"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func testImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 320, 180))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{30, 30, 60, 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 0, 160, 90), &image.Uniform{color.RGBA{220, 220, 220, 255}}, image.Point{}, draw.Src)
	return img
}

// Lines must be exactly Cols cells wide so the TUI layout and frame
// truncation treat them like any other row.
func TestLineWidths(t *testing.T) {
	for _, mode := range []Mode{Kitty, Blocks} {
		cols, rows := SizeFor(40)
		img, _, err := Prepare(testImage(), cols, rows, mode, 7)
		if err != nil {
			t.Fatalf("%v: %v", mode, err)
		}
		if len(img.Lines) != rows {
			t.Fatalf("%v: %d lines, want %d", mode, len(img.Lines), rows)
		}
		for i, l := range img.Lines {
			if w := ansi.StringWidth(l); w != cols {
				t.Fatalf("%v line %d width %d, want %d", mode, i, w, cols)
			}
		}
	}
}

func TestSizeForImageKeepsAspect(t *testing.T) {
	// 16:9 screenshot fills the box.
	if c, r := SizeForImage(48, 14, 1920, 1080); c != 48 || r != 14 {
		t.Errorf("16:9 -> %dx%d, want 48x14", c, r)
	}
	// Wide contact sheet (3 thumbs in a row) gets fewer rows, full width.
	if c, r := SizeForImage(48, 14, 1504, 340); c != 48 || r > 6 {
		t.Errorf("wide sheet -> %dx%d, want 48 cols and <= 6 rows", c, r)
	}
	// Tall sheet is limited by rows and narrows instead of stretching.
	if c, r := SizeForImage(48, 14, 1984, 3200); r != 14 || c >= 48 {
		t.Errorf("tall sheet -> %dx%d, want 14 rows and < 48 cols", c, r)
	}
}

func TestKittyTransmitChunks(t *testing.T) {
	t.Setenv("TMUX", "")
	_, seqs, err := Prepare(testImage(), 40, 11, Kitty, 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(seqs) == 0 {
		t.Fatal("no transmit sequences")
	}
	first := seqs[0]
	for _, want := range []string{"a=T", "f=100", "i=9", "U=1", "c=40", "r=11", "q=2"} {
		if !strings.Contains(first, want) {
			t.Errorf("first chunk missing %q", want)
		}
	}
	last := seqs[len(seqs)-1]
	if !strings.Contains(last, "m=0;") {
		t.Errorf("last chunk not terminated with m=0: %.40q", last)
	}
	for i, s := range seqs {
		if !strings.HasPrefix(s, "\x1b_G") || !strings.HasSuffix(s, "\x1b\\") {
			t.Fatalf("chunk %d is not a complete APC sequence", i)
		}
	}
}

func TestTmuxWrapping(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1,0")
	s := DeleteSeq(3)
	if !strings.HasPrefix(s, "\x1bPtmux;\x1b\x1b_G") || !strings.HasSuffix(s, "\x1b\\") {
		t.Fatalf("not tmux-wrapped: %q", s)
	}
}

func TestDetectOverride(t *testing.T) {
	t.Setenv("PIKVM_PREVIEW", "off")
	if Detect() != Off {
		t.Fatal("PIKVM_PREVIEW=off not honored")
	}
	t.Setenv("PIKVM_PREVIEW", "blocks")
	if Detect() != Blocks {
		t.Fatal("PIKVM_PREVIEW=blocks not honored")
	}
}
