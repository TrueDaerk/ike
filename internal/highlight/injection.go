package highlight

import "ike/internal/lang"

// injection.go layers embedded-language highlighting (issue #299) over the host
// span set: the host grammar's injection query marks fragments (see fragment.go),
// each fragment is parsed with its own language's grammar, and the resulting
// spans are shifted into host coordinates. Injected spans are placed before the
// host spans so Index.CaptureAt prefers them over the host's enclosing capture
// (typically "string") — host colouring still shows through between injected
// tokens.

// overlayFragments returns spans for lines parsed with the host language l's
// grammar, prefixed with the spans of every embedded fragment parsed with its
// own grammar. Fragments come from the host's injection query or, for hosts
// registering one, its Go-level region detector (#1303). Hosts without either,
// fragments without a registered grammar, and CGo-disabled builds all degrade
// to the plain host spans.
// It also returns the fragments' foldable ranges in host coordinates (#1329):
// a JSON body embedded in a .http request folds by JSON's own fold nodes, so
// collapsing a large request body works exactly as it does in a .json buffer.
func overlayFragments(l lang.Language, lines []string, host []Span) ([]Span, []Fold) {
	frags := fragmentsFor(l, lines)
	if len(frags) == 0 {
		return host, nil
	}
	var injected []Span
	var folds []Fold
	for _, f := range frags {
		// The built-in regex mini-grammar (#1631) is an injection target
		// without a registered language: fragment.regex captures route here
		// instead of through a Tree-sitter grammar.
		if f.Lang == "regex" {
			injected = append(injected, offsetSpans(RegexSpans(f.Lines), f)...)
			continue
		}
		el, ok := lang.ByID(f.Lang)
		if !ok || el.Grammar == nil {
			continue
		}
		spans, _, ff := parseScoped(el.Grammar, nil, foldKinds(el), f.Lines)
		injected = append(injected, offsetSpans(spans, f)...)
		folds = append(folds, offsetFolds(ff, f)...)
	}
	if len(injected) == 0 {
		return host, folds
	}
	return append(injected, host...), folds
}

// foldKinds are the node kinds that fold in language l: its FoldNodes, falling
// back to its sticky-scroll scopes — the same resolution HighlightScoped uses
// for the host language.
func foldKinds(l lang.Language) []string {
	if len(l.FoldNodes) > 0 {
		return l.FoldNodes
	}
	return l.ScopeNodes
}

// offsetFolds shifts fragment-local folds into host coordinates. Only lines
// shift: a fold is line-granular, and a fragment's lines are exactly the host
// lines in its range.
func offsetFolds(folds []Fold, f Fragment) []Fold {
	out := folds[:0]
	for _, fold := range folds {
		fold.HeaderLine += f.StartLine
		fold.EndLine += f.StartLine
		out = append(out, fold)
	}
	return out
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
