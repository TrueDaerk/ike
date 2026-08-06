// Package bracket is the buffer-level bracket-pair tracker (#1628): it scans a
// buffer's lines, pairs `()`, `[]` and `{}` with a stack, and reports every
// bracket with the nesting depth it sits at — the two halves of a pair share
// one depth, so a depth-keyed palette colours them the same. Brackets that
// never find their partner come back marked Unmatched, which the renderer
// styles as an error.
//
// It is a leaf package: pure Go, no CGo, no Tree-sitter, no registry import —
// the pairing rules are the same for every language, and everything language
// specific reaches it through Syntax. Pairing therefore needs no grammar: it
// is testable on its own, and a language whose grammar has no bracket nodes
// (or no grammar at all) still gets depth colours and unmatched marks.
//
// Brackets inside strings and comments must not pair (`"a ) b"` closes
// nothing). Two ways to skip them, in order of accuracy:
//
//   - Skip: an exact mask. The highlighter passes one built from the parsed
//     string/comment captures, so the grammar decides what a string is —
//     Python triple quotes, Go raw strings, nested comments and all.
//   - The heuristics in Syntax (LineComment/BlockComment/Quotes), used when no
//     parse is available. Quotes are line-local: an opening quote with no
//     partner on the same line is treated as an ordinary rune, so a lone
//     apostrophe or a Rust lifetime does not swallow the rest of the line.
package bracket

// Mark is one bracket occurrence in a buffer, at rune column Col of Line.
// Depth is the nesting level: the outermost pair is 0, and a closer carries
// its opener's depth so both ends share a palette slot. Unmatched marks a
// closer with no opener still waiting, or an opener never closed.
type Mark struct {
	Line      int
	Col       int
	Depth     int
	Open      bool
	Unmatched bool
}

// Syntax carries the language-specific parts of a scan. The zero value scans
// raw text: every bracket pairs, nothing is skipped.
type Syntax struct {
	// LineComment starts a comment running to the end of the line ("//", "#").
	LineComment string
	// BlockComment is the open/close pair ("/*", "*/"); both empty means the
	// language has none. Block comments may span lines.
	BlockComment [2]string
	// Quotes are the string delimiters scanned line-locally, e.g. '"', '\''
	// and '`'. Empty means DefaultQuotes.
	Quotes []rune
	// Skip reports whether the rune at (line, col) is inside a string or a
	// comment. Non-nil switches the heuristics off entirely: an exact mask is
	// strictly better than guessing, and running both would double-skip (an
	// apostrophe inside a masked comment must not open a string).
	Skip func(line, col int) bool
}

// DefaultQuotes are the delimiters used when Syntax.Quotes is empty: the three
// that open a string in nearly every language IKE highlights.
var DefaultQuotes = []rune{'"', '\'', '`'}

// Unmatched is the theme capture an unmatched bracket renders with. It has no
// palette entry of its own: consumers fall back to the theme's error colour,
// so `theme.captures.bracket.unmatched` is an override, not a requirement.
const Unmatched = "bracket.unmatched"

// closerOf maps a closing bracket to the opener it matches.
var closerOf = map[rune]rune{')': '(', ']': '[', '}': '{'}

// open is one bracket still waiting for its closer: where its Mark sits in the
// output and which opener it is.
type open struct {
	idx  int
	kind rune
}

