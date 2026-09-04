// Package pathglob matches shell-style globs against slash-separated paths.
//
// It grew out of the LSP watched-files matcher (#1144) and is shared with the
// conceal file filter (#1704), which needs the same vocabulary to decide where
// a stand-in family may draw. Supported: `**` (any number of path segments,
// including none), `*` (any run within one segment), `?` (one character within
// a segment), `{a,b}` (alternation, nestable) and `[...]` (character class
// with ranges and `!`/`^` negation).
//
// Limits, documented rather than solved: matching is byte-wise (multi-byte
// runes count per byte under `?`/`[...]`), there is no escape character, and
// an unterminated group matches literally.
package pathglob

import "strings"

// Match reports whether pattern matches s. Both are slash-separated; s is
// matched in full, so an anchored pattern (`src/**/*.go`) only matches paths
// starting at that segment.
func Match(pattern, s string) bool {
	// Expand top-level {a,b} groups first: each alternative becomes its own
	// pattern. Nested groups expand recursively.
	if alts, ok := expandBraces(pattern); ok {
		for _, alt := range alts {
			if Match(alt, s) {
				return true
			}
		}
		return false
	}
	return matchGlob(pattern, s)
}

// expandBraces rewrites the first {…} group into one pattern per alternative.
// ok=false when the pattern has no (complete) group.
func expandBraces(pattern string) ([]string, bool) {
	open := strings.IndexByte(pattern, '{')
	if open < 0 {
		return nil, false
	}
	depth := 0
	var alts []string
	segStart := open + 1
	for i := open; i < len(pattern); i++ {
		switch pattern[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				alts = append(alts, pattern[segStart:i])
				out := make([]string, 0, len(alts))
				for _, a := range alts {
					out = append(out, pattern[:open]+a+pattern[i+1:])
				}
				return out, true
			}
		case ',':
			if depth == 1 {
				alts = append(alts, pattern[segStart:i])
				segStart = i + 1
			}
		}
	}
	return nil, false // unterminated: treat literally
}

// matchGlob matches a brace-free pattern against s.
func matchGlob(p, s string) bool {
	if p == "" {
		return s == ""
	}
	if strings.HasPrefix(p, "**") {
		rest := strings.TrimPrefix(p, "**")
		rest = strings.TrimPrefix(rest, "/")
		if rest == "" {
			return true
		}
		// Try the remainder at every segment boundary, including the start
		// ( `**/` matches zero directories).
		if matchGlob(rest, s) {
			return true
		}
		for i := 0; i < len(s); i++ {
			if s[i] == '/' && matchGlob(rest, s[i+1:]) {
				return true
			}
		}
		return false
	}
	if s == "" {
		// Only a trailing run of `*`s can match emptiness.
		return strings.Trim(p, "*") == ""
	}
	switch p[0] {
	case '*':
		// Any run of non-separator bytes (including empty).
		for i := 0; ; i++ {
			if matchGlob(p[1:], s[i:]) {
				return true
			}
			if i >= len(s) || s[i] == '/' {
				return false
			}
		}
	case '?':
		if s[0] == '/' {
			return false
		}
		return matchGlob(p[1:], s[1:])
	case '[':
		end := strings.IndexByte(p, ']')
		if end < 0 {
			break // unterminated class: literal '['
		}
		if s[0] == '/' {
			return false
		}
		if !classMatch(p[1:end], s[0]) {
			return false
		}
		return matchGlob(p[end+1:], s[1:])
	}
	if p[0] != s[0] {
		return false
	}
	return matchGlob(p[1:], s[1:])
}

// StarMatch reports whether s matches pattern, where `*` stands for any run of
// characters (including none) and every other character matches itself. It is
// deliberately not Match: there is no `**`, `?`, `[...]` or `{a,b}` here, and
// no path-segment awareness — `/` and `\` are ordinary characters, matched
// like any other, because the strings compared (a field name, a secret-scan
// key) may hold either and neither has any business acting as a separator in
// this position. Shared by the number-hint unit overrides (numhint) and the
// secret-pattern key matcher (secret).
func StarMatch(pattern, s string) bool {
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

// classMatch evaluates one [...] body (without brackets) against a byte.
func classMatch(class string, c byte) bool {
	if class == "" {
		return false
	}
	negate := false
	if class[0] == '!' || class[0] == '^' {
		negate = true
		class = class[1:]
	}
	hit := false
	for i := 0; i < len(class); i++ {
		if i+2 < len(class) && class[i+1] == '-' {
			if class[i] <= c && c <= class[i+2] {
				hit = true
			}
			i += 2
			continue
		}
		if class[i] == c {
			hit = true
		}
	}
	return hit != negate
}
