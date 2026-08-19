package version

import "testing"

func TestParseForms(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"1.2.3", "1.2.3", true},
		{"v1.2.3", "1.2.3", true},
		{"1.2.3-beta.1", "1.2.3-beta.1", true},
		{"v2.0.0-rc.2", "2.0.0-rc.2", true},
		{"1.2.3+build.9", "1.2.3", true},
		{"1.2", "", false},
		{"1.2.x", "", false},
		{"dev", "", false},
		{"", "", false},
		{"01.2.3", "1.2.3", true}, // ParseUint accepts leading zero; canonical rendering normalizes
		{"1.2.3-", "", false},
		{"1.2.3-!", "", false},
	}
	for _, c := range cases {
		v, err := Parse(c.in)
		if c.ok && err != nil {
			t.Fatalf("Parse(%q): %v", c.in, err)
		}
		if !c.ok {
			if err == nil {
				t.Fatalf("Parse(%q) accepted, want rejection", c.in)
			}
			continue
		}
		if v.String() != c.want {
			t.Fatalf("Parse(%q) = %s, want %s", c.in, v.String(), c.want)
		}
	}
}

func TestPrecedence(t *testing.T) {
	ordered := []string{"1.2.3-alpha.1", "1.2.3-alpha.2", "1.2.3-beta", "1.2.3-beta.1", "1.2.3-rc.1", "1.2.3", "1.2.4", "1.3.0", "2.0.0"}
	for i := 0; i+1 < len(ordered); i++ {
		a, _ := Parse(ordered[i])
		b, _ := Parse(ordered[i+1])
		if a.Compare(b) >= 0 {
			t.Fatalf("%s should rank below %s", ordered[i], ordered[i+1])
		}
	}
}

func TestCanaryCannotSatisfyStableMinimum(t *testing.T) {
	min, _ := Parse("1.5.0")
	cases := []struct {
		in   string
		want bool
	}{
		{"1.5.0", true},
		{"v1.6.1", true},
		{"1.5.0-beta.2", false}, // same triple prerelease
		{"1.5.1-rc.1", true},    // higher triple prerelease still above floor
		{"1.4.9", false},
	}
	for _, c := range cases {
		v, err := Parse(c.in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.in, err)
		}
		if got := v.SatisfiesMinimum(min); got != c.want {
			t.Fatalf("%s.SatisfiesMinimum(1.5.0) = %v, want %v", c.in, got, c.want)
		}
	}
	// A prerelease minimum is never satisfiable (defense in depth).
	pre, _ := Parse("2.0.0-beta.1")
	stable, _ := Parse("2.0.0")
	if stable.SatisfiesMinimum(pre) {
		t.Fatal("prerelease floor accepted a stable version")
	}
}
