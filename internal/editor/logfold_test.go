package editor

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
)

// repeatDoc is a poll-service log: three identical lines (only the timestamps
// move), then a distinct one.
const repeatDoc = `2026-08-06T19:06:13.303770698Z time="2026-08-06T19:06:13Z" level=info msg="Session done"
2026-08-06T19:06:43.272659223Z time="2026-08-06T19:06:43Z" level=info msg="Session done"
2026-08-06T19:07:13.290797001Z time="2026-08-06T19:07:13Z" level=info msg="Session done"
2026-08-06T19:07:43.274874796Z time="2026-08-06T19:07:43Z" level=info msg="Session started"
`

// TestLogRepeatRunHidesFollowers: the run folds into its first line — the
// followers hide, the header does not, and the distinct line stays visible.
func TestLogRepeatRunHidesFollowers(t *testing.T) {
	m := logLoaded(t, repeatDoc)
	m.cursor = buffer.Position{Line: 3}

	if m.lineHidden(0) {
		t.Error("the run's first line must stay visible")
	}
	for _, l := range []int{1, 2} {
		if !m.lineHidden(l) {
			t.Errorf("line %d must fold into the run", l)
		}
	}
	if m.lineHidden(3) {
		t.Error("a distinct line must never fold")
	}
	end, ok := m.logRunAt(0)
	if !ok || end != 2 {
		t.Errorf("logRunAt(0) = %d, %v; want 2, true", end, ok)
	}
	if !m.hasFolds() {
		t.Error("a collapsed run must engage the fold-aware paths")
	}
}

// TestLogRepeatMarkerCountsRun: the header row carries a dimmed ×N marker
// counting the whole run, and vertical motion steps over the folded lines.
func TestLogRepeatMarkerCountsRun(t *testing.T) {
	m := logLoaded(t, repeatDoc)
	m.cursor = buffer.Position{Line: 3}

	if got := m.View(); !strings.Contains(got, "×3") {
		t.Errorf("view must mark the run with ×3:\n%s", got)
	}
	m.cursor = buffer.Position{Line: 0}
	if got := m.View(); strings.Contains(got, "×3") {
		t.Errorf("a revealed run must drop its marker:\n%s", got)
	}
}

// TestLogRepeatRevealsUnderCursor: the run reveals positionally like the
// conceal layer — every line renders raw while the cursor is inside it, and
// folds again once the cursor leaves.
func TestLogRepeatRevealsUnderCursor(t *testing.T) {
	m := logLoaded(t, repeatDoc)

	m.cursor = buffer.Position{Line: 0} // on the run's first line
	for _, l := range []int{1, 2} {
		if m.lineHidden(l) {
			t.Errorf("line %d must reveal while the cursor is in the run", l)
		}
	}
	if _, ok := m.logRunAt(0); ok {
		t.Error("a revealed run must not render as a collapsed header")
	}

	m.cursor = buffer.Position{Line: 3} // out again
	if !m.lineHidden(1) {
		t.Error("leaving the run must fold it back")
	}
}

// TestLogRepeatStepsAsOneRow: j from the run's last visible row lands on the
// next distinct line, so the run costs a single row of scrolling.
func TestLogRepeatStepsAsOneRow(t *testing.T) {
	m := logLoaded(t, repeatDoc)
	m.cursor = buffer.Position{Line: 3}

	if got, ok := m.visibleStep(0, 1); !ok || got != 3 {
		t.Errorf("visibleStep(0, +1) = %d, %v; want 3, true", got, ok)
	}
	if got, ok := m.visibleStep(3, -1); !ok || got != 0 {
		t.Errorf("visibleStep(3, -1) = %d, %v; want 0, true", got, ok)
	}
}

// TestLogRepeatToggleOffRendersRaw: raw mode (view.toggleLogRendering) shows
// every repeat.
func TestLogRepeatToggleOffRendersRaw(t *testing.T) {
	m := logLoaded(t, repeatDoc)
	m.cursor = buffer.Position{Line: 3}

	m.toggleLogRendering()
	for l := 0; l < 4; l++ {
		if m.lineHidden(l) {
			t.Errorf("line %d must render raw with log rendering off", l)
		}
	}
	if m.hasLogRuns() {
		t.Error("raw mode must report no runs")
	}
	if got := m.View(); strings.Contains(got, "×3") {
		t.Errorf("raw mode must drop the marker:\n%s", got)
	}
}

// TestLogRepeatBlankLinesNeverFold: consecutive blank lines are whitespace,
// not a repeated statement.
func TestLogRepeatBlankLinesNeverFold(t *testing.T) {
	m := logLoaded(t, "boom\n\n\n\nboom\n")
	m.cursor = buffer.Position{Line: 0}
	for l := 1; l < 4; l++ {
		if m.lineHidden(l) {
			t.Errorf("blank line %d must not fold", l)
		}
	}
}

// TestLogRepeatNonLogBufferUnaffected: the collapse is a log-rendering
// feature; an ordinary text buffer with duplicate lines renders every one.
func TestLogRepeatNonLogBufferUnaffected(t *testing.T) {
	m := New()
	m.buf = buffer.FromString("dup\ndup\ndup")
	m.path = "notes.txt"
	m.SetSize(60, 10)
	m.cursor = buffer.Position{Line: 2}
	for l := 0; l < 3; l++ {
		if m.lineHidden(l) {
			t.Errorf("line %d of a text buffer must not fold", l)
		}
	}
}

// TestLogRepeatRunsFollowEdits: the run map is keyed by document version, so
// breaking a repeat by editing it un-folds the line.
func TestLogRepeatRunsFollowEdits(t *testing.T) {
	m := logLoaded(t, repeatDoc)
	m.cursor = buffer.Position{Line: 3}
	if !m.lineHidden(1) {
		t.Fatal("precondition: line 1 folds")
	}

	lines := m.buf.Lines()
	lines[1] = `2026-08-06T19:06:43.272659223Z time="x" level=warn msg="Session stalled"`
	m.buf.ReplaceAll(strings.Join(lines, "\n"))
	m.docVersion++
	if m.lineHidden(1) {
		t.Error("an edited repeat must stop folding")
	}
}
