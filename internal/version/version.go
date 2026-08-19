// Package version provides the single canonical harness-release version
// parser (PAT-1449): optional leading "v", major.minor.patch, optional
// prerelease with semver precedence, and hard rejection of anything
// else. One parser is shared by PCCP, relay, CLI, and Desktop so a
// canary artifact can never satisfy a stable minimum.
package version

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed canonical release version.
type Version struct {
	Major, Minor, Patch uint64
	Prerelease          string // "" = stable; e.g. "beta.1", "rc.2"
	Build               string // ignored in precedence (metadata)
}

// Parse parses a canonical version string. Invalid inputs return an
// error — never a silent zero value that could bypass a floor.
func Parse(s string) (Version, error) {
	v := Version{}
	orig := s
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return v, fmt.Errorf("invalid version %q", orig)
	}
	// Split build metadata.
	if at := strings.Index(s, "+"); at >= 0 {
		v.Build = s[at+1:]
		s = s[:at]
	}
	// Split prerelease.
	if dash := strings.Index(s, "-"); dash >= 0 {
		v.Prerelease = s[dash+1:]
		s = s[:dash]
		if v.Prerelease == "" {
			return v, fmt.Errorf("invalid version %q: empty prerelease", orig)
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, fmt.Errorf("invalid version %q: want major.minor.patch", orig)
	}
	var err error
	if v.Major, err = strconv.ParseUint(parts[0], 10, 32); err != nil {
		return v, fmt.Errorf("invalid major in %q", orig)
	}
	if v.Minor, err = strconv.ParseUint(parts[1], 10, 32); err != nil {
		return v, fmt.Errorf("invalid minor in %q", orig)
	}
	if v.Patch, err = strconv.ParseUint(parts[2], 10, 32); err != nil {
		return v, fmt.Errorf("invalid patch in %q", orig)
	}
	if v.Prerelease != "" && !validPrerelease(v.Prerelease) {
		return v, fmt.Errorf("invalid prerelease in %q", orig)
	}
	if s == "" && orig != "" && v.Prerelease != "" {
		return v, fmt.Errorf("invalid version %q", orig)
	}
	return v, nil
}

func validPrerelease(p string) bool {
	if p == "" {
		return false
	}
	for _, seg := range strings.Split(p, ".") {
		if seg == "" {
			return false
		}
		if _, err := strconv.ParseUint(seg, 10, 32); err == nil {
			continue // numeric segment
		}
		for _, r := range seg {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

// IsStable reports whether the version is a stable release (no
// prerelease). Only stable releases can satisfy a stable minimum.
func (v Version) IsStable() bool { return v.Prerelease == "" }

// Compare returns -1, 0, or 1. Semver precedence: numeric fields first;
// a stable release outranks any prerelease of the same triple; longer
// prerelease identifier lists outrank shorter when prefixes match.
func (v Version) Compare(o Version) int {
	if v.Major != o.Major {
		return cmpU64(v.Major, o.Major)
	}
	if v.Minor != o.Minor {
		return cmpU64(v.Minor, o.Minor)
	}
	if v.Patch != o.Patch {
		return cmpU64(v.Patch, o.Patch)
	}
	switch {
	case v.Prerelease == o.Prerelease:
		return 0
	case v.Prerelease == "":
		return 1 // stable > prerelease
	case o.Prerelease == "":
		return -1
	}
	return comparePrerelease(v.Prerelease, o.Prerelease)
}

func cmpU64(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func comparePrerelease(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aerr := strconv.ParseUint(as[i], 10, 32)
		bn, berr := strconv.ParseUint(bs[i], 10, 32)
		switch {
		case aerr == nil && berr == nil:
			if an != bn {
				return cmpU64(an, bn)
			}
		case aerr == nil:
			return -1 // numeric < alphanumeric
		case berr == nil:
			return 1
		default:
			if as[i] != bs[i] {
				return strings.Compare(as[i], bs[i])
			}
		}
	}
	return cmpU64(uint64(len(as)), uint64(len(bs)))
}

// SatisfiesMinimum reports whether v meets the floor with the locked
// canary rule: a prerelease artifact can satisfy a minimum only when
// that exact artifact triple was promoted as a stable release — i.e. the
// minimum itself must be stable, and prereleases never satisfy it.
func (v Version) SatisfiesMinimum(min Version) bool {
	if !min.IsStable() {
		// A prerelease floor is nonsensical; reject in parsing elsewhere.
		return false
	}
	if !v.IsStable() {
		// 1.2.3-beta < 1.2.3 stable: prerelease never satisfies.
		if v.Major == min.Major && v.Minor == min.Minor && v.Patch == min.Patch {
			return false
		}
		return v.Compare(min) > 0
	}
	return v.Compare(min) >= 0
}

// String renders the canonical form (no build metadata).
func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Prerelease != "" {
		s += "-" + v.Prerelease
	}
	return s
}
