package diff

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/textsel"
)

// selection_test.go covers the diff viewer's mouse text selection and copy
// (#2070), analog to the HTTP response viewer's suite (#1266): drag / word /
// line gestures over the composed lines, side-bound selection in side-by-side
// layout, collapsed-context extraction, and the copy chords.

// fixedClock pins textsel.Now so click streaks are deterministic; the
// returned func advances it.
func fixedClock(t *testing.T) func(time.Duration) {
	t.Helper()
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	prev := textsel.Now
	textsel.Now = func() time.Time { return now }
	t.Cleanup(func() { textsel.Now = prev })
	return func(d time.Duration) { now = now.Add(d) }
}

// selModel is a unified-layout model over a small three-row diff whose
// visual lines are: 0 "one two" (same), 1 "gamma delta" (changed, left),
// 2 "gamma DELTA" (changed, right), 3 "shared" (same).
func selModel(t *testing.T) *Model {
	t.Helper()
	m := testModel(t, "one two\ngamma delta\nshared\n", "one two\ngamma DELTA\nshared\n")
	m.SetUnified(true)
	return m
}

// uniCell maps a (visual line, content column) onto pane-local mouse cells
// in unified layout: two 3-digit gutters plus their trailing spaces.
func (m *Model) uniCell(line, col int) (x, y int) {
	lw, rw := m.gutterWidths()
	return (lw + 2 + rw + 2) + col - m.hoff, line - m.top
}

func uniPress(m *Model, line, col int) {
	x, y := m.uniCell(line, col)
	m.MousePress(x, y)
}

func uniDrag(m *Model, line, col int) {
	x, y := m.uniCell(line, col)
	m.MouseDrag(x, y)
}

func TestUnifiedDragSelectsAcrossLines(t *testing.T) {
	m := selModel(t)
	fixedClock(t)
	uniPress(m, 0, 4)
	uniDrag(m, 1, 5)
	m.MouseRelease()
	if !m.HasSelection() {
		t.Fatal("drag must produce a selection")
	}
	if got, want := m.SelectionText(), "two\ngamma"; got != want {
		t.Errorf("selection text: %q, want %q", got, want)
	}
}

func TestUnifiedDragBackwards(t *testing.T) {
	m := selModel(t)
	fixedClock(t)
	uniPress(m, 1, 5)
	uniDrag(m, 0, 4)
	if got, want := m.SelectionText(), "two\ngamma"; got != want {
		t.Errorf("backwards selection: %q, want %q", got, want)
	}
}

func TestDoubleClickSelectsWord(t *testing.T) {
	m := selModel(t)
	step := fixedClock(t)
	uniPress(m, 1, 2)
	step(50 * time.Millisecond)
	uniPress(m, 1, 2)
	if got := m.SelectionText(); got != "gamma" {
		t.Errorf("double click: %q, want gamma", got)
	}
}

func TestTripleClickSelectsLine(t *testing.T) {
	m := selModel(t)
	step := fixedClock(t)
	for i := 0; i < 3; i++ {
		uniPress(m, 1, 2)
		step(50 * time.Millisecond)
	}
	if got := m.SelectionText(); got != "gamma delta" {
		t.Errorf("triple click: %q, want the whole line", got)
	}
}

// TestClickStreakExpires: a slow second click starts a fresh char selection
// instead of growing into a word selection.
func TestClickStreakExpires(t *testing.T) {
	m := selModel(t)
	step := fixedClock(t)
	uniPress(m, 1, 2)
	step(textsel.MultiClickWindow + time.Millisecond)
	uniPress(m, 1, 2)
	if m.HasSelection() {
		t.Errorf("expired streak must not select a word, got %q", m.SelectionText())
	}
}

func TestCopySelectionEmitsCopyMsg(t *testing.T) {
	m := selModel(t)
	fixedClock(t)
	uniPress(m, 1, 0)
	uniDrag(m, 1, 5)
	cmd := m.handleKey(key("y"))
	if cmd == nil {
		t.Fatal("y with a selection must emit a copy command")
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("message type: %T", cmd())
	}
	if msg.Text != "gamma" || msg.What != "selection" {
		t.Errorf("copy message: %+v", msg)
	}
	if m.HasSelection() {
		t.Error("copying must clear the selection")
	}
}

