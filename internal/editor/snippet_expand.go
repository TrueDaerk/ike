package editor

// snippet_expand.go is the insert-mode live-template expansion (#1152): Tab
// with the cursor right after a trigger word — a user [[snippets]] entry or a
// built-in, resolved for the buffer's language by internal/snippets — replaces
// the word with the template body through the existing LSP snippet placeholder
// engine, and the tabstop session (#846) takes over tab/shift+tab exactly like
// an accepted snippet completion. No trigger match leaves Tab to its normal
// indent insertion (#1137).

import (
	"strings"

	"ike/internal/editor/buffer"
	"ike/internal/lsp/snippet"
	"ike/internal/snippets"
)

// expandSnippetTrigger tries the Tab expansion at the cursor and reports
// whether it fired. Only the single-caret case expands — with secondary carets
// the trigger word (and indentation) would differ per caret, so Tab keeps its
// indent meaning there.
func (m *Model) expandSnippetTrigger() bool {
	if m.hasCarets() {
		return false
	}
	start := m.identifierStart(m.cursor)
	if start.Col >= m.cursor.Col {
		return false // no word immediately before the cursor
	}
	runes := []rune(m.buf.Line(m.cursor.Line))
	if m.cursor.Col > len(runes) {
		return false
	}
	word := string(runes[start.Col:m.cursor.Col])
	body, ok := snippets.Lookup(m.path, word)
	if !ok {
		return false
	}
	src := m.reindentSnippetBody(body)
	text, stops, err := snippet.Expand(src)
	if err != nil {
		text, stops = src, nil // malformed body: insert it raw
	}
	if m.insert.rec == nil {
		m.insert.rec = m.newRecorder()
	}
	m.cursor = m.insert.rec.Apply(buffer.Edit{Range: buffer.Range{Start: start, End: m.cursor}, Text: text})
	m.desiredCol = m.cursor.Col
	m.insert.typed = "" // the expansion is not replayable "." text
	m.dirtyFromInsert()
	if len(stops) > 0 {
		m.startSnippetSession(text, stops)
	}
	return true
}

// reindentSnippetBody adapts a template body to the cursor's context (#1152):
// literal tabs become the buffer's indent unit (tab settings, #1137) and every
// continuation line inherits the current line's leading whitespace, so a
// multi-line template lands at the surrounding indentation.
func (m *Model) reindentSnippetBody(body string) string {
	body = strings.ReplaceAll(body, "\t", m.tabText())
	if indent := leadingWhitespace(m.buf.Line(m.cursor.Line)); indent != "" {
		body = strings.ReplaceAll(body, "\n", "\n"+indent)
	}
	return body
}
