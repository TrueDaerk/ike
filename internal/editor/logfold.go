package editor

// logfold.go collapses runs of consecutive identical log lines (#1650). A
// polling service repeats the same line for pages — same message, only the
// timestamp moves — so in log rendering mode a run folds into its first line
// plus a `×N` badge and the rest of the run stops occupying rows.
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
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

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
	// Follow-mode append hint (#1928): appendFrom is the first line whose
	// content is new (or changed, for a continued tail line) since the last
	// recompute — 0 means no incremental extension is possible — and
	// appendVersion the document version those appends produced. When the
	// version moved exactly to appendVersion, logRuns extends the cached runs
	// from appendFrom instead of rescanning the whole buffer.
	appendFrom    int
	appendVersion int
}

// logInsight reports whether the whole-buffer log analyses apply right now:
// a log buffer with log rendering on, and insight not disabled by the
// large-file guard (#149) — scanning a multi-megabyte log per version is
// exactly the work that guard exists to avoid. Shared with the inter-line
// deltas of #1651 (logdelta.go).
func (m Model) logInsight() bool {
	return m.logRenderOn() && m.logBuffer() && !m.largeFile
}

// logFoldActive reports whether repeat collapsing applies to this buffer.
func (m Model) logFoldActive() bool { return m.logInsight() }

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
	if st.valid && st.path == m.path && st.appendFrom > 0 &&
		st.appendVersion == m.docVersion {
		// Only follow-mode appends happened since the cached scan (#1928):
		// extend the runs over the new tail instead of rescanning everything.
		m.extendLogRuns(st)
		return st
	}
	st.valid, st.version, st.path = true, m.docVersion, m.path
	st.head, st.end = nil, nil
	st.appendFrom, st.appendVersion = 0, 0
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

// noteLogAppend registers a follow-mode append (#1928) with the run cache so
// logRuns can extend the cached runs incrementally: prevCount is the line
// count before the append, merged whether the previous last line's content
// changed (an unterminated line was continued, so it must be rescanned too).
// Called after the append's EventChange, so m.docVersion is the appended
// version.
func (m *Model) noteLogAppend(prevCount int, merged bool) {
	st := m.logRunCache
	if st == nil || !st.valid || st.path != m.path {
		return
	}
	from := prevCount
	if merged {
		from--
	}
	if st.appendFrom == 0 || from < st.appendFrom {
		st.appendFrom = from
	}
	st.appendVersion = m.docVersion
}

// extendLogRuns continues the cached repeat runs over the lines follow mode
// appended (#1928): only the tail from st.appendFrom on is rescanned, seeded
// with the run state of the line above it, so an append costs the new lines —
// not the whole buffer.
func (m Model) extendLogRuns(st *logRunState) {
	start := st.appendFrom
	lc := m.buf.LineCount()
	for i := start; i < lc; i++ {
		delete(st.head, i)
	}
	// A run reaching into the rescanned tail keeps only its part above it;
	// the loop below re-extends it line by line if the tail still matches.
	for h, e := range st.end {
		if e < start {
			continue
		}
		if start-1 > h {
			st.end[h] = start - 1
		} else {
			delete(st.end, h)
		}
	}
	prevKey, prevOK, header := "", false, -1
	if start > 0 {
		prev := m.buf.Line(start - 1)
		if !blankLine(prev) {
			prevKey, prevOK = logline.RepeatKey(prev), true
			if h, ok := st.head[start-1]; ok {
				header = h
			}
		}
	}
	for i := start; i < lc; i++ {
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
	st.version = m.docVersion
	st.appendFrom, st.appendVersion = 0, 0
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

// Thresholds at which the `×N` badge gains weight (#1734). A ×2 is a detail;
// a ×500 means the buffer is almost entirely this one line, and the marker
// should say so at a glance.
const (
	logRunMany = 10  // bold from here on
	logRunLoud = 100 // and the warning colour from here on
)

// logRunMarkerStyle is the badge style for a run of n lines: theme colours
// rather than `Faint`, so the marker reads as a marker against ordinary log
// text in light and dark themes alike, with emphasis scaling with n.
func (m Model) logRunMarkerStyle(n int) lipgloss.Style {
	switch {
	case n >= logRunLoud:
		return lipgloss.NewStyle().Foreground(m.theme().Warning).Bold(true)
	case n >= logRunMany:
		return lipgloss.NewStyle().Foreground(m.theme().Info).Bold(true)
	}
	return lipgloss.NewStyle().Foreground(m.theme().Info)
}

// renderLogRunHeader renders the first line of a collapsed repeat run with its
// `×N` badge, N counting the whole run including this line.
//
// The badge right-aligns into the shared annotation column (annotWidth, the
// column the delta hints and inline blame use), so the markers of a file form
// one scannable column instead of a ragged trail of line ends. There is no
// conflict with the one-annotation-per-row rule: a collapsed header carries no
// delta hint, and blame only annotates the cursor line, which never renders as
// a collapsed header (the cursor inside a run reveals it).
//
// A line too long for the column falls back to appending the badge right after
// the text, budgeted like renderFoldHeader: the count is buffer *structure*,
// not an optional hint, so unlike a delta it is never dropped.
func (m Model) renderLogRunHeader(line, end, width, annotWidth int, cursorStyle, selStyle lipgloss.Style) string {
	n := end - line + 1
	badge := " ×" + strconv.Itoa(n)
	style := m.logRunMarkerStyle(n)
	badgeW := ansi.StringWidth(badge)

	row, _ := m.renderLine(line, width, cursorStyle, selStyle)
	content := strings.TrimRight(ansi.Strip(row), " ")
	// Two spaces of air between the log line and the badge, as for the hints.
	if ansi.StringWidth(content)+badgeW+2 <= annotWidth {
		body := ansi.Truncate(row, annotWidth-badgeW, "")
		if pad := annotWidth - badgeW - ansi.StringWidth(body); pad > 0 {
			body += strings.Repeat(" ", pad)
		}
		return body + style.Render(badge)
	}
	if badgeW >= width {
		return row
	}
	short, _ := m.renderLine(line, width-badgeW, cursorStyle, selStyle)
	return short + style.Render(badge)
}
