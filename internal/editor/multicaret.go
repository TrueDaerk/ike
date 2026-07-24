package editor

import (
	"sort"
	"strings"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/editor/motion"
	"ike/internal/editor/operator"
	"ike/internal/editor/register"
	"ike/internal/editor/search"
	"ike/internal/editor/textobject"
)

// multicaret.go implements multi-caret editing (#145): the single cursor stays
// the primary caret, and an ordered set of secondary carets fans every edit
// out. Carets are per-view state like the cursor (#142) — they are never
// shared between panes. Edits from all carets go through one
// history.Recorder, so the whole fan-out is a single undo unit.

// caret is one secondary caret: its position plus the remembered column for
// vertical motion, mirroring the primary cursor/desiredCol pair.
type caret struct {
	pos        buffer.Position
	desiredCol int
}

// hasCarets reports whether any secondary caret is active.
func (m Model) hasCarets() bool { return len(m.carets) > 0 }

// Carets exposes the secondary caret positions (ascending) for tests and the
// status line.
func (m Model) Carets() []buffer.Position {
	out := make([]buffer.Position, len(m.carets))
	for i, c := range m.carets {
		out[i] = c.pos
	}
	return out
}

// collapseCarets drops every secondary caret and the remembered add-next
// query, returning to single-cursor editing.
func (m *Model) collapseCarets() {
	if m.carets == nil && m.caretQuery.Empty() {
		return
	}
	m.carets = nil
	m.caretQuery = search.Query{}
	m.emit(EventCursorMove)
}

// addCaret inserts a secondary caret at p unless a caret (primary or
// secondary) already sits there.
func (m *Model) addCaret(p buffer.Position) {
	if p.Equal(m.cursor) {
		return
	}
	for _, c := range m.carets {
		if c.pos.Equal(p) {
			return
		}
	}
	m.carets = append(m.carets, caret{pos: p, desiredCol: p.Col})
	m.sortCarets()
	m.emit(EventCursorMove)
}

// toggleCaret adds a caret at p, or removes the secondary caret already there
// (alt+click). A click on the primary caret is a no-op.
func (m *Model) toggleCaret(p buffer.Position) {
	for i, c := range m.carets {
		if c.pos.Equal(p) {
			m.carets = append(m.carets[:i], m.carets[i+1:]...)
			m.emit(EventCursorMove)
			return
		}
	}
	m.addCaret(p)
}

// sortCarets keeps the secondary carets in ascending buffer order and drops
// duplicates and any caret colliding with the primary cursor.
func (m *Model) sortCarets() {
	sort.SliceStable(m.carets, func(i, j int) bool { return m.carets[i].pos.Before(m.carets[j].pos) })
	out := m.carets[:0]
	for _, c := range m.carets {
		if c.pos.Equal(m.cursor) {
			continue
		}
		if n := len(out); n > 0 && out[n-1].pos.Equal(c.pos) {
			continue
		}
		out = append(out, c)
	}
	m.carets = out
	if len(m.carets) == 0 {
		m.carets = nil
	}
}

// clampCarets snaps every secondary caret into the current buffer (after an
// external reload or a shared-document sync, #142) and re-normalizes the set.
func (m *Model) clampCarets() {
	if !m.hasCarets() {
		return
	}
	for i := range m.carets {
		m.carets[i].pos = m.buf.ClampCursor(m.carets[i].pos)
		m.carets[i].desiredCol = m.carets[i].pos.Col
	}
	m.sortCarets()
}

// caretOnLine reports whether a secondary caret sits at (line, col) — the
// render probe for the dimmed caret cell.
func (m Model) caretOnLine(line, col int) bool {
	for _, c := range m.carets {
		if c.pos.Line == line && c.pos.Col == col {
			return true
		}
	}
	return false
}

// bufSize returns the buffer length in caret-offset space: runes plus one per
// line break.
func (m Model) bufSize() int {
	n := m.buf.LineCount() - 1
	for i := 0; i < m.buf.LineCount(); i++ {
		n += m.buf.RuneLen(i)
	}
	return n
}

// posToOffset converts a position to its rune offset (line breaks count one).
func (m Model) posToOffset(p buffer.Position) int {
	off := 0
	for i := 0; i < p.Line && i < m.buf.LineCount(); i++ {
		off += m.buf.RuneLen(i) + 1
	}
	return off + p.Col
}

// offsetToPos converts a rune offset back to a position, clamped to the buffer.
func (m Model) offsetToPos(off int) buffer.Position {
	if off < 0 {
		off = 0
	}
	line := 0
	for line < m.buf.LineCount()-1 && off > m.buf.RuneLen(line) {
		off -= m.buf.RuneLen(line) + 1
		line++
	}
	if l := m.buf.RuneLen(line); off > l {
		off = l
	}
	return buffer.Position{Line: line, Col: off}
}

