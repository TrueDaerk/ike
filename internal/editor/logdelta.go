package editor

// logdelta.go renders the inter-line elapsed times of a log buffer (#1651): a
// dimmed `+30s` at the right edge of every line whose timestamp moved on from
// the previous one, so a stall shows up as a column entry instead of arithmetic
// done by eye. Deltas at or above the file's stall threshold — ten times its
// median cadence, see logline.GapThreshold — render in the warning colour, so
// hangs stand out from the ordinary ticking.
//
// The hint lives in the row's right padding, spliced in exactly like the inline
// blame annotation (vcs_blame.go): it only appears when the line text leaves
// room for it plus two columns of air, so no buffer content is ever hidden
// behind it. Blame wins on the cursor line — one annotation per row.
//
// The chain itself is logline.Deltas: whole-buffer, cached per document version
// and path like the repeat runs of #1650, and gated by the same
// log-rendering/large-file conditions, so raw mode shows the untouched file.

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/logline"
)

// logDeltaState caches the per-line deltas of one document version. A pointer
// field on Model, like logRunCache, so the value copies each Update returns
// share it.
type logDeltaState struct {
	valid   bool
	version int
	path    string
	deltas  []logline.Delta
	layout  logline.DeltaLayout
}

// logDeltas returns the deltas of the current document version together with
// the buffer-wide hint layout, recomputing both when the version moved.
func (m Model) logDeltas() ([]logline.Delta, logline.DeltaLayout) {
	st := m.logDeltaCache
	if st == nil {
		return nil, logline.DeltaLayout{}
	}
	if st.valid && st.version == m.docVersion && st.path == m.path {
		return st.deltas, st.layout
	}
	st.valid, st.version, st.path = true, m.docVersion, m.path
	st.deltas = logline.Deltas(m.buf.Lines())
	st.layout = logline.LayoutDeltas(st.deltas)
	return st.deltas, st.layout
}

// logDeltaAt returns the hint text for a line and whether it marks a stall.
func (m Model) logDeltaAt(line int) (string, bool, bool) {
	if !m.logInsight() {
		return "", false, false
	}
	ds, layout := m.logDeltas()
	if line < 0 || line >= len(ds) || !ds[line].OK {
		return "", false, false
	}
	text := layout.Format(ds[line].D)
	if text == "" {
		return "", false, false
	}
	return text, ds[line].Gap, true
}

// LogSpanLabel is the label for the elapsed time a visual selection covers in
// a log buffer (#1729) — "Δ +2m30s". It feeds both the status-line segment and
// the in-editor annotation on the cursor row (logSpanAnnotate, #1736), so the
// two can never disagree. The per-line hints
// only answer "how long since the previous line"; measuring a whole section —
// request to response, the first to the last line of a stall — otherwise means
// reading two timestamps and subtracting by eye.
//
// Only the outermost timestamped lines of the selection count; unstamped ones
// in between are irrelevant. Empty (which hides the segment) outside visual
// mode, outside a log buffer, with fewer than two comparable stamps in the
// selection, and for a non-positive span — a second-resolution log where both
// ends land in the same second has nothing to report.
func (m Model) LogSpanLabel() string {
	if !m.logInsight() || !m.mode.IsVisual() {
		return ""
	}
	start, end := m.anchor.Line, m.cursor.Line
	if start > end {
		start, end = end, start
	}
	if start < 0 {
		start = 0
	}
	if n := m.buf.LineCount(); end >= n {
		end = n - 1
	}
	if end <= start {
		return ""
	}
	d, ok := logline.SpanDelta(m.buf.Lines()[start : end+1])
	if !ok {
		return ""
	}
	text := logline.FormatDelta(d)
	if text == "" {
		return ""
	}
	return "Δ " + text
}

// logSpanAnnotate places the selection's span delta at the right edge of the
// cursor row (#1736). The status-line segment alone is too easy to miss — the
// eye is on the selection in the buffer, not on the bottom bar, and the segment
// can be dropped by the status line's width budgeting — so the value renders
// where it is being read.
//
// Only the cursor row carries it: that is the end the user is moving, so the
// number sits beside the boundary that changes. It wins over the per-line hint
// and inline blame on that row (one annotation per row, view.go), and unlike
// them it is worth truncating the line for — a row whose text fills the width
// still gives up its last columns, because a selection is a deliberate question
// and the answer must not silently vanish.
//
// The second return value reports whether the annotation was placed, so the
// caller knows to skip the lower-priority ones.
func (m Model) logSpanAnnotate(row string, line, textWidth int) (string, bool) {
	if line != m.cursor.Line {
		return row, false
	}
	label := m.LogSpanLabel()
	if label == "" {
		return row, false
	}
	ann := " " + label
	annW := ansi.StringWidth(ann)
	// Without room for the annotation plus a column of text there is nothing
	// sensible to render.
	if annW+1 > textWidth {
		return row, false
	}
	style := lipgloss.NewStyle().Foreground(m.theme().Accent).Bold(true)
	body := ansi.Truncate(row, textWidth-annW, "")
	if pad := textWidth - annW - ansi.StringWidth(body); pad > 0 {
		body += strings.Repeat(" ", pad)
	}
	return body + style.Render(ann), true
}

// logDeltaAnnotate places a line's delta hint at the row's right edge. Rows
// without a delta, or without room for one, render unchanged.
//
// The hint is right-aligned rather than appended to the text: a ragged trail of
// numbers is exactly as hard to scan as the timestamps it saves reading, while
// a column of them can be swept by eye. Short rows therefore pad out to the
// hint's column first. The text itself comes from the buffer-wide
// logline.DeltaLayout (#1730), so every hint is the same width and its unit
// fields line up with the rows above and below.
func (m Model) logDeltaAnnotate(row string, line, textWidth int) string {
	text, gap, ok := m.logDeltaAt(line)
	if !ok {
		return row
	}
	hint := " " + text
	hintW := ansi.StringWidth(hint)
	content := strings.TrimRight(ansi.Strip(row), " ")
	// Two spaces of air between the log line and the hint.
	if ansi.StringWidth(content)+hintW+2 > textWidth {
		return row
	}
	style := lipgloss.NewStyle().Faint(true)
	if gap {
		style = lipgloss.NewStyle().Foreground(m.theme().Warning).Bold(true)
	}
	body := ansi.Truncate(row, textWidth-hintW, "")
	if pad := textWidth - hintW - ansi.StringWidth(body); pad > 0 {
		body += strings.Repeat(" ", pad)
	}
	return body + style.Render(hint)
}
