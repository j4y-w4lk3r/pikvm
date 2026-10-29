package tui

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // decode saved snapshots for the preview
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"pikvm/internal/api"
	"pikvm/internal/config"
	"pikvm/internal/preview"
	"pikvm/internal/tour"
)

// Snapshots (s), tours (S) and the in-TUI preview (v).
//
//	s   save one screenshot to ~/Pictures/pikvm/<host>/snaps/<date>/NN.jpg
//	    (during a tour: force-capture the current screen into the tour)
//	S   start a tour (asks for a name) / stop the running tour
//	v   show or hide the preview of the last screenshot

// Version is the pikvm build version, recorded in tour.json. Set by main.
var Version = "dev"

// tourRun is a running tour. Shared by pointer across Model copies.
type tourRun struct {
	name   string
	dir    string
	cancel context.CancelFunc
	manual chan struct{}
	events chan tour.Event
	saved  int
}

type snapDoneMsg struct {
	path string
	n    int
	err  error
}

type tourStartedMsg struct {
	run *tourRun
	err error
}

type tourEventMsg struct {
	run *tourRun
	ev  tour.Event
}

type previewReadyMsg struct {
	img     *preview.Image
	caption string
	err     error
}

// previewCols is the preview width for the current layout.
func (m Model) previewCols() int {
	w := m.contentWidth() - 4
	if m.useTwoColumns() {
		_, right, _ := m.columnWidths()
		w = right - 4
	}
	if w > 56 {
		w = 56
	}
	if w > preview.MaxCols {
		w = preview.MaxCols
	}
	return w
}

// previewFits reports whether the terminal has room for the preview rows.
func (m Model) previewFits() bool {
	if m.previewMode == preview.Off || m.previewHidden || m.preview == nil {
		return false
	}
	return m.termHeight == 0 || m.contentHeight() >= 34
}

func snapCmd(port int, host string) tea.Cmd {
	return func() tea.Msg {
		root, err := tour.PicturesRoot()
		if err != nil {
			return snapDoneMsg{err: err}
		}
		path, n, err := tour.NextSnapPath(root, host, time.Now())
		if err != nil {
			return snapDoneMsg{err: err}
		}
		if _, err := api.SaveSnapshot(port, 90, path); err != nil {
			return snapDoneMsg{err: err}
		}
		return snapDoneMsg{path: path, n: n}
	}
}

// previewCmd decodes path and (for kitty) uploads it as image id, freeing
// the previous image.
func previewCmd(path, caption string, mode preview.Mode, cols int, id, oldID uint8) tea.Cmd {
	if mode == preview.Off {
		return nil
	}
	return func() tea.Msg {
		f, err := os.Open(path)
		if err != nil {
			return previewReadyMsg{err: err}
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			return previewReadyMsg{err: err}
		}
		b := img.Bounds()
		maxCols, maxRows := preview.SizeFor(cols)
		cols, rows := preview.SizeForImage(maxCols, maxRows, b.Dx(), b.Dy())
		p, seqs, err := preview.Prepare(img, cols, rows, mode, id)
		if err != nil {
			return previewReadyMsg{err: err}
		}
		if mode == preview.Kitty {
			if oldID != 0 && oldID != id {
				seqs = append([]string{preview.DeleteSeq(oldID)}, seqs...)
			}
			if err := preview.Transmit(seqs); err != nil {
				return previewReadyMsg{err: err}
			}
		}
		return previewReadyMsg{img: p, caption: caption}
	}
}

// nextPreviewID cycles kitty image ids 1..250.
func (m *Model) nextPreviewID() (id, old uint8) {
	if m.preview != nil {
		old = m.preview.ID
	}
	m.previewSeq = m.previewSeq%250 + 1
	return uint8(m.previewSeq), old
}

func (m Model) showPreviewOf(path, caption string) (Model, tea.Cmd) {
	id, old := m.nextPreviewID()
	return m, previewCmd(path, caption, m.previewMode, m.previewCols(), id, old)
}

