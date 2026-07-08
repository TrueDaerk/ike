package editor

import (
	"sort"
	"strings"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
)

// replace.go is the buffer half of replace-in-path (Roadmap 0150, #86): a
// dirty buffer's file must not be rewritten on disk, so project-wide
// replacements route through the open buffer as ordinary edits — one undo
// unit per file, exactly like an operator.

// Replacement is one range rewrite: on the 1-based Line, the [StartCol,
// EndCol) rune range becomes Text. Expect is the line's text at match time; a
// line whose prefix up to EndCol no longer reads as scanned (a stale match)
// is skipped rather than corrupted. Prefix — not whole-line — comparison
// keeps several matches on one line independently valid: applying the
// rightmost only changes the line right of the next one's range.
type Replacement struct {
	Line     int
	StartCol int
	EndCol   int
	Text     string
	Expect   string
}

// ApplyReplacements applies every still-valid replacement as one history
// change (a single undo reverts the whole file's batch) and returns how many
// were applied. Edits are applied bottom-up, right-to-left, so earlier
// positions stay valid while later ones shift.
func (m *Model) ApplyReplacements(reps []Replacement) int {
	valid := make([]Replacement, 0, len(reps))
	for _, r := range reps {
		line := r.Line - 1
		if line < 0 || line >= m.buf.LineCount() || !prefixMatches(m.buf.Line(line), r.Expect, r.EndCol) {
			continue // stale: the buffer moved on since the scan
		}
		valid = append(valid, r)
	}
	if len(valid) == 0 {
		return 0
	}
	sort.Slice(valid, func(i, j int) bool {
		if valid[i].Line != valid[j].Line {
			return valid[i].Line > valid[j].Line
		}
		return valid[i].StartCol > valid[j].StartCol
	})

	cursorBefore := m.cursor
	var fwd, inv []buffer.Edit
	for _, r := range valid {
		e := buffer.Edit{
			Range: buffer.Range{
				Start: buffer.Position{Line: r.Line - 1, Col: r.StartCol},
				End:   buffer.Position{Line: r.Line - 1, Col: r.EndCol},
			},
			Text: r.Text,
		}
		inverse, _ := m.buf.Apply(e)
		fwd = append(fwd, e)
		inv = append(inv, inverse)
	}
	m.cursor = m.buf.ClampCursor(m.cursor)
	m.hist.Push(history.Change{
		Forwards:     fwd,
		Inverses:     inv,
		CursorBefore: cursorBefore,
		CursorAfter:  m.cursor,
	})
	m.dirty = true
	m.scroll()
	m.emit(EventChange)
	return len(valid)
}

// prefixMatches reports whether cur still reads like expect up to the endCol
// rune — the staleness guard for one match range.
func prefixMatches(cur, expect string, endCol int) bool {
	er := []rune(expect)
	if endCol > len(er) {
		return false
	}
	return strings.HasPrefix(cur, string(er[:endCol]))
}
