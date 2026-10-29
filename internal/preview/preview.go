// Package preview shows a screenshot inside the TUI.
//
// Kitty-compatible terminals (kitty, Ghostty) get the real image through
// kitty's Unicode placeholders: the image is transmitted once as an escape
// sequence, and the TUI then prints ordinary placeholder characters where it
// should appear. Because placeholders are plain text, Bubble Tea's renderer
// and tmux (with allow-passthrough) handle them like any other cell.
//
// Other terminals get a coarse half-block rendering (▀ with truecolor fg/bg).
//
// PIKVM_PREVIEW=kitty|blocks|off overrides detection.
package preview

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"strings"

	"pikvm/internal/tour"
)

// Mode is how previews are drawn.
type Mode int

const (
	Off Mode = iota
	Blocks
	Kitty
)

func (m Mode) String() string {
	switch m {
	case Kitty:
		return "kitty"
	case Blocks:
		return "blocks"
	default:
		return "off"
	}
}

// Detect picks the best mode for the current terminal.
func Detect() Mode {
	switch strings.ToLower(os.Getenv("PIKVM_PREVIEW")) {
	case "kitty":
		return Kitty
	case "blocks":
		return Blocks
	case "off", "0", "none":
		return Off
	}
	if os.Getenv("TMUX") != "" {
		if tmuxKitty() {
			return Kitty
		}
		return Blocks
	}
	if kittyTerm(os.Getenv("TERM"), os.Getenv("TERM_PROGRAM")) || os.Getenv("KITTY_WINDOW_ID") != "" {
		return Kitty
	}
	return Blocks
}

func kittyTerm(term, program string) bool {
	t := strings.ToLower(term + " " + program)
	return strings.Contains(t, "kitty") || strings.Contains(t, "ghostty")
}

// tmuxKitty reports whether the tmux client is a kitty-protocol terminal and
// tmux forwards graphics escapes (allow-passthrough on/all).
func tmuxKitty() bool {
	pass, err := exec.Command("tmux", "show", "-gv", "allow-passthrough").Output()
	if err != nil {
		return false
	}
	if p := strings.TrimSpace(string(pass)); p != "on" && p != "all" {
		return false
	}
	client, err := exec.Command("tmux", "display", "-p", "#{client_termtype} #{client_termname}").Output()
	if err != nil {
		return false
	}
	return kittyTerm(string(client), "")
}

// Image is a prepared preview: Lines are printed by the TUI (one string per
// terminal row, each exactly Cols cells wide).
type Image struct {
	ID    uint8
	Cols  int
	Rows  int
	Lines []string
	Mode  Mode
}

// SizeFor returns preview rows for a 16:9 image cols cells wide (cells are
// roughly twice as tall as wide).
func SizeFor(cols int) (int, int) {
	if cols < 8 {
		cols = 8
	}
	rows := (cols*9/16 + 1) / 2
	if rows < 3 {
		rows = 3
	}
	return cols, rows
}

// SizeForImage fits a w×h image into at most maxCols×maxRows cells while
// keeping its aspect ratio (a cell is about twice as tall as wide).
func SizeForImage(maxCols, maxRows, w, h int) (int, int) {
	if w <= 0 || h <= 0 {
		return SizeFor(maxCols)
	}
	if maxCols < 8 {
		maxCols = 8
	}
	if maxRows < 3 {
		maxRows = 3
	}
	cols := maxCols
	rows := (cols*h/w + 1) / 2
	if rows > maxRows {
		rows = maxRows
		cols = rows * 2 * w / h
	}
	if rows < 1 {
		rows = 1
	}
	if cols < 1 {
		cols = 1
	}
	return cols, rows
}

// Prepare builds the preview for a decoded image. For Kitty mode it also
// returns the escape sequences that transmit the image (send them with
// Transmit before the lines are displayed).
func Prepare(img image.Image, cols, rows int, mode Mode, id uint8) (*Image, []string, error) {
	switch mode {
	case Kitty:
		if id == 0 {
			id = 1
		}
		seqs, err := kittyTransmit(img, cols, rows, id)
		if err != nil {
			return nil, nil, err
		}
		return &Image{ID: id, Cols: cols, Rows: rows, Lines: placeholderLines(id, cols, rows), Mode: Kitty}, seqs, nil
	case Blocks:
		return &Image{Cols: cols, Rows: rows, Lines: blockLines(img, cols, rows), Mode: Blocks}, nil, nil
	default:
		return nil, nil, nil
	}
}

