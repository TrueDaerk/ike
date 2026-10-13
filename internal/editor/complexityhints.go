package editor

// complexityhints.go is the editor side of inline complexity hints (0550,
// #2919): every function header carries a trailing virtual-text annotation
// such as `CCN 12 · cog 8`, dimmed like a code lens, in the warning tone when
// a metric reaches its threshold. The app owns the complexity index and the
// per-buffer measurement; it pushes ComplexityHintsMsg stamped with the
// document version the hints were measured at, and the editor only stores
// and renders. Stale pushes (an edit landed meanwhile) are dropped at the
// door — the app's debounce re-measures the newer version anyway. Rendering
// rides the end-of-line flush after the inlay hints and code lenses, and
// unlike those the hint never truncates: a line that leaves no room shows
// none, so buffer text is never overdrawn. The editor.marks.complexity
// toggle gates visibility like every other mark class (#1259); cached hints
// survive a toggle round-trip.

import (
	"charm.land/lipgloss/v2"
)

// ComplexityHint is one function header's annotation: Line is the 0-based
// buffer line, Text the rendered metrics, Warn whether a metric is at or
// above its warning threshold.
type ComplexityHint struct {
	Line int
	Text string
	Warn bool
}

// ComplexityHintsMsg replaces a document's complexity hints. Version is the
// DocVersion the hints were measured at; an editor at another version drops
// the message. Nil Hints clear.
type ComplexityHintsMsg struct {
	Path    string
	Hints   []ComplexityHint
	Version int
}

// complexityHintGap is the air between the last buffer cell (or code lens)
// and the hint.
const complexityHintGap = "  "

// SetComplexityHints replaces the hint set when docVersion is the current
// document version and reports whether it was adopted. Nil clears
// regardless of the version — a clear is never stale.
func (m *Model) SetComplexityHints(hints []ComplexityHint, docVersion int) bool {
	if len(hints) == 0 {
		if m.cxHints != nil {
			m.cxHints = nil
			m.bumpRender()
		}
		return true
	}
	if docVersion != m.docVersion {
		return false
	}
	byLine := make(map[int]ComplexityHint, len(hints))
	for _, h := range hints {
		if h.Text != "" {
			byLine[h.Line] = h
		}
	}
	m.cxHints = byLine
	m.bumpRender()
	return true
}

// HasComplexityHints reports whether the buffer holds any hints (app
// symmetry: a clear is only worth sending while there is something to
// clear).
func (m Model) HasComplexityHints() bool { return len(m.cxHints) > 0 }

// ComplexityHintAt returns the hint on a 0-based line, if the line carries
// one and the toggle is on (tests and app symmetry).
func (m Model) ComplexityHintAt(line int) (ComplexityHint, bool) {
	if !m.complexityHintsVisible() {
		return ComplexityHint{}, false
	}
	h, ok := m.cxHints[line]
	return h, ok
}

// complexityHintsVisible reports whether hints render; unset means on,
// matching the config default.
func (m Model) complexityHintsVisible() bool {
	if m.cfg == nil {
		return true
	}
	v, ok := m.cfg.Get("editor.marks.complexity")
	return !ok || (v != "off" && v != "false")
}

// complexityHintTrailer renders a line's hint for the end-of-line flush:
// the styled text and its display width, or "" when the line has none or
// it would not fit whole in the room cells left on the row. The hint sits
// after any code lens, so room is what the lens left; on the caret line the
// caret cell is already spent, so a header at full width shows no hint
// rather than one under the typed text.
func (m Model) complexityHintTrailer(line, room int) (string, int) {
	h, ok := m.ComplexityHintAt(line)
	if !ok {
		return "", 0
	}
	text := complexityHintGap + h.Text
	w := lipgloss.Width(text)
	if w > room {
		return "", 0
	}
	st := lipgloss.NewStyle().Italic(true)
	if h.Warn {
		st = st.Foreground(m.theme().Warning)
	} else {
		st = st.Foreground(m.theme().InlayHint).Faint(true)
	}
	return st.Render(text), w
}
