package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestAppendReadOnlyKeepsView (#2796): appending to a read-only buffer grows
// it in place — the cursor, the scroll position and the search stay where
// they were, the new lines are searchable, and a writable buffer refuses.
func TestAppendReadOnlyKeepsView(t *testing.T) {
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, "line "+strings.Repeat("x", i%7))
	}
	m := readOnlyBuffer(t, "a.txt", strings.Join(lines, "\n"))
	m.SetCursor(40, 1)
	top := m.ScrollTop()
	cur, _ := m.Cursor()
	if top == 0 {
		t.Fatal("the cursor at line 40 must have scrolled the view")
	}
	m.AppendReadOnly("\nneedle here\nlast")
	if got := m.LineCount(); got != 62 {
		t.Fatalf("line count = %d, want 62", got)
	}
	if line, _ := m.Cursor(); line != cur || m.ScrollTop() != top {
		t.Fatalf("append moved the view: cursor line %d, top %d (was %d)", line, m.ScrollTop(), top)
	}
	if !strings.HasSuffix(m.Text(), "\nneedle here\nlast") {
		t.Fatalf("text tail = %q", m.Text()[len(m.Text())-30:])
	}
	// The appended line is found by the ordinary search.
	for _, k := range []string{"/", "n", "e", "e", "d", "l", "e"} {
		m, _ = m.Update(tea.KeyPressMsg{Text: k, Code: rune(k[0])})
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if line, _ := m.Cursor(); line != 61 {
		t.Fatalf("search landed on line %d, want 61", line)
	}
	w := New()
	w.SetSize(80, 20)
	w.NewFile("")
	if cmd := w.AppendReadOnly("x"); cmd != nil || w.Text() != "" {
		t.Fatal("a writable buffer is not appended to")
	}
}
