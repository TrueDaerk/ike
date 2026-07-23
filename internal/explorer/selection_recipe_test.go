package explorer

import (
	"testing"

	"ike/internal/theme"
	"ike/internal/vcs"
)

// TestSelectionKeepsSemanticForeground guards #1052: the focused cursor adds
// the Selection background + bold but keeps the row's semantic foreground —
// a modified file stays in its VCS hue under the cursor, matching the
// structure/problems/VCS lists.
func TestSelectionKeepsSemanticForeground(t *testing.T) {
	m := New(".")
	n := &node{name: "main.go", path: "main.go"}
	m.rows = []*node{n}
	m.focused = true
	m.SetVCS(vcs.NewSnapshot(".", map[string]vcs.FileStatus{"main.go": vcs.StatusModified}))
	got := m.rowStyle(0, n)
	if rgb(got.GetForeground()) != rgb(theme.DefaultPalette().VCSModified) {
		t.Fatalf("selected modified row fg = %v want VCSModified", got.GetForeground())
	}
	if rgb(got.GetBackground()) != rgb(theme.DefaultPalette().Selection) {
		t.Fatalf("selected row bg = %v want Selection", got.GetBackground())
	}
	if !got.GetBold() {
		t.Fatal("selected row must stay bold")
	}
}

// TestUnfocusedCursorMuted guards #1034: an unfocused explorer keeps a muted
// cursor row (SelectionMuted background, no bold) instead of hiding it.
func TestUnfocusedCursorMuted(t *testing.T) {
	m := New(".")
	n := &node{name: "main.go", path: "main.go"}
	m.rows = []*node{n}
	m.focused = false
	if k := m.rowKind(0); k != rowCursorIdle {
		t.Fatalf("kind = %d want rowCursorIdle", k)
	}
	got := m.rowStyle(0, n)
	if rgb(got.GetBackground()) != rgb(theme.DefaultPalette().SelectionMuted) {
		t.Fatalf("idle cursor bg = %v want SelectionMuted", got.GetBackground())
	}
	if got.GetBold() {
		t.Fatal("idle cursor row must not be bold")
	}
}