// TestCtrlCCopies mirrors the platform copy chords onto the same path.
func TestCtrlCCopies(t *testing.T) {
	m := selModel(t)
	fixedClock(t)
	uniPress(m, 1, 0)
	uniDrag(m, 1, 5)
	cmd := m.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c with a selection must copy")
	}
	if msg := cmd().(CopyMsg); msg.Text != "gamma" {
		t.Errorf("ctrl+c copy: %+v", msg)
	}
}

// TestCopyWithoutSelectionCopiesHunkPatch: the copy key without a selection
// copies the current hunk as a minimal unified patch.
func TestCopyWithoutSelectionCopiesHunkPatch(t *testing.T) {
	m := selModel(t)
	cmd := m.handleKey(key("y"))
	if cmd == nil {
		t.Fatal("y without a selection must copy the hunk")
	}
	msg := cmd().(CopyMsg)
	want := "@@ -2,1 +2,1 @@\n-gamma delta\n+gamma DELTA"
	if msg.Text != want || msg.What != "hunk" {
		t.Errorf("hunk copy: %+v, want text %q", msg, want)
	}
}

// TestCopyEmptyDiffEmitsNothing: no hunks, no selection — the clipboard must
// not be clobbered with an empty write.
func TestCopyEmptyDiffEmitsNothing(t *testing.T) {
	m := testModel(t, "same\n", "same\n")
	if cmd := m.handleKey(key("y")); cmd != nil {
		t.Errorf("an empty diff must not clobber the clipboard: %+v", cmd())
	}
}

// sbsCell maps (visual line, column) onto cells of one side-by-side column.
func (m *Model) sbsCell(line, col int, right bool) (x, y int) {
	lw, rw := m.gutterWidths()
	colL, _ := m.columnWidths()
	x0 := lw + 2
	if right {
		x0 = lw + 2 + colL + 3 + rw + 2
	}
	return x0 + col - m.hoff, line - m.top
}

func TestSideBySideSelectionStaysInColumn(t *testing.T) {
	m := testModel(t, "one two\ngamma delta\nshared\n", "one two\ngamma DELTA\nshared\n")
	fixedClock(t)
	// Press in the left column and drag deep into the right column: the
	// selection clamps to the left line's end instead of mixing sides.
	x, y := m.sbsCell(1, 0, false)
	m.MousePress(x, y)
	x, y = m.sbsCell(1, 5, true)
	m.MouseDrag(x, y)
	m.MouseRelease()
	if got, want := m.SelectionText(), "gamma delta"; got != want {
		t.Errorf("left-column selection: %q, want %q", got, want)
	}

	// A press in the right column selects the right side's text.
	x, y = m.sbsCell(1, 0, true)
	m.MousePress(x, y)
	x, y = m.sbsCell(1, 11, true)
	m.MouseDrag(x, y)
	if got, want := m.SelectionText(), "gamma DELTA"; got != want {
		t.Errorf("right-column selection: %q, want %q", got, want)
	}
}

// gapModel builds a diff whose middle rows fold behind a separator: changes
// at both ends, ten identical lines between them, context 3 → m4..m7 hidden.
func gapModel(t *testing.T) *Model {
	t.Helper()
	mid := "m1\nm2\nm3\nm4\nm5\nm6\nm7\nm8\nm9\nm10\n"
	m := testModel(t, "A\n"+mid+"B\n", "A2\n"+mid+"B2\n")
	m.SetUnified(true)
	return m
}

// TestSelectionCopiesCollapsedGapContent: a selection covering the separator
// copies the hidden rows as real text, never the placeholder label (#1741's
// rule).
func TestSelectionCopiesCollapsedGapContent(t *testing.T) {
	m := gapModel(t)
	step := fixedClock(t)
	if len(m.sepLines) != 1 {
		t.Fatalf("expected one separator, got %v", m.sepLines)
	}
	var sepLine int
	for line := range m.sepLines {
		sepLine = line
	}
	// Triple-click the separator: the line gesture claims the whole label —
	// and the extraction yields the fold's real content.
	for i := 0; i < 3; i++ {
		m.MousePress(20, sepLine-m.top)
		step(50 * time.Millisecond)
	}
	if got, want := m.SelectionText(), "m4\nm5\nm6\nm7"; got != want {
		t.Errorf("collapsed gap copy: %q, want %q", got, want)
	}
}

