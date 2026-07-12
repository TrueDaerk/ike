package editorconfig

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// glob.go translates EditorConfig section globs into regular expressions and
// matches them against slash-separated paths relative to the .editorconfig's
// directory. Supported special characters, per the spec:
//
//	*            any string, except path separators
//	**           any string, including path separators
//	?            any single character, except a path separator
//	[seq]        any single character in seq
//	[!seq]       any single character not in seq
//	{s1,s2,s3}   any of the strings (nestable); a brace pair without a
//	             top-level comma is literal (e.g. "{single}")
//	{n1..n2}     any integer between n1 and n2 inclusive
//	\c           character c, literally
//
// A pattern containing a '/' is anchored to the .editorconfig's directory
// (a leading '/' is equivalent); one without matches in any subdirectory.

// patternCache memoizes compiled patterns — the same "[*.go]" section is
// matched for every buffer in the project.
var (
	patternMu    sync.Mutex
	patternCache = map[string]*regexp.Regexp{}
)

// match reports whether the section pattern matches rel, a slash-separated
// path relative to the pattern's .editorconfig directory. Unmatchable or
// oversized patterns match nothing.
func match(pattern, rel string) bool {
	patternMu.Lock()
	re, ok := patternCache[pattern]
	if !ok {
		re = compile(pattern)
		patternCache[pattern] = re
	}
	patternMu.Unlock()
	return re != nil && re.MatchString(rel)
}

// compile turns one section glob into an anchored regexp; nil when the
// resulting expression is invalid.
func compile(pattern string) *regexp.Regexp {
	p := pattern
	anchored := false
	if strings.HasPrefix(p, "/") {
		p = p[1:]
		anchored = true
	} else if strings.Contains(p, "/") {
		anchored = true
	}
	body := translate(p)
	if !anchored {
		// A bare-name pattern like "*.go" matches at any depth.
		body = "(?:.*/)?" + body
	}
	re, err := regexp.Compile("^" + body + "$")
	if err != nil {
		return nil
	}
	return re
}

// translate converts glob syntax to regexp syntax, recursing into brace
// groups.
func translate(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch c {
		case '\\':
			if i+1 < len(p) {
				i++
				b.WriteString(regexp.QuoteMeta(string(p[i])))
			} else {
				b.WriteString(regexp.QuoteMeta(`\`))
			}
		case '*':
			if i+1 < len(p) && p[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '[':
			cls, next, ok := charClass(p, i)
			if ok {
				b.WriteString(cls)
				i = next
			} else {
				b.WriteString(regexp.QuoteMeta("["))
			}
		case '{':
			grp, next, ok := braceGroup(p, i)
			if ok {
				b.WriteString(grp)
				i = next
			} else {
				b.WriteString(regexp.QuoteMeta("{"))
			}
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}

// charClass translates a "[seq]"/"[!seq]" starting at p[i]; ok is false when
// the class never closes (the '[' is then literal).
func charClass(p string, i int) (cls string, next int, ok bool) {
	j := i + 1
	negate := j < len(p) && p[j] == '!'
	if negate {
		j++
	}
	var content strings.Builder
	for ; j < len(p); j++ {
		switch p[j] {
		case '\\':
			if j+1 < len(p) {
				j++
				content.WriteString(regexp.QuoteMeta(string(p[j])))
			}
		case ']':
			if content.Len() == 0 {
				// "[]" or "[!]" — treat the ']' as content, per fnmatch.
				content.WriteString(regexp.QuoteMeta("]"))
				continue
			}
			open := "["
			if negate {
				open = "[^"
			}
			return open + content.String() + "]", j, true
		case '-':
			content.WriteString("-") // keep ranges like a-z working
		default:
			content.WriteString(regexp.QuoteMeta(string(p[j])))
		}
	}
	return "", i, false
}

// braceGroup translates a "{...}" starting at p[i]: a top-level-comma group
// becomes an alternation, "{n1..n2}" a numeric alternation, and anything else
// literal braces around the translated content. ok is false when the group
// never closes.
func braceGroup(p string, i int) (grp string, next int, ok bool) {
	depth := 0
	j := i
	for ; j < len(p); j++ {
		switch p[j] {
		case '\\':
			j++
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				goto closed
			}
		}
	}
	return "", i, false
closed:
	inner := p[i+1 : j]
	if parts, has := splitTopLevel(inner); has {
		alts := make([]string, len(parts))
		for k, part := range parts {
			alts[k] = translate(part)
		}
		return "(?:" + strings.Join(alts, "|") + ")", j, true
	}
	if lo, hi, isRange := numRange(inner); isRange {
		var alts []string
		for n := lo; n <= hi; n++ {
			alts = append(alts, regexp.QuoteMeta(strconv.Itoa(n)))
		}
		return "(?:" + strings.Join(alts, "|") + ")", j, true
	}
	// "{single}" is literal per the spec.
	return regexp.QuoteMeta("{") + translate(inner) + regexp.QuoteMeta("}"), j, true
}

// splitTopLevel splits inner on commas outside nested braces; has is false
// when there is no top-level comma at all.
func splitTopLevel(inner string) (parts []string, has bool) {
	depth := 0
	start := 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, inner[start:i])
				start = i + 1
				has = true
			}
		}
	}
	parts = append(parts, inner[start:])
	return parts, has
}

// numRange parses "n1..n2"; the expansion is capped so a pathological range
// cannot blow up the regexp.
func numRange(inner string) (lo, hi int, ok bool) {
	dots := strings.Index(inner, "..")
	if dots < 0 {
		return 0, 0, false
	}
	lo, err1 := strconv.Atoi(inner[:dots])
	hi, err2 := strconv.Atoi(inner[dots+2:])
	if err1 != nil || err2 != nil || lo > hi || hi-lo > 4096 {
		return 0, 0, false
	}
	return lo, hi, true
}
