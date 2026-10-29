// Package tour captures one screenshot per distinct screen while the user
// clicks through something (a BIOS, an installer) in the PiKVM web UI.
//
// Frames are compared through a tiny grayscale fingerprint. A frame is saved
// once the screen has settled (two polls in a row look the same) and it
// differs enough from every frame already saved, so revisiting a page or
// moving the highlight bar does not produce duplicates.
package tour

import (
	"bytes"
	"image"
	_ "image/jpeg" // PiKVM snapshots are JPEG
)

// Fingerprint grid size. 64x36 keeps the 16:9 aspect of a 1080p capture;
// each cell averages a 30x30 pixel block.
const (
	fpW = 64
	fpH = 36
)

// cellDelta is how far (0-255) a cell's brightness must move to count as
// changed. It absorbs JPEG noise between identical frames.
const cellDelta = 18

// Fingerprint is a downscaled grayscale view of one frame.
type Fingerprint [fpW * fpH]uint8

// FingerprintJPEG decodes a JPEG and reduces it to a Fingerprint.
func FingerprintJPEG(data []byte) (Fingerprint, image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Fingerprint{}, nil, err
	}
	return FingerprintImage(img), img, nil
}

// FingerprintImage averages img into the fingerprint grid.
func FingerprintImage(img image.Image) Fingerprint {
	var fp Fingerprint
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return fp
	}
	var sum [fpW * fpH]uint64
	var cnt [fpW * fpH]uint64
	// Sample every 2nd pixel; plenty for 30x30 cells and twice as fast.
	for y := 0; y < h; y += 2 {
		cy := y * fpH / h
		for x := 0; x < w; x += 2 {
			cx := x * fpW / w
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			// Rec. 601 luma on 16-bit channels -> 0..255.
			lum := (299*uint64(r) + 587*uint64(g) + 114*uint64(bl)) / 1000 >> 8
			i := cy*fpW + cx
			sum[i] += lum
			cnt[i]++
		}
	}
	for i := range fp {
		if cnt[i] > 0 {
			fp[i] = uint8(sum[i] / cnt[i])
		}
	}
	return fp
}

// rowMinCells is how many changed cells make a fingerprint row count as
// changed in DiffStats (filters single noisy cells).
const rowMinCells = 3

// Diff returns the fraction (0..1) of cells that changed between a and b.
func Diff(a, b Fingerprint) float64 {
	frac, _ := DiffStats(a, b)
	return frac
}

// DiffStats returns the fraction (0..1) of changed cells and how many of the
// fpH rows contain changes. A moved highlight bar touches a few rows; a new
// page touches rows over most of the screen height.
func DiffStats(a, b Fingerprint) (frac float64, rows int) {
	changed := 0
	for y := 0; y < fpH; y++ {
		inRow := 0
		for x := 0; x < fpW; x++ {
			i := y*fpW + x
			d := int(a[i]) - int(b[i])
			if d < 0 {
				d = -d
			}
			if d > cellDelta {
				inRow++
			}
		}
		changed += inRow
		if inRow >= rowMinCells {
			rows++
		}
	}
	return float64(changed) / float64(len(a)), rows
}
