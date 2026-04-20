package cli

import "testing"

// TestIsLikelyName covers the profile-name vs numeric discrimination. The
// tricky case is "j4yn0" and "web03" which contain digits but are names.
func TestIsLikelyName(t *testing.T) {
	cases := []struct {
		in   string
		name bool
	}{
		{"5", false},
		{"2.3", false},
		{"j4yn0", true},
		{"web03", true},
		{"my-host", true},
		{"my_host", true},
		{"", false},
	}
	for _, c := range cases {
		if got := isLikelyName(c.in); got != c.name {
			t.Errorf("isLikelyName(%q) = %v, want %v", c.in, got, c.name)
		}
	}
}