// fanApply runs apply once per caret — the primary cursor and every secondary
// — in ascending buffer order. Each application may mutate the buffer; the
// engine measures how much the buffer grew or shrank and shifts the remaining
// carets by that delta, so every caret's edit lands where it would have
// before the earlier edits moved the text. floor is the previous caret's
// landing position (Line -1 for the first): backward deletes clamp to it so
// one caret's kill never eats another caret's edit. Carets landing on the
// same position merge. Without secondary carets it degenerates to a single
// call at the cursor.
func (m *Model) fanApply(apply func(pos, floor buffer.Position) buffer.Position) {
	noFloor := buffer.Position{Line: -1}
	if !m.hasCarets() {
		m.cursor = apply(m.cursor, noFloor)
		m.desiredCol = m.cursor.Col
		return
	}
	type slot struct {
		off     int
		primary bool
	}
	slots := make([]slot, 0, len(m.carets)+1)
	slots = append(slots, slot{off: m.posToOffset(m.cursor), primary: true})
	for _, c := range m.carets {
		slots = append(slots, slot{off: m.posToOffset(c.pos)})
	}
	sort.SliceStable(slots, func(i, j int) bool { return slots[i].off < slots[j].off })

	delta := 0
	floorOff := -1
	for i := range slots {
		before := m.bufSize()
		pos := m.offsetToPos(slots[i].off + delta)
		floor := noFloor
		if floorOff >= 0 {
			floor = m.offsetToPos(floorOff)
		}
		slots[i].off = m.posToOffset(apply(pos, floor))
		delta += m.bufSize() - before
		floorOff = slots[i].off
	}

	m.carets = m.carets[:0]
	for _, s := range slots {
		p := m.offsetToPos(s.off)
		if s.primary {
			m.cursor = p
			m.desiredCol = p.Col
			continue
		}
		m.carets = append(m.carets, caret{pos: p, desiredCol: p.Col})
	}
	m.sortCarets()
}

// fanMutate is fanApply committing through one recorder: every caret's edits
// join a single history.Change, so undo reverts the whole fan-out at once.
func (m *Model) fanMutate(apply func(rec *history.Recorder, pos, floor buffer.Position) buffer.Position) {
	rec := m.newRecorder()
	m.fanApply(func(pos, floor buffer.Position) buffer.Position {
		return apply(rec, pos, floor)
	})
	if !rec.Empty() {
		m.pushChange(rec.Commit(m.cursor))
		m.dirty = true
		m.emit(EventChange)
	}
	m.cursor = m.buf.ClampCursor(m.cursor)
	m.desiredCol = m.cursor.Col
}

// moveCarets applies a pure position transform to every secondary caret (no
// buffer mutation), then re-normalizes the set. clampInsert allows the
// one-past-end column insert mode uses.
func (m *Model) moveCarets(clampInsert bool, f func(pos buffer.Position, desired int) (buffer.Position, int)) {
	if !m.hasCarets() {
		return
	}
	for i := range m.carets {
		p, d := f(m.carets[i].pos, m.carets[i].desiredCol)
		if clampInsert {
			p = m.buf.Clamp(p)
		} else {
			p = m.buf.ClampCursor(p)
		}
		m.carets[i].pos = p
		m.carets[i].desiredCol = d
	}
	m.sortCarets()
}

// caretsOnePerLine drops all but the first caret on each line (including the
// primary's line) — the normalization linewise operators need so two carets
// on one line don't delete two lines.
func (m *Model) caretsOnePerLine() {
	seen := map[int]bool{m.cursor.Line: true}
	out := m.carets[:0]
	for _, c := range m.carets {
		if seen[c.pos.Line] {
			continue
		}
		seen[c.pos.Line] = true
		out = append(out, c)
	}
	m.carets = out
	if len(m.carets) == 0 {
		m.carets = nil
	}
}

// fanMotionSecondaries applies the motion keyed by s to every secondary caret
// (the caller moves the primary itself). insertClamp allows the one-past-end
// column. Motions resolve against each caret's own position.
func (m *Model) fanMotionSecondaries(s string, r rune, count int, insertClamp bool) {
	if !m.hasCarets() {
		return
	}
	savedCur, savedDes := m.cursor, m.desiredCol
	for i := range m.carets {
		m.cursor, m.desiredCol = m.carets[i].pos, m.carets[i].desiredCol
		res, ok := m.resolveMotion(s, r, count)
		if !ok {
			continue
		}
		if res.Kind == motion.Linewise {
			// Vertical motion keeps each caret's remembered column.
			m.carets[i].pos = m.buf.ClampCursor(buffer.Position{Line: res.Pos.Line, Col: m.carets[i].desiredCol})
		} else {
			p := res.Pos
			if insertClamp {
				p = m.buf.Clamp(p)
			} else {
				p = m.buf.ClampCursor(p)
			}
			m.carets[i].pos = p
			m.carets[i].desiredCol = p.Col
		}
	}
	m.cursor, m.desiredCol = savedCur, savedDes
	m.sortCarets()
}

