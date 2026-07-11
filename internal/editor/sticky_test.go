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
