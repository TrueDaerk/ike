package market

// version.go implements the minimal MAJOR.MINOR.PATCH version scheme the
// marketplace uses to decide whether a catalog entry is newer than an
// installed plugin (Roadmap 0310, #444). It is deliberately not full semver —
// no pre-release or build metadata — because the catalog is the only producer
// of these strings and keeps them simple; anything else fails to parse and the
// entry is rejected at validation time.

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed MAJOR.MINOR.PATCH version.
type Version struct {
	Major, Minor, Patch int
}

// ParseVersion parses "MAJOR.MINOR.PATCH" with plain decimal components.
// A leading "v" is tolerated (authors type it); anything else is an error.
func ParseVersion(s string) (Version, error) {
	raw := strings.TrimPrefix(strings.TrimSpace(s), "v")
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("version %q: want MAJOR.MINOR.PATCH", s)
	}
	var nums [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || (len(p) > 1 && p[0] == '0') {
			return Version{}, fmt.Errorf("version %q: bad component %q", s, p)
		}
		nums[i] = n
	}
	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2]}, nil
}

// Compare returns -1, 0 or 1 as v is older than, equal to, or newer than o.
func (v Version) Compare(o Version) int {
	for _, d := range [3]int{v.Major - o.Major, v.Minor - o.Minor, v.Patch - o.Patch} {
		if d < 0 {
			return -1
		}
		if d > 0 {
			return 1
		}
	}
	return 0
}

// String renders the canonical "MAJOR.MINOR.PATCH" form.
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}
