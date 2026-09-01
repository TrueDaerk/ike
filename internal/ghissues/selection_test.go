package ghissues

// selection_test.go covers the detail views' mouse text selection (#2374):
// the drag across lines, the copy chord, the demarcation against a plain
// click and a double click, the wheel extending a running drag, and the
// stale-selection retirement when the shown detail changes.

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
	"ike/internal/textsel"
)

// selClock pins the click-streak clock so double clicks are deterministic,
// and returns a stepper.
func selClock(t *testing.T) func(time.Duration) {
	t.Helper()
	now := fixedNow
	prev := textsel.Now
	textsel.Now = func() time.Time { return now }
	t.Cleanup(func() { textsel.Now = prev })
	return func(d time.Duration) { now = now.Add(d) }
}

// openDetail opens issue #2's detail and replaces the glamour-rendered lines
// with a known grid, so the column arithmetic under test is not a test of the
// markdown renderer. The render guard keys on issue, width and timeline
// revision, all unchanged here, so the stub survives the next View().
func openDetail(t *testing.T, lines ...string) *Model {
	t.Helper()
	m := filled(t)
	press(m, "down", "enter")
	if !m.DetailOpen() {
		t.Fatal("enter must open the issue detail")
	}
	m.View() // render once so ensureDetail records what it rendered for
	m.detailLines = lines
	m.detailTop = 0
	return m
}

// dragText runs a press/drag/release over the body and returns what the
// selection would copy.
func dragText(m *Model, x0, y0, x1, y1 int) string {
	m.MousePress(x0, m.bodyTop()+y0)
	m.MouseDrag(x1, m.bodyTop()+y1)
	m.MouseRelease()
	return m.SelectionText()
}

func TestDetailDragSelectsAcrossLines(t *testing.T) {
	selClock(t)
	m := openDetail(t, "alpha beta", "gamma delta", "epsilon")
	got := dragText(m, 6, 0, 5, 2)
	if want := "beta\ngamma delta\nepsil"; got != want {
		t.Fatalf("selection = %q, want %q", got, want)
	}
	if !m.HasSelection() {
		t.Fatal("the released drag must leave a visible selection")
	}
}

func TestDetailDragBackwardsSelects(t *testing.T) {
	selClock(t)
	m := openDetail(t, "alpha beta", "gamma delta")
	if got, want := dragText(m, 5, 1, 6, 0), "beta\ngamma"; got != want {
		t.Fatalf("upward selection = %q, want %q", got, want)
	}
}

func TestDetailSelectionCopiesWithChord(t *testing.T) {
	selClock(t)
	m := openDetail(t, "alpha beta", "gamma delta")
	dragText(m, 0, 0, 5, 1)
	cmd := m.Update(key("y"))
	if cmd == nil {
		t.Fatal("y must copy the selection")
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("y must emit a CopyMsg, got %T", cmd())
	}
	if want := "alpha beta\ngamma"; msg.Text != want {
		t.Fatalf("copied %q, want %q", msg.Text, want)
	}
	if msg.What != "selection" {
		t.Fatalf("copied thing = %q, want \"selection\"", msg.What)
	}
	if m.HasSelection() {
		t.Fatal("a copy must drop the selection, like the response viewer's")
	}
	if !m.DetailOpen() {
		t.Fatal("copying must not leave the detail")
	}
}

func TestDetailCopyChordAliasCopies(t *testing.T) {
	selClock(t)
	m := openDetail(t, "alpha beta")
	dragText(m, 0, 0, 5, 0)
	cmd := m.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModSuper})
	if cmd == nil {
		t.Fatal("cmd+c must copy the selection too")
	}
	if msg, ok := cmd().(CopyMsg); !ok || msg.Text != "alpha" {
		t.Fatalf("cmd+c copied %#v", cmd())
	}
}

func TestDetailCopyWithoutSelectionIsInert(t *testing.T) {
	selClock(t)
	m := openDetail(t, "alpha beta")
	if cmd := m.Update(key("y")); cmd != nil {
		t.Fatal("y without a selection must copy nothing")
	}
}

func TestDetailClickWithoutDragSelectsNothing(t *testing.T) {
	selClock(t)
	m := openDetail(t, "alpha beta", "gamma delta")
	m.MousePress(4, m.bodyTop()+1)
	m.MouseRelease()
	if m.HasSelection() {
		t.Fatal("a click without a drag must not select")
	}
	if !m.DetailOpen() || m.Cursor() != 1 {
		t.Fatal("a click in the detail body must not disturb the list state")
	}
}

func TestDetailDoubleClickSelectsWord(t *testing.T) {
	step := selClock(t)
	m := openDetail(t, "alpha beta", "gamma delta")
	m.MousePress(7, m.bodyTop())
	m.MouseRelease()
	step(50 * time.Millisecond)
	m.MousePress(7, m.bodyTop())
	m.MouseRelease()
	if got := m.SelectionText(); got != "beta" {
		t.Fatalf("a double click must select the word, got %q", got)
	}
}

