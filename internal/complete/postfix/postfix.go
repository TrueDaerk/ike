// Package postfix is the postfix-completion source (#1913) — the JetBrains
// habit of writing the expression first and the construct after it:
// `err.nil` becomes `if err == nil { … }`, `foo(bar).if` wraps the whole call,
// `items.for` becomes a range loop. Unlike every other source it does not
// insert at the cursor: the accepted item replaces the `<expr>.<template>` span
// with the expansion, which the editor does through the item's ReplacePrefix.
//
// Templates are registered per language on lang.Language (Postfix), so a
// language plugin contributes its own set and this package stays neutral. The
// expression before the dot is found with Tree-sitter — the widest node of a
// language-declared expression kind ending exactly at the dot, which survives
// the broken mid-typing tree through error recovery — and falls back to a
// bracket-aware token scan when no grammar is available (CGO_ENABLED=0, an
// unparsed language, or a tree with no usable node).
package postfix

import (
	"context"
	"strings"
	"sync"
	"unicode"

	"ike/internal/complete"
	"ike/internal/highlight"
	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// SourceName tags this source's completion batches; it is ilsp.SourcePostfix so
// the editor's accept path can recognise the items without importing this
// package.
const SourceName = ilsp.SourcePostfix

// maxExprRunes caps the expression a template may wrap. A whole wrapped line is
// still readable in the popup detail; beyond that the "expression" is almost
// certainly a mis-detection, and silently rewriting half a screen would be the
// worst possible failure mode.
const maxExprRunes = 200

// Source is the postfix-completion source. It implements complete.Source,
// complete.EventObserver (it needs the buffer text to find the expression) and
// complete.TriggerSource (it is the one local source that answers after a ".").
type Source struct {
	mu      sync.RWMutex
	texts   map[string]string
	enabled func() bool
}

// New returns the source. enabled is consulted per query — nil means always on
// — so the editor.postfix_completion setting applies on a config reload without
// any re-wiring.
func New(enabled func() bool) *Source {
	return &Source{texts: map[string]string{}, enabled: enabled}
}

// Name implements complete.Source.
func (s *Source) Name() string { return SourceName }

// Priority implements complete.Source: below every member the server offers on
// the same dot, so an exact LSP match always ranks first.
func (s *Source) Priority() int { return ilsp.PriorityPostfix }

// TriggerChar implements complete.TriggerSource: the dot is what makes a
// postfix template reachable, and typing it must open the popup even where no
// language server answers.
func (s *Source) TriggerChar(ch string) bool { return ch == "." }

// Observe implements complete.EventObserver: the latest buffer text is stashed
// for the next query. Large-file buffers drop out — postfix completion parses
// the buffer, which is exactly the work large-file mode exists to avoid.
func (s *Source) Observe(ev host.EditorEvent) {
	if ev.Kind != host.EditorChange {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ev.Large {
		delete(s.texts, ev.Path)
		return
	}
	s.texts[ev.Path] = ev.Text
}

// Complete implements complete.Source: with a `<expr>.<word>` shape before the
// request position and templates registered for the buffer's language, every
// template is offered (the popup's fuzzy filter narrows by <word>). Anything
// else — no dot, no expression, no templates — answers nothing.
func (s *Source) Complete(_ context.Context, req complete.Request) ([]ilsp.CompletionItem, error) {
	if s.enabled != nil && !s.enabled() {
		return nil, nil
	}
	templates, exprNodes := lang.PostfixFor(req.Path)
	if len(templates) == 0 {
		return nil, nil
	}
	s.mu.RLock()
	text := s.texts[req.Path]
	s.mu.RUnlock()
	if text == "" {
		return nil, nil
	}
	lines := strings.Split(text, "\n")
	expr, _, ok := ExpressionBefore(req.Path, lines, req.Line, req.Col, exprNodes)
	if !ok {
		return nil, nil
	}
	// Everything from the expression's first rune through the dot is replaced
	// together with the typed template word.
	prefix := expr + "."
	errish := ErrorLike(expr)

	items := make([]ilsp.CompletionItem, 0, len(templates))
	for _, t := range templates {
		if t.ErrorLike && !errish {
			continue
		}
		body := strings.ReplaceAll(t.Body, lang.ExprPlaceholder, escapeSnippet(expr))
		items = append(items, ilsp.CompletionItem{
			Label:         t.Trigger,
			FilterText:    t.Trigger,
			SortText:      t.Trigger,
			InsertText:    body,
			ReplacePrefix: prefix,
			IsSnippet:     true,
			Detail:        "postfix " + detail(t, expr),
			Kind:          protocol.KindSnippet,
		})
	}
	return items, nil
}

// escapeSnippet makes the detected expression literal for the snippet expander
// (#846): a `$` in the source text would otherwise read as a tabstop and a `\`
// as an escape, so `os.Getenv("$HOME").if` would lose its variable.
func escapeSnippet(expr string) string {
	expr = strings.ReplaceAll(expr, `\`, `\\`)
	return strings.ReplaceAll(expr, "$", `\$`)
}

// detail is the popup preview: the template's own Detail with the expression
// substituted, falling back to a flattened body.
func detail(t lang.PostfixTemplate, expr string) string {
	src := t.Detail
	if src == "" {
		src = t.Body
	}
	return oneLine(strings.ReplaceAll(src, lang.ExprPlaceholder, expr))
}

// oneLine flattens an expansion preview to a short single-line popup detail.
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", "")
	for _, ph := range []string{"$1", "$2", "$3", "$0"} {
		s = strings.ReplaceAll(s, ph, "…")
	}
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 34 {
		s = string(r[:33]) + "…"
	}
	return s
}

// ExpressionBefore returns the expression the postfix template at (line, col)
// would wrap, plus the rune column of the dot separating them. The shape it
// looks for is `<expr>.<partial word>` ending at col: the dot may be the last
// character typed (`err.`) or already carry a partial trigger (`err.ni`).
//
// Detection is Tree-sitter first — the widest node of one of exprNodes ending
// exactly at the dot — and falls back to a bracket-aware token scan when the
// tree yields nothing (no grammar, no cgo, an unusable recovery). Exported for
// the tests, which exercise both paths directly.
func ExpressionBefore(path string, lines []string, line, col int, exprNodes []string) (expr string, dotCol int, ok bool) {
	if line < 0 || line >= len(lines) {
		return "", 0, false
	}
	runes := []rune(lines[line])
	if col > len(runes) {
		col = len(runes)
	}
	// Walk back over the partial template word, then require a dot.
	word := col
	for word > 0 && isIdentRune(runes[word-1]) {
		word--
	}
	if word == 0 || runes[word-1] != '.' {
		return "", 0, false
	}
	dotCol = word - 1
	// A float literal, a `..` range or a leading dot is not a member access.
	if dotCol == 0 || unicode.IsDigit(runes[dotCol-1]) || runes[dotCol-1] == '.' {
		return "", 0, false
	}

	if r, found := highlight.ExpressionEndingAt(path, lines, line, dotCol, exprNodes); found &&
		r.EndLine == line && r.EndCol == dotCol && r.StartCol < dotCol {
		expr = string(runes[r.StartCol:dotCol])
	} else {
		start, found := tokenStart(runes, dotCol)
		if !found {
			return "", 0, false
		}
		expr = string(runes[start:dotCol])
	}
	if expr == "" || len([]rune(expr)) > maxExprRunes {
		return "", 0, false
	}
	return expr, dotCol, true
}

// closers maps a closing bracket to its opener for the fallback scan.
var closers = map[rune]rune{')': '(', ']': '[', '}': '{'}

// tokenStart is the no-tree fallback: it walks left from end over the token
// chain a member access can be applied to — identifier runes, dots between
// them, and balanced bracket groups, so `foo(bar).` and `m["k"].` still find
// their expression. It deliberately stops at operators and whitespace, which
// makes it narrower than the Tree-sitter answer (`a + b.if` wraps `b`) rather
// than wrong.
func tokenStart(runes []rune, end int) (int, bool) {
	i := end
	for i > 0 {
		r := runes[i-1]
		switch {
		case isIdentRune(r):
			i--
		case r == '.' && i-1 > 0 && (isIdentRune(runes[i-2]) || closers[runes[i-2]] != 0):
			i-- // a member chain: keep going through `a.b.c`
		case closers[r] != 0:
			j, ok := matchOpener(runes, i-1)
			if !ok {
				return 0, false
			}
			i = j
		default:
			return i, i < end
		}
	}
	return 0, end > 0
}

// matchOpener returns the index of the bracket opening the closer at idx.
func matchOpener(runes []rune, idx int) (int, bool) {
	open := closers[runes[idx]]
	depth := 0
	for i := idx; i >= 0; i-- {
		switch runes[i] {
		case runes[idx]:
			depth++
		case open:
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// errNames are the identifiers (case-insensitively, as whole words or trailing
// camel-case segments) that mark an expression as an error value, gating the
// templates that only make sense on one — Go's `.err` → `if err != nil { … }`.
var errNames = []string{"err", "error"}

// ErrorLike reports whether expr looks like an error value: the identifier is
// `err`/`error`, ends in one as a camel-case or snake-case segment (`myErr`,
// `read_error`), or the expression is a call returning into one (`f().err`
// keeps the last chain segment's name). Exported for the tests.
func ErrorLike(expr string) bool {
	name := expr
	// A member/call chain is judged by its last named segment.
	if i := strings.LastIndexAny(name, ".)]"); i >= 0 && i < len(name)-1 {
		name = name[i+1:]
	}
	name = strings.TrimRight(name, "()[]")
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	lower := strings.ToLower(name)
	for _, e := range errNames {
		if lower == e || strings.HasSuffix(lower, "_"+e) || (strings.HasSuffix(lower, e) && hasUpperTail(name, len(e))) {
			return true
		}
	}
	return false
}

// hasUpperTail reports whether the last n runes of s start with an upper-case
// rune preceded by something — the camel-case boundary in "myErr".
func hasUpperTail(s string, n int) bool {
	r := []rune(s)
	if len(r) <= n {
		return false
	}
	return unicode.IsUpper(r[len(r)-n])
}

func isIdentRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
