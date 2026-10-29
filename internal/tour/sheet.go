package tour

import (
	"fmt"
	"html"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
)

// Contact sheet layout.
const (
	thumbW     = 480
	thumbH     = 270
	sheetCols  = 4
	sheetPad   = 16
	labelH     = 38
	digitScale = 3
)

var (
	sheetBG    = color.RGBA{0x1e, 0x1e, 0x2e, 0xff}
	sheetLabel = color.RGBA{0x9b, 0xf0, 0x9d, 0xff} // the TUI's pastel green
	sheetHand  = color.RGBA{0xfa, 0xb3, 0x87, 0xff} // manual captures
)

// Downscale box-filters img to w×h (sampling every other source pixel).
func Downscale(img image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw == 0 || sh == 0 {
		return dst
	}
	for dy := 0; dy < h; dy++ {
		y0 := dy * sh / h
		y1 := (dy + 1) * sh / h
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for dx := 0; dx < w; dx++ {
			x0 := dx * sw / w
			x1 := (dx + 1) * sw / w
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, bl, n uint64
			for y := y0; y < y1; y += 2 {
				for x := x0; x < x1; x += 2 {
					cr, cg, cb, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
					r += uint64(cr)
					g += uint64(cg)
					bl += uint64(cb)
					n++
				}
			}
			dst.SetRGBA(dx, dy, color.RGBA{uint8(r / n >> 8), uint8(g / n >> 8), uint8(bl / n >> 8), 0xff})
		}
	}
	return dst
}

// WriteContactSheet renders every frame of meta as a numbered thumbnail grid.
func WriteContactSheet(dir string, meta *Meta, out string) error {
	n := len(meta.Frames)
	if n == 0 {
		return fmt.Errorf("no frames")
	}
	cols := sheetCols
	if n < cols {
		cols = n
	}
	rows := (n + cols - 1) / cols
	cellW := thumbW + sheetPad
	cellH := thumbH + labelH + sheetPad
	sheet := image.NewRGBA(image.Rect(0, 0, cols*cellW+sheetPad, rows*cellH+sheetPad))
	draw.Draw(sheet, sheet.Bounds(), &image.Uniform{sheetBG}, image.Point{}, draw.Src)

	for i, fr := range meta.Frames {
		x := sheetPad + (i%cols)*cellW
		y := sheetPad + (i/cols)*cellH
		labelColor := sheetLabel
		if fr.Manual {
			labelColor = sheetHand
		}
		drawNumber(sheet, x, y+4, i+1, labelColor)
		f, err := os.Open(filepath.Join(dir, fr.File))
		if err != nil {
			continue
		}
		img, err := jpeg.Decode(f)
		f.Close()
		if err != nil {
			continue
		}
		thumb := Downscale(img, thumbW, thumbH)
		draw.Draw(sheet, image.Rect(x, y+labelH, x+thumbW, y+labelH+thumbH), thumb, image.Point{}, draw.Src)
	}

	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	return jpeg.Encode(f, sheet, &jpeg.Options{Quality: 85})
}

// WriteIndexHTML writes a small dark page listing every frame full size.
func WriteIndexHTML(dir string, meta *Meta) error {
	var b strings.Builder
	esc := html.EscapeString
	b.WriteString("<!DOCTYPE html>\n<html><head><meta charset=\"utf-8\">\n")
	fmt.Fprintf(&b, "<title>%s — pikvm tour</title>\n", esc(meta.Name))
	b.WriteString(`<style>
body{background:#1e1e2e;color:#cdd6f4;font:15px/1.5 system-ui,sans-serif;margin:24px}
h1{color:#9bf09d;margin:0 0 4px}.meta{color:#a6adc8;margin-bottom:20px}
.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(420px,1fr));gap:16px}
figure{margin:0}img{width:100%;border:1px solid #45475a;border-radius:6px}
figcaption{color:#9bf09d}.manual{color:#fab387}
</style></head><body>
`)
	fmt.Fprintf(&b, "<h1>%s</h1>\n<div class=\"meta\">", esc(meta.Name))
	if meta.Note != "" {
		fmt.Fprintf(&b, "%s<br>", esc(meta.Note))
	}
	fmt.Fprintf(&b, "%s (%s) · port %d · %s → %s · %d frames · pikvm %s",
		esc(meta.Host), esc(meta.HostAddr), meta.Port,
		meta.Started.Format("2006-01-02 15:04:05"), meta.Ended.Format("15:04:05"),
		len(meta.Frames), esc(meta.ToolVersion))
	if meta.KvmdVersion != "" {
		fmt.Fprintf(&b, " · kvmd %s", esc(meta.KvmdVersion))
	}
	b.WriteString("</div>\n<div class=\"grid\">\n")
	for i, fr := range meta.Frames {
		cls, tag := "", ""
		if fr.Manual {
			cls, tag = ` class="manual"`, " · manual"
		}
		fmt.Fprintf(&b, "<figure><a href=\"%s\"><img src=\"%s\" loading=\"lazy\"></a><figcaption%s>#%d · %s%s</figcaption></figure>\n",
			esc(fr.File), esc(fr.File), cls, i+1, fr.Time.Format("15:04:05"), tag)
	}
	b.WriteString("</div>\n</body></html>\n")
	return os.WriteFile(filepath.Join(dir, "index.html"), []byte(b.String()), 0o644)
}

// 3×5 bitmap digits for contact-sheet labels (no font dependency).
var digitGlyphs = [10][5]string{
	{"###", "#.#", "#.#", "#.#", "###"},
	{".#.", "##.", ".#.", ".#.", "###"},
	{"###", "..#", "###", "#..", "###"},
	{"###", "..#", "###", "..#", "###"},
	{"#.#", "#.#", "###", "..#", "..#"},
	{"###", "#..", "###", "..#", "###"},
	{"###", "#..", "###", "#.#", "###"},
	{"###", "..#", ".#.", ".#.", ".#."},
	{"###", "#.#", "###", "#.#", "###"},
	{"###", "#.#", "###", "..#", "###"},
}

// drawNumber draws "#n" at (x, y) with digitScale-sized pixels.
func drawNumber(img *image.RGBA, x, y, n int, c color.RGBA) {
	s := fmt.Sprintf("%d", n)
	px := x
	for _, ch := range s {
		g := digitGlyphs[ch-'0']
		for row := 0; row < 5; row++ {
			for col := 0; col < 3; col++ {
				if g[row][col] != '#' {
					continue
				}
				r := image.Rect(px+col*digitScale*2, y+row*digitScale*2, px+(col+1)*digitScale*2, y+(row+1)*digitScale*2)
				draw.Draw(img, r, &image.Uniform{c}, image.Point{}, draw.Src)
			}
		}
		px += 4 * digitScale * 2
	}
}
