package editor

import (
	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/editor/motion"
	"ike/internal/editor/operator"
	"ike/internal/editor/search"
	"ike/internal/editor/textobject"
)

// newRecorder opens a history recorder anchored at the current cursor.
func (m *Model) newRecorder() *history.Recorder { return history.NewRecorder(m.buf, m.cursor) }

// cursorRightForAppend moves the cursor one column right for "a", allowing the
// one-past-end position insert mode uses.
func (m *Model) cursorRightForAppend() {
	if m.buf.RuneLen(m.cursor.Line) > 0 {
		m.cursor.Col++
	}
	if max := m.buf.RuneLen(m.cursor.Line); m.cursor.Col > max {
		m.cursor.Col = max
	}
}

// openLine implements o/O: open a new (optionally auto-indented) line and enter
// insert mode, with the structural edit recorded for undo and "." replay.
func (m *Model) openLine(below bool) {
	rec := m.newRecorder()
	m.applyOpenLine(rec, below)
	m.startInsertWith(rec, func(mm *Model, r *history.Recorder) buffer.Position {
		return mm.applyOpenLine(r, below)
	})
}

// applyOpenLine performs the structural part of o/O through rec and returns the
// cursor position where insertion begins.
func (m *Model) applyOpenLine(rec *history.Recorder, below bool) buffer.Position {
	indent := ""
	if m.autoIndent {
		if below {
			// "o" opens a block body when the current line ends with an
			// opener (Roadmap 0260); "O" keeps plain copy-indent.
			indent = m.smartIndent(m.buf.Line(m.cursor.Line))
		} else {
			indent = m.indentOf(m.cursor.Line)
		}
	}
	if below {
		at := buffer.Position{Line: m.cursor.Line, Col: m.buf.RuneLen(m.cursor.Line)}
		m.cursor = rec.Apply(buffer.Insert(at, "\n"+indent))
	} else {
		at := buffer.Position{Line: m.cursor.Line, Col: 0}
		rec.Apply(buffer.Insert(at, indent+"\n"))
		m.cursor = buffer.Position{Line: m.cursor.Line, Col: len([]rune(indent))}
	}
	m.desiredCol = m.cursor.Col
	return m.cursor
}

// applyLinewiseOperator runs op over count whole lines from the cursor.
func (m *Model) applyLinewiseOperator(op rune, count int) {
	if count < 1 {
		count = 1
	}
	end := m.cursor.Line + count - 1
	if end > m.buf.LineCount()-1 {
		end = m.buf.LineCount() - 1
	}
	target := operator.LineTarget(m.cursor.Line, end)
	reg := m.pending.Register
	m.runOperator(op, target, reg)
	if op == 'd' || op == '>' || op == '<' {
		m.dot = &dotCommand{run: func(mm *Model) { mm.applyLinewiseOperator(op, count) }}
	}
}

// searchWord implements "*"/"#": search for the word under the cursor forward
// (true) or backward (false).
func (m *Model) searchWord(forward bool) {
	res := textobject.Word(m.buf, m.cursor, false, false)
	if !res.OK {
		return
	}
	word := m.buf.Slice(res.Range)
	if word == "" {
		return
	}
	m.query = search.CompileExact(word) // "*"/"#" match the word exactly, no smartcase
	if forward {
		m.searchDir = search.Forward
	} else {
		m.searchDir = search.Backward
	}
	if p, ok := m.query.Next(m.buf, m.cursor, m.searchDir, 1); ok {
		m.hlActive = true
		m.jumpTo(p) // "*"/"#" landings are jumps (Roadmap 0220)
	}
}

// applyFind runs an in-line character search, applying a pending operator or
// moving the cursor.
func (m *Model) applyFind(f motion.Find) {
	res, ok := f.Apply(m.buf, m.cursor, m.pending.EffectiveCount())
	if !ok {
		return
	}
	if m.pending.HasOperator() {
		target := operator.Compose(m.buf, m.cursor, res.Pos, res.Kind)
		m.runOperator(m.pending.Operator, target, m.pending.Register)
		return
	}
	m.moveTo(res.Pos)
}

// resolveTextObject maps an object selector rune to a buffer range, honouring
// the inner/around flag (m.around).
func (m *Model) resolveTextObject(r rune) textobject.Result {
	switch r {
	case 'w':
		return textobject.Word(m.buf, m.cursor, m.around, false)
	case 'W':
		return textobject.Word(m.buf, m.cursor, m.around, true)
	case '"', '\'', '`':
		return textobject.Quote(m.buf, m.cursor, r, m.around)
	default:
		if o, c, ok := textobject.CloseFor(r); ok {
			return textobject.Pair(m.buf, m.cursor, o, c, m.around)
		}
	}
	return textobject.Result{}
}

// applyTextObject resolves the text object named by r and applies the pending
// operator to it. Without a pending operator it is a no-op (vim ignores a bare
// "iw" in normal mode).
func (m *Model) applyTextObject(r rune) {
	res := m.resolveTextObject(r)
	if !res.OK || !m.pending.HasOperator() {
		return
	}
	m.runOperator(m.pending.Operator, operator.CharTarget(res.Range), m.pending.Register)
}
