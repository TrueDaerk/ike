package complexity

import (
	"strconv"
	"strings"

	"ike/internal/config"
	"ike/internal/lang"
)

// Thresholds are the warning thresholds a file's functions are judged by
// (#2960): CCN and Cog the cyclomatic and cognitive limits (a function at or
// above one is over threshold), Cmt the comment-density limit in percent (0
// turns that check off). Source is the complexity.overrides selector that
// set them — "" for the global complexity.ccn_warn / cognitive_warn /
// comment_density_warn values.
type Thresholds struct {
	CCN    int
	Cog    int
	Cmt    int
	Source string
}

// Over reports whether f's CCN or cognitive complexity is at or above t.
func (t Thresholds) Over(f Function) bool { return f.Over(t.CCN, t.Cog) }

// Excess is f's excess above t's CCN and cognitive thresholds.
func (t Thresholds) Excess(f Function) int { return f.Excess(t.CCN, t.Cog) }

// CommentsOver reports whether f's comment density is at or above t.Cmt.
func (t Thresholds) CommentsOver(f Function) bool { return f.CommentsOver(t.Cmt) }

// Overridden reports whether an override entry set t.
func (t Thresholds) Overridden() bool { return t.Source != "" }

// Label renders t for a header: "ccn 30 cog 30 (legacy/**)", the comment
// density appended when it differs from global's; "" for the globals.
func (t Thresholds) Label(global Thresholds) string {
	if !t.Overridden() {
		return ""
	}
	s := "ccn " + strconv.Itoa(t.CCN) + " cog " + strconv.Itoa(t.Cog)
	if t.Cmt != global.Cmt {
		s += " cmt " + strconv.Itoa(t.Cmt) + "%"
	}
	return s + " (" + t.Source + ")"
}

// Resolver maps an absolute file path to the thresholds its functions are
// judged by — every threshold consumer takes one, so a per-path or
// per-language override reaches it.
type Resolver func(path string) Thresholds

// Uniform is the resolver of the global case: t for every path.
func Uniform(t Thresholds) Resolver { return func(string) Thresholds { return t } }

// Global is the configured global thresholds of c.
func Global(c config.Complexity) Thresholds {
	return Thresholds{CCN: c.CCNWarn, Cog: c.CognitiveWarn, Cmt: c.CommentDensityWarn}
}

// ThresholdsFor resolves path's thresholds under c (#2960): the first
// complexity.overrides entry whose selector matches — a glob against the
// project-relative path under root (the complexity.exclude rules), or
// lang:<id> against the language the path resolves to — with the keys it
// leaves out filled from the globals; the globals when none matches.
func ThresholdsFor(c config.Complexity, root, path string) Thresholds {
	return resolve(Global(c), c.ThresholdOverrides(), root, path)
}

// ResolverFor is ThresholdsFor with the overrides parsed once; with no
// override it is Uniform over the globals.
func ResolverFor(c config.Complexity, root string) Resolver {
	global, overrides := Global(c), c.ThresholdOverrides()
	if len(overrides) == 0 {
		return Uniform(global)
	}
	return func(path string) Thresholds { return resolve(global, overrides, root, path) }
}

// resolve is the first-match walk over overrides.
func resolve(global Thresholds, overrides []config.ThresholdOverride, root, path string) Thresholds {
	langID := ""
	for _, o := range overrides {
		if o.Lang != "" {
			if langID == "" {
				langID = "\x00" // looked up, none
				if l, ok := lang.ByPath(path); ok {
					langID = l.ID
				}
			}
			if !strings.EqualFold(langID, o.Lang) {
				continue
			}
		} else if !Excluded(root, []string{o.Glob}, path) {
			continue
		}
		t := global
		t.Source = o.Selector()
		if o.CCN != config.ThresholdUnset {
			t.CCN = o.CCN
		}
		if o.Cog != config.ThresholdUnset {
			t.Cog = o.Cog
		}
		if o.Cmt != config.ThresholdUnset {
			t.Cmt = o.Cmt
		}
		return t
	}
	return global
}
