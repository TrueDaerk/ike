package editor

// selrange.go implements syntax-aware extend/shrink selection (#1912),
// JetBrains' Expand/Shrink Selection on the editor's visual mode. Extend asks
// the LSP bridge for the textDocument/selectionRange ladder at the cursor
// (ilsp.RequestSelectionRanges) — or the Tree-sitter fallback
// (highlight.SelectionRangesAt) when no provider is registered — and selects
// the innermost range; each further extend grows the selection to the next
// ladder step, shrink walks back down, and shrinking below the first step
// restores the cursor and returns to normal mode. Replies arrive as an
// ilsp.SelectionRangesMsg either way (the fallback command builds the same
// message type), so the Update path is one funnel. When both sources come up
// empty a word → line → buffer ladder keeps the command useful in any file.

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/textobject"
	"ike/internal/highlight"
	ilsp "ike/internal/lsp"
)

// selRangeState is the ladder state between an extend request and the shrink
// steps that unwind it. The ladder is innermost-first with strictly growing
// containment; depth indexes the currently applied step (-1 while the request
// is in flight). docVersion pins the ladder to the buffer text it was computed
// for — any edit invalidates it wholesale, and the selection-equals check in
// extend/shrink covers cursor motion and mode changes in between.
type selRangeState struct {
	ladder []buffer.Range
	depth  int
	// origin is the cursor position of the initial extend; the bottom shrink
	// restores it.
	origin buffer.Position
	// req anchors the pending request; a reply for another position is stale.
	req     buffer.Position
	pending bool
	// base is the charwise extent of the visual selection the initial extend
	// started from (nil when it started in normal mode): the first applied
	// step must strictly contain it, JetBrains style.
	base *buffer.Range
	// fallbackTried marks that the Tree-sitter fallback already ran, so an
	// empty reply falls through to the word/line/buffer ladder instead of
	// looping.
	fallbackTried bool
	docVersion    int
}

// extendSelection runs the selection_extend action: grow an active ladder one
// step, or start a fresh request at the cursor. The returned command is the
// LSP request or the Tree-sitter fallback; nil when the ladder grew (or could
// not grow) synchronously.
func (m *Model) extendSelection() tea.Cmd {
	if s := m.selRange; s != nil && !s.pending && s.docVersion == m.docVersion &&
		s.depth >= 0 && s.depth < len(s.ladder) && m.selectionMatches(s.ladder[s.depth]) {
		// The ladder is still live and the selection is exactly the applied
		// step: grow to the next-larger range (top step: nothing left to do).
		if s.depth+1 < len(s.ladder) {
			s.depth++
			m.applySelRange(s.ladder[s.depth])
		}
		return nil
	}
	at := m.cursor
	s := &selRangeState{req: at, origin: at, depth: -1, pending: true, docVersion: m.docVersion}
	if m.mode.IsVisual() {
		// A manually made selection: the first step must strictly contain it.
		r := m.selectionCharRange()
		s.base = &r
	}
	m.selRange = s
	if cmd := ilsp.RequestSelectionRanges(m.path, at.Line, at.Col); cmd != nil {
		return cmd
	}
	// No LSP provider registered: go straight to the Tree-sitter fallback.
	s.fallbackTried = true
	return m.tsSelectionRangesCmd(at)
}

// shrinkSelection runs the selection_shrink action: step back down the ladder;
// below the first step the cursor returns to where the initial extend started
// and the editor drops to normal mode. A stale or absent ladder is a no-op.
func (m *Model) shrinkSelection() {
	s := m.selRange
	if s == nil || s.pending || s.docVersion != m.docVersion ||
		s.depth < 0 || s.depth >= len(s.ladder) || !m.selectionMatches(s.ladder[s.depth]) {
		return
	}
	if s.depth == 0 {
		m.bumpRender()
		m.mode = Normal
		m.cursor = m.buf.ClampCursor(s.origin)
		m.desiredCol = m.cursor.Col
		m.selRange = nil
		m.emit(EventCursorMove)
		return
	}
	s.depth--
	m.applySelRange(s.ladder[s.depth])
}

// handleSelectionRanges consumes a SelectionRangesMsg — the LSP reply or the
// Tree-sitter fallback's self-built equivalent. Stale replies (wrong path or
// anchor, no request pending, buffer edited since) are dropped. An empty or
// fully filtered ladder chains into the fallback once, then into the
// word/line/buffer last resort. The returned command is that chained fallback,
// nil otherwise.
func (m *Model) handleSelectionRanges(msg ilsp.SelectionRangesMsg) tea.Cmd {
	s := m.selRange
	if s == nil || !s.pending || msg.Path != m.path ||
		msg.Line != s.req.Line || msg.Col != s.req.Col || s.docVersion != m.docVersion {
		return nil
	}
	ladder := m.sanitizeLadder(msg.Ranges, s.req, s.base)
	if len(ladder) == 0 {
		if !s.fallbackTried {
			s.fallbackTried = true
			return m.tsSelectionRangesCmd(s.req)
		}
		ladder = m.sanitizeLadder(m.lastResortLadder(s.req), s.req, s.base)
		if len(ladder) == 0 {
			m.selRange = nil // nothing selectable (e.g. empty buffer)
			return nil
		}
	}
	s.pending = false
	s.ladder = ladder
	s.depth = 0
	m.applySelRange(ladder[0])
	return nil
}