// kittyTransmit encodes img as PNG and returns chunked APC sequences that
// upload it as image id with a virtual placement of cols×rows cells.
func kittyTransmit(img image.Image, cols, rows int, id uint8) ([]string, error) {
	// ~12 px per cell is plenty for a thumbnail and keeps the upload small.
	w, h := cols*12, rows*24
	small := tour.Downscale(img, w, h)
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, small); err != nil {
		return nil, err
	}
	data := base64.StdEncoding.EncodeToString(buf.Bytes())
	const chunk = 4096
	var seqs []string
	for i := 0; i < len(data); i += chunk {
		end := i + chunk
		if end > len(data) {
			end = len(data)
		}
		more := 1
		if end == len(data) {
			more = 0
		}
		var ctrl string
		if i == 0 {
			ctrl = fmt.Sprintf("a=T,f=100,i=%d,U=1,c=%d,r=%d,q=2,m=%d", id, cols, rows, more)
		} else {
			ctrl = fmt.Sprintf("m=%d", more)
		}
		seqs = append(seqs, wrapTmux("\x1b_G"+ctrl+";"+data[i:end]+"\x1b\\"))
	}
	return seqs, nil
}

// DeleteSeq frees image id in the terminal.
func DeleteSeq(id uint8) string {
	return wrapTmux(fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id))
}

// wrapTmux wraps an escape sequence for tmux passthrough when inside tmux.
func wrapTmux(seq string) string {
	if os.Getenv("TMUX") == "" {
		return seq
	}
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// Transmit writes the sequences to the terminal, one write per chunk so a
// concurrent Bubble Tea repaint can only land between complete sequences.
func Transmit(seqs []string) error {
	for _, s := range seqs {
		if _, err := os.Stdout.WriteString(s); err != nil {
			return err
		}
	}
	return nil
}

// placeholder is kitty's U+10EEEE image placeholder.
const placeholder = "\U0010EEEE"

// placeholderLines returns rows strings of cols placeholder cells. Each cell
// carries explicit row and column diacritics; the foreground color (256-color
// index) carries the image id.
func placeholderLines(id uint8, cols, rows int) []string {
	lines := make([]string, rows)
	for r := 0; r < rows; r++ {
		var b strings.Builder
		fmt.Fprintf(&b, "\x1b[38;5;%dm", id)
		for c := 0; c < cols; c++ {
			b.WriteString(placeholder)
			b.WriteRune(diacritic(r))
			b.WriteRune(diacritic(c))
		}
		b.WriteString("\x1b[39m")
		lines[r] = b.String()
	}
	return lines
}

// blockLines renders img with upper-half blocks: each cell shows two pixels.
func blockLines(img image.Image, cols, rows int) []string {
	small := tour.Downscale(img, cols, rows*2)
	lines := make([]string, rows)
	for r := 0; r < rows; r++ {
		var b strings.Builder
		for c := 0; c < cols; c++ {
			top := small.RGBAAt(c, r*2)
			bot := small.RGBAAt(c, r*2+1)
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", top.R, top.G, top.B, bot.R, bot.G, bot.B)
		}
		b.WriteString("\x1b[0m")
		lines[r] = b.String()
	}
	return lines
}

// diacritic returns kitty's row/column diacritic for index n
// (rowcolumn-diacritics.txt, first 64 entries).
func diacritic(n int) rune {
	if n < 0 || n >= len(diacritics) {
		n = len(diacritics) - 1
	}
	return diacritics[n]
}

var diacritics = [...]rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F,
	0x0346, 0x034A, 0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357,
	0x035B, 0x0363, 0x0364, 0x0365, 0x0366, 0x0367, 0x0368, 0x0369,
	0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F, 0x0483, 0x0484,
	0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597,
	0x0598, 0x0599, 0x059C, 0x059D, 0x059E, 0x059F, 0x05A0, 0x05A1,
	0x05A8, 0x05A9, 0x05AB, 0x05AC, 0x05AF, 0x05C4, 0x0610, 0x0611,
	0x0612, 0x0613, 0x0614, 0x0615, 0x0616, 0x0617, 0x0657, 0x0658,
}

// MaxCols is the widest preview the diacritics table supports.
const MaxCols = len(diacritics)
