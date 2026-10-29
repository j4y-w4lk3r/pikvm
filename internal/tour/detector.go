package tour

// Default thresholds, estimated for 1080p ASUS-style BIOS pages (verify
// against a real tour; tour.json records the change value of every frame).
//
// A frame counts as a new screen only when it differs from every saved frame
// in both ways:
//   - area: at least MinChange of the fingerprint cells changed, and
//   - spread: changed cells appear in at least MinRows of the 36 rows.
//
// Moving the highlight bar changes up to ~10% of cells but only in a few
// rows (old bar, new bar, help text). A new tab, submenu or scrolled list
// changes rows over most of the screen height. The hardware-monitor panel
// ticking stays under SettleMax, so it never blocks "settled".
const (
	DefaultMinChange = 0.06
	DefaultMinRows   = 9
	DefaultSettleMax = 0.015
)

// Detector decides which observed frames are worth saving.
type Detector struct {
	MinChange float64 // changed-cell fraction needed vs every saved frame
	MinRows   int     // changed rows needed vs every saved frame
	SettleMax float64 // two polls closer than this count as "settled"

	prev  *Fingerprint
	saved []Fingerprint
}

// NewDetector returns a detector with default thresholds unless overridden.
func NewDetector(minChange float64) *Detector {
	if minChange <= 0 {
		minChange = DefaultMinChange
	}
	return &Detector{MinChange: minChange, MinRows: DefaultMinRows, SettleMax: DefaultSettleMax}
}

// Observe feeds one polled frame. It returns save=true when the screen has
// settled on something not saved before, plus the changed-cell fraction
// against the most similar saved frame (1 for the first frame).
func (d *Detector) Observe(fp Fingerprint) (save bool, change float64) {
	settled := d.prev != nil && Diff(fp, *d.prev) <= d.SettleMax
	cp := fp
	d.prev = &cp
	if !settled {
		return false, 0
	}
	change, isNew := d.nearestSaved(fp)
	if !isNew {
		return false, change
	}
	d.saved = append(d.saved, fp)
	return true, change
}

// Force records fp as saved regardless of thresholds (manual capture).
func (d *Detector) Force(fp Fingerprint) float64 {
	change, _ := d.nearestSaved(fp)
	d.saved = append(d.saved, fp)
	return change
}

// Saved returns how many frames the detector has accepted.
func (d *Detector) Saved() int { return len(d.saved) }

// nearestSaved returns the smallest changed-cell fraction to any saved frame
// and whether fp is new compared with all of them.
func (d *Detector) nearestSaved(fp Fingerprint) (float64, bool) {
	if len(d.saved) == 0 {
		return 1, true
	}
	best := 1.0
	isNew := true
	for _, s := range d.saved {
		frac, rows := DiffStats(fp, s)
		if frac < best {
			best = frac
		}
		if frac < d.MinChange || rows < d.MinRows {
			isNew = false
		}
	}
	return best, isNew
}
