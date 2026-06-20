package explorer

import (
	"os"
	"path/filepath"
	"strings"
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

// pumpScans drives the async scan loop to quiescence: it runs cmd, feeds any
// ScanDoneMsg straight back into Update (so directory children load), and stops
// at the first non-scan message, returning a Cmd that re-emits it so callers can
// still inspect an OpenFileMsg. Directory scans are a tea.Cmd now, so tests must
// pump them to observe loaded children.
func pumpScans(m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			return m, nil
		}
		if sd, ok := msg.(ScanDoneMsg); ok {
			m, cmd = m.Update(sd)
			continue
		}
		return m, func() tea.Msg { return msg }
	}
	return m, nil
}

// mounted builds an explorer rooted at dir, sizes it, and drains the initial
// root scan so the children are visible.
func mounted(t *testing.T, dir string, w, h int) Model {
	t.Helper()
	m := New(dir)
	m.SetSize(w, h)
	m, _ = pumpScans(m, m.Init())
	return m
}

func send(m Model, keys ...tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	for _, k := range keys {
		m, cmd = m.Update(k)
		m, cmd = pumpScans(m, cmd)
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
	m := mounted(t, root, 30, 20)
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
	m := mounted(t, root, 30, 20)
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
	m := mounted(t, root, 30, 20)
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
	m := mounted(t, root, 30, 20)
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

// tall builds a root with n files so the tree overflows a short pane.
func tall(t *testing.T, n int) string {
	t.Helper()
	root := t.TempDir()
	for i := 0; i < n; i++ {
		mustWrite(t, filepath.Join(root, "file"+string(rune('a'+i))+".txt"), "x")
	}
	return root
}

func TestMouseClickSelectsRow(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 30, 20)
	m.SetFocused(true)
	// rows: root(0) sub(1) a.txt(2) b.txt(3); click local y=2 selects a.txt.
	m, cmd := m.MouseClick(0, 2)
	if m.current().name != "a.txt" {
		t.Fatalf("cursor on %q want a.txt", m.current().name)
	}
	if cmd == nil {
		t.Fatal("clicking a file should emit an open command")
	}
	if msg, ok := cmd().(OpenFileMsg); !ok || msg.Path != filepath.Join(root, "a.txt") {
		t.Fatalf("open msg = %#v", cmd())
	}
}

func TestMouseClickTogglesDir(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 30, 20)
	m.SetFocused(true)
	// click "sub" (y=1) expands it in place; c.txt appears beneath (scan is async).
	m, c := m.MouseClick(0, 1)
	m, _ = pumpScans(m, c)
	if got := names(m); len(got) != 5 || got[2] != "c.txt" {
		t.Fatalf("after click rows = %v", names(m))
	}
	// click again collapses.
	m, _ = m.MouseClick(0, 1)
	if len(m.rows) != 4 {
		t.Fatalf("after second click rows = %v", names(m))
	}
}

func TestWheelScrollsWithoutMovingCursor(t *testing.T) {
	root := tall(t, 30)
	m := mounted(t, root, 30, 8) // 31 rows into 8 → vertical overflow
	cur := m.cursor
	m.ScrollBy(5)
	if m.offset != 5 {
		t.Fatalf("offset = %d want 5", m.offset)
	}
	if m.cursor != cur {
		t.Fatalf("wheel moved cursor to %d", m.cursor)
	}
	// cannot scroll above the top.
	m.ScrollBy(-100)
	if m.offset != 0 {
		t.Fatalf("offset = %d want 0", m.offset)
	}
}

func TestVerticalScrollbarRendersOnOverflow(t *testing.T) {
	root := tall(t, 30)
	m := mounted(t, root, 30, 8)
	_, _, needV, _, _ := m.viewport()
	if !needV {
		t.Fatal("expected vertical overflow")
	}
	if !strings.Contains(m.View(), "┃") { // the heavy thumb glyph (│ also appears as an indent guide)
		t.Fatalf("vertical scrollbar thumb missing:\n%s", m.View())
	}
}

