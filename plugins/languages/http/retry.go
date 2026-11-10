package langhttp

// retry.go highlights the `# @retry 3 2s on 5xx,timeout` directive (#3116).
// The line is a comment to the grammar; like `@timeout` the marker reads as a
// keyword and the count and delay as numbers, and — like `@assert`'s
// structure — the `on` keyword as a keyword and the condition words as
// constants, so the policy stands out from the prose of a comment above it.

import (
	"ike/internal/httpfile"
	"ike/internal/lang"
)

// retrySpans produces the directive spans of the whole buffer. The parser
// fills a Raw part only once it read it successfully, so a directive it
// rejected keeps its good parts painted and the part it stumbled over in the
// comment colour — the parser already reported it on the line.
func retrySpans(lines []string) []lang.Span {
	var out []lang.Span
	for i, line := range lines {
		r, ok := httpfile.RetryDirective(line)
		if !ok {
			continue
		}
		runes := []rune(line)
		marker := indexRunes(runes, 0, httpfile.RetryKeyword)
		if marker < 0 {
			continue
		}
		at := marker + len(httpfile.RetryKeyword)
		out = append(out, span(i, marker, at, "keyword"))
		for _, part := range []struct{ text, capture string }{
			{r.CountRaw, "number"}, {r.DelayRaw, "number"}, {r.OnRaw, "keyword"},
		} {
			if part.text != "" {
				at = appendPart(&out, runes, i, at, part.text, part.capture)
			}
		}
		for _, w := range r.CondRaw {
			at = appendPart(&out, runes, i, at, w, "constant")
		}
	}
	return out
}
