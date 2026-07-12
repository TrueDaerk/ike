package editor

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/host"
)

// wrapLoaded loads content with soft wrap enabled and no gutter, so the text
// width equals the pane width.
func wrapLoaded(t *testing.T, content string, w, h int) Model {
	t.Helper()
	m, _ := loaded(t, content)
	m.Configure(host.MapConfig{"editor.wrap": "true", "editor.line_numbers": "false"})
	m.SetSize(w, h)
	return m
}

func TestSoftWrapRendersContinuationRows(t *testing.T) {
	m, _ := loaded(t, strings.Repeat("a", 50)+"\nshort\n")
	m.Configure(host.MapConfig{"editor.wrap": "true", "editor.line_numbers": "true"})
	m.SetSize(24, 10)
	v := ansi.Strip(m.View())
	rows := strings.Split(v, "\n")
	if len(rows) < 4 {
		t.Fatalf("view has %d rows, want the long line wrapped over several:\n%s", len(rows), v)
	}
	// Row 0 carries the line number, rows 1..n the wrap marker.
	if !strings.Contains(rows[0], "1") {
		t.Errorf("first row %q lacks its line number", rows[0])
	}
	if !strings.Contains(rows[1], "↪") {
		t.Errorf("continuation row %q lacks the wrap marker", rows[1])
	}
	for i, r := range rows {
		if got := lipgloss.Width(r); got > 24 {
			t.Errorf("row %d width %d exceeds pane width 24", i, got)
		}
	}
	// All 50 cells of the long line must survive the wrap.
	joined := ""
	for _, r := range rows {
		joined += strings.TrimRight(strings.TrimLeft(strings.ReplaceAll(r, "↪", " "), " 1234567890"), " ")
	}
	if n := strings.Count(joined, "a"); n != 50 {
		t.Errorf("wrapped rows carry %d of 50 cells", n)
	}
}

func TestSoftWrapViewStaysWithinHeight(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 30; i++ {
		sb.WriteString(strings.Repeat("x", 100) + "\n")
	}
	m := wrapLoaded(t, sb.String(), 20, 8)
	if got := lipgloss.Height(m.View()); got > 8 {
		t.Fatalf("view height %d exceeds pane height 8", got)
	}
}

func TestWrapVerticalMotionMovesByVisualRow(t *testing.T) {
	m := wrapLoaded(t, strings.Repeat("a", 50)+"\nnext\n", 20, 10)
	m = typeKeys(m, "j")
	if m.cursor.Line != 0 || m.cursor.Col != 20 {
		t.Fatalf("j on a wrapped line moved to %d:%d, want 0:20 (next visual row)", m.cursor.Line, m.cursor.Col)
	}
	m = typeKeys(m, "j")
	if m.cursor.Line != 0 || m.cursor.Col != 40 {
		t.Fatalf("second j moved to %d:%d, want 0:40", m.cursor.Line, m.cursor.Col)
	}
	m = typeKeys(m, "j")
	if m.cursor.Line != 1 {
		t.Fatalf("j on the last visual row moved to line %d, want 1", m.cursor.Line)
	}
	m = typeKeys(m, "k")
	if m.cursor.Line != 0 || m.cursor.Col != 40 {
		t.Fatalf("k moved to %d:%d, want 0:40 (last visual row of the wrapped line)", m.cursor.Line, m.cursor.Col)
	}
}

func TestWrapVerticalKeepsRowOffset(t *testing.T) {
	m := wrapLoaded(t, strings.Repeat("a", 50)+"\n", 20, 10)
	m = typeKeys(m, "lllll") // col 5
	m = typeKeys(m, "j")
	if m.cursor.Col != 25 {
		t.Fatalf("j moved to col %d, want 25 (offset 5 in the next visual row)", m.cursor.Col)
	}
}

