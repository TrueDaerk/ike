package highlight

import "ike/internal/lang"

// injection.go layers embedded-language highlighting (issue #299) over the host
// span set: the host grammar's injection query marks fragments (see fragment.go),
// each fragment is parsed with its own language's grammar, and the resulting
// spans are shifted into host coordinates. Injected spans are placed before the
// host spans so Index.CaptureAt prefers them over the host's enclosing capture
// (typically "string") — host colouring still shows through between injected
// tokens.

// overlayFragments returns spans for lines parsed with the host grammar g,
// prefixed with the spans of every embedded fragment parsed with its own
// grammar. Hosts without an injection query, fragments without a registered
// grammar, and CGo-disabled builds all degrade to the plain host spans.
func overlayFragments(g lang.Grammar, lines []string, host []Span) []Span {
	frags := detectFragments(g, lines)
	if len(frags) == 0 {
		return host
	}
	var injected []Span
	for _, f := range frags {
		l, ok := lang.ByID(f.Lang)
		if !ok || l.Grammar == nil {
			continue
		}
		injected = append(injected, offsetSpans(parse(l.Grammar, f.Lines), f)...)
	}
	if len(injected) == 0 {
		return host
	}
	return append(injected, host...)
}

// offsetSpans shifts fragment-local spans into host coordinates: lines shift by
// the fragment's start line, and columns on the fragment's first line shift by
// its start column (later fragment lines start at host column 0, since
// Fragment.Lines is exactly the host text in the fragment's range).
func offsetSpans(spans []Span, f Fragment) []Span {
	out := spans[:0]
	for _, s := range spans {
		if s.Line == 0 {
			s.StartCol += f.StartCol
			s.EndCol += f.StartCol
		}
		s.Line += f.StartLine
		out = append(out, s)
	}
	return out
}
