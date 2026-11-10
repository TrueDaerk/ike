package langhttp

// configskip.go highlights the `# @no-curlrc` and `# @no-netrc` directives
// (#3114). Like `# @timeout` (timeout.go) the line is a comment to the
// grammar; the marker reads as a keyword so an opt-out of the local client
// configuration stands out from the prose of an ordinary comment. Neither
// directive takes a value, so the marker is the only span.

import (
	"ike/internal/httpfile"
	"ike/internal/lang"
)

// configSkipSpans produces the directive spans of the whole buffer. A
// directive carrying a stray value still gets its marker painted — the parser
// reports the value on that line.
func configSkipSpans(lines []string) []lang.Span {
	var out []lang.Span
	for i, line := range lines {
		name, _, ok := httpfile.ConfigSkipDirective(line)
		if !ok {
			continue
		}
		marker := indexRunes([]rune(line), 0, "@"+name)
		if marker < 0 {
			continue
		}
		out = append(out, span(i, marker, marker+1+len(name), "keyword"))
	}
	return out
}
