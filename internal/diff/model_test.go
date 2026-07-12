package diff

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	r := []rune(s)[0]
	k := tea.KeyPressMsg{Code: r, Text: s}
	if r >= 'A' && r <= 'Z' {
		k.Mod = tea.ModShift
	}
	return k
}

func testModel(t *testing.T, left, right string) *Model {
	t.Helper()
	m := NewFiles("diff", "/tmp/left.txt", "/tmp/right.txt", nil)
	m.SetSize(80, 10)
	m.SetContents(left, right)
	return &m
}

func plainView(m *Model) string {
	return ansi.Strip(m.View())
}

func TestViewShowsBothSidesWithLineNumbers(t *testing.T) {
	m := testModel(t, "alpha\nbravo", "alpha\ncharlie")
	v := plainView(m)
	for _, want := range []string{"alpha", "bravo", "charlie", "  1 ", "  2 "} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}
	first := strings.SplitN(v, "\n", 2)[0]
	if strings.Count(first, "alpha") != 2 {
		t.Fatalf("side-by-side row should show the unchanged line twice: %q", first)
	}
	if !strings.Contains(first, "│") {
		t.Fatalf("side-by-side row should carry the column separator: %q", first)
	}
}

func TestUnifiedToggle(t *testing.T) {
	m := testModel(t, "alpha\nbravo", "alpha\ncharlie")
	m.Update(key("u"))
	if !m.Unified() {
		t.Fatal("u should switch to unified layout")
	}
	v := plainView(m)
	lines := strings.Split(v, "\n")
	if strings.Count(lines[0], "alpha") != 1 {
		t.Fatalf("unified row should show the unchanged line once: %q", lines[0])
	}
	// The changed pair renders as two rows: removed then added.
	if !strings.Contains(lines[1], "bravo") || !strings.Contains(lines[2], "charlie") {
		t.Fatalf("unified changed pair should render removed then added:\n%s", v)
	}
	m.Update(key("u"))
	if m.Unified() {
		t.Fatal("u again should switch back to side-by-side")
	}
}

func TestHunkNavigation(t *testing.T) {
	// Two hunks separated by unchanged lines, with enough rows to scroll.
	left := "x1\n" + strings.Repeat("same\n", 20) + "x2\ntail"
	right := "y1\n" + strings.Repeat("same\n", 20) + "y2\ntail"
	m := testModel(t, left, right)
	if m.HunkCount() != 2 {
		t.Fatalf("want 2 hunks, got %d", m.HunkCount())
	}
	if m.CurrentHunk() != -1 {
		t.Fatalf("current hunk should start at -1, got %d", m.CurrentHunk())
	}
	m.Update(key("n"))
	if m.CurrentHunk() != 0 {
		t.Fatalf("n should land on hunk 0, got %d", m.CurrentHunk())
	}
	m.Update(key("n"))
	if m.CurrentHunk() != 1 {
		t.Fatalf("second n should land on hunk 1, got %d", m.CurrentHunk())
	}
	if !strings.Contains(plainView(m), "y2") {
		t.Fatalf("view should have scrolled to the second hunk:\n%s", plainView(m))
	}
	m.Update(key("n"))
	if m.CurrentHunk() != 1 {
		t.Fatalf("n past the last hunk should clamp, got %d", m.CurrentHunk())
	}
	m.Update(key("N"))
	if m.CurrentHunk() != 0 {
		t.Fatalf("N should step back to hunk 0, got %d", m.CurrentHunk())
	}
}

func TestBigNBeforeAnyNStartsAtLastHunk(t *testing.T) {
	m := testModel(t, "x1\nsame\nx2", "y1\nsame\ny2")
	m.Update(key("N"))
	if m.CurrentHunk() != m.HunkCount()-1 {
		t.Fatalf("N before any n should land on the last hunk, got %d", m.CurrentHunk())
	}
}

func TestEnterDispatchesJump(t *testing.T) {
	m := testModel(t, "a\nold\nc", "a\nnew\nc")
	m.Update(key("n"))
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter on a hunk should return a command")
	}
	msg, ok := cmd().(JumpMsg)
	if !ok {
		t.Fatalf("want JumpMsg, got %T", cmd())
	}
	if msg.Path != "/tmp/right.txt" || msg.Line != 2 {
		t.Fatalf("jump: got %+v want /tmp/right.txt line 2", msg)
	}
}

func TestEnterWithoutNavigationUsesFirstHunk(t *testing.T) {
	m := testModel(t, "a\nold\nc", "a\nnew\nc")
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter should target the first hunk when none was navigated to")
	}
	if msg := cmd().(JumpMsg); msg.Line != 2 {
		t.Fatalf("jump line: got %d want 2", msg.Line)
	}
}

func TestEnterOnPureRemovalJumpsToNeighbour(t *testing.T) {
	m := testModel(t, "a\ngone\nc", "a\nc")
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter should still jump on a pure-removal hunk")
	}
	if msg := cmd().(JumpMsg); msg.Line != 1 {
		t.Fatalf("removal hunk should land on the preceding right line, got %d", msg.Line)
	}
}

func TestEnterWithoutPathIsNoop(t *testing.T) {
	m := New("diff", "HEAD", "buffer", "", nil)
	m.SetSize(80, 10)
	m.SetContents("a", "b")
	if cmd := m.Update(key("enter")); cmd != nil {
		t.Fatal("enter without a right path should be a no-op")
	}
}

func TestLongLinesWrap(t *testing.T) {
	long := strings.Repeat("wxyz ", 30) // ~150 cells, wraps in a 40-cell pane
	m := testModel(t, long, long)
	m.SetSize(40, 20)
	v := plainView(m)
	if !strings.Contains(v, "↪") {
		t.Fatalf("wrapped rows should carry the continuation marker:\n%s", v)
	}
}

func TestScrollClamps(t *testing.T) {
	m := testModel(t, "a\nb\nc", "a\nb\nc")
	m.ScrollBy(-10)
	if m.top != 0 {
		t.Fatalf("scroll should clamp at 0, got %d", m.top)
	}
	m.ScrollBy(100)
	if m.top != 0 {
		t.Fatalf("3 rows in a 10-line pane leave no scroll range, got top %d", m.top)
	}
}

func TestGapRowsOnAddedLines(t *testing.T) {
	m := testModel(t, "a", "a\nadded")
	v := plainView(m)
	lines := strings.Split(v, "\n")
	// Row 2 is an add: the left column has no line number, the right shows 2.
	if !strings.Contains(lines[1], "added") {
		t.Fatalf("added line missing: %q", lines[1])
	}
	leftHalf := lines[1][:strings.Index(lines[1], "│")]
	if strings.ContainsAny(leftHalf, "0123456789") {
		t.Fatalf("gap side should not carry a line number: %q", leftHalf)
	}
}

func TestViewEmptyBeforeSizing(t *testing.T) {
	m := NewFiles("diff", "l", "r", nil)
	m.SetContents("a", "b")
	if m.View() != "" {
		t.Fatal("unsized view should render empty")
	}
}
