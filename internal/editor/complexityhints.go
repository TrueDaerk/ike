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
	"strings"

	"charm.land/lipgloss/v2"
)

// ComplexityHint is one function header's annotation: Line is the 0-based
// buffer line, Text the rendered metrics, Warn whether a metric is at or
// above its warning threshold. Deltas (#2949) are the live change against
// the Changes tab's base revision, each rendered right after its metric in
// its own tone; a hint without them has zero values.
type ComplexityHint struct {
	Line   int
	Text   string
	Warn   bool
	Deltas [2]ComplexityDelta
}

// ComplexityDelta is one metric's change against the base (#2949), `(−6)`:
// it renders after the first At bytes of the hint's Text, in the success
// tone when the metric fell and the warning tone (DeltaWarn) when it rose.
// An empty Delta is none.
type ComplexityDelta struct {
	At        int
	Delta     string
	DeltaWarn bool
}

// Plain is the hint as it reads: Text with every delta spliced in after its
// metric — `CCN 14 (−6) · cog 24 (−7)`.
func (h ComplexityHint) Plain() string {
	var b strings.Builder
	for _, p := range h.pieces() {
		b.WriteString(p.text)
	}
	return b.String()
}

// cxHintPiece is a run of a hint's rendering: metric text (delta false) or
// one delta with its tone.
type cxHintPiece struct {
	text  string
	delta bool
	warn  bool
}

// pieces splits the hint into its rendering runs, deltas in At order.
func (h ComplexityHint) pieces() []cxHintPiece {
	var out []cxHintPiece
	at := 0
	for _, d := range h.Deltas {
		if d.Delta == "" || d.At < at || d.At > len(h.Text) {
			continue
		}
		out = append(out, cxHintPiece{text: h.Text[at:d.At]}, cxHintPiece{text: " " + d.Delta, delta: true, warn: d.DeltaWarn})
		at = d.At
	}
	return append(out, cxHintPiece{text: h.Text[at:]})
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
	return m.complexityHintText(h, room)
}

// complexityHintText styles a header hint (#2949): the metrics in the
// hint's tone like complexityTrailerText, each delta in its own — success
// for a fall, warning for a rise — or "" when it would not fit whole.
func (m Model) complexityHintText(h ComplexityHint, room int) (string, int) {
	pieces := h.pieces()
	if len(pieces) == 1 {
		return m.complexityTrailerText(h.Text, h.Warn, room)
	}
	w := lipgloss.Width(complexityHintGap + h.Plain())
	if w > room {
		return "", 0
	}
	base := m.complexityTrailerStyle(h.Warn)
	var b strings.Builder
	b.WriteString(base.Render(complexityHintGap))
	for _, p := range pieces {
		st := base
		if p.delta {
			st = lipgloss.NewStyle().Italic(true).Foreground(m.theme().Success)
			if p.warn {
				st = st.Foreground(m.theme().Warning)
			}
		}
		if p.text != "" {
			b.WriteString(st.Render(p.text))
		}
	}
	return b.String(), w
}

// complexityTrailerText styles one trailer — a header hint or a site mark
// (#2947): the gap and the text, dimmed or in the warning tone, or "" when
// it would not fit whole in room cells.
func (m Model) complexityTrailerText(body string, warn bool, room int) (string, int) {
	text := complexityHintGap + body
	w := lipgloss.Width(text)
	if w > room {
		return "", 0
	}
	return m.complexityTrailerStyle(warn).Render(text), w
}

// complexityTrailerStyle is a trailer's tone: the warning colour, or dimmed
// like an inlay hint.
func (m Model) complexityTrailerStyle(warn bool) lipgloss.Style {
	st := lipgloss.NewStyle().Italic(true)
	if warn {
		return st.Foreground(m.theme().Warning)
	}
	return st.Foreground(m.theme().InlayHint).Faint(true)
}
