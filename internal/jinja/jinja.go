// Package jinja is the Jinja layer of template files (#3059): a small pure-Go
// tokenizer — no Tree-sitter grammar — that finds the {{ … }}, {% … %} and
// {# … #} blocks of a .j2/.jinja/.jinja2 file, masks them before the inner
// language parses the text, and classifies their contents into highlight
// spans painted over the inner language's spans.
//
// Masking replaces every block with text of the same byte length and the same
// rune count, so byte offsets and line/column positions of the inner parse
// are unchanged: statement and comment tags become spaces (a {% if %} line
// inside a YAML mapping reads as a blank line), expressions become a run of
// "x" (a value, key or identifier the inner grammar accepts). The content of
// a {% raw %}…{% endraw %} block is inner-language text and stays as is.
package jinja

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"ike/internal/lang"
)

// Kind is the kind of a Jinja block.
type Kind int

const (
	// Expression is a {{ … }} block.
	Expression Kind = iota
	// Statement is a {% … %} block.
	Statement
	// Comment is a {# … #} block.
	Comment
)

// Block is one Jinja block: the half-open byte range [Start, End) of the
// text (lines joined by "\n"), delimiters included. Closed is false for a
// block left unterminated, which runs to the end of the text.
type Block struct {
	Kind       Kind
	Start, End int
	Closed     bool
}

// Class classifies a token inside a block.
type Class int

const (
	Delimiter   Class = iota // {{ }} {% %} {# #} with whitespace control
	Keyword                  // statement names (if, for, endif, …) and keyword operators
	Variable                 // plain names
	Filter                   // a filter after |, a test after is, a called name
	Property                 // a name after .
	Constant                 // true / false / none
	String                   // '…' and "…" literals
	Number                   // integer and float literals
	Operator                 // + - * / // % ** ~ = == != < <= > >= |
	Bracket                  // ( ) [ ] { }
	Punctuation              // , . :
	CommentText              // a whole {# … #} block
)

// captures maps every class to the existing theme capture it paints with.
var captures = [...]string{
	Delimiter:   "punctuation.special",
	Keyword:     "keyword",
	Variable:    "variable",
	Filter:      "function",
	Property:    "property",
	Constant:    "constant.builtin",
	String:      "string",
	Number:      "number",
	Operator:    "operator",
	Bracket:     "punctuation.bracket",
	Punctuation: "punctuation.delimiter",
	CommentText: "comment",
}

// Capture returns the theme capture name class c paints with.
func (c Class) Capture() string { return captures[c] }

// Token is one classified byte range [Start, End) of the text.
type Token struct {
	Start, End int
	Class      Class
}

// Template is the scan of one text: its blocks and tokens in source order.
type Template struct {
	src    string
	starts []int // byte offset of every line start
	Blocks []Block
	Tokens []Token
}

// keywords are the names that colour as keywords anywhere inside a tag;
// statementKeywords add the ones only a statement uses. The first name of a
// statement is always a keyword (custom extension tags included).
var (
	keywords = map[string]bool{
		"and": true, "or": true, "not": true, "in": true, "is": true,
		"if": true, "else": true,
	}
	statementKeywords = map[string]bool{
		"as": true, "import": true, "from": true, "with": true, "without": true,
		"context": true, "ignore": true, "missing": true, "recursive": true,
		"scoped": true, "required": true,
	}
	constants = map[string]bool{
		"true": true, "false": true, "none": true,
		"True": true, "False": true, "None": true,
	}
)

// Scan tokenizes lines as a Jinja template. Text outside blocks is the inner
// language's and yields no tokens. Unterminated blocks and strings end at
// the end of the text.
func Scan(lines []string) *Template {
	src := strings.Join(lines, "\n")
	t := &Template{src: src, starts: make([]int, 0, len(lines))}
	off := 0
	for _, l := range lines {
		t.starts = append(t.starts, off)
		off += len(l) + 1
	}
	t.scan()
	return t
}

// scan walks the text block by block.
func (t *Template) scan() {
	i := 0
	for {
		start, kind, ok := nextOpener(t.src, i)
		if !ok {
			return
		}
		var b Block
		if kind == Comment {
			b = t.comment(start)
		} else {
			b = t.tag(start, kind)
		}
		t.Blocks = append(t.Blocks, b)
		i = b.End
		if kind == Statement && b.Closed && t.firstName(b) == "raw" {
			// The raw body is inner-language text: skip to the endraw tag
			// without looking inside; an unterminated raw block owns the
			// rest of the text.
			end, ok := t.endRaw(i)
			if !ok {
				return
			}
			i = end
		}
	}
}

