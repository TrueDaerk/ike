package highlight

import (
	"strings"

	"ike/internal/lang"
)

// segments.go is the completion indexes' view of a text (#2652): which
// language each part of it belongs to, and which of that part is code. The
// word and symbol indexes used to tokenize raw text (strings, comments and
// Markdown prose included) and to merge every language into one pool; now
// they index code tokens of one language at a time, and an embedded fragment
// — a ```php fence in README.md, the <script> body of an HTML page, HTML
// inside a Python string — belongs to the fragment's language, not the host's.
//
// The layer reuses what highlighting already has: the grammar's string /
// comment / char captures mask non-code exactly as they do for the bracket
// scanner (brackets.go), and the injection layer (injection.go, #299/#1697)
// resolves fragments to their own language and grammar.

// Segment is one language's share of a text: the host language's own code, or
// one embedded fragment (recursively, to maxInjectionDepth). Coordinates are
// host rune coordinates; the segment covers [StartLine:StartCol,
// EndLine:EndCol) of the host lines.
type Segment struct {
	Lang      string
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
	// Depth is 0 for the host segment, 1 for its direct fragments, and so on.
	Depth int
	// Spans are the spans this segment's grammar produced, in host
	// coordinates — only this grammar's, never a nested fragment's.
	Spans []Span
	// Masked are the ranges inside the segment that are not its code: its
	// string / comment / char captures plus the regions of the fragments
	// nested directly inside it (a fence's content is the fence language's
	// code, not the host's).
	Masked []Span
}

// deepFragment is a resolved fragment in host coordinates with its nesting
// bookkeeping: parent indexes the enclosing fragment in the same slice, -1
// for the host.
type deepFragment struct {
	Fragment
	depth  int
	parent int
}

// CodeLanguage reports whether id names a language whose grammar tokens
// count as code for the completion indexes (#2652): a registered file
// language (one that claims extensions or file names — markdown_inline and
// the regex mini-grammar are internal injection targets, not languages a
// buffer is in) with a compiled-in grammar, and not prose. A prose language
// (Markdown, plain text, logs) has no code tokens of its own: only the
// fragments it embeds are indexed.
func CodeLanguage(id string) bool {
	l, ok := lang.ByID(id)
	return ok && l.Grammar != nil && fileLanguage(l) && !proseLangs[l.ID]
}

// fileLanguage reports whether l is a language a buffer or file can be in,
// as opposed to an internal grammar only injections resolve.
func fileLanguage(l lang.Language) bool {
	return len(l.Extensions) > 0 || len(l.Filenames) > 0
}

// Segments splits lines, a text of language langID, into its per-language
// code segments: the host's own code (when the host is a CodeLanguage), then
// every embedded fragment whose language is one, parents before children.
// only, when non-nil, restricts the full parse to the languages it accepts —
// fragment *detection* still walks every host and fragment, so a
// per-language project scan pays one injection parse for a file it does not
// own, and nothing more. Without cgo, or for a language without a grammar,
// the result is nil.
func Segments(langID string, lines []string, only func(langID string) bool) []Segment {
	l, ok := lang.ByID(langID)
	if !ok || l.Grammar == nil {
		return nil
	}
	frags := embeddedDeep(l, lines, 1, -1, nil)
	var out []Segment
	if CodeLanguage(l.ID) && (only == nil || only(l.ID)) {
		last := len(lines) - 1
		endCol := 0
		if last >= 0 {
			endCol = len([]rune(lines[last]))
		}
		spans := parse(l.Grammar, lines)
		out = append(out, Segment{
			Lang: l.ID, StartLine: 0, StartCol: 0, EndLine: last, EndCol: endCol,
			Spans:  spans,
			Masked: append(maskedSpans(spans), childRegions(frags, -1, lines)...),
		})
	}
	for i, f := range frags {
		if !CodeLanguage(f.Lang) || (only != nil && !only(f.Lang)) {
			continue
		}
		el, _ := lang.ByID(f.Lang)
		src, wrapped := wrapFragment(f.Fragment)
		spans := parse(el.Grammar, src)
		if wrapped {
			spans = unwrapSpans(spans, len(f.Lines))
		}
		spans = offsetSpans(spans, f.Fragment)
		out = append(out, Segment{
			Lang: f.Lang, StartLine: f.StartLine, StartCol: f.StartCol, EndLine: f.EndLine, EndCol: f.EndCol,
			Depth:  f.depth,
			Spans:  spans,
			Masked: append(maskedSpans(spans), childRegions(frags, i, lines)...),
		})
	}
	return out
}

