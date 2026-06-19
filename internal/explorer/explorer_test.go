package explorer

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// tree builds: root/{a.txt, b.txt, sub/c.txt}
func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), "a")
	mustWrite(t, filepath.Join(root, "b.txt"), "b")
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "sub", "c.txt"), "c")
	return root
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func send(m Model, keys ...tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	for _, k := range keys {
		m, cmd = m.Update(k)
	}
	return m, cmd
}

// names returns the visible row labels for assertions.
func names(m Model) []string {
	out := make([]string, len(m.rows))
	for i, n := range m.rows {
		out[i] = n.name
	}
	return out
}

func TestRootExpandedWithChildren(t *testing.T) {
	root := tree(t)
	m := New(root)
	m.SetSize(30, 20)
	// row 0 = root, then sub/ (dir first), a.txt, b.txt
	want := []string{filepath.Base(root), "sub", "a.txt", "b.txt"}
	got := names(m)
	if len(got) != len(want) {
		t.Fatalf("rows = %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %q want %q", i, got[i], want[i])
		}
	}
}

func TestExpandCollapseDirInPlace(t *testing.T) {
	root := tree(t)
	m := New(root)
	m.SetSize(30, 20)
	m.SetFocused(true)
	// cursor on "sub" (index 1), expand it: c.txt appears beneath, root unchanged
	m, _ = send(m, key("j"), key("enter"))
	if m.Root() != root {
		t.Fatalf("root changed to %q", m.Root())
	}
	got := names(m)
	want := []string{filepath.Base(root), "sub", "c.txt", "a.txt", "b.txt"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expanded rows = %v want %v", got, want)
		}
	}
	// collapse again
	m, _ = send(m, key("enter"))
	if len(m.rows) != 4 {
		t.Fatalf("after collapse rows = %v", names(m))
	}
}

func TestNeverAscendsAboveRoot(t *testing.T) {
	root := tree(t)
	m := New(root)
	m.SetSize(30, 20)
	m.SetFocused(true)
	// h on the root node must not change the root or move anywhere illegal
	m, _ = send(m, key("h"), key("h"), key("h"))
	if m.Root() != root {
		t.Fatalf("root escaped to %q", m.Root())
	}
	if m.cursor != 0 {
		t.Fatalf("cursor = %d want 0", m.cursor)
	}
}

func TestCollapseWithH(t *testing.T) {
	root := tree(t)
	m := New(root)
	m.SetSize(30, 20)
	m.SetFocused(true)
	// expand sub, then h on c.txt jumps to parent (sub), h again collapses sub
	m, _ = send(m, key("j"), key("l")) // sub expanded
	m, _ = send(m, key("j"))           // on c.txt
	if m.current().name != "c.txt" {
		t.Fatalf("cursor on %q want c.txt", m.current().name)
	}
	m, _ = send(m, key("h")) // jump to parent sub
	if m.current().name != "sub" {
		t.Fatalf("after h cursor on %q want sub", m.current().name)
	}
	m, _ = send(m, key("h")) // collapse sub
	if m.current().name != "sub" || m.rows[m.cursor].expanded {
		t.Fatalf("sub should be collapsed")
	}
	if len(m.rows) != 4 {
		t.Fatalf("rows after collapse = %v", names(m))
	}
}

func TestOpenFileEmitsMsg(t *testing.T) {
	root := tree(t)
	m := New(root)
	m.SetSize(30, 20)
	// move down to a.txt (root, sub, a.txt = index 2) and open
	m, _ = send(m, key("j"), key("j"))
	if m.current().name != "a.txt" {
		t.Fatalf("cursor on %q want a.txt", m.current().name)
	}
	_, cmd := send(m, key("enter"))
	if cmd == nil {
		t.Fatal("opening a file should emit a command")
	}
	msg, ok := cmd().(OpenFileMsg)
	if !ok {
		t.Fatalf("msg = %T want OpenFileMsg", cmd())
	}
	if msg.Path != filepath.Join(root, "a.txt") {
		t.Fatalf("path = %q", msg.Path)
	}
}
