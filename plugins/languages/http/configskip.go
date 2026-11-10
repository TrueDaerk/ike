package langhttp

// configskip.go highlights the `# @no-curlrc` and `# @no-netrc` directives
// (#3114) and `# @no-log` (#3115). Like `# @timeout` (timeout.go) the line is
// a comment to the grammar; the marker reads as a keyword so an opt-out of
// the local client configuration or of the on-disk history stands out from
// the prose of an ordinary comment. No such directive takes a value, so the
// marker is the only span.

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
			// `# @no-log` (#3115) is the other value-less opt-out: of the
			// on-disk response history. Same single keyword span.
			if _, ok = httpfile.NoLogDirectiveAt(line); !ok {
				continue
			}
			name = httpfile.NoLogDirective
		}
		marker := indexRunes([]rune(line), 0, "@"+name)
		if marker < 0 {
			continue
		}
		out = append(out, span(i, marker, marker+1+len(name), "keyword"))
	}
	return out
}