// tsSelectionRangesCmd snapshots the buffer (parseCmd's pattern) and computes
// the Tree-sitter ladder off the event loop, delivering it as the same
// SelectionRangesMsg an LSP reply arrives as.
func (m *Model) tsSelectionRangesCmd(at buffer.Position) tea.Cmd {
	path := m.path
	lines := m.buf.Lines()
	return func() tea.Msg {
		var ranges []buffer.Range
		for _, nr := range highlight.SelectionRangesAt(path, lines, at.Line, at.Col) {
			ranges = append(ranges, buffer.Range{
				Start: buffer.Position{Line: nr.StartLine, Col: nr.StartCol},
				End:   buffer.Position{Line: nr.EndLine, Col: nr.EndCol},
			})
		}
		return ilsp.SelectionRangesMsg{Path: path, Line: at.Line, Col: at.Col, Ranges: ranges}
	}
}

// sanitizeLadder turns a raw reply into an applicable ladder: ranges clamped
// to the buffer, an End at column 0 pulled back onto the previous line's end
// (so the applied visual selection covers exactly the range's text — a
// trailing newline is not a selectable cell), empties dropped, and only ranges
// containing the request position with strictly growing containment kept —
// which also removes duplicates. base, when set, additionally requires every
// step to strictly contain the pre-existing selection.
func (m *Model) sanitizeLadder(raw []buffer.Range, at buffer.Position, base *buffer.Range) []buffer.Range {
	var out []buffer.Range
	for _, r := range raw {
		r.Start, r.End = m.buf.Clamp(r.Start), m.buf.Clamp(r.End)
		for r.End.Col == 0 && r.End.Line > r.Start.Line {
			r.End = buffer.Position{Line: r.End.Line - 1, Col: m.buf.RuneLen(r.End.Line - 1)}
		}
		if !r.Start.Before(r.End) {
			continue // empty or inverted
		}
		if at.Before(r.Start) || !at.Before(r.End) {
			continue // does not contain the request position
		}
		if len(out) > 0 && !strictlyContains(r, out[len(out)-1]) {
			continue
		}
		if base != nil && !strictlyContains(r, *base) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// strictlyContains reports whether outer covers inner and is larger.
func strictlyContains(outer, inner buffer.Range) bool {
	if inner.Start.Before(outer.Start) || outer.End.Before(inner.End) {
		return false
	}
	return outer != inner
}

// lastResortLadder is the ladder used when neither the server nor Tree-sitter
// produced one: the word under the cursor, its line, the whole buffer —
// sanitized like any other reply, so empty steps simply drop out.
func (m *Model) lastResortLadder(at buffer.Position) []buffer.Range {
	var raw []buffer.Range
	if res := textobject.Word(m.buf, at, false, false); res.OK && !res.Linewise {
		raw = append(raw, res.Range)
	}
	raw = append(raw,
		buffer.Range{Start: buffer.Position{Line: at.Line}, End: buffer.Position{Line: at.Line, Col: m.buf.RuneLen(at.Line)}},
		buffer.Range{End: m.buf.EndOfBuffer()},
	)
	return raw
}

// applySelRange selects r as a charwise visual selection: anchor on the first
// rune, cursor on the last (r.End is exclusive; sanitizeLadder guarantees
// End.Col > 0, so the inclusive cursor stays on End's line). The bumped-back
// cursor plus motion.Inclusive composition make visualSelection() reproduce r
// exactly — the equality extend/shrink validate against.
func (m *Model) applySelRange(r buffer.Range) {
	m.bumpRender()
	m.mode = Visual
	m.shiftSelect = false
	m.clickVisual = false
	m.anchor = r.Start
	m.cursor = buffer.Position{Line: r.End.Line, Col: r.End.Col - 1}
	m.desiredCol = m.cursor.Col
	m.emit(EventCursorMove)
	m.scroll()
}

// selectionMatches reports whether the active selection is exactly the
// charwise range r — the guard that ties ladder steps to what is really on
// screen: any motion, mode switch or click in between breaks the match and
// the next extend starts fresh.
func (m *Model) selectionMatches(r buffer.Range) bool {
	return m.mode == Visual && m.visualSelection().Range == r
}

// selectionCharRange resolves the current visual selection to the charwise
// text extent it covers: linewise selections become start-of-first-line to
// end-of-last-line, charwise selections their (already end-exclusive) range.
func (m *Model) selectionCharRange() buffer.Range {
	t := m.visualSelection()
	if t.Linewise {
		return buffer.Range{
			Start: buffer.Position{Line: t.Range.Start.Line},
			End:   buffer.Position{Line: t.Range.End.Line, Col: m.buf.RuneLen(t.Range.End.Line)},
		}
	}
	return t.Range
}
