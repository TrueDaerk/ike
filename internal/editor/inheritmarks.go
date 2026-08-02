package editor

import (
	ilsp "ike/internal/lsp"
)

// inheritmarks.go holds the gutter inheritance-arrow state (#1453): the LSP
// bridge computes per-line ↑/↓ marks from documentSymbol + implementation
// probes and pushes them per document; the editor only stores and renders
// them. Visibility is gated by the editor.marks.inheritance toggle like every
// other mark class (#1259).

// setInheritanceMarks replaces the inheritance-mark set; an empty slice
// clears it.
func (m *Model) setInheritanceMarks(marks []ilsp.InheritanceMark) {
	if len(marks) == 0 {
		m.inheritMarks = nil
		return
	}
	byLine := make(map[int]int, len(marks))
	for _, mk := range marks {
		byLine[mk.Line] = mk.Kind
	}
	m.inheritMarks = byLine
}

// inheritMarkAt reports the inheritance arrow on a 0-based line, nothing while
// the visibility toggle is off (cached marks survive a toggle round-trip, they
// just stop rendering, like inlay hints).
func (m Model) inheritMarkAt(line int) (int, bool) {
	if len(m.inheritMarks) == 0 || !m.inheritVisible() {
		return 0, false
	}
	kind, ok := m.inheritMarks[line]
	return kind, ok
}

// inheritVisible reports whether inheritance marks render; unset means on,
// matching the config default.
func (m Model) inheritVisible() bool {
	if m.cfg == nil {
		return true
	}
	return boolOr(m.cfg, "editor.marks.inheritance", true)
}

// inheritSign is the gutter glyph for a mark kind.
func inheritSign(kind int) string {
	if kind == ilsp.InheritanceImplemented {
		return "↓"
	}
	return "↑"
}
