package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/vcs"
)

// hunkModel: 30 lines, hunks at 2-4 (run incl. kind change), 10 (deleted
// single row), 20-21.
func hunkModel(t *testing.T) Model {
	t.Helper()
	m, _ := loaded(t, strings.Repeat("  x\n", 30))
	m.gitMarks = map[int]vcs.LineMark{
		2: vcs.LineAdded, 3: vcs.LineChanged, 4: vcs.LineAdded,
		10: vcs.LineDeleted,
		20: vcs.LineChanged, 21: vcs.LineChanged,
	}
	return m
}

// TestHunkStartsCollapsesRuns guards #1170: consecutive marked lines form one
// hunk regardless of kind; deleted single rows stand alone.
func TestHunkStartsCollapsesRuns(t *testing.T) {
	m := hunkModel(t)
	got := m.hunkStarts()
	want := []int{2, 10, 20}
	if len(got) != len(want) {
		t.Fatalf("starts = %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("starts = %v want %v", got, want)
		}
	}
}

// TestHunkJumpWalksAndWraps guards #1170: strictly-past semantics with wrap,
// first-non-blank landing, and the n/m notice.
func TestHunkJumpWalksAndWraps(t *testing.T) {
	m := hunkModel(t)
	m.SetCursor(0, 0)
	_ = m.hunkJump(true)
	if m.cursor.Line != 2 || m.cursor.Col != 2 {
		t.Fatalf("]c from 0 → %v, want line 2 first-non-blank", m.cursor)
	}
	// Standing INSIDE hunk 1 (line 3) still moves on to hunk 2.
	m.SetCursor(3, 0)
	_ = m.hunkJump(true)
	if m.cursor.Line != 10 {
		t.Fatalf("]c from inside hunk 1 → line %d, want 10", m.cursor.Line)
	}
	_ = m.hunkJump(true)
	if m.cursor.Line != 20 {
		t.Fatalf("→ %d want 20", m.cursor.Line)
	}
	// Past the last hunk: wrap to the first, notice says so.
	cmd := m.hunkJump(true)
	if m.cursor.Line != 2 {
		t.Fatalf("wrap → %d want 2", m.cursor.Line)
	}
	if msg := cmd(); !strings.Contains(noticeText(msg), "(wrapped)") {
		t.Fatalf("notice = %v", msg)
	}
	// Backwards from 2 wraps to the last.
	cmd = m.hunkJump(false)
	if m.cursor.Line != 20 {
		t.Fatalf("[c wrap → %d want 20", m.cursor.Line)
	}
	if msg := cmd(); !strings.Contains(noticeText(msg), "change 3/3") {
		t.Fatalf("notice = %v", msg)
	}
}

// TestHunkJumpEmpty guards #1170.
func TestHunkJumpEmpty(t *testing.T) {
	m, _ := loaded(t, "x\n")
	cmd := m.hunkJump(true)
	if msg := cmd(); !strings.Contains(noticeText(msg), "no changes") {
		t.Fatalf("notice = %v", msg)
	}
}

// TestBracketCKeys guards #1170: the ]c / [c sequences dispatch the jumps and
// a stray continuation drops the pending state.
func TestBracketCKeys(t *testing.T) {
	m := hunkModel(t)
	m.SetCursor(0, 0)
	m, _ = m.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m, _ = m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if m.cursor.Line != 2 {
		t.Fatalf("]c → line %d want 2", m.cursor.Line)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	m, _ = m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.cursor.Line != 2 {
		t.Fatalf("stray [x must not move, line %d", m.cursor.Line)
	}
}

// noticeText extracts the ex-line notice text from a command's message.
func noticeText(msg tea.Msg) string {
	if n, ok := msg.(NoticeMsg); ok {
		return n.Text
	}
	return ""
}