func startTourCmd(name string, port int) tea.Cmd {
	return func() tea.Msg {
		root, err := tour.PicturesRoot()
		if err != nil {
			return tourStartedMsg{err: err}
		}
		dir := tour.TourDir(root, config.HostName, name)
		ctx, cancel := context.WithCancel(context.Background())
		stream, err := api.OpenSnapshotStream(ctx, port, 90)
		if err != nil {
			cancel()
			return tourStartedMsg{err: err}
		}
		info, _ := api.FetchInfoState()
		run := &tourRun{
			name:   filepath.Base(dir),
			dir:    dir,
			cancel: cancel,
			manual: make(chan struct{}, 1),
			events: make(chan tour.Event, 32),
		}
		opts := tour.Options{
			Name: run.name, Host: config.HostName, HostAddr: config.Host, Port: port,
			Dir: dir, Interval: time.Second, ToolVersion: Version, KvmdVersion: info.KvmdVersion,
		}
		go func() {
			defer stream.Close()
			_, _ = tour.Run(ctx, opts, stream, run.manual, run.events)
		}()
		return tourStartedMsg{run: run}
	}
}

func waitTourEvent(run *tourRun) tea.Cmd {
	return func() tea.Msg {
		return tourEventMsg{run: run, ev: <-run.events}
	}
}

// handleSnapKey owns s / S / v. ok=false means the key was not handled.
func (m Model) handleSnapKey(key string) (Model, tea.Cmd, bool) {
	busyModal := m.selectingISO || m.selectingBIOSKey || m.showHelp
	switch key {
	case "s":
		if busyModal {
			return m, nil, false
		}
		if m.tour != nil {
			select {
			case m.tour.manual <- struct{}{}:
				m.result = " Capturing this screen into the tour…"
			default:
			}
			return m, nil, true
		}
		if m.snapBusy {
			return m, nil, true
		}
		m.snapBusy = true
		m.result = " Taking screenshot…"
		return m, snapCmd(m.port, config.HostName), true

	case "S":
		if busyModal {
			return m, nil, false
		}
		if m.tour != nil {
			m.tour.cancel()
			m.result = fmt.Sprintf(" Finishing tour %s (contact sheet)…", m.tour.name)
			return m, nil, true
		}
		if m.tourStarting {
			return m, nil, true
		}
		m.tourNaming = true
		m.tourNameBuf = tour.DefaultName(time.Now())
		m.focusMode = ""
		return m, nil, true

	case "v":
		if busyModal || m.previewMode == preview.Off {
			return m, nil, false
		}
		m.previewHidden = !m.previewHidden
		return m, nil, true
	}
	return m, nil, false
}

// handleTourNameKey edits the tour name prompt.
func (m Model) handleTourNameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.tourNaming = false
		m.result = "Tour cancelled"
		return m, nil
	case tea.KeyEnter:
		name := tour.SanitizeName(m.tourNameBuf)
		m.tourNaming = false
		m.tourStarting = true
		m.result = fmt.Sprintf(" Starting tour %s — warming up the stream…", name)
		return m, startTourCmd(name, m.port)
	case tea.KeyBackspace:
		if r := []rune(m.tourNameBuf); len(r) > 0 {
			m.tourNameBuf = string(r[:len(r)-1])
		}
		return m, nil
	case tea.KeyCtrlU:
		m.tourNameBuf = ""
		return m, nil
	case tea.KeyCtrlC:
		m.quitting = true
		return m, tea.Quit
	case tea.KeySpace:
		m.tourNameBuf += "-"
		return m, nil
	case tea.KeyRunes:
		for _, r := range msg.Runes {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("-_.", r) {
				m.tourNameBuf += string(r)
			}
		}
		return m, nil
	}
	return m, nil
}

