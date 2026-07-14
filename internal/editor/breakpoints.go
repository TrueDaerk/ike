package editor

// breakpoints.go is the editor's view of the app-owned breakpoint store
// (0350, #577): the gutter renders breakpoint lines through an injected
// source, and gutter clicks map back to buffer lines for toggling. The
// editor itself stores nothing — folds, wrap and shared documents all stay
// correct because the source is queried per frame.

// SetBreakpointSource injects the live breakpoint lookup (0-based lines for
// an absolute file path). Nil disables the feature.
func (m *Model) SetBreakpointSource(src func(path string) []int) { m.bpSource = src }

// SetBreakpointAdjuster injects the callback fired after a buffer mutation
// changed the line count, so the store can shift breakpoints like the editor
// shifts folds (dissolveFoldsAtEdit): cursorAfter is the 0-based edit site,
// delta the line-count change.
func (m *Model) SetBreakpointAdjuster(adjust func(path string, cursorAfter, delta int)) {
	m.bpAdjust = adjust
	m.bpLines = m.buf.LineCount()
}

// notifyBreakpointEdit runs on every EventChange: it tracks the buffer's line
// count and reports the delta to the adjuster. Line-count seams that replace
// the buffer wholesale (Load, ShareDocumentWith, remote sync) reset the
// counter instead of reporting (seedBreakpointLines).
func (m *Model) notifyBreakpointEdit() {
	lc := m.buf.LineCount()
	delta := lc - m.bpLines
	m.bpLines = lc
	if delta == 0 || m.bpAdjust == nil || !m.HasFile() {
		return
	}
	m.bpAdjust(m.path, m.cursor.Line, delta)
}

// seedBreakpointLines re-baselines the line counter after a wholesale buffer
// replacement, so the swap never reads as an edit.
func (m *Model) seedBreakpointLines() { m.bpLines = m.buf.LineCount() }

// SetPausedLine marks the debugger's current line (0350, #579): the gutter
// renders it in the warning tone, winning over every other marker.
func (m *Model) SetPausedLine(line int) { m.paused, m.pausedLine = true, line }

// ClearPausedLine removes the paused marker (resume, step, session end).
func (m *Model) ClearPausedLine() { m.paused = false }

// PausedLine reports the marker, ok=false when none is set.
func (m *Model) PausedLine() (int, bool) { return m.pausedLine, m.paused }

// breakpointSet snapshots the current breakpoint lines as a set, empty when
// no source is wired or the buffer has no file.
func (m Model) breakpointSet() map[int]bool {
	if m.bpSource == nil || !m.HasFile() {
		return nil
	}
	lines := m.bpSource(m.Path())
	if len(lines) == 0 {
		return nil
	}
	set := make(map[int]bool, len(lines))
	for _, l := range lines {
		set[l] = true
	}
	return set
}

// LineCount reports the buffer's line count (breakpoint edit-adjustment and
// tests read it).
func (m Model) LineCount() int { return m.buf.LineCount() }

// GutterHit maps a content-local click to the 0-based buffer line when x
// lands inside the gutter column, honouring folds, wrap and sticky headers
// exactly like a body click.
func (m *Model) GutterHit(x, y int) (int, bool) {
	if x < 0 || x >= m.view.GutterWidth(m.buf.LineCount()) {
		return 0, false
	}
	p := m.clickPosition(x, y)
	return p.Line, true
}