// targetText extracts what target covers, for the joined multi-caret yank:
// whole lines (with trailing newline) for a linewise target, the sliced span
// otherwise.
func (m Model) targetText(t operator.Target) string {
	if !t.Linewise {
		return m.buf.Slice(t.Range)
	}
	var sb strings.Builder
	for i := t.Range.Start.Line; i <= t.Range.End.Line && i < m.buf.LineCount(); i++ {
		sb.WriteString(m.buf.Line(i))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// caretTarget resolves the motion keyed by s at pos into an operator target,
// temporarily parking the cursor there so motion resolution sees the caret.
func (m *Model) caretTarget(s string, r rune, count int, pos buffer.Position) (operator.Target, bool) {
	savedCur, savedDes := m.cursor, m.desiredCol
	m.cursor = pos
	res, ok := m.resolveMotion(s, r, count)
	m.cursor, m.desiredCol = savedCur, savedDes
	if !ok {
		return operator.Target{}, false
	}
	return operator.Compose(m.buf, pos, res.Pos, res.Kind), true
}

// fanOperatorMotion applies the pending d/c/y operator with its motion at
// every caret, as one undo unit. The indent operators stay primary-only.
func (m *Model) fanOperatorMotion(s string, r rune, count int) {
	op, reg := m.pending.Operator, m.pending.Register
	switch op {
	case 'y', 'd', 'c':
		m.fanOperatorTargets(op, reg, func(pos buffer.Position) (operator.Target, bool) {
			return m.caretTarget(s, r, count, pos)
		})
	default:
		// >/<: indent at the primary caret only; carets re-clamp via mutate.
		if res, ok := m.resolveMotion(s, r, count); ok {
			target := operator.Compose(m.buf, m.cursor, res.Pos, res.Kind)
			m.runOperator(op, target, reg)
		}
	}
}

// fanOperatorTargets runs a d/c/y operator over the per-caret targets resolve
// yields, as one undo unit. Yank joins the per-caret spans with newlines into
// the register; delete and change record the joined text the same way (the
// per-caret deletes go to a scratch store so the real register holds every
// caret's span instead of just the last).
func (m *Model) fanOperatorTargets(op, reg rune, resolve func(pos buffer.Position) (operator.Target, bool)) {
	if op == 'y' {
		var parts []string
		for _, pos := range m.allCaretPositions() {
			if t, ok := resolve(pos); ok {
				parts = append(parts, strings.TrimSuffix(m.targetText(t), "\n"))
			}
		}
		if len(parts) > 0 {
			m.regs.Yank(reg, register.Entry{Text: strings.Join(parts, "\n")})
		}
		return
	}
	var parts []string
	scratch := register.New()
	rec := m.newRecorder()
	m.fanApply(func(pos, _ buffer.Position) buffer.Position {
		t, ok := resolve(pos)
		if !ok {
			return pos
		}
		parts = append(parts, strings.TrimSuffix(m.targetText(t), "\n"))
		if op == 'd' {
			return operator.Delete(m.buf, rec, scratch, reg, t)
		}
		return operator.Change(m.buf, rec, scratch, reg, t)
	})
	if len(parts) > 0 {
		m.regs.Delete(reg, register.Entry{Text: strings.Join(parts, "\n")})
	}
	if op == 'c' {
		m.startInsertWith(rec, nil)
		return
	}
	if !rec.Empty() {
		m.pushChange(rec.Commit(m.cursor))
		m.dirty = true
		m.emit(EventChange)
	}
}

// fanLinewiseOperator fans dd/cc/yy over every caret's line span as one undo
// unit; the register receives the concatenated lines. Carets were already
// merged to one per line by the caller.
func (m *Model) fanLinewiseOperator(op rune, count int) {
	m.caretsOnePerLine()
	reg := m.pending.Register
	lineTarget := func(pos buffer.Position) operator.Target {
		end := pos.Line + count - 1
		if end > m.buf.LineCount()-1 {
			end = m.buf.LineCount() - 1
		}
		return operator.LineTarget(pos.Line, end)
	}
	switch op {
	case 'y':
		var sb strings.Builder
		for _, pos := range m.allCaretPositions() {
			sb.WriteString(m.targetText(lineTarget(pos)))
		}
		if sb.Len() > 0 {
			m.regs.Yank(reg, register.Entry{Text: sb.String(), Linewise: true})
		}
	case 'd', 'c':
		var sb strings.Builder
		scratch := register.New()
		rec := m.newRecorder()
		m.fanApply(func(pos, _ buffer.Position) buffer.Position {
			t := lineTarget(pos)
			sb.WriteString(m.targetText(t))
			if op == 'd' {
				return operator.Delete(m.buf, rec, scratch, reg, t)
			}
			return operator.Change(m.buf, rec, scratch, reg, t)
		})
		if sb.Len() > 0 {
			m.regs.Delete(reg, register.Entry{Text: sb.String(), Linewise: true})
		}
		if op == 'c' {
			m.startInsertWith(rec, nil)
			return
		}
		if !rec.Empty() {
			m.pushChange(rec.Commit(m.cursor))
			m.dirty = true
			m.emit(EventChange)
		}
	}
}

// allCaretPositions returns every caret position — primary plus secondaries —
// in ascending buffer order.
func (m Model) allCaretPositions() []buffer.Position {
	out := make([]buffer.Position, 0, len(m.carets)+1)
	out = append(out, m.cursor)
	for _, c := range m.carets {
		out = append(out, c.pos)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// caretWordQuery compiles the exact-match query for the word under the
// primary caret and snaps the cursor onto the word start, remembering the
// query for the occurrence walk. ok is false when no word is under the caret.
func (m *Model) caretWordQuery() bool {
	res := textobject.Word(m.buf, m.cursor, false, false)
	if !res.OK {
		return false
	}
	word := m.buf.Slice(res.Range)
	if word == "" {
		return false
	}
	m.caretQuery = search.CompileExact(word)
	m.moveTo(res.Range.Start)
	return true
}

// caretAddNext implements editor.caret.addNext: the first invocation locks
// onto the word under the primary caret; each following one leaves a caret
// behind and jumps the primary to the next occurrence, wrapping and skipping
// occurrences that already hold a caret.
func (m *Model) caretAddNext() {
	if m.caretQuery.Empty() {
		if !m.caretWordQuery() {
			return
		}
		m.emit(EventCursorMove)
		return
	}
	all := m.caretQuery.AllMatches(m.buf)
	if len(all) == 0 {
		return
	}
	occupied := func(p buffer.Position) bool {
		if p.Equal(m.cursor) {
			return true
		}
		for _, c := range m.carets {
			if c.pos.Equal(p) {
				return true
			}
		}
		return false
	}
	// Walk forward from the primary caret, wrapping once around the buffer.
	start := 0
	for i, s := range all {
		if s.Line > m.cursor.Line || (s.Line == m.cursor.Line && s.Start > m.cursor.Col) {
			start = i
			break
		}
	}
	for i := 0; i < len(all); i++ {
		s := all[(start+i)%len(all)]
		p := buffer.Position{Line: s.Line, Col: s.Start}
		if occupied(p) {
			continue
		}
		// The old primary becomes a secondary caret; the primary jumps on.
		m.carets = append(m.carets, caret{pos: m.cursor, desiredCol: m.desiredCol})
		m.moveTo(p)
		m.sortCarets()
		return
	}
	m.cmdMsg = "all occurrences have carets"
}

// caretAddAll implements editor.caret.addAll: a caret at every occurrence of
// the word under the primary caret, the primary staying on its own occurrence.
func (m *Model) caretAddAll() {
	if m.caretQuery.Empty() && !m.caretWordQuery() {
		return
	}
	for _, s := range m.caretQuery.AllMatches(m.buf) {
		m.addCaret(buffer.Position{Line: s.Line, Col: s.Start})
	}
}

// blockCarets converts the visual-block rectangle into carets — one per line
// — and enters insert mode: I at the block's left edge (skipping lines
// shorter than it, vim-style), A one past its right edge, clamped to each
// line's end. The primary caret takes the top line.
func (m *Model) blockCarets(appendSide bool) {
	lo, hi := m.anchor.Line, m.cursor.Line
	if hi < lo {
		lo, hi = hi, lo
	}
	cLo, cHi := minInt(m.anchor.Col, m.cursor.Col), maxInt(m.anchor.Col, m.cursor.Col)
	var positions []buffer.Position
	for l := lo; l <= hi; l++ {
		n := m.buf.RuneLen(l)
		if appendSide {
			col := cHi + 1
			if col > n {
				col = n
			}
			positions = append(positions, buffer.Position{Line: l, Col: col})
			continue
		}
		if n < cLo {
			continue // vim's block-I skips lines shorter than the left edge
		}
		positions = append(positions, buffer.Position{Line: l, Col: cLo})
	}
	m.mode = Normal
	if len(positions) == 0 {
		return
	}
	m.cursor = positions[0]
	m.desiredCol = m.cursor.Col
	m.carets = nil
	for _, p := range positions[1:] {
		m.carets = append(m.carets, caret{pos: p, desiredCol: p.Col})
	}
	m.startInsertWith(m.newRecorder(), nil)
}