// EmbeddedLangs lists the languages of every fragment embedded in lines, a
// text of language langID, recursively — registered or not, deduplicated.
// A per-language project scan records it per file, so the next language's
// scan skips the files that cannot contain it without parsing them again.
func EmbeddedLangs(langID string, lines []string) []string {
	l, ok := lang.ByID(langID)
	if !ok || (l.Grammar == nil && l.Regions == nil) {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, f := range embeddedDeep(l, lines, 1, -1, nil) {
		if !seen[f.Lang] {
			seen[f.Lang] = true
			out = append(out, f.Lang)
		}
	}
	return out
}

// Embedded resolves every embedded fragment of lines, a text of language
// langID, recursively to maxInjectionDepth and in host coordinates — parents
// before their children. Fragments whose language is not registered (the
// regex mini-grammar) are reported as well; EmbeddedAt filters them.
func Embedded(langID string, lines []string) []Fragment {
	l, ok := lang.ByID(langID)
	if !ok {
		return nil
	}
	deep := embeddedDeep(l, lines, 1, -1, nil)
	out := make([]Fragment, len(deep))
	for i, f := range deep {
		out[i] = f.Fragment
	}
	return out
}

// EmbeddedAt returns the innermost embedded fragment of a registered file
// language covering (line, col) in lines, a text of language langID (#2652):
// the language completion resolves at the cursor. A cursor inside a Markdown
// paragraph is not "in markdown_inline" — that grammar is the host's own
// inline pass — and a regex literal is not a language a buffer can be in, so
// both are skipped; the position then belongs to the enclosing language. The
// end of a fragment counts as inside it, since that is where typing appends.
func EmbeddedAt(langID string, lines []string, line, col int) (Fragment, bool) {
	return InnermostAt(Embedded(langID, lines), line, col)
}

// InnermostAt is EmbeddedAt over an already-resolved fragment list (the
// completion engine caches Embedded per buffer text and asks per cursor):
// the last covering fragment of a registered file language, since parents
// precede their children.
func InnermostAt(frags []Fragment, line, col int) (Fragment, bool) {
	var found Fragment
	ok := false
	for _, f := range frags {
		if !fragmentCovers(f, line, col) {
			continue
		}
		if l, reg := lang.ByID(f.Lang); !reg || !fileLanguage(l) {
			continue
		}
		found, ok = f, true
	}
	return found, ok
}

// fragmentCovers reports whether (line, col) lies in f, end inclusive.
func fragmentCovers(f Fragment, line, col int) bool {
	if line < f.StartLine || line > f.EndLine {
		return false
	}
	if line == f.StartLine && col < f.StartCol {
		return false
	}
	if line == f.EndLine && col > f.EndCol {
		return false
	}
	return true
}

// embeddedDeep resolves l's fragments in lines at nesting level depth,
// recursing into each fragment's own language (a Python string that is
// HTML, whose <script> is JavaScript) until maxInjectionDepth, and appends
// them to acc in host coordinates. parent is acc's index of the fragment
// lines belong to, -1 for the host buffer.
func embeddedDeep(l lang.Language, lines []string, depth, parent int, acc []deepFragment) []deepFragment {
	if depth > maxInjectionDepth {
		return acc
	}
	for _, f := range fragmentsFor(l, lines) {
		acc = append(acc, deepFragment{Fragment: f, depth: depth, parent: parent})
		self := len(acc) - 1
		el, ok := lang.ByID(f.Lang)
		if !ok || (el.Grammar == nil && el.Regions == nil) {
			continue
		}
		src, wrapped := wrapFragment(f)
		nested := embeddedDeep(el, src, depth+1, -1, nil)
		for _, n := range nested {
			if wrapped {
				// Wrapper lines are synthetic: a fragment touching them is
				// not host text; the rest shift up by the prefix line.
				if n.StartLine < 1 || n.EndLine > len(f.Lines) {
					continue
				}
				n.StartLine--
				n.EndLine--
			}
			// Fragment-local → host: the first fragment line shifts by its
			// start column, every line by its start line (offsetSpans'
			// rule, applied to both ends).
			if n.StartLine == 0 {
				n.StartCol += f.StartCol
			}
			if n.EndLine == 0 {
				n.EndCol += f.StartCol
			}
			n.StartLine += f.StartLine
			n.EndLine += f.StartLine
			if n.parent == -1 {
				n.parent = self
			} else {
				n.parent += self + 1
			}
			acc = append(acc, n)
		}
	}
	return acc
}

// maskedSpans keeps the string / comment / char spans — the non-code the
// bracket scanner skips too.
func maskedSpans(spans []Span) []Span {
	var out []Span
	for _, s := range spans {
		if masked(s.Capture) {
			out = append(out, s)
		}
	}
	return out
}

// childRegions returns the per-line ranges of the fragments nested directly
// in parent (-1: the host), so the enclosing segment's tokenizer skips them.
func childRegions(frags []deepFragment, parent int, lines []string) []Span {
	var out []Span
	for _, f := range frags {
		if f.parent != parent {
			continue
		}
		for line := f.StartLine; line <= f.EndLine && line < len(lines); line++ {
			from, to := 0, len([]rune(lines[line]))
			if line == f.StartLine {
				from = f.StartCol
			}
			if line == f.EndLine && f.EndCol < to {
				to = f.EndCol
			}
			if from < to {
				out = append(out, Span{Line: line, StartCol: from, EndCol: to, Capture: "fragment"})
			}
		}
	}
	return out
}

// CodeText returns the segment's code as text in the segment's own line
// layout: everything outside the segment is dropped, every masked rune
// (string, comment, char, nested fragment) becomes a space, so a plain
// identifier tokenizer over the result sees exactly the code tokens.
func (s Segment) CodeText(lines []string) string {
	if s.EndLine < s.StartLine || s.StartLine < 0 {
		return ""
	}
	byLine := map[int][]Span{}
	for _, m := range s.Masked {
		byLine[m.Line] = append(byLine[m.Line], m)
	}
	hidden := func(line, col int) bool {
		for _, m := range byLine[line] {
			if col >= m.StartCol && col < m.EndCol {
				return true
			}
		}
		return false
	}
	var b strings.Builder
	for line := s.StartLine; line <= s.EndLine && line < len(lines); line++ {
		runes := []rune(lines[line])
		from, to := 0, len(runes)
		if line == s.StartLine {
			from = s.StartCol
		}
		if line == s.EndLine && s.EndCol < to {
			to = s.EndCol
		}
		if line > s.StartLine {
			b.WriteByte('\n')
		}
		for col := from; col < to; col++ {
			if hidden(line, col) {
				b.WriteByte(' ')
			} else {
				b.WriteRune(runes[col])
			}
		}
	}
	return b.String()
}
