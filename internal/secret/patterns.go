package secret

import (
	"strings"
	"sync/atomic"
)

// patterns.go is the user-configurable half of secret masking (#1712). The
// built-in tables in secret.go are a guess about naming conventions, and a
// guess is wrong in both directions: a house-style key (`MY_API_KEY_V2`,
// `*_LICENSE`) names a credential the tables never heard of, while a key the
// tables mask (`PUBLIC_TOKEN`) may hold nothing worth hiding. The list here
// lets the user say it outright, and what the user says is final:
//
//   - A positive entry (`db_pass*`) masks every key it matches, whatever the
//     built-in tables think.
//   - A negative entry (`-PUBLIC_TOKEN`, or `!PUBLIC_TOKEN`) exempts every key
//     it matches, so a key the built-ins would mask stays readable.
//   - Earlier entries win, so a specific name can precede a wildcard covering
//     it, and a key no entry matches falls through to the built-in tables.
//
// The list is a process-wide global for the same reason numhint's field units
// are: the producer is a lang.Language.Spans hook with no config plumbing of
// its own. app pushes editor.secret_masking_keys into it on every config load.

// keyRule is one configured pattern plus what a match means.
type keyRule struct {
	pattern string // lowercase, `*` wildcards, matched over the whole key
	exempt  bool   // true for a `-`/`!` entry: match means "never mask"
}

// keyRules holds the installed list, installedKeys the entries it was built
// from so a config reload can tell whether anything actually changed. Atomic
// pointers: config reloads run on the UI loop while span production may read
// from a render elsewhere.
var (
	keyRules      atomic.Pointer[[]keyRule]
	installedKeys atomic.Pointer[string]
)

// SetKeyPatterns installs the user's custom key patterns. Empty entries are
// skipped rather than failing the whole list — a stray blank line must not
// silence the rest — as is a bare `-`, which names no key.
//
// It reports whether the list changed, which is what tells the app a config
// reload has to re-parse the open editors: the spans are cached until then.
func SetKeyPatterns(entries []string) bool {
	joined := strings.Join(entries, "\n")
	if prev := installedKeys.Load(); prev != nil && *prev == joined {
		return false
	}
	installedKeys.Store(&joined)
	var out []keyRule
	for _, e := range entries {
		p := strings.ToLower(strings.TrimSpace(e))
		exempt := false
		if strings.HasPrefix(p, "-") || strings.HasPrefix(p, "!") {
			exempt = true
			p = strings.TrimSpace(p[1:])
		} else if strings.HasPrefix(p, "+") {
			p = strings.TrimSpace(p[1:])
		}
		if p == "" {
			continue
		}
		out = append(out, keyRule{pattern: p, exempt: exempt})
	}
	keyRules.Store(&out)
	return true
}

// keyVerdict returns what the configured patterns say about key: masked/not,
// whether any entry matched at all, and the pattern of the entry that did —
// the explain popover names it (#1998). A key nothing matches is left to the
// built-in tables.
func keyVerdict(key string) (mask, matched bool, pattern string) {
	rs := keyRules.Load()
	if rs == nil || len(*rs) == 0 || key == "" {
		return false, false, ""
	}
	lower := strings.ToLower(key)
	for _, r := range *rs {
		if globMatch(r.pattern, lower) {
			return !r.exempt, true, r.pattern
		}
	}
	return false, false, ""
}

// globMatch reports whether s matches pattern, where `*` stands for any run of
// characters (including none) and every other character matches itself. Own
// matcher rather than path.Match: `/` and `\` must be ordinary characters here
// — a key name may hold either, and neither has any business being a
// separator in this position.
func globMatch(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	last := parts[len(parts)-1]
	for _, p := range parts[1 : len(parts)-1] {
		i := strings.Index(s, p)
		if i < 0 {
			return false
		}
		s = s[i+len(p):]
	}
	if len(last) > len(s) {
		return false
	}
	return strings.HasSuffix(s, last)
}