// updateSnap handles snapshot / tour / preview messages.
func (m Model) updateSnap(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case snapDoneMsg:
		m.snapBusy = false
		if msg.err != nil {
			m.result = fmt.Sprintf(" Screenshot failed: %v", msg.err)
			return m, nil, true
		}
		m.result = fmt.Sprintf("  #%02d saved %s", msg.n, msg.path)
		m2, cmd := m.showPreviewOf(msg.path, fmt.Sprintf("snap #%02d · %s", msg.n, time.Now().Format("15:04:05")))
		return m2, cmd, true

	case tourStartedMsg:
		m.tourStarting = false
		if msg.err != nil {
			m.result = fmt.Sprintf(" Tour failed to start: %v", msg.err)
			return m, nil, true
		}
		m.tour = msg.run
		m.result = fmt.Sprintf(" Tour %s running — click through the screens in the PiKVM web UI. s: capture now  S: stop", msg.run.name)
		return m, waitTourEvent(msg.run), true

	case tourEventMsg:
		if m.tour == nil || msg.run != m.tour {
			return m, nil, true
		}
		switch msg.ev.Kind {
		case "saved":
			m.tour.saved = msg.ev.Index
			how := "new screen"
			if msg.ev.Manual {
				how = "manual"
			}
			m.result = fmt.Sprintf("  %s: #%d saved (%s, %.0f%% changed)", m.tour.name, msg.ev.Index, how, msg.ev.Change*100)
			m2, cmd := m.showPreviewOf(msg.ev.Path, fmt.Sprintf("%s #%d · %s", m.tour.name, msg.ev.Index, time.Now().Format("15:04:05")))
			return m2, tea.Batch(cmd, waitTourEvent(m2.tour)), true
		case "error":
			m.result = fmt.Sprintf(" Tour %s: %v", m.tour.name, msg.ev.Err)
			return m, waitTourEvent(m.tour), true
		case "done":
			run := m.tour
			m.tour = nil
			n := 0
			if msg.ev.Meta != nil {
				n = len(msg.ev.Meta.Frames)
			}
			m.result = fmt.Sprintf(" Tour %s saved: %d screens → %s (contact-sheet.jpg, index.html)", run.name, n, msg.ev.Path)
			if msg.ev.Err != nil {
				m.result = fmt.Sprintf(" Tour %s saved %d screens to %s, but: %v", run.name, n, msg.ev.Path, msg.ev.Err)
			}
			if n > 0 {
				sheet := filepath.Join(msg.ev.Path, "contact-sheet.jpg")
				if _, err := os.Stat(sheet); err == nil {
					m2, cmd := m.showPreviewOf(sheet, fmt.Sprintf("%s · contact sheet (%d screens)", run.name, n))
					return m2, cmd, true
				}
			}
			return m, nil, true
		}
		return m, waitTourEvent(m.tour), true

	case previewReadyMsg:
		if msg.err != nil {
			m.result += fmt.Sprintf("  (preview: %v)", msg.err)
			return m, nil, true
		}
		m.preview = msg.img
		m.previewCaption = msg.caption
		return m, nil, true
	}
	return m, nil, false
}

// renderPreviewBlock is the preview with its caption, or "" when hidden.
func (m Model) renderPreviewBlock() string {
	if !m.previewFits() {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n  " + unselectedStyle.Render(" "+m.previewCaption) + "\n")
	for _, line := range m.preview.Lines {
		b.WriteString("  " + line + "\n")
	}
	b.WriteString("  " + helpStyle.Render("v: hide preview") + "\n")
	return b.String()
}

// renderTourNamePrompt is the modal shown after S.
func (m Model) renderTourNamePrompt() string {
	var s strings.Builder
	s.WriteString("\n")
	s.WriteString(headerStyle.Render("  Start a tour ") + "\n\n")
	s.WriteString("  " + portInfoStyle.Render(fmt.Sprintf(" Host: %s   port %d", config.HostName, m.port+1)) + "\n\n")
	s.WriteString("  " + unselectedStyle.Render("Name (e.g. bios-1605, bios-2503-after):") + "\n")
	s.WriteString("  " + selectedStyle.Render(" "+m.tourNameBuf+"█ ") + "\n\n")
	s.WriteString("  " + helpStyle.Render("Then click through the screens in the PiKVM web UI. One screenshot is") + "\n")
	s.WriteString("  " + helpStyle.Render("saved per new screen; moving the highlight bar does not count.") + "\n\n")
	s.WriteString("  " + helpStyle.Render("Enter: start   Backspace: delete   Ctrl+U: clear   ESC: cancel") + "\n\n")
	return s.String()
}

// tourStatusPart is the status-bar chunk for a running tour.
func (m Model) tourStatusPart() string {
	if m.tour == nil {
		return ""
	}
	return errorStyle.Render("●") + " " + warningStyle.Render(fmt.Sprintf("TOUR %s · %d screens", m.tour.name, m.tour.saved))
}
