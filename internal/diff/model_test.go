package diff

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "shift+left":
		return tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift}
	case "shift+right":
		return tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift}
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
	// The current hunk syncs to the viewport (#2494): a fresh view at the
	// top starts on hunk 0.
	if m.CurrentHunk() != 0 {
		t.Fatalf("current hunk should sync to the view, got %d", m.CurrentHunk())
	}
	m.Update(key("n"))
	if m.CurrentHunk() != 1 {
		t.Fatalf("n should step to hunk 1, got %d", m.CurrentHunk())
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

// hscrollModel builds a two-row diff whose second row is far wider than the
// pane, so the horizontal offset has room to move (#1700).
func hscrollModel(t *testing.T) *Model {
	t.Helper()
	long := "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	m := testModel(t, "short\n"+long, "short\n"+long)
	m.SetSize(40, 20)
	return m
}

func TestLongLinesDoNotWrap(t *testing.T) {
	long := strings.Repeat("wxyz ", 30) // ~150 cells, far wider than a 40-cell pane
	m := testModel(t, long, long)
	m.SetSize(40, 20)
	v := plainView(m)
	if strings.Contains(v, "↪") {
		t.Fatalf("the diff must not soft-wrap:\n%s", v)
	}
	// One row in, one visual line out — the rest of the pane is empty.
	if len(m.lines) != 1 {
		t.Fatalf("a long line should render as exactly one visual line, got %d", len(m.lines))
	}
	first := strings.SplitN(v, "\n", 2)[0]
	if w := ansi.StringWidth(first); w > 40 {
		t.Fatalf("row should clip at the pane edge, got width %d: %q", w, first)
	}
}

func TestUnifiedLongLinesDoNotWrap(t *testing.T) {
	long := strings.Repeat("wxyz ", 30)
	m := testModel(t, long, long+"!")
	m.SetSize(40, 20)
	m.SetUnified(true)
	// One changed pair: the removed line and the added line, nothing more.
	if len(m.lines) != 2 {
		t.Fatalf("unified changed pair should render as two visual lines, got %d", len(m.lines))
	}
}

func TestHorizontalScrollMovesBothSidesInLockstep(t *testing.T) {
	m := hscrollModel(t)
	before := strings.Split(plainView(m), "\n")
	m.Update(key("right"))
	if m.HOffset() != 1 {
		t.Fatalf("right should scroll one column, got offset %d", m.HOffset())
	}
	after := strings.Split(plainView(m), "\n")
	if len(before) != len(after) {
		t.Fatalf("horizontal scrolling must not change the row count: %d vs %d", len(before), len(after))
	}
	// Row alignment: the separator sits in the same column on every row.
	// Measured in display cells, not bytes: a scrolled row carries the
	// multi-byte, one-cell edge marks (#2377).
	sepCell := func(l string) int {
		at := strings.Index(l, "│")
		if at < 0 {
			return -1
		}
		return ansi.StringWidth(l[:at])
	}
	sepCol := sepCell(after[0])
	for i, l := range after[:2] {
		if got := sepCell(l); got != sepCol {
			t.Fatalf("row %d separator moved to column %d, want %d: %q", i, got, sepCol, l)
		}
	}
	// Both sides dropped the same first column.
	left, right, _ := strings.Cut(after[1], "│")
	if strings.Contains(left, "abc") || strings.Contains(right, "abc") {
		t.Fatalf("both sides should have scrolled past the first column: %q", after[1])
	}
	// The window opens one column later; its first cell carries the
	// left-edge mark (#2377), so the text visible after it starts at "c".
	if !strings.Contains(left, "cde") || !strings.Contains(right, "cde") {
		t.Fatalf("both sides should start one column later: %q", after[1])
	}
}

func TestHorizontalScrollKeysAndClamping(t *testing.T) {
	m := hscrollModel(t)
	m.Update(key("left"))
	if m.HOffset() != 0 {
		t.Fatalf("left at column 0 should clamp, got %d", m.HOffset())
	}
	m.Update(key("shift+right"))
	if want := m.hStep(); m.HOffset() != want {
		t.Fatalf("shift+right should page by %d, got %d", want, m.HOffset())
	}
	m.Update(key("shift+left"))
	if m.HOffset() != 0 {
		t.Fatalf("shift+left should page back to 0, got %d", m.HOffset())
	}
	m.Update(key("$"))
	if want := m.maxHOff(); m.HOffset() != want || want == 0 {
		t.Fatalf("$ should scroll to the widest line's end (%d), got %d", want, m.HOffset())
	}
	m.ScrollXBy(1000)
	if want := m.maxHOff(); m.HOffset() != want {
		t.Fatalf("horizontal scroll should clamp at %d, got %d", want, m.HOffset())
	}
	m.Update(key("0"))
	if m.HOffset() != 0 {
		t.Fatalf("0 should reset the horizontal offset, got %d", m.HOffset())
	}
}

func TestHorizontalScrollNoopWithoutOverflow(t *testing.T) {
	m := testModel(t, "a\nb", "a\nb")
	m.ScrollXBy(10)
	if m.HOffset() != 0 {
		t.Fatalf("lines that fit leave no horizontal range, got %d", m.HOffset())
	}
}

func TestHorizontalOffsetReclampsOnResize(t *testing.T) {
	m := hscrollModel(t)
	m.ScrollXBy(1000)
	off := m.HOffset()
	m.SetSize(200, 20) // now the widest line fits: no scroll range left
	if m.HOffset() != 0 {
		t.Fatalf("widening the pane should reset the offset (was %d), got %d", off, m.HOffset())
	}
}

func TestHorizontalScrollUnified(t *testing.T) {
	m := hscrollModel(t)
	m.SetUnified(true)
	m.Update(key("right"))
	if m.HOffset() != 1 {
		t.Fatalf("unified layout should scroll horizontally too, got %d", m.HOffset())
	}
	if v := plainView(m); strings.Contains(v, "abcde") {
		t.Fatalf("unified row should have scrolled past the first column:\n%s", v)
	}
}

func TestSpansSurviveHorizontalOffset(t *testing.T) {
	// A changed pair differing only in a late rune: the intra-line emphasis
	// must land on that rune after the view scrolled right.
	pal := theme.DefaultPalette()
	m := NewFiles("diff", "/tmp/left.txt", "/tmp/right.txt", pal)
	m.SetSize(30, 10)
	prefix := strings.Repeat("x", 40)
	m.SetContents(prefix+"L"+prefix, prefix+"R"+prefix)
	m.ScrollXBy(35)
	want := lipgloss.NewStyle().Background(pal.DiffAddedEmph).Bold(true).Render("R")
	if !strings.Contains(m.View(), want) {
		t.Fatalf("changed rune should stay emphasized at a horizontal offset:\n%q", m.View())
	}
	// Scrolled past the change, the emphasis is off-screen.
	m.ScrollXBy(20)
	if strings.Contains(m.View(), want) {
		t.Fatalf("emphasis should scroll out of view:\n%q", m.View())
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