func TestWrapScrollFollowsCursorInVisualRows(t *testing.T) {
	// Three lines, each wrapping to 5 rows of width 10; a 6-row window must
	// scroll Top to line 1 once the cursor enters line 2.
	content := strings.Repeat("a", 50) + "\n" + strings.Repeat("b", 50) + "\n" + strings.Repeat("c", 50) + "\n"
	m := wrapLoaded(t, content, 10, 6)
	m = typeKeys(m, "G")
	if top, _ := m.ScrollOffset(); top < 1 {
		t.Fatalf("Top=%d; wrapped scroll must advance past line 0 to keep the cursor visible", top)
	}
}

func TestWrapMouseClickMapsThroughSegments(t *testing.T) {
	m := wrapLoaded(t, strings.Repeat("a", 50)+"\nnext\n", 20, 10)
	m.MouseClick(3, 1) // second visual row of the wrapped line
	if m.cursor.Line != 0 || m.cursor.Col != 23 {
		t.Fatalf("click mapped to %d:%d, want 0:23 (segment start 20 + x 3)", m.cursor.Line, m.cursor.Col)
	}
	m.MouseClick(0, 3) // first row past the wrapped line (3 rows) is line 1
	if m.cursor.Line != 1 || m.cursor.Col != 0 {
		t.Fatalf("click mapped to %d:%d, want 1:0", m.cursor.Line, m.cursor.Col)
	}
}

func TestScrollXByNoopUnderWrap(t *testing.T) {
	m := wrapLoaded(t, strings.Repeat("a", 100)+"\n", 20, 10)
	m.ScrollXBy(5)
	if _, left := m.ScrollOffset(); left != 0 {
		t.Fatalf("left=%d; horizontal scroll must stay 0 under soft wrap", left)
	}
}

func TestToggleWrapOverridesConfig(t *testing.T) {
	m, _ := loaded(t, strings.Repeat("a", 100)+"\n")
	m.Configure(host.MapConfig{"editor.wrap": "false"})
	m, _ = m.Update(ActionMsg{Action: "toggle_wrap"})
	if !m.softWrap {
		t.Fatal("view.toggleWrap did not enable soft wrap")
	}
	// The per-Update config refresh must not clobber the toggle.
	m = typeKeys(m, "j")
	if !m.softWrap {
		t.Fatal("config refresh clobbered the soft-wrap toggle")
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_wrap"})
	if m.softWrap {
		t.Fatal("second toggle did not disable soft wrap")
	}
}

func TestToggleWhitespaceAndGuides(t *testing.T) {
	m, _ := loaded(t, "  ab  \n")
	m.Configure(host.MapConfig{"editor.show_whitespace": "none", "editor.indent_guides": "false"})
	m, _ = m.Update(ActionMsg{Action: "toggle_whitespace"})
	if m.wsMode != wsAll {
		t.Fatalf("wsMode=%v want wsAll after toggle", m.wsMode)
	}
	m = typeKeys(m, "j") // config refresh must keep the override
	if m.wsMode != wsAll {
		t.Fatal("config refresh clobbered the whitespace toggle")
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_indent_guides"})
	if !m.indentGuides {
		t.Fatal("view.toggleIndentGuides did not enable indent guides")
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_whitespace"})
	if m.wsMode != wsNone {
		t.Fatalf("wsMode=%v want wsNone after second toggle", m.wsMode)
	}
}

func TestDisplayRowUnderWrap(t *testing.T) {
	m := wrapLoaded(t, strings.Repeat("a", 50)+"\nnext\n", 20, 10)
	if got := m.DisplayRow(1, 0); got != 3 {
		t.Fatalf("DisplayRow(1,0)=%d want 3 (line 0 wraps over three rows)", got)
	}
	if got := m.DisplayRow(0, 25); got != 1 {
		t.Fatalf("DisplayRow(0,25)=%d want 1 (second segment)", got)
	}
	if got := m.DisplayOffset(0, 25); got != 5 {
		t.Fatalf("DisplayOffset(0,25)=%d want 5 (offset within its segment)", got)
	}
}