// nextOpener finds the next block opener at or after i.
func nextOpener(src string, i int) (int, Kind, bool) {
	for {
		j := strings.IndexByte(src[i:], '{')
		if j < 0 || i+j+1 >= len(src) {
			return 0, 0, false
		}
		i += j
		switch src[i+1] {
		case '{':
			return i, Expression, true
		case '%':
			return i, Statement, true
		case '#':
			return i, Comment, true
		}
		i++
	}
}

// openerLen is the length of the opener at start: two bytes plus an optional
// whitespace-control sign.
func openerLen(src string, start int) int {
	if start+2 < len(src) && (src[start+2] == '-' || src[start+2] == '+') {
		return 3
	}
	return 2
}

// closerAt reports the length of the closer of kind at i (with an optional
// leading whitespace-control sign), 0 when there is none.
func closerAt(src string, i int, kind Kind) int {
	c := byte('}')
	switch kind {
	case Statement:
		c = '%'
	case Comment:
		c = '#'
	}
	j := i
	if j < len(src) && (src[j] == '-' || src[j] == '+') {
		j++
	}
	if j+1 < len(src) && src[j] == c && src[j+1] == '}' {
		return j + 2 - i
	}
	return 0
}

// comment scans the {# … #} block opening at start; it holds no tokens of
// its own, the whole block is one comment.
func (t *Template) comment(start int) Block {
	b := Block{Kind: Comment, Start: start, End: len(t.src)}
	for i := start + openerLen(t.src, start); i < len(t.src); i++ {
		if n := closerAt(t.src, i, Comment); n > 0 {
			b.End, b.Closed = i+n, true
			break
		}
	}
	t.Tokens = append(t.Tokens, Token{Start: b.Start, End: b.End, Class: CommentText})
	return b
}

// tag scans the expression or statement block opening at start, emitting
// its delimiters and every token in between. Closers count only outside
// strings and at brace depth 0, so a dict literal or a quoted "}}" never ends
// the block. Only braces count: an unclosed "(" while typing must not run the
// block to the end of the text.
func (t *Template) tag(start int, kind Kind) Block {
	b := Block{Kind: kind, Start: start, End: len(t.src)}
	i := start + openerLen(t.src, start)
	t.emit(start, i, Delimiter)
	if end, ok := t.body(i, kind, true); ok {
		b.End, b.Closed = end, true
	}
	return b
}

// ExpressionTokens tokenizes text as the body of a {{ … }} block without
// delimiters (#3061): an Ansible `when:` value is a bare Jinja expression.
// Token offsets are bytes of text.
func ExpressionTokens(text string) []Token {
	t := &Template{src: text}
	t.body(0, Expression, false)
	return t.Tokens
}

// body emits the tokens of a block body from i. With closers it stops after
// the block's closer and returns its end; without, or when the closer never
// comes, it runs to the end of the text and reports false.
func (t *Template) body(i int, kind Kind, closers bool) (int, bool) {
	src := t.src
	depth := 0
	first := kind == Statement // the next name is the statement's keyword
	prev := Class(-1)          // class of the previous significant token
	prevText := ""
	for i < len(src) {
		r, size := utf8.DecodeRuneInString(src[i:])
		if unicode.IsSpace(r) {
			i += size
			continue
		}
		if closers && depth == 0 {
			if n := closerAt(src, i, kind); n > 0 {
				t.emit(i, i+n, Delimiter)
				return i + n, true
			}
		}
		end, class := i+size, Class(-1)
		switch {
		case r == '\'' || r == '"':
			end, class = scanString(src, i), String
		case r >= '0' && r <= '9':
			end, class = scanNumber(src, i), Number
		case r == '_' || unicode.IsLetter(r):
			end = scanName(src, i)
			class = t.classifyName(src, src[i:end], end, kind, first, prev, prevText)
			first = false
		case r == '{':
			depth++
			class = Bracket
		case r == '}':
			if depth > 0 {
				depth--
			}
			class = Bracket
		case strings.ContainsRune("()[]", r):
			class = Bracket
		case strings.ContainsRune(",.:", r):
			class = Punctuation
		case strings.ContainsRune("+-*/%~=!<>|", r):
			end, class = scanOperator(src, i), Operator
		}
		if class >= 0 {
			t.emit(i, end, class)
			prev, prevText = class, src[i:end]
		}
		i = end
	}
	return len(src), false
}