// TestDragAcrossSeparatorIncludesGap: a drag from above the separator to
// below it copies the visible rows plus the hidden run in between.
func TestDragAcrossSeparatorIncludesGap(t *testing.T) {
	m := gapModel(t)
	fixedClock(t)
	var sepLine int
	for line := range m.sepLines {
		sepLine = line
	}
	uniPress(m, sepLine-1, 0)
	x, _ := m.uniCell(sepLine+1, 2)
	m.MouseDrag(x, sepLine+1-m.top)
	m.MouseRelease()
	if got, want := m.SelectionText(), "m3\nm4\nm5\nm6\nm7\nm8"; got != want {
		t.Errorf("drag across separator: %q, want %q", got, want)
	}
}

// TestSelectionCopiesRealTabs: extraction maps display columns back onto the
// raw text, so a tab-indented line copies with its tab, not four spaces.
func TestSelectionCopiesRealTabs(t *testing.T) {
	m := testModel(t, "\tindented\nx\n", "\tindented\ny\n")
	m.SetUnified(true)
	step := fixedClock(t)
	for i := 0; i < 3; i++ {
		uniPress(m, 0, 2)
		step(50 * time.Millisecond)
	}
	if got, want := m.SelectionText(), "\tindented"; got != want {
		t.Errorf("tab extraction: %q, want %q", got, want)
	}
}

func TestSelectionRendersAndEscClears(t *testing.T) {
	m := selModel(t)
	fixedClock(t)
	plain := m.View()
	uniPress(m, 1, 0)
	uniDrag(m, 1, 5)
	if m.View() == plain {
		t.Error("a selection must change the rendered output")
	}
	m.handleKey(keyEsc())
	if m.HasSelection() {
		t.Error("esc must clear the selection")
	}
	if m.View() != plain {
		t.Error("clearing must restore the plain render")
	}
}

func TestSelectionClearedOnNewContents(t *testing.T) {
	m := selModel(t)
	fixedClock(t)
	uniPress(m, 1, 0)
	uniDrag(m, 1, 5)
	m.SetContents("fresh\n", "fresher\n")
	if m.HasSelection() {
		t.Error("new contents must drop the stale selection")
	}
}

func TestSelectionClearedOnLayoutToggle(t *testing.T) {
	m := selModel(t)
	fixedClock(t)
	uniPress(m, 1, 0)
	uniDrag(m, 1, 5)
	m.SetUnified(false)
	if m.HasSelection() {
		t.Error("a layout toggle must drop the selection — positions are layout-specific")
	}
}

// TestEditModeIgnoresSelection: while the embedded editor owns the pane
// (#496) the press/drag surface is the editor's, not the diff selection's.
func TestEditModeIgnoresSelection(t *testing.T) {
	m := selModel(t)
	fixedClock(t)
	m.SetEditMode(true)
	uniPress(m, 1, 0)
	uniDrag(m, 1, 5)
	if m.HasSelection() {
		t.Error("edit mode must ignore selection presses")
	}
}

// TestEnteringEditModeDropsSelection: a live selection dies when the editor
// takes over, so the two selection models never overlay.
func TestEnteringEditModeDropsSelection(t *testing.T) {
	m := selModel(t)
	fixedClock(t)
	uniPress(m, 1, 0)
	uniDrag(m, 1, 5)
	if !m.HasSelection() {
		t.Fatal("setup: selection expected")
	}
	m.SetEditMode(true)
	if m.HasSelection() {
		t.Error("entering edit mode must drop the selection")
	}
}

// keyEsc builds the escape key; the rune-based key() helper cannot.
func keyEsc() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEscape} }

// TestHunkPatchPureAddition anchors the zero-count side on the line before
// the insertion, the unified diff convention.
func TestHunkPatchPureAddition(t *testing.T) {
	m := testModel(t, "a\nb\n", "a\nnew\nb\n")
	got := m.HunkPatchText()
	want := "@@ -1,0 +2,1 @@\n+new"
	if got != want {
		t.Errorf("pure-addition patch: %q, want %q", got, want)
	}
}

func TestSelectionSurvivesScroll(t *testing.T) {
	m := selModel(t)
	fixedClock(t)
	uniPress(m, 1, 0)
	uniDrag(m, 1, 5)
	m.ScrollBy(1)
	if !m.HasSelection() {
		t.Error("scrolling must not drop the selection")
	}
	if got := m.SelectionText(); got != "gamma" {
		t.Errorf("selection after scroll: %q", got)
	}
	if !strings.Contains(m.SelectionText(), "gamma") {
		t.Error("selection text lost after scroll")
	}
}
