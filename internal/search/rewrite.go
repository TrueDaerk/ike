package search

// rewrite.go computes replacement text for matches (Roadmap 0150, #86). It is
// pure string work shared by the replace-in-path preview and both apply paths
// (editor buffer and disk): one match range within a line is rewritten, with
// $1-style capture-group references expanded for regex queries.

// RewriteRange returns line with the [start,end) rune range replaced. For a
// regex query the replacement is a template: $1/$name expand against the
// matched text (Go regexp Expand syntax, $$ for a literal $); literal queries
// insert the replacement verbatim. ok is false when a regex query fails to
// re-match the range (a stale match — the caller skips it).
func RewriteRange(line string, start, end int, q Query, replacement string) (string, bool) {
	runes := []rune(line)
	start, end = clampRewrite(start, end, len(runes))
	pre, mid, post := string(runes[:start]), string(runes[start:end]), string(runes[end:])
	if !q.Regex {
		return pre + replacement + post, true
	}
	re, err := compileQuery(q)
	if err != nil {
		return line, false
	}
	loc := re.FindStringSubmatchIndex(mid)
	if loc == nil || loc[0] != 0 || loc[1] != len(mid) {
		return line, false // the range no longer matches the pattern
	}
	expanded := re.Expand(nil, []byte(replacement), []byte(mid), loc)
	return pre + string(expanded) + post, true
}

// clampRewrite sanitizes a rune range against the line length.
func clampRewrite(start, end, n int) (int, int) {
	if start < 0 {
		start = 0
	}
	if start > n {
		start = n
	}
	if end < start {
		end = start
	}
	if end > n {
		end = n
	}
	return start, end
}