// classifyName classifies the name ending at end inside a block of kind.
func (t *Template) classifyName(src, name string, end int, kind Kind, first bool, prev Class, prevText string) Class {
	switch {
	case first:
		return Keyword
	case prev == Operator && prevText == "|":
		return Filter
	case prev == Keyword && !keywords[name] && t.afterIs():
		return Filter
	case prev == Punctuation && prevText == ".":
		return Property
	case constants[name]:
		return Constant
	case keywords[name], kind == Statement && statementKeywords[name]:
		return Keyword
	case nextNonSpace(src, end) == '(':
		return Filter
	}
	return Variable
}

// afterIs reports whether the last keyword tokens form "is" or "is not", the
// spot where a test name (defined, none, divisibleby, …) follows.
func (t *Template) afterIs() bool {
	n := len(t.Tokens)
	if n == 0 {
		return false
	}
	last := t.src[t.Tokens[n-1].Start:t.Tokens[n-1].End]
	if last == "is" {
		return true
	}
	return last == "not" && n >= 2 && t.src[t.Tokens[n-2].Start:t.Tokens[n-2].End] == "is"
}

// firstName returns the first name inside statement b ("raw", "if", …).
func (t *Template) firstName(b Block) string {
	for _, tok := range t.Tokens {
		if tok.Start >= b.Start && tok.End <= b.End && tok.Class == Keyword {
			return t.src[tok.Start:tok.End]
		}
	}
	return ""
}

// endRaw finds the {% endraw %} tag at or after i, scans it as a statement
// block and returns its end; false when the raw block never ends.
func (t *Template) endRaw(i int) (int, bool) {
	for {
		start, kind, ok := nextOpener(t.src, i)
		if !ok {
			return 0, false
		}
		if kind == Statement && isEndRaw(t.src, start) {
			b := t.tag(start, Statement)
			t.Blocks = append(t.Blocks, b)
			return b.End, true
		}
		i = start + 2
	}
}

// isEndRaw reports whether the statement opening at start is {% endraw %}.
func isEndRaw(src string, start int) bool {
	i := start + openerLen(src, start)
	for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
		i++
	}
	if !strings.HasPrefix(src[i:], "endraw") {
		return false
	}
	end := scanName(src, i)
	return end == i+len("endraw")
}

// emit appends one token.
func (t *Template) emit(start, end int, class Class) {
	if end > start {
		t.Tokens = append(t.Tokens, Token{Start: start, End: end, Class: class})
	}
}

// scanString returns the end of the string literal opening at i; an
// unterminated string runs to the end of the text.
func scanString(src string, i int) int {
	q := src[i]
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case q:
			return j + 1
		}
	}
	return len(src)
}

// scanNumber returns the end of the number literal starting at i: digits
// with "_" separators, an optional fraction and an optional exponent.
func scanNumber(src string, i int) int {
	digits := func(j int) int {
		for j < len(src) && (src[j] >= '0' && src[j] <= '9' || src[j] == '_') {
			j++
		}
		return j
	}
	j := digits(i)
	if j+1 < len(src) && src[j] == '.' && src[j+1] >= '0' && src[j+1] <= '9' {
		j = digits(j + 1)
	}
	if j < len(src) && (src[j] == 'e' || src[j] == 'E') {
		k := j + 1
		if k < len(src) && (src[k] == '+' || src[k] == '-') {
			k++
		}
		if k < len(src) && src[k] >= '0' && src[k] <= '9' {
			j = digits(k)
		}
	}
	return j
}

// scanName returns the end of the name starting at i.
func scanName(src string, i int) int {
	for i < len(src) {
		r, size := utf8.DecodeRuneInString(src[i:])
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		i += size
	}
	return i
}

// scanOperator returns the end of the operator starting at i, preferring the
// two-byte operators.
func scanOperator(src string, i int) int {
	if i+1 < len(src) {
		switch src[i : i+2] {
		case "**", "//", "==", "!=", "<=", ">=":
			return i + 2
		}
	}
	return i + 1
}

// nextNonSpace returns the first non-space byte at or after i, 0 at the end.
func nextNonSpace(src string, i int) byte {
	for i < len(src) {
		if c := src[i]; c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			return c
		}
		i++
	}
	return 0
}

// Mask returns lines with every block replaced by same-length text the inner
// grammar accepts: statements and comments by blanks, expressions by a run of
// "x". Line breaks inside a block stay, every line keeps its byte length and
// rune count. lines is returned as is when the text holds no block.
func (t *Template) Mask(lines []string) []string {
	if len(t.Blocks) == 0 {
		return lines
	}
	buf := []byte(t.src)
	for _, b := range t.Blocks {
		fill := blank
		if b.Kind == Expression {
			fill = placeholder
		}
		for i := b.Start; i < b.End; {
			if buf[i] == '\n' {
				i++
				continue
			}
			_, size := utf8.DecodeRune(buf[i:])
			copy(buf[i:], fill[size-1])
			i += size
		}
	}
	return strings.Split(string(buf), "\n")
}

