package sandbox

import "testing"

func TestImageAllowlisted(t *testing.T) {
	allowed := []string{"patty/sandbox-base", "registry.io/team/tools:1.2.3", "patty/strict:*"}
	cases := []struct {
		image string
		want  bool
	}{
		{"patty/sandbox-base:latest", true},       // repo pin → any tag
		{"patty/sandbox-base", true},              // exact bare
		{"patty/sandbox-base@sha256:abc", true},   // digest form of repo pin
		{"patty/sandbox-base-evil:latest", false}, // repo-prefix trick must fail
		{"registry.io/team/tools:1.2.3", true},    // exact tag pin
		{"registry.io/team/tools:1.2.4", false},   // other tag refused
		{"registry.io/team/toolsome:1", false},    // repo-prefix trick must fail
		{"patty/strict:9.9.9", true},              // explicit wildcard tag
		{"evil/base:latest", false},               // unrelated
		{"", false},
	}
	for _, c := range cases {
		if got := imageAllowlisted(c.image, allowed); got != c.want {
			t.Errorf("imageAllowlisted(%q) = %v, want %v", c.image, got, c.want)
		}
	}
}
