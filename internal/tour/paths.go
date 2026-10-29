package tour

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PicturesRoot is where snaps and tours live:
// $PIKVM_PICTURES_DIR, else ~/Pictures/pikvm.
func PicturesRoot() (string, error) {
	if env := os.Getenv("PIKVM_PICTURES_DIR"); env != "" {
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Pictures", "pikvm"), nil
}

// SanitizeName turns free text into a safe directory name:
// lowercase letters, digits, '.', '_' and '-'; spaces become '-'.
func SanitizeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		case r == ' ' || r == '/':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), ".-")
	if out == "" {
		out = DefaultName(time.Now())
	}
	return out
}

// DefaultName is the suggested tour name, e.g. tour-20261029-1912.
func DefaultName(t time.Time) string {
	return "tour-" + t.Format("20060102-1504")
}

// hostDir returns root/<host>, falling back to "pikvm" for an empty host.
func hostDir(root, host string) string {
	if host == "" {
		host = "pikvm"
	}
	return filepath.Join(root, SanitizeName(host))
}

// TourDir picks root/<host>/tours/<name>, adding -2, -3, … instead of
// reusing an existing tour directory.
func TourDir(root, host, name string) string {
	base := filepath.Join(hostDir(root, host), "tours", name)
	dir := base
	for i := 2; ; i++ {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return dir
		}
		dir = fmt.Sprintf("%s-%d", base, i)
	}
}

// NextSnapPath returns root/<host>/snaps/<YYYY-MM-DD>/NN.jpg with the next
// free number for that day, plus the number.
func NextSnapPath(root, host string, now time.Time) (string, int, error) {
	dir := filepath.Join(hostDir(root, host), "snaps", now.Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	for n := 1; ; n++ {
		p := filepath.Join(dir, fmt.Sprintf("%02d.jpg", n))
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p, n, nil
		}
	}
}
