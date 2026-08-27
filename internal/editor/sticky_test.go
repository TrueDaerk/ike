package editor

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
)

// stickyModel builds an editor over 40 numbered lines with an outer scope at
// 0-30 holding an inner scope at 5-20, sized to 10 visible rows.
func stickyModel(t *testing.T) Model {
	t.Helper()
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "line" + itoa(i)
	}
	m := New()
	m.buf = buffer.FromString(strings.Join(lines, "\n"))
	m.path = "main.go"
	m.SetSize(40, 10)
	m = feedSpans(t, m, highlight.SpansMsg{
		Path: "main.go",
		Scopes: []highlight.Scope{
			{HeaderLine: 0, EndLine: 30},
			{HeaderLine: 5, EndLine: 20},
		},
	})
	return m
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestStickyLinesAtTopOfBuffer(t *testing.T) {
	m := stickyModel(t)
	if got := m.stickyLines(); got != nil {
		t.Errorf("no sticky rows expected at Top=0, got %v", got)
	}
}

func TestStickyLinesInsideNestedScopes(t *testing.T) {
	m := stickyModel(t)
	m.view.Top = 10
	got := m.stickyLines()
	if len(got) != 2 || got[0] != 0 || got[1] != 5 {
		t.Fatalf("stickyLines = %v, want [0 5]", got)
	}
}

func TestStickyLinesFixedPoint(t *testing.T) {
	// Top=5: line 5 is the inner header itself, only the outer scope encloses
	// it — but pinning that one row makes line 6 the first content line, which
	// IS inside the inner scope, so both headers pin.
	m := stickyModel(t)
	m.view.Top = 5
	got := m.stickyLines()
	if len(got) != 2 || got[0] != 0 || got[1] != 5 {
		t.Fatalf("stickyLines = %v, want [0 5]", got)
	}
}

func TestStickyLinesDepthCapKeepsInnermost(t *testing.T) {
	m := stickyModel(t)
	m.view.Top = 10
	m.stickyDepth = 1
	got := m.stickyLines()
	if len(got) != 1 || got[0] != 5 {
		t.Errorf("stickyLines with depth 1 = %v, want [5] (innermost)", got)
	}
}

func TestStickyLinesDisabled(t *testing.T) {
	m := stickyModel(t)
	m.view.Top = 10
	m.stickyScroll = false
	if got := m.stickyLines(); got != nil {
		t.Errorf("sticky disabled should yield nothing, got %v", got)
	}
}

func TestStickyDisabledInLargeFile(t *testing.T) {
	// Large-file mode (#1910): even with scope data present (e.g. stale after
	// a same-path reload crossed the size limit), nothing pins.
	m := stickyModel(t)
	m.view.Top = 10
	m.largeFile = true
	if got := m.stickyLines(); got != nil {
		t.Errorf("large-file mode should yield no sticky rows, got %v", got)
	}
}

func TestStickySeparatorOnLastPinnedRow(t *testing.T) {
	// The last pinned row fills its right padding with a faint dashed rule
	// (#1910); the rows above it stay plain.
	m := stickyModel(t)
	m.view.Top = 10
	rows := strings.Split(m.View(), "\n")
	if !strings.Contains(rows[1], "╌") {
		t.Errorf("last sticky row should carry the separator rule, got %q", rows[1])
	}
	if strings.Contains(rows[0], "╌") || strings.Contains(rows[2], "╌") {
		t.Errorf("only the last sticky row carries the rule, got %q / %q", rows[0], rows[2])
	}
}

func TestStickyViewPinsHeaders(t *testing.T) {
	m := stickyModel(t)
	m.view.Top = 10
	m.cursor = buffer.Position{Line: 15}
	rows := strings.Split(m.View(), "\n")
	if len(rows) != 10 {
		t.Fatalf("expected 10 rows, got %d", len(rows))
	}
	if !strings.Contains(rows[0], "line0") || !strings.Contains(rows[1], "line5") {
		t.Errorf("top rows should pin headers line0/line5, got %q / %q", rows[0], rows[1])
	}
	// The rows the headers cover are skipped: content resumes at Top+2.
	if !strings.Contains(rows[2], "line12") {
		t.Errorf("first content row should be line12, got %q", rows[2])
	}
	// Total window still ends at Top+height-1.
	if !strings.Contains(rows[9], "line19") {
		t.Errorf("last row should be line19, got %q", rows[9])
	}
}

func TestStickyMouseClickJumpsToHeader(t *testing.T) {
	m := stickyModel(t)
	m.view.Top = 10
	m.cursor = buffer.Position{Line: 15}
	m.MouseClick(0, 1) // second sticky row = inner header at line 5
	if m.cursor.Line != 5 {
		t.Errorf("click on sticky row should jump to line 5, got %d", m.cursor.Line)
	}
}

func TestStickyScrollKeepsCursorVisible(t *testing.T) {
	m := stickyModel(t)
	m.view.ScrollOff = 0
	m.view.Top = 10
	// Cursor on the first buffer line of the viewport — covered by two pinned
	// headers; scroll must move Top up until the cursor row is uncovered.
	m.cursor = buffer.Position{Line: 10}
	m.scroll()
	n := m.stickyCount()
	if m.cursor.Line < m.view.Top+n {
		t.Errorf("cursor line %d still hidden behind %d sticky rows at Top %d", m.cursor.Line, n, m.view.Top)
	}
}

// TestStickyLinesMemoized (#2187): View, the scroll paths and the mouse map
// all ask per frame, and each ask ran the fixed-point loop over every scope.
// An unchanged viewport serves the memo; a scroll and a fresh parse recompute.
func TestStickyLinesMemoized(t *testing.T) {
	m := stickyModel(t)
	m.view.Top = 10
	first := m.stickyLines()
	if len(first) != 2 {
		t.Fatalf("setup: stickyLines = %v, want two headers", first)
	}
	second := m.stickyLines()
	if &second[0] != &first[0] {
		t.Fatal("an unchanged viewport must serve the memoized header set")
	}
	m.view.Top = 25 // out of the inner scope, still inside the outer one
	scrolled := m.stickyLines()
	if len(scrolled) != 1 || scrolled[0] != 0 {
		t.Fatalf("after scrolling past the inner scope: %v, want [0]", scrolled)
	}
	// A parse landing replaces the scopes without bumping the document
	// version, so the memo has to key on the scope epoch too.
	m = feedSpans(t, m, highlight.SpansMsg{
		Path:   "main.go",
		Scopes: []highlight.Scope{{HeaderLine: 1, EndLine: 30}},
	})
	if got := m.stickyLines(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("fresh scopes must invalidate the memo: %v, want [1]", got)
	}
}
