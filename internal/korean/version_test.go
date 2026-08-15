package korean

import "testing"

func TestIsVersionBelowFloor(t *testing.T) {
	cases := []struct {
		version, floor string
		want           bool
	}{
		{"1.0.0", "1.2.0", true},
		{"1.2.0", "1.2.0", false},
		{"1.3.0", "1.2.0", false},
		{"2.0.0", "1.9.9", false},
		{"0.9.5", "1.0.0", true},
		{"1.0", "1.0.1", true},
		{"", "1.0.0", true},     // unknown version fails closed
		{"1.0.0", "", false},    // no floor → never blocked
		{"1.2", "1.2.1", true},  // missing patch treated as 0
		{"1.2.10", "1.2.9", false},
		{"v1.0.0", "1.1.0", true}, // leading v tolerated
	}
	for _, c := range cases {
		if got := IsVersionBelowFloor(c.version, c.floor); got != c.want {
			t.Errorf("IsVersionBelowFloor(%q, %q) = %v, want %v", c.version, c.floor, got, c.want)
		}
	}
}
