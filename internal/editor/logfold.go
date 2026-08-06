package editor

// logfold.go collapses runs of consecutive identical log lines (#1650). A
// polling service repeats the same line for pages — same message, only the
// timestamp moves — so in log rendering mode a run folds into its first line
// plus a dimmed `×N` marker and the rest of the run stops occupying rows.
//
// "Identical" is logline.RepeatKey equality: the timestamps the #1621 parser
// recognizes are blanked before comparing, so `time=` differences still count
// as a repeat. Blank lines never fold — a run of them is whitespace, not a
// repeated statement.
//
// The collapse rides the fold machinery (fold.go): a hidden line is hidden for
// motions, scrolling, mouse mapping and the render loop alike, so no caller
// has to know a run from a closed fold. Unlike a fold, though, the run has no
// open/close command — it reveals positionally, like the conceal layer
// (#1594): while the cursor sits anywhere inside the run, every line of it
// renders raw, and the marker disappears. Moving out collapses it again.
//
// Everything is gated by the log-rendering toggle, so raw mode
// (view.toggleLogRendering) shows the untouched file.

import (
	"strconv"

	"charm.land/lipgloss/v2"

	"ike/internal/logline"
)

// logRunState caches the repeat runs of one document version. A pointer field
// on Model, like svTable, so the value copies each Update returns share it.
type logRunState struct {
	valid   bool
	version int
	path    string
	// head maps every line of a run *after* its first onto that run's header
	// line; end maps a header line onto the run's last line.
	head map[int]int
	end  map[int]int
}

// logFoldActive reports whether repeat collapsing applies to this buffer right
// now: a log buffer with log rendering on, and code insight not disabled by
// the large-file guard (#149) — scanning a multi-megabyte log per version is
// exactly the work that guard exists to avoid.
func (m Model) logFoldActive() bool {
	return m.logRender && m.logBuffer() && !m.largeFile
}

// logRuns returns the repeat runs of the current document version, recomputing
// them when the version moved. Runs are a whole-buffer property (a run may
// start above the viewport), so unlike the sv layout this scan is not
// viewport-scoped; the large-file guard bounds its cost.
func (m Model) logRuns() *logRunState {
	st := m.logRunCache
	if st == nil {
		return &logRunState{}
	}
	if st.valid && st.version == m.docVersion && st.path == m.path {
		return st
	}
	st.valid, st.version, st.path = true, m.docVersion, m.path
	st.head, st.end = nil, nil
	lc := m.buf.LineCount()
	prevKey := ""
	prevOK := false
	header := -1
	for i := 0; i < lc; i++ {
		text := m.buf.Line(i)
		if blankLine(text) {
			prevOK, header = false, -1
			continue
		}
		key := logline.RepeatKey(text)
		if prevOK && key == prevKey {
			if header < 0 {
				header = i - 1
			}
			if st.head == nil {
				st.head, st.end = make(map[int]int), make(map[int]int)
			}
			st.head[i] = header
			st.end[header] = i
		} else {
			header = -1
		}
		prevKey, prevOK = key, true
	}
	return st
}

// blankLine reports whether a line carries nothing but whitespace.
func blankLine(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\r' {
			return false
		}
	}
	return true
}

// logRunHidden reports whether line is folded away inside a repeat run: it
// belongs to one, and the cursor is not inside that run (the positional
// reveal).
func (m Model) logRunHidden(line int) bool {
	if !m.logFoldActive() {
		return false
	}
	st := m.logRuns()
	h, ok := st.head[line]
	if !ok {
		return false
	}
	return !m.inLogRun(h, st.end[h])
}

// inLogRun reports whether the cursor sits inside the run [header, end].
func (m Model) inLogRun(header, end int) bool {
	return m.cursor.Line >= header && m.cursor.Line <= end
}

// logRunAt returns the last line of the collapsed repeat run whose header is
// line. It reports false when line starts no run, or when the cursor revealed
// it — a revealed run renders as ordinary lines.
func (m Model) logRunAt(line int) (int, bool) {
	if !m.logFoldActive() {
		return 0, false
	}
	end, ok := m.logRuns().end[line]
	if !ok || m.inLogRun(line, end) {
		return 0, false
	}
	return end, true
}

// hasLogRuns reports whether this view has any repeat run at all, the gate
// hasFolds folds into the fold-aware render/motion/scroll paths.
func (m Model) hasLogRuns() bool {
	return m.logFoldActive() && len(m.logRuns().end) > 0
}

// renderLogRunHeader renders the first line of a collapsed repeat run with a
// dimmed `×N` marker appended, N counting the whole run including this line —
// the same budgeting as renderFoldHeader, so the row stays within the text
// width.
func (m Model) renderLogRunHeader(line, end, width int, cursorStyle, selStyle lipgloss.Style) string {
	tag := " ×" + strconv.Itoa(end-line+1)
	tw := lipgloss.Width(tag)
	if tw >= width {
		return m.renderLine(line, width, cursorStyle, selStyle)
	}
	body := m.renderLine(line, width-tw, cursorStyle, selStyle)
	return body + lipgloss.NewStyle().Faint(true).Render(tag)
}
