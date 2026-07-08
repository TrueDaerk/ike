package editor

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/lang"
)

// comment.go implements comment toggling (Roadmap 0120): editor.commentLine
// (cmd+7) toggles the language's line-comment marker on the current line or
// the visual selection, JetBrains-style. The syntax comes from the language
// registry (lang.Comments); a buffer without comment syntax is a no-op with a
// NoticeMsg toast.

// NoticeMsg is user-facing feedback from an editor action ("no comment syntax
// for this file"). The root model renders it as an info toast; the editor
// itself stays host-free.
type NoticeMsg struct{ Text string }

// notice wraps text in a NoticeMsg command.
func notice(text string) tea.Cmd {
	return func() tea.Msg { return NoticeMsg{Text: text} }
}

// commentLine toggles line comments on the current line or every line of the
// visual selection (which is preserved). A single-line toggle advances the
// cursor one line, JetBrains-style. One undo unit; dot-repeatable.
func (m *Model) commentLine() tea.Cmd {
	if m.insert.active {
		m.commitInsert()
	}
	marker, _, ok := lang.Comments(m.path)
	if !ok || marker == "" {
		return notice("no line-comment syntax for this file")
	}

	a, z := m.cursor.Line, m.cursor.Line
	visual := m.mode.IsVisual()
	if visual {
		a, z = m.anchor.Line, m.cursor.Line
		if z < a {
			a, z = z, a
		}
	}
	m.toggleLineComments(a, z, marker, !visual)
	m.dot = &dotCommand{run: func(mm *Model) { mm.commentLine() }}
	return nil
}

// toggleLineComments applies one toggle to lines a..z: a fully commented range
// uncomments, a mixed or uncommented range comments its uncommented lines at
// the range's minimal indent. Blank lines are skipped both ways. advance moves
// the cursor down one line afterwards (the single-line JetBrains behavior).
func (m *Model) toggleLineComments(a, z int, marker string, advance bool) {
	if last := m.buf.LineCount() - 1; z > last {
		z = last
	}
	allCommented := true
	anyContent := false
	minIndent := 0
	for l := a; l <= z; l++ {
		trimmed := strings.TrimLeft(m.buf.Line(l), " \t")
		if trimmed == "" {
			continue
		}
		ind := m.buf.RuneLen(l) - len([]rune(trimmed))
		if !anyContent || ind < minIndent {
			minIndent = ind
		}
		anyContent = true
		if !strings.HasPrefix(trimmed, marker) {
			allCommented = false
		}
	}
	if !anyContent {
		return
	}

	cur := m.cursor
	m.mutate(func(rec *history.Recorder) buffer.Position {
		for l := a; l <= z; l++ {
			line := m.buf.Line(l)
			trimmed := strings.TrimLeft(line, " \t")
			if trimmed == "" {
				continue
			}
			ind := m.buf.RuneLen(l) - len([]rune(trimmed))
			if allCommented {
				n := len([]rune(marker))
				if strings.HasPrefix(strings.TrimPrefix(trimmed, marker), " ") {
					n++
				}
				rec.Apply(buffer.Delete(buffer.Range{
					Start: buffer.Position{Line: l, Col: ind},
					End:   buffer.Position{Line: l, Col: ind + n},
				}))
			} else if !strings.HasPrefix(trimmed, marker) {
				rec.Apply(buffer.Insert(buffer.Position{Line: l, Col: minIndent}, marker+" "))
			}
		}
		if advance {
			return buffer.Position{Line: cur.Line + 1, Col: cur.Col}
		}
		return cur
	})
}
