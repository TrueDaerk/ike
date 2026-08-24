package merge

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/diff"
	"ike/internal/textsel"
)

// selection_test.go covers the merge view's side-column text selection and
// copy (#2070): ours/theirs are read-only and otherwise uncopyable; the
// middle result column belongs to the embedded editor.

func fixedClock(t *testing.T) func(time.Duration) {
	t.Helper()
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	prev := textsel.Now
	textsel.Now = func() time.Time { return now }
	t.Cleanup(func() { textsel.Now = prev })
	return func(d time.Duration) { now = now.Add(d) }
}

// selView seeds a conflicted merge whose side columns hold known lines.
func selView(t *testing.T) *Model {
	t.Helper()
	m := newView(t)
	m.SetContents("a\nx\nz\n", "a\nours-line\nz\n", "a\ntheirs-line\nz\n")
	return &m
}

// oursCell / theirsCell map a (document line, column) onto pane-local cells:
// the header takes y == 0 and the columns follow the editor's scroll (0 in
// these tests).
func (m *Model) oursCell(line, col int) (x, y int) { return col, line + 1 }

func (m *Model) theirsCell(line, col int) (x, y int) {
	left, mid, _ := m.colWidths()
	return left + sepWidth + mid + sepWidth + col, line + 1
}

func TestOursColumnDragSelects(t *testing.T) {
	m := selView(t)
	fixedClock(t)
	x, y := m.oursCell(1, 0)
	if !m.MousePress(x, y) {
		t.Fatal("a press in the ours column must claim the mouse")
	}
	x, y = m.oursCell(1, 9)
	m.MouseDrag(x, y)
	m.MouseRelease()
	if got, want := m.SelectionText(), "ours-line"; got != want {
		t.Errorf("ours selection: %q, want %q", got, want)
	}
}

func TestTheirsColumnDoubleClickSelectsWord(t *testing.T) {
	m := selView(t)
	step := fixedClock(t)
	x, y := m.theirsCell(1, 3)
	m.MousePress(x, y)
	step(50 * time.Millisecond)
	m.MousePress(x, y)
	if got, want := m.SelectionText(), "theirs-line"; got != want {
		t.Errorf("theirs word: %q, want %q", got, want)
	}
}

// TestMiddleColumnPressIsNotClaimed: the result editor's ground stays the
// editor's — the view reports the press unclaimed and arms no selection.
func TestMiddleColumnPressIsNotClaimed(t *testing.T) {
	m := selView(t)
	fixedClock(t)
	left, mid, _ := m.colWidths()
	if m.MousePress(left+sepWidth+mid/2, 2) {
		t.Error("a middle-column press must not claim the mouse")
	}
	if m.HasSelection() {
		t.Error("no selection may exist after a middle-column press")
	}
}

func TestHeaderPressIsNotClaimed(t *testing.T) {
	m := selView(t)
	fixedClock(t)
	if m.MousePress(2, 0) {
		t.Error("a header press must not claim the mouse")
	}
}

func TestCopyChordCopiesSideSelection(t *testing.T) {
	m := selView(t)
	fixedClock(t)
	x, y := m.oursCell(1, 0)
	m.MousePress(x, y)
	x, y = m.oursCell(1, 4)
	m.MouseDrag(x, y)
	cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c with a side selection must copy")
	}
	msg, ok := cmd().(diff.CopyMsg)
	if !ok {
		t.Fatalf("message type: %T", cmd())
	}
	if msg.Text != "ours" || msg.What != "selection" {
		t.Errorf("copy message: %+v", msg)
	}
	if m.HasSelection() {
		t.Error("copying must clear the selection")
	}
}

// TestBareYStaysWithCapturingEditor: while the result editor captures text
// (insert mode), "y" is typing — the selection must not steal it.
func TestBareYStaysWithCapturingEditor(t *testing.T) {
	m := selView(t)
	fixedClock(t)
	ed, _ := m.Editor().Update(tea.KeyPressMsg{Code: 'i', Text: "i"}) // enter insert mode
	*m.Editor() = ed
	if !m.Editor().Capturing() {
		t.Skip("editor did not enter a capturing mode; key model changed")
	}
	x, y := m.oursCell(1, 0)
	m.MousePress(x, y)
	x, y = m.oursCell(1, 4)
	m.MouseDrag(x, y)
	before := m.Editor().Text()
	m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if m.Editor().Text() == before {
		t.Error("bare y must reach the capturing editor as typing")
	}
	if !m.HasSelection() {
		t.Error("the side selection must survive typing")
	}
}

func TestSelectionRendersInView(t *testing.T) {
	m := selView(t)
	fixedClock(t)
	plain := m.View()
	x, y := m.oursCell(1, 0)
	m.MousePress(x, y)
	x, y = m.oursCell(1, 9)
	m.MouseDrag(x, y)
	if m.View() == plain {
		t.Error("a selection must change the rendered output")
	}
	if !strings.Contains(m.View(), "ours-line") {
		t.Error("the selected text itself must stay visible")
	}
}

func TestSetContentsClearsSelection(t *testing.T) {
	m := selView(t)
	fixedClock(t)
	x, y := m.oursCell(1, 0)
	m.MousePress(x, y)
	x, y = m.oursCell(1, 4)
	m.MouseDrag(x, y)
	m.SetContents("a\n", "a\n", "a\n")
	if m.HasSelection() {
		t.Error("new contents must drop the stale selection")
	}
}
