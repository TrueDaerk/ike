package jqplay

// highlight.go is the cheap jq colorizer for the playground's query line: a
// single-pass rune scanner producing typed runs, not a parser. The query line
// is one line of text that changes on every keystroke and must still render
// while the program does not compile, so a tokenizer that never fails is the
// right shape — tree-sitter has no jq grammar in this build, and reusing the
// gojq AST would leave a half-typed program uncolored, which is exactly when
// the color helps most.

// Kind classifies one run of a jq program.
type Kind int

const (
	// KindPlain is unclassified text — whitespace and anything the scanner
	// does not recognise. The renderer paints it in the default foreground.
	KindPlain Kind = iota
	// KindPath is a field access: `.foo`, `.["a b"]`'s leading dot, `..`.
	KindPath
	// KindString is a double-quoted string literal (escapes included).
	KindString
	// KindNumber is a numeric literal.
	KindNumber
	// KindKeyword is a jq keyword: def, if/then/elif/else/end, reduce,
	// foreach, try/catch, as, label, import, include, and, or.
	KindKeyword
	// KindFunc is a bare identifier — a builtin or user-defined function.
	KindFunc
	// KindVariable is a `$name` reference.
	KindVariable
	// KindFormat is an `@base64`-style format string.
	KindFormat
	// KindOperator is punctuation and operators: `|`, `,`, arithmetic,
	// comparisons, brackets.
	KindOperator
	// KindComment is a `#` comment to the end of the line.
	KindComment
)

// Token is one classified run of the program, in *rune* indices — the query
// line renders rune by rune (cursor, windowing), so byte offsets would only
// have to be converted back.
type Token struct {
	Start int
	End   int
	Kind  Kind
}

// keywords are the jq words that are syntax rather than functions. `not` and
// `empty` are ordinary builtins and stay KindFunc, as in jq's own manual.
var keywords = map[string]bool{
	"def": true, "as": true, "if": true, "then": true, "elif": true,
	"else": true, "end": true, "reduce": true, "foreach": true, "try": true,
	"catch": true, "label": true, "import": true, "include": true,
	"and": true, "or": true, "__loc__": true,
}

// Tokens scans program and returns its runs in order. It never fails and
// never panics: an unterminated string runs to the end of the line, an
// unknown rune becomes KindOperator. Runs of whitespace produce no token, so
// the renderer's default (KindPlain) covers the gaps.
func Tokens(program string) []Token {
	r := []rune(program)
	var out []Token
	for i := 0; i < len(r); {
		switch c := r[i]; {
		case c == ' ' || c == '\t' || c == '\n':
			i++
		case c == '#':
			start := i
			for i < len(r) && r[i] != '\n' {
				i++
			}
			out = append(out, Token{start, i, KindComment})
		case c == '"':
			start := i
			i++
			for i < len(r) {
				if r[i] == '\\' && i+1 < len(r) {
					i += 2
					continue
				}
				if r[i] == '"' {
					i++
					break
				}
				i++
			}
			out = append(out, Token{start, i, KindString})
		case c == '$':
			start := i
			i++
			for i < len(r) && identRune(r[i]) {
				i++
			}
			out = append(out, Token{start, i, KindVariable})
		case c == '@':
			start := i
			i++
			for i < len(r) && identRune(r[i]) {
				i++
			}
			out = append(out, Token{start, i, KindFormat})
		case c == '.':
			start := i
			i++
			if i < len(r) && r[i] == '.' { // recursive descent `..`
				i++
			} else {
				for i < len(r) && identRune(r[i]) {
					i++
				}
			}
			out = append(out, Token{start, i, KindPath})
		case c >= '0' && c <= '9':
			start := i
			for i < len(r) && numberRune(r[i]) {
				// An exponent may carry a sign: `1e-6` is one literal.
				if (r[i] == 'e' || r[i] == 'E') && i+1 < len(r) && (r[i+1] == '+' || r[i+1] == '-') {
					i++
				}
				i++
			}
			out = append(out, Token{start, i, KindNumber})
		case identStart(c):
			start := i
			for i < len(r) && identRune(r[i]) {
				i++
			}
			kind := KindFunc
			if keywords[string(r[start:i])] {
				kind = KindKeyword
			}
			out = append(out, Token{start, i, kind})
		default:
			out = append(out, Token{i, i + 1, KindOperator})
			i++
		}
	}
	return out
}

// KindAt reports the kind covering rune index i, KindPlain when none does.
// The renderer walks the line left to right, so a linear scan over the (few)
// tokens of a query line is the whole lookup this needs.
func KindAt(tokens []Token, i int) Kind {
	for _, t := range tokens {
		if i >= t.Start && i < t.End {
			return t.Kind
		}
		if t.Start > i {
			break // tokens are ordered
		}
	}
	return KindPlain
}

// identStart reports whether c may begin a jq identifier.
func identStart(c rune) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// identRune reports whether c may continue a jq identifier.
func identRune(c rune) bool { return identStart(c) || (c >= '0' && c <= '9') }

// numberRune reports whether c may continue a numeric literal (fraction and
// exponent included; the scanner colors, it does not validate the shape).
func numberRune(c rune) bool {
	return (c >= '0' && c <= '9') || c == '.' || c == 'e' || c == 'E'
}
