package editor

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor/buffer"
)

// repeatDoc is a poll-service log: three identical lines (only the timestamps
// move), then a distinct one.
const repeatDoc = `2026-08-06T19:06:13.303770698Z time="2026-08-06T19:06:13Z" level=info msg="Session done"
2026-08-06T19:06:43.272659223Z time="2026-08-06T19:06:43Z" level=info msg="Session done"
2026-08-06T19:07:13.290797001Z time="2026-08-06T19:07:13Z" level=info msg="Session done"
2026-08-06T19:07:43.274874796Z time="2026-08-06T19:07:43Z" level=info msg="Session started"
`

// movingDoc is a paging fetch loop: every line moves its timestamp, its page
// number and its elapsed time, and the last one says something else (#1758).
const movingDoc = `2026-08-06T19:06:13Z level=info msg="fetching page 17 of 240" elapsed=1.2s
2026-08-06T19:06:43Z level=info msg="fetching page 18 of 240" elapsed=980ms
2026-08-06T19:07:13Z level=info msg="fetching page 19 of 240" elapsed=1.4s
2026-08-06T19:07:43Z level=info msg="fetch done" status=200
`

// TestLogRepeatRunFoldsMovingParts: a run whose lines differ in timestamp,
// page number and elapsed time collapses like a timestamp-only run does, and
// the line that differs in what it *says* stays out of it.
func TestLogRepeatRunFoldsMovingParts(t *testing.T) {
	m := logLoaded(t, movingDoc)
	m.cursor = buffer.Position{Line: 3}

	for _, l := range []int{1, 2} {
		if !m.lineHidden(l) {
			t.Errorf("line %d must fold into the run", l)
		}
	}
	if m.lineHidden(3) {
		t.Error("a line differing in its message must never fold")
	}
	if end, ok := m.logRunAt(0); !ok || end != 2 {
		t.Errorf("logRunAt(0) = %d, %v; want 2, true", end, ok)
	}
	if got := m.View(); !strings.Contains(got, "×3") {
		t.Errorf("view must mark the run with ×3:\n%s", got)
	}
}

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

// TestLogRepeatMarkerCountsRun: the header row carries a ×N badge counting the
// whole run, and vertical motion steps over the folded lines.
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

// TestLogRepeatMarkerBadgeStyled: the marker draws in theme colours rather
// than `Faint`, so it stands out from ordinary log text in light and dark
// themes alike, and its emphasis scales with the run length (#1734).
func TestLogRepeatMarkerBadgeStyled(t *testing.T) {
	m := logLoaded(t, repeatDoc)
	pal := m.theme()

	for _, tc := range []struct {
		n    int
		fg   color.Color
		bold bool
	}{
		{2, pal.Info, false},
		{logRunMany, pal.Info, true},
		{logRunMany + 5, pal.Info, true},
		{logRunLoud, pal.Warning, true},
		{500, pal.Warning, true},
	} {
		st := m.logRunMarkerStyle(tc.n)
		if got := st.GetForeground(); got != tc.fg {
			t.Errorf("×%d foreground = %v; want %v", tc.n, got, tc.fg)
		}
		if got := st.GetBold(); got != tc.bold {
			t.Errorf("×%d bold = %v; want %v", tc.n, got, tc.bold)
		}
		if st.GetFaint() {
			t.Errorf("×%d must not render faint — the marker is easy to miss that way", tc.n)
		}
	}

	// The rendered row carries the colour, not a bare marker.
	m.cursor = buffer.Position{Line: 3}
	row := strings.Split(m.View(), "\n")[0]
	if !strings.Contains(row, m.logRunMarkerStyle(3).Render(" ×3")) {
		t.Errorf("the header row must carry the styled badge:\n%q", row)
	}
}

// TestLogRepeatMarkerColumn: the badges right-align into the shared annotation
// column, so runs of different lengths form a scannable column instead of a
// ragged trail of line ends (#1734).
func TestLogRepeatMarkerColumn(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 3; i++ { // run of 3
		fmt.Fprintf(&b, "10:11:%02d INFO poll\n", i)
	}
	for i := 0; i < 12; i++ { // run of 12 — a wider badge
		fmt.Fprintf(&b, "10:12:%02d INFO retry\n", i)
	}
	b.WriteString("10:13:00 INFO done\n")

	m := logLoaded(t, b.String())
	m.cursor = buffer.Position{Line: 15}
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	if len(rows) < 2 {
		t.Fatalf("view has %d rows; want the two run headers", len(rows))
	}
	for i, want := range []string{"×3", "×12"} {
		if !strings.Contains(rows[i], want) {
			t.Fatalf("row %d must carry %s: %q", i, want, rows[i])
		}
	}
	// Right-aligned: both badges end in the same column.
	a := len(strings.TrimRight(rows[0], " "))
	c := len(strings.TrimRight(rows[1], " "))
	if a != c {
		t.Errorf("badges end at columns %d and %d; want one shared column:\n%q\n%q", a, c, rows[0], rows[1])
	}
	// And detached from the text, not glued to the line end.
	if i := strings.Index(rows[0], "×3"); i > 0 && rows[0][i-1] != ' ' {
		t.Errorf("the badge must sit in its own column, not against the text: %q", rows[0])
	}
}

// TestLogRepeatMarkerLongLineFallback: a line too long for the annotation
// column keeps its badge appended after the text — the count is structure, not
// an optional hint like a delta (#1734).
func TestLogRepeatMarkerLongLineFallback(t *testing.T) {
	long := strings.Repeat("x", 200)
	m := logLoaded(t, "10:11:10 INFO "+long+"\n10:11:12 INFO "+long+"\n10:11:20 INFO done\n")
	m.cursor = buffer.Position{Line: 2}

	if end, ok := m.logRunAt(0); !ok || end != 1 {
		t.Fatalf("precondition: logRunAt(0) = %d, %v; want 1, true", end, ok)
	}
	if got := m.View(); !strings.Contains(got, "×2") {
		t.Errorf("a full-width run header must still carry its count:\n%s", got)
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
