package config

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// ThresholdUnset marks a complexity.overrides key the entry leaves out: the
// global threshold applies to it.
const ThresholdUnset = -1

// ThresholdOverride is one parsed complexity.overrides entry (#2960):
// `<selector> ccn=N cog=N cmt=N`. The selector is either a glob over the
// project-relative path (Glob, the complexity.exclude rules) or a language
// id (Lang, written `lang:<id>`); CCN, Cog and Cmt are the thresholds the
// entry sets, ThresholdUnset for a key it leaves out.
type ThresholdOverride struct {
	Glob string
	Lang string
	CCN  int
	Cog  int
	Cmt  int
}

// Selector is the entry's selector as written: the glob, or "lang:<id>".
func (o ThresholdOverride) Selector() string {
	if o.Lang != "" {
		return "lang:" + o.Lang
	}
	return o.Glob
}

// thresholdKeys are the keys an override entry takes, with their ranges —
// the bounds of complexity.ccn_warn / cognitive_warn / comment_density_warn.
var thresholdKeys = []struct {
	name     string
	min, max int
}{{"ccn", 1, 200}, {"cog", 1, 200}, {"cmt", 0, 100}}

// ParseThresholdOverride parses one complexity.overrides entry. The error
// names the entry and what is wrong with it: a missing or malformed
// selector, an unknown or repeated key, a value outside its range, or no key
// at all.
func ParseThresholdOverride(entry string) (ThresholdOverride, error) {
	o := ThresholdOverride{CCN: ThresholdUnset, Cog: ThresholdUnset, Cmt: ThresholdUnset}
	fail := func(format string, a ...any) (ThresholdOverride, error) {
		return ThresholdOverride{}, fmt.Errorf("override %q: %s", strings.TrimSpace(entry), fmt.Sprintf(format, a...))
	}
	fields := strings.Fields(entry)
	if len(fields) == 0 {
		return fail("empty entry")
	}
	sel := fields[0]
	switch {
	case strings.Contains(sel, "="):
		return fail("needs a selector (a path glob or lang:<id>) before the thresholds")
	case strings.HasPrefix(sel, "lang:"):
		o.Lang = strings.TrimPrefix(sel, "lang:")
		if o.Lang == "" {
			return fail("lang: needs a language id (lang:php)")
		}
	default:
		if _, err := filepath.Match(sel, "x"); err != nil {
			return fail("invalid glob pattern %q", sel)
		}
		o.Glob = sel
	}
	slots := []*int{&o.CCN, &o.Cog, &o.Cmt}
	for _, f := range fields[1:] {
		name, val, ok := strings.Cut(f, "=")
		idx := -1
		for i, k := range thresholdKeys {
			if k.name == name {
				idx = i
			}
		}
		if !ok || idx < 0 {
			return fail("unknown threshold %q (want ccn=, cog= or cmt=)", f)
		}
		k := thresholdKeys[idx]
		if *slots[idx] != ThresholdUnset {
			return fail("%s= given twice", k.name)
		}
		n, err := strconv.Atoi(val)
		if err != nil || n < k.min || n > k.max {
			return fail("%s=%s out of range (%d–%d)", k.name, val, k.min, k.max)
		}
		*slots[idx] = n
	}
	if o.CCN == ThresholdUnset && o.Cog == ThresholdUnset && o.Cmt == ThresholdUnset {
		return fail("sets no threshold (want ccn=, cog= or cmt=)")
	}
	return o, nil
}

// ThresholdOverrides parses complexity.overrides in list order, skipping an
// entry that does not parse (Validate has already dropped those from a
// loaded config).
func (c Complexity) ThresholdOverrides() []ThresholdOverride {
	out := make([]ThresholdOverride, 0, len(c.Overrides))
	for _, e := range c.Overrides {
		if o, err := ParseThresholdOverride(e); err == nil {
			out = append(out, o)
		}
	}
	return out
}
