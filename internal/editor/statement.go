package editor

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/lang"
)

// statement.go implements Complete Current Statement (#2726, JetBrains'
// cmd+shift+enter): the buffer language's StatementCompleter says what the
// caret line is missing — balanced closers, the block colon or brace, the
// closing line — and the editor applies it as one structural edit, indents
// the body by the buffer's own settings, and leaves the caret in insert mode
// where the body goes, exactly the way "o" opens a line. A header that is
// already complete only moves the caret into its block (opening an empty
// indented line when the block has no body yet), so pressing the chord twice
// never adds a second colon or brace.

// completeStatement runs the command on the primary caret's line. An open
// insert session commits first, so the completion is its own undo step and
// typing continues in a fresh one.
func (m *Model) completeStatement() tea.Cmd {
	id := m.langID()
	line := m.buf.Line(m.cursor.Line)
	_, supported, ok := lang.CompleteStatement(id, line)
	if !supported {
		name := id
		if name == "" {
			name = "this file"
		}
		return notice("complete statement: not supported for " + name)
	}
	if !ok {
		return nil
	}
	// A read-only buffer refuses (#1762); a locked dependency file stashes
	// the whole command for the host's confirm to replay (#565), so the
	// caret is never placed on lines a locked recorder did not create.
	if m.refuseRO() {
		return nil
	}
	if m.blockDep() {
		m.stashDep(func(mm *Model) { mm.completeStatement() })
		return nil
	}
	if m.insert.active {
		m.commitInsert()
	}
	m.collapseCarets()
	rec := m.newRecorder()
	m.cursor = m.applyCompleteStatement(rec)
	m.desiredCol = m.cursor.Col
	m.startInsertWith(rec, func(mm *Model, r *history.Recorder) buffer.Position {
		return mm.applyCompleteStatement(r)
	})
	m.scroll()
	return nil
}

// applyCompleteStatement performs the structural edit through rec and returns
// the caret position inside the completed statement. It is the "." replay
// body too, so it re-reads the line it acts on.
func (m *Model) applyCompleteStatement(rec *history.Recorder) buffer.Position {
	row := m.cursor.Line
	line := m.buf.Line(row)
	c, _, ok := lang.CompleteStatement(m.langID(), line)
	if !ok {
		return m.cursor
	}
	indent := leadingWhitespace(line)
	if c.Head != strings.TrimRight(line, " \t") {
		rec.Apply(buffer.Edit{
			Range: buffer.Range{Start: buffer.Position{Line: row}, End: buffer.Position{Line: row, Col: m.buf.RuneLen(row)}},
			Text:  c.Head,
		})
	}
	end := buffer.Position{Line: row, Col: m.buf.RuneLen(row)}
	if !c.Body {
		// A simple statement: terminated, and the caret moves on to a fresh
		// line at the same indentation.
		return rec.Apply(buffer.Insert(end, "\n"+indent))
	}
	bodyIndent := indent + m.tabText()
	if row+1 < m.buf.LineCount() {
		next := m.buf.Line(row + 1)
		nextIndent := leadingWhitespace(next)
		deeper := strings.HasPrefix(nextIndent, indent) && len(nextIndent) > len(indent)
		if deeper && (strings.TrimSpace(next) != "" || next == nextIndent) {
			// The block already has a body (or the empty indented line a
			// previous press opened): the caret goes to its first line.
			return buffer.Position{Line: row + 1, Col: m.buf.RuneLen(row + 1)}
		}
		if len(c.Tail) > 0 && next == indent+c.Tail[0] {
			// The block is closed but empty ("{" directly above "}"): open
			// the body line between them, keeping the closer.
			return rec.Apply(buffer.Insert(end, "\n"+bodyIndent))
		}
	}
	text := "\n" + bodyIndent
	for _, t := range c.Tail {
		text += "\n" + indent + t
	}
	rec.Apply(buffer.Insert(end, text))
	return buffer.Position{Line: row + 1, Col: len([]rune(bodyIndent))}
}
