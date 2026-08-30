package highlight

import "ike/internal/lang"

// injection.go layers embedded-language highlighting (issue #299) over the host
// span set: the host grammar's injection query marks fragments (see fragment.go),
// each fragment is parsed with its own language's grammar, and the resulting
// spans are shifted into host coordinates. Injected spans are placed before the
// host spans so Index.CaptureAt prefers them over the host's enclosing capture
// (typically "string") — host colouring still shows through between injected
// tokens.

// maxInjectionDepth bounds recursive injection resolution (#1697): a buffer
// supports up to this many nested injection levels below the host (Python →
// HTML is level 1, the HTML's <script> JavaScript is level 2, an HTML template
// literal inside that script is level 3). Deeper fragments keep their
// enclosing language's styling, so pathological nesting cannot blow up a
// parse.
const maxInjectionDepth = 3

// overlayFragments returns spans for lines parsed with the host language l's
// grammar, prefixed with the spans of every embedded fragment parsed with its
// own grammar. Fragments come from the host's injection query or, for hosts
// registering one, its Go-level region detector (#1303). Hosts without either,
// fragments without a registered grammar, and CGo-disabled builds all degrade
// to the plain host spans.
// Injections resolve recursively (#1697): each fragment's own language runs
// its injection query in turn, so HTML injected into a Python string still
// highlights its <script> and <style> bodies, down to maxInjectionDepth.
// It also returns the fragments' foldable ranges in host coordinates (#1329):
// a JSON body embedded in a .http request folds by JSON's own fold nodes, so
// collapsing a large request body works exactly as it does in a .json buffer.
func overlayFragments(l lang.Language, lines []string, host []Span) ([]Span, []Fold) {
	return overlayFragmentsAt(l, lines, host, 1)
}

// overlayFragmentsAt is overlayFragments at nesting level depth: the host
// buffer resolves its fragments at depth 1, a fragment's own fragments at
// depth 2, and so on until maxInjectionDepth is exhausted.
func overlayFragmentsAt(l lang.Language, lines []string, host []Span, depth int) ([]Span, []Fold) {
	if depth > maxInjectionDepth {
		return host, nil
	}
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
		// A partial fragment (#2329) parses inside the language's synthetic
		// wrapper, on lines of its own, so the snippet reaches the grammar as
		// the construct it expects; the wrapper lines are stripped back out
		// before the spans return to host coordinates.
		src, wrapped := wrapFragment(f)
		spans, _, ff := parseScoped(el.Grammar, nil, foldKinds(el), src)
		spans, nested := overlayFragmentsAt(el, src, spans, depth+1)
		if wrapped {
			spans = unwrapSpans(spans, len(f.Lines))
			ff = unwrapFolds(ff, len(f.Lines))
			nested = unwrapFolds(nested, len(f.Lines))
		}
		injected = append(injected, offsetSpans(spans, f)...)
		folds = append(folds, offsetFolds(ff, f)...)
		folds = append(folds, offsetFolds(nested, f)...)
	}
	if len(injected) == 0 {
		return host, folds
	}
	return append(injected, host...), folds
}

// wrapFragment returns the lines to parse for fragment f: its own lines, or —
// for a partial fragment whose language registers a wrapper (#2329) — the
// wrapper's prefix line, the fragment's lines, and the wrapper's suffix line.
// wrapped reports whether the wrapper was applied, i.e. whether the resulting
// spans need unwrapSpans before they return to fragment coordinates.
func wrapFragment(f Fragment) (lines []string, wrapped bool) {
	if !f.Partial {
		return f.Lines, false
	}
	prefix, suffix, ok := fragmentWrapper(f.Lang)
	if !ok {
		return f.Lines, false
	}
	out := make([]string, 0, len(f.Lines)+2)
	out = append(out, prefix)
	out = append(out, f.Lines...)
	return append(out, suffix), true
}

// unwrapSpans maps spans of a wrapped parse back to fragment coordinates:
// spans on the wrapper's own lines are dropped, the rest shift up by the one
// prefix line. Columns need no correction — the wrapper occupies whole lines.
func unwrapSpans(spans []Span, n int) []Span {
	out := spans[:0]
	for _, s := range spans {
		if s.Line < 1 || s.Line > n {
			continue
		}
		s.Line--
		out = append(out, s)
	}
	return out
}

// unwrapFolds is unwrapSpans for folds: a fold anchored on a wrapper line (the
// synthetic rule the wrapper introduces) is dropped, the rest shift up.
func unwrapFolds(folds []Fold, n int) []Fold {
	out := folds[:0]
	for _, f := range folds {
		if f.HeaderLine < 1 || f.HeaderLine > n {
			continue
		}
		f.HeaderLine--
		if f.EndLine > n {
			f.EndLine = n
		}
		f.EndLine--
		out = append(out, f)
	}
	return out
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