// blank and placeholder are the per-rune fillers of Mask, indexed by the
// byte length of the masked rune minus one, so a masked rune keeps both its
// byte length and its single rune. Non-ASCII runes inside a tag are rare;
// their fillers are a whitespace-like rune for blanks and a letter for
// expressions.
var (
	blank       = [4]string{" ", " ", "　", "\U000e0020"}
	placeholder = [4]string{"x", "é", "一", "\U00020000"}
)

// Spans returns the tokens as highlight spans in rune columns, one span per
// line a token covers.
func (t *Template) Spans() []lang.Span {
	return t.spans(t.Tokens)
}

// BlockSpans returns the spans of the tokens inside block b (#3061): an
// Ansible YAML file keeps the blocks of its scalar values and drops the ones
// in its comments.
func (t *Template) BlockSpans(b Block) []lang.Span {
	lo := sort.Search(len(t.Tokens), func(i int) bool { return t.Tokens[i].Start >= b.Start })
	hi := lo
	for hi < len(t.Tokens) && t.Tokens[hi].Start < b.End {
		hi++
	}
	return t.spans(t.Tokens[lo:hi])
}

// Position returns the 0-based line and rune column of byte offset off.
func (t *Template) Position(off int) (int, int) { return t.position(off) }

// spans converts tokens to highlight spans in rune columns, one span per
// line a token covers.
func (t *Template) spans(toks []Token) []lang.Span {
	if len(toks) == 0 {
		return nil
	}
	out := make([]lang.Span, 0, len(toks))
	for _, tok := range toks {
		capture := tok.Class.Capture()
		line, col := t.position(tok.Start)
		for off := tok.Start; off < tok.End; {
			lineEnd := len(t.src)
			if line+1 < len(t.starts) {
				lineEnd = t.starts[line+1] - 1
			}
			end := min(tok.End, lineEnd)
			endCol := col + utf8.RuneCountInString(t.src[off:end])
			if endCol > col {
				out = append(out, lang.Span{Line: line, StartCol: col, EndCol: endCol, Capture: capture})
			}
			off, line, col = lineEnd+1, line+1, 0
		}
	}
	return out
}

// position converts a byte offset to its 0-based line and rune column.
func (t *Template) position(off int) (int, int) {
	lo, hi := 0, len(t.starts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if t.starts[mid] <= off {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo, utf8.RuneCountInString(t.src[t.starts[lo]:off])
}

// MaskPath returns lines masked for the inner parse when path is a Jinja
// template (lang.IsJinjaTemplate), and lines as is otherwise.
func MaskPath(path string, lines []string) []string {
	if !lang.IsJinjaTemplate(path) {
		return lines
	}
	return Scan(lines).Mask(lines)
}

// Overlaps reports whether the editor range from (startLine, startCol) to
// (endLine, endCol) — rune columns, end exclusive — touches any block
// (#3060): the test that drops a language server's diagnostic on a Jinja tag
// it saw as broken inner-language text. An empty range counts when its point
// lies inside a block.
func (t *Template) Overlaps(startLine, startCol, endLine, endCol int) bool {
	if len(t.Blocks) == 0 {
		return false
	}
	start := t.offset(startLine, startCol)
	end := t.offset(endLine, endCol)
	if end < start {
		start, end = end, start
	}
	if end == start {
		end++ // a point covers its own byte
	}
	for _, b := range t.Blocks {
		if b.Start >= end {
			break // blocks are in source order: the rest lie further on
		}
		if start < b.End {
			return true
		}
	}
	return false
}

// offset converts a 0-based line and rune column to a byte offset of the
// text, clamped to it: a line past the end maps to the end, a column past
// the line's end to the line break.
func (t *Template) offset(line, col int) int {
	if line < 0 {
		return 0
	}
	if line >= len(t.starts) {
		return len(t.src)
	}
	off := t.starts[line]
	lineEnd := len(t.src)
	if line+1 < len(t.starts) {
		lineEnd = t.starts[line+1] - 1
	}
	for n := 0; n < col && off < lineEnd; n++ {
		_, size := utf8.DecodeRuneInString(t.src[off:])
		off += size
	}
	return off
}