func TestListPressLeavesTheClickGesturesAlone(t *testing.T) {
	selClock(t)
	m := filled(t)
	now := fixedNow
	m.now = func() time.Time { return now }
	if m.MousePress(4, m.bodyTop()+1) {
		t.Fatal("the list view must not arm a selection drag")
	}
	m.Click(4, m.bodyTop()+1)
	if m.Cursor() != 1 {
		t.Fatalf("a list click must still select the row, cursor = %d", m.Cursor())
	}
	now = now.Add(100 * time.Millisecond)
	m.Click(4, m.bodyTop()+1)
	if !m.DetailOpen() {
		t.Fatal("a list double click must still open the detail")
	}
	// The detail's chrome rows keep their click targets: no drag is armed on
	// the tab bar or the filter row.
	if m.MousePress(m.tabBarSpans()[1][0], 0) || m.MousePress(0, 1) {
		t.Fatal("the tab bar and the filter row must not start a selection")
	}
}

func TestWheelDuringDragExtendsSelection(t *testing.T) {
	selClock(t)
	m := openDetail(t, "one", "two", "three", "four", "five", "six", "seven", "eight",
		"nine", "ten", "eleven", "twelve", "thirteen", "fourteen")
	m.MousePress(0, m.bodyTop())
	m.MouseDrag(3, m.bodyTop())
	m.Wheel(2) // the pointer rests, the lines move under it
	m.MouseRelease()
	// The pointer never moved, but two more lines scrolled under it, so the
	// span grew to the third line (up to the resting column).
	if got, want := m.SelectionText(), "one\ntwo\nthr"; got != want {
		t.Fatalf("the wheel must grow the running selection, got %q want %q", got, want)
	}
}

func TestSelectionRetiresWhenTheDetailChanges(t *testing.T) {
	selClock(t)
	m := openDetail(t, "alpha beta", "gamma delta")
	dragText(m, 0, 0, 5, 1)
	m.Update(key("ctrl+j"))
	if m.HasSelection() || m.SelectionText() != "" {
		t.Fatal("walking to the next issue must retire the selection")
	}
}

func TestSelectionHighlightRenders(t *testing.T) {
	selClock(t)
	m := openDetail(t, "alpha beta", "gamma delta")
	dragText(m, 0, 0, 5, 0)
	view := m.View()
	if !strings.Contains(view, "alpha") {
		t.Fatalf("the selected text must still render:\n%s", view)
	}
	// The span is repainted in the selection colours, so the line no longer
	// renders as the plain string it was stored as.
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "alpha") {
			if line == "alpha beta" {
				t.Fatal("the selection must be visible, not plain")
			}
			return
		}
	}
	t.Fatal("the body line was not rendered")
}

func TestPRDetailSelectsAndCopies(t *testing.T) {
	selClock(t)
	m, _ := prActive(t)
	m.Update(key("enter"))
	if !m.PRDetailOpen() {
		t.Fatal("enter must open the PR detail")
	}
	m.SetPRDetailResult(forge.PRDetailMsg{Number: 9, Detail: forge.PRDetail{
		PR:      forge.PR{Number: 9, Title: "fix the explorer crash", State: "OPEN"},
		Body:    "## Fix\nDone.",
		BaseRef: "main",
	}})
	m.View()
	m.prdLines = []string{"first line", "second line"}
	m.prdTop = 0
	if got, want := dragText(m, 0, 0, 6, 1), "first line\nsecond"; got != want {
		t.Fatalf("PR detail selection = %q, want %q", got, want)
	}
	cmd := m.Update(key("y"))
	if cmd == nil {
		t.Fatal("y must copy the PR detail selection")
	}
	if msg, ok := cmd().(CopyMsg); !ok || msg.Text != "first line\nsecond" {
		t.Fatalf("PR detail copy = %#v", cmd())
	}
}

func TestSelectionOverStyledLines(t *testing.T) {
	selClock(t)
	// A glamour-rendered line carries escape sequences; the selection
	// addresses its display runes, so neither the copy nor the untouched
	// remainder may show them.
	styled := "\x1b[1malpha\x1b[0m beta"
	m := openDetail(t, styled, "gamma")
	if got, want := dragText(m, 0, 0, 4, 1), "alpha beta\ngamm"; got != want {
		t.Fatalf("selection over a styled line = %q, want %q", got, want)
	}
	painted := m.highlightSel(styled, 0)
	if painted == styled {
		t.Fatal("the covered span must be repainted")
	}
	if !strings.Contains(painted, "alpha") || !strings.Contains(painted, "beta") {
		t.Fatalf("the painted line lost text: %q", painted)
	}
}

func TestSelectionIgnoresRenderPadding(t *testing.T) {
	selClock(t)
	m := openDetail(t, "alpha        ", "beta        ")
	// A line-spanning drag past the text end must copy the text, not the
	// block padding the markdown renderer pads its lines with.
	if got, want := dragText(m, 0, 0, 40, 1), "alpha\nbeta"; got != want {
		t.Fatalf("selection = %q, want %q", got, want)
	}
}
