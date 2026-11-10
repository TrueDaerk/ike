package langhttp

// bodylimit.go highlights the `# @stream` and `# @max-body 5mb` directives
// (#3117). Like `@timeout` (timeout.go) the line is a comment to the grammar;
// the marker reads as a keyword and the size as a number, so a request whose
// body is read differently stands out from the prose of an ordinary comment.

import (
	"ike/internal/httpfile"
	"ike/internal/lang"
)

// bodyLimitSpans produces the directive spans of the whole buffer. A
// directive the parser rejected keeps its marker painted and nothing else —
// a stray value after `@stream` or a size that does not parse is not a number
// to point at, and the parser already reported it on the line.
func bodyLimitSpans(lines []string) []lang.Span {
	var out []lang.Span
	for i, line := range lines {
		runes := []rune(line)
		if _, ok := httpfile.StreamDirective(line); ok {
			if at := indexRunes(runes, 0, httpfile.StreamKeyword); at >= 0 {
				out = append(out, span(i, at, at+len(httpfile.StreamKeyword), "keyword"))
			}
			continue
		}
		_, raw, err, ok := httpfile.MaxBodyDirective(line)
		if !ok {
			continue
		}
		marker := indexRunes(runes, 0, httpfile.MaxBodyKeyword)
		if marker < 0 {
			continue
		}
		at := marker + len(httpfile.MaxBodyKeyword)
		out = append(out, span(i, marker, at, "keyword"))
		if err != nil || raw == "" {
			continue
		}
		if start := indexRunes(runes, at, raw); start >= 0 {
			out = append(out, span(i, start, start+len([]rune(raw)), "number"))
		}
	}
	return out
}