// Scan returns every bracket in lines, in reading order, with its nesting
// depth and match state. Depths are assigned across the whole buffer, so a
// brace opened on line 1 still deepens line 400.
func Scan(lines []string, syn Syntax) []Mark {
	if syn.Quotes == nil {
		syn.Quotes = DefaultQuotes
	}
	var (
		out     []Mark
		stack   []open
		inBlock bool // inside a multi-line block comment (heuristic mode only)
	)
	for ln, line := range lines {
		runes := []rune(line)
		for col := 0; col < len(runes); col++ {
			if syn.Skip != nil {
				if syn.Skip(ln, col) {
					continue
				}
			} else {
				var skip int
				skip, inBlock = syn.skipAt(runes, col, inBlock)
				if skip > 0 {
					col += skip - 1
					continue
				}
			}
			switch r := runes[col]; r {
			case '(', '[', '{':
				stack = append(stack, open{idx: len(out), kind: r})
				out = append(out, Mark{Line: ln, Col: col, Depth: len(stack) - 1, Open: true})
			case ')', ']', '}':
				i := innermost(stack, closerOf[r])
				if i < 0 {
					// Nothing of this kind is open: a stray closer. The
					// openers still on the stack keep waiting — in `[ ) ]`
					// only the `)` is wrong.
					out = append(out, Mark{Line: ln, Col: col, Unmatched: true})
					continue
				}
				// Anything opened inside the pair we just closed can never be
				// closed any more: in `{ ( }` the `}` matches its `{` and the
				// `(` is the mistake.
				for _, o := range stack[i+1:] {
					out[o.idx].Unmatched = true
				}
				stack = stack[:i]
				out = append(out, Mark{Line: ln, Col: col, Depth: len(stack)})
			}
		}
	}
	for _, o := range stack {
		out[o.idx].Unmatched = true
	}
	return out
}

// innermost returns the stack index of the closest open bracket of kind, or -1
// when none is open.
func innermost(stack []open, kind rune) int {
	if kind == 0 {
		return -1
	}
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].kind == kind {
			return i
		}
	}
	return -1
}

// skipAt reports how many runes to skip at col — a comment to the end of the
// line, a block comment (possibly closing on a later line) or a quoted string
// — plus the block-comment state carried to the next line. 0 means the rune at
// col is ordinary. Only used when Syntax.Skip is nil.
func (syn Syntax) skipAt(runes []rune, col int, inBlock bool) (int, bool) {
	if inBlock {
		if end := indexAt(runes, col, syn.BlockComment[1]); end >= 0 {
			return end + len([]rune(syn.BlockComment[1])) - col, false
		}
		return len(runes) - col, true
	}
	if hasAt(runes, col, syn.LineComment) {
		return len(runes) - col, false
	}
	if hasAt(runes, col, syn.BlockComment[0]) {
		after := col + len([]rune(syn.BlockComment[0]))
		if end := indexAt(runes, after, syn.BlockComment[1]); end >= 0 {
			return end + len([]rune(syn.BlockComment[1])) - col, false
		}
		return len(runes) - col, true
	}
	for _, q := range syn.Quotes {
		if runes[col] != q {
			continue
		}
		if end := closingQuote(runes, col, q); end >= 0 {
			return end + 1 - col, false
		}
		// Unterminated on this line: not a string opener (an apostrophe in
		// prose, a Rust lifetime, a shell `don't`).
		return 0, false
	}
	return 0, false
}

// closingQuote returns the column of the unescaped q closing the run opened at
// col, or -1 when the line has none. Backslash escapes count everywhere except
// in backtick runs, which are raw in every language that uses them.
func closingQuote(runes []rune, col int, q rune) int {
	for i := col + 1; i < len(runes); i++ {
		if runes[i] == '\\' && q != '`' {
			i++
			continue
		}
		if runes[i] == q {
			return i
		}
	}
	return -1
}

// hasAt reports whether the non-empty s starts at col of runes.
func hasAt(runes []rune, col int, s string) bool {
	if s == "" {
		return false
	}
	sr := []rune(s)
	if col+len(sr) > len(runes) {
		return false
	}
	for i, r := range sr {
		if runes[col+i] != r {
			return false
		}
	}
	return true
}

// indexAt returns the column of the first s at or after col, or -1.
func indexAt(runes []rune, col int, s string) int {
	if s == "" {
		return -1
	}
	for i := max(col, 0); i < len(runes); i++ {
		if hasAt(runes, i, s) {
			return i
		}
	}
	return -1
}
