package lang

import (
	"strings"
	"unicode"
)

// CompletionContext classifies where the cursor sits when completion is
// requested (#2654). The editor computes it at trigger time and both
// producers — the local engine and the LSP bridge — gate their sources by
// it, so the popup stops opening where every candidate is noise: inside a
// comment or a string literal, right after a keyword that declares a new
// name, on an import line. The zero value is ordinary code, so a request
// built without a context behaves as before.
type CompletionContext string

const (
	// CtxCode is an ordinary code position: every source answers, the
	// auto-popup opens as it always did.
	CtxCode CompletionContext = ""
	// CtxComment is inside a comment: no auto-popup; a manual request
	// offers the current buffer's words only and never asks the server.
	CtxComment CompletionContext = "comment"
	// CtxString is inside a string literal: no auto-popup; no local source
	// answers (sources that claim strings — path completion, the `.http`
	// `{{` claim — keep answering), the server is still asked because it
	// may know import paths and the like.
	CtxString CompletionContext = "string"
	// CtxDecl is right after a declaring keyword (`func `, `def `, …): the
	// user is inventing a name, so no auto-popup; a manual request behaves
	// as in code.
	CtxDecl CompletionContext = "declaration"
	// CtxImport is on an import line: the auto-popup opens, but only the
	// server answers — the local indexes are skipped.
	CtxImport CompletionContext = "import"
)

// AutoTriggers reports whether the auto-popup may open in this context.
func (c CompletionContext) AutoTriggers() bool {
	return c == CtxCode || c == CtxImport
}

// LocalSources reports whether the ordinary local sources (word and symbol
// indexes, snippets, postfix, Emmet) answer in this context. Sources that
// declare a context of their own are consulted separately.
func (c CompletionContext) LocalSources() bool {
	return c == CtxCode || c == CtxDecl
}

// AsksServer reports whether the LSP bridge sends a completion request in
// this context.
func (c CompletionContext) AsksServer() bool {
	return c != CtxComment
}

// CompletionContextAt classifies the position col on line for language l.
// capture is the highlight capture covering the character before the
// current word (the syntax highlighter's answer, "" without a grammar):
// a `comment…` capture makes the context CtxComment, a `string…` capture
// CtxString. Without either, an import line (ImportLine) is CtxImport, and
// a DeclKeyword as the identifier-delimited word left of the current word
// makes it CtxDecl — `func na` is a declaration, `func(x` is a parameter
// list and `f(a, b` an argument list, since a bracket or comma, not
// whitespace, separates the words there. Everything else is CtxCode.
func CompletionContextAt(l Language, capture, line string, col int) CompletionContext {
	switch {
	case strings.HasPrefix(capture, "comment"):
		return CtxComment
	case strings.HasPrefix(capture, "string"):
		return CtxString
	}
	if l.ImportLine != nil && l.ImportLine.MatchString(line) {
		return CtxImport
	}
	if len(l.DeclKeywords) > 0 && isDeclKeyword(l.DeclKeywords, wordBefore(line, col)) {
		return CtxDecl
	}
	return CtxCode
}

// WordStart returns the rune column where the identifier ending at col
// starts — col itself when no identifier rune precedes it.
func WordStart(line string, col int) int {
	r := []rune(line)
	if col > len(r) {
		col = len(r)
	}
	start := col
	for start > 0 && isIdentRune(r[start-1]) {
		start--
	}
	return start
}

// wordBefore returns the identifier-delimited word left of the current word
// at col: the current word is the identifier run ending at col; at least one
// blank must separate the two; the previous word must end in an identifier
// rune (so `func(x` and `f(a, b` yield ""). Returns "" when there is none.
func wordBefore(line string, col int) string {
	r := []rune(line)
	if col > len(r) {
		col = len(r)
	}
	start := WordStart(line, col)
	end := start
	for end > 0 && (r[end-1] == ' ' || r[end-1] == '\t') {
		end--
	}
	if end == start {
		return "" // no blank between the words: a bracket or an operator sits there
	}
	if end == 0 || !isIdentRune(r[end-1]) {
		return ""
	}
	prev := end
	for prev > 0 && isIdentRune(r[prev-1]) {
		prev--
	}
	return string(r[prev:end])
}

func isDeclKeyword(keywords []string, w string) bool {
	if w == "" {
		return false
	}
	for _, k := range keywords {
		if strings.EqualFold(k, w) {
			return true
		}
	}
	return false
}

func isIdentRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
