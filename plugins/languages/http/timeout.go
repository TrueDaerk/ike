package langhttp

// timeout.go highlights the `# @timeout 5s` directive (#2630). The line is a
// comment to the grammar, so all of it would read as a comment — wrong for a
// directive that decides when the request is given up on. The marker reads as
// a keyword like every other directive marker, and the duration as a number,
// so a deadline stands out from the prose of an ordinary comment above it.

import (
	"ike/internal/httpfile"
	"ike/internal/lang"
)

// timeoutSpans produces the directive spans of the whole buffer. A directive
// whose value does not parse gets its marker painted and nothing else — the
// value is not a duration to point at, and the parser already reported it.
func timeoutSpans(lines []string) []lang.Span {
	var out []lang.Span
	for i, line := range lines {
		_, raw, err, ok := httpfile.TimeoutDirective(line)
		if !ok {
			continue
		}
		runes := []rune(line)
		marker := indexRunes(runes, 0, "@timeout")
		if marker < 0 {
			continue
		}
		at := marker + len("@timeout")
		out = append(out, span(i, marker, at, "keyword"))
		if err != nil || raw == "" {
			continue
		}
		start := indexRunes(runes, at, raw)
		if start < 0 {
			continue
		}
		out = append(out, span(i, start, start+len([]rune(raw)), "number"))
	}
	return out
}
