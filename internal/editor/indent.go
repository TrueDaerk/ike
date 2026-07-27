package editor

import (
	"strings"

	"ike/internal/editor/buffer"
	"ike/internal/lang"
)

// indent.go implements smart indentation (Roadmap 0260): a freshly opened line
// derives its indent from the language's block-opening suffixes (lang.IndentAfter)
// instead of only copying the reference line's leading whitespace. The check is a
// pure text heuristic — no Tree-sitter — so an opener inside a trailing string
// literal false-positives; acceptable for v1.

// indentOpeners returns the block-opening suffixes governing line: the
// embedded region's language when one covers it (#1304) — a JSON body inside a
// .http file indents by JSON's rules, an XML body by XML's — otherwise the
// buffer's own language. A region whose language has no rules deliberately
// yields none: a plain-text body must not inherit the host's openers.
func (m *Model) indentOpeners(line int) ([]string, bool) {
	if l, ok := lang.ByPath(m.path); ok && l.Regions != nil {
		if r, ok := lang.RegionAt(l.ID, m.buf.Lines(), line); ok {
			el, known := lang.ByID(r.Lang)
			if !known || len(el.IndentAfter) == 0 {
				return nil, false
			}
			return el.IndentAfter, true
		}
	}
	return lang.IndentAfter(m.path)
}

// smartIndent returns the leading whitespace for a line opened after ref, the
// text of line: ref's own leading whitespace, deepened by one tab unit when the
// trimmed text ends with one of the governing language's IndentAfter suffixes.
// Without rules (or without an opener) it degrades to plain copy-indent.
func (m *Model) smartIndent(line int, ref string) string {
	indent := leadingWhitespace(ref)
	openers, ok := m.indentOpeners(line)
	if !ok {
		return indent
	}
	trimmed := strings.TrimRight(ref, " \t")
	for _, suf := range openers {
		if suf != "" && strings.HasSuffix(trimmed, suf) {
			return indent + m.tabText()
		}
	}
	return indent
}

// splitBlock handles Enter with the caret between a bracket pair (#518):
// "{|}" opens a three-line block — the closer moves to its own line at the
// reference line's indent, the caret lands on the (smart-indented) middle
// line. Returns ok=false when the caret is not between a matching pair, so
// the plain newline insert proceeds. In languages without IndentAfter rules
// (and plain text) the middle line keeps the copy-indent.
func (m *Model) splitBlock(pos buffer.Position) (buffer.Position, bool) {
	if pos.Col == 0 {
		return pos, false
	}
	closer, ok := closePairs[m.runeAt(buffer.Position{Line: pos.Line, Col: pos.Col - 1})]
	if !ok || m.runeAt(pos) != closer {
		return pos, false
	}
	left := []rune(m.buf.Line(pos.Line))
	ref := string(left[:min(pos.Col, len(left))])
	mid := m.smartIndent(pos.Line, ref)
	m.insert.rec.Apply(buffer.Insert(pos, "\n"+mid+"\n"+leadingWhitespace(ref)))
	return buffer.Position{Line: pos.Line + 1, Col: len([]rune(mid))}, true
}

// leadingWhitespace returns s's leading run of spaces and tabs.
func leadingWhitespace(s string) string {
	j := 0
	for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	return s[:j]
}