func TestScrollbarHiddenWhenFits(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	if _, _, needV, needH, _ := m.viewport(); needV || needH {
		t.Fatalf("no scrollbars expected for small tree: V=%v H=%v", needV, needH)
	}
}

func TestClickVerticalScrollbarJumps(t *testing.T) {
	root := tall(t, 30)
	m := mounted(t, root, 30, 8)
	textW, textH, needV, _, _ := m.viewport()
	if !needV {
		t.Fatal("need vertical bar")
	}
	// click the bottom of the scrollbar column → jump near the end.
	m, _ = m.MouseClick(textW, textH-1)
	maxOff := len(m.rows) - textH
	if m.offset != maxOff {
		t.Fatalf("offset = %d want %d (max)", m.offset, maxOff)
	}
}

func TestHoverHighlightsRowUnderPointer(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 30, 20)
	m.SetHoverAt(0, 2) // a.txt
	if m.hover != 2 {
		t.Fatalf("hover = %d want 2", m.hover)
	}
	// pointer off a content row clears it.
	m.SetHoverAt(0, 99)
	if m.hover != -1 {
		t.Fatalf("hover = %d want -1 after leaving", m.hover)
	}
	m.SetHoverAt(0, 1)
	m.ClearHover()
	if m.hover != -1 {
		t.Fatalf("hover = %d want -1 after ClearHover", m.hover)
	}
}

func TestActiveFileHighlighted(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 30, 20)
	m.SetFocused(false) // active highlight is independent of focus
	// rows: root(0) sub(1) a.txt(2) b.txt(3)
	m.SetActive(filepath.Join(root, "a.txt"))
	if k := m.rowKind(2); k != rowActive {
		t.Fatalf("a.txt kind = %d want rowActive", k)
	}
	if k := m.rowKind(3); k != rowPlain {
		t.Fatalf("b.txt kind = %d want rowPlain", k)
	}
	m.SetActive("")
	if k := m.rowKind(2); k != rowPlain {
		t.Fatalf("a.txt kind after clear = %d want rowPlain", k)
	}
}

func TestHighlightPrecedence(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 30, 20)
	m.SetFocused(true)
	m.SetActive(filepath.Join(root, "a.txt"))
	// a.txt is row 2: active by file, hover by pointer, selected by cursor.
	m.cursor = 2
	m.hover = 2
	if k := m.rowKind(2); k != rowSelected {
		t.Fatalf("cursor row kind = %d want rowSelected (cursor wins)", k)
	}
	m.SetFocused(false) // no focus → cursor highlight yields to hover
	if k := m.rowKind(2); k != rowHover {
		t.Fatalf("kind = %d want rowHover (hover beats active)", k)
	}
	m.hover = -1 // no hover → active shows
	if k := m.rowKind(2); k != rowActive {
		t.Fatalf("kind = %d want rowActive", k)
	}
}

func TestHorizontalScrollClampsAndShowsBar(t *testing.T) {
	root := t.TempDir()
	long := "this_is_a_very_long_file_name_that_overflows_the_pane.txt"
	mustWrite(t, filepath.Join(root, long), "x")
	m := mounted(t, root, 12, 20) // narrower than the long name
	if _, _, _, needH, _ := m.viewport(); !needH {
		t.Fatal("expected horizontal overflow")
	}
	m.ScrollXBy(4)
	if m.offsetX != 4 {
		t.Fatalf("offsetX = %d want 4", m.offsetX)
	}
	m.ScrollXBy(-100) // cannot scroll left of column 0
	if m.offsetX != 0 {
		t.Fatalf("offsetX = %d want 0", m.offsetX)
	}
	m.ScrollXBy(1000) // cannot scroll past the content
	textW, _, _, _, contentW := m.viewport()
	if m.offsetX != contentW-textW {
		t.Fatalf("offsetX = %d want max %d", m.offsetX, contentW-textW)
	}
	if !strings.ContainsAny(m.View(), "━─") {
		t.Fatalf("horizontal scrollbar glyphs missing:\n%s", m.View())
	}
}

func TestOpenFileEmitsMsg(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 30, 20)
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
