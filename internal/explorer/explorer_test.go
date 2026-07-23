package explorer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	default:
		return tea.KeyPressMsg{Text: s}
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
	pending := []tea.Cmd{cmd}
	for len(pending) > 0 {
		cmd, pending = pending[0], pending[1:]
		if cmd == nil {
			continue
		}
		msg := cmd()
		if msg == nil {
			continue
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			pending = append(pending, batch...)
			continue
		}
		if sd, ok := msg.(ScanDoneMsg); ok {
			var next tea.Cmd
			m, next = m.Update(sd)
			pending = append(pending, next)
			continue
		}
		return m, func() tea.Msg { return msg }
	}
	return m, nil
}

// mounted builds an explorer rooted at dir, sizes it, and drains the initial
// root scan so the children are visible. Auto-refresh is disabled: its poll Cmd
// sleeps for the poll interval, which would stall every test that pumps Cmds.
func mounted(t *testing.T, dir string, w, h int) Model {
	t.Helper()
	m := New(dir)
	m.autoRefresh = false
	m.SetSize(w, h)
	m, _ = pumpScans(m, m.Init())
	return m
}

// clickAt simulates a mouse press with a controlled clock so tests can produce
// exact single- and double-clicks. gap is the delay since the previous click.
func clickAt(m Model, x, y int, gap time.Duration) (Model, tea.Cmd) {
	now := m.now().Add(gap)
	m.now = func() time.Time { return now }
	return m.MouseClick(x, y)
}

func send(m Model, keys ...tea.KeyPressMsg) (Model, tea.Cmd) {
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

func TestNewFileCreatesAndSelects(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, cmd := m.Update(NewFileMsg{}) // cursor on root -> create inside root
	m, _ = pumpScans(m, cmd)
	if !m.Prompting() {
		t.Fatal("expected name prompt to open")
	}
	m, _ = send(m, key("new.txt"), key("enter"))
	if m.Prompting() {
		t.Fatal("prompt should close on submit")
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if c := m.current(); c == nil || c.name != "new.txt" {
		t.Fatalf("cursor not on new file: %v", names(m))
	}
}

func TestNewFolderCreates(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, cmd := m.Update(NewDirMsg{})
	m, _ = pumpScans(m, cmd)
	m, _ = send(m, key("pkg"), key("enter"))
	info, err := os.Stat(filepath.Join(root, "pkg"))
	if err != nil || !info.IsDir() {
		t.Fatalf("folder not created: %v", err)
	}
}

func TestDeleteMovesToTrashAndUndoRestores(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j")) // root, sub, a.txt
	if c := m.current(); c == nil || c.name != "a.txt" {
		t.Fatalf("cursor not on a.txt: %v", names(m))
	}
	m, cmd := m.Update(DeleteMsg{})
	m, _ = pumpScans(m, cmd)
	m, _ = send(m, key("y")) // confirm delete
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("a.txt should be deleted, err=%v", err)
	}
	m, cmd = m.Update(UndoMsg{}) // undo applies instantly, no confirmation
	m, _ = pumpScans(m, cmd)
	if m.Prompting() {
		t.Fatal("undo must not open a confirmation prompt")
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("a.txt should be restored: %v", err)
	}
	m, cmd = m.Update(RedoMsg{}) // redo re-deletes
	m, _ = pumpScans(m, cmd)
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("a.txt should be re-deleted by redo, err=%v", err)
	}
	m, cmd = m.Update(UndoMsg{}) // and undo restores again
	pumpScans(m, cmd)
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("a.txt should be restored again: %v", err)
	}
}

func TestUndoCreateDeletesFile(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, cmd := m.Update(NewFileMsg{})
	m, _ = pumpScans(m, cmd)
	m, _ = send(m, key("tmp.txt"), key("enter"))
	if _, err := os.Stat(filepath.Join(root, "tmp.txt")); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	m, cmd = m.Update(UndoMsg{})
	m, _ = pumpScans(m, cmd)
	if _, err := os.Stat(filepath.Join(root, "tmp.txt")); !os.IsNotExist(err) {
		t.Fatalf("tmp.txt should be removed by undo, err=%v", err)
	}
	// The undone create sits in the trash, so redo can bring it back.
	m, cmd = m.Update(RedoMsg{})
	pumpScans(m, cmd)
	if _, err := os.Stat(filepath.Join(root, "tmp.txt")); err != nil {
		t.Fatalf("tmp.txt should be restored by redo: %v", err)
	}
}

func TestUndoRedoRename(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j")) // a.txt
	m, cmd := m.Update(RenameMsg{})
	m, _ = pumpScans(m, cmd)
	for range len("a.txt") {
		m, _ = send(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	m, _ = send(m, key("z.txt"), key("enter"))
	if _, err := os.Stat(filepath.Join(root, "z.txt")); err != nil {
		t.Fatalf("rename failed: %v", err)
	}
	m, cmd = m.Update(UndoMsg{})
	m, _ = pumpScans(m, cmd)
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("undo should rename back to a.txt: %v", err)
	}
	m, cmd = m.Update(RedoMsg{})
	pumpScans(m, cmd)
	if _, err := os.Stat(filepath.Join(root, "z.txt")); err != nil {
		t.Fatalf("redo should rename to z.txt again: %v", err)
	}
}

func TestNewOperationClearsRedo(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, cmd := m.Update(NewFileMsg{})
	m, _ = pumpScans(m, cmd)
	m, _ = send(m, key("one.txt"), key("enter"))
	m, cmd = m.Update(UndoMsg{})
	m, _ = pumpScans(m, cmd)
	if len(m.redoOps) != 1 {
		t.Fatalf("redo stack = %d want 1", len(m.redoOps))
	}
	m, cmd = m.Update(NewFileMsg{})
	m, _ = pumpScans(m, cmd)
	m, _ = send(m, key("two.txt"), key("enter"))
	if len(m.redoOps) != 0 {
		t.Fatal("a new operation must clear the redo stack")
	}
}

func TestRenamePrefillsAndRenames(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j")) // a.txt
	m, cmd := m.Update(RenameMsg{})
	m, _ = pumpScans(m, cmd)
	if !m.Prompting() {
		t.Fatal("expected rename prompt to open")
	}
	if m.prompt.input != "a.txt" {
		t.Fatalf("prompt input = %q want prefilled %q", m.prompt.input, "a.txt")
	}
	// clear the prefilled name and type a new one.
	for range "a.txt" {
		m, _ = send(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	m, _ = send(m, key("renamed.txt"), key("enter"))
	if m.Prompting() {
		t.Fatal("prompt should close on submit")
	}
	if _, err := os.Stat(filepath.Join(root, "renamed.txt")); err != nil {
		t.Fatalf("renamed.txt not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("a.txt should no longer exist, err=%v", err)
	}
	if c := m.current(); c == nil || c.name != "renamed.txt" {
		t.Fatalf("cursor not on renamed file: %v", names(m))
	}
}

func TestRenameArrowKeysMoveCursor(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j")) // a.txt
	m, cmd := m.Update(RenameMsg{})
	m, _ = pumpScans(m, cmd)
	if m.prompt.pos != len("a.txt") {
		t.Fatalf("pos = %d want %d (end)", m.prompt.pos, len("a.txt"))
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.prompt.pos != len("a.txt")-2 {
		t.Fatalf("pos after 2x left = %d want %d", m.prompt.pos, len("a.txt")-2)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	if m.prompt.pos != 0 {
		t.Fatalf("pos after Home = %d want 0", m.prompt.pos)
	}
	// typing at pos 0 inserts before the existing text.
	m, _ = send(m, key("x"))
	if m.prompt.input != "xa.txt" || m.prompt.pos != 1 {
		t.Fatalf("input = %q pos = %d, want %q pos 1", m.prompt.input, m.prompt.pos, "xa.txt")
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if m.prompt.pos != len("xa.txt") {
		t.Fatalf("pos after End = %d want %d", m.prompt.pos, len("xa.txt"))
	}
	// forward-delete at End is a no-op; Left then Delete removes "t".
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if m.prompt.input != "xa.tx" {
		t.Fatalf("input after Left+Delete = %q want %q", m.prompt.input, "xa.tx")
	}
}

func TestPromptMouseClickMovesCursor(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j")) // a.txt
	m, cmd := m.Update(RenameMsg{})
	m, _ = pumpScans(m, cmd)
	bx, by, _, _, ok := m.promptBoxOrigin()
	if !ok {
		t.Fatal("expected prompt box to fit the pane")
	}
	inputRow := by + 2
	textX := bx + 2 + len(promptInputPrefix)
	m.PromptMouseClick(textX+2, inputRow)
	if m.prompt.pos != 2 {
		t.Fatalf("pos after click at col 2 = %d want 2", m.prompt.pos)
	}
	// clicking past the end of the text clamps to the text length.
	m.PromptMouseClick(textX+999, inputRow)
	if m.prompt.pos != len("a.txt") {
		t.Fatalf("pos after far click = %d want %d", m.prompt.pos, len("a.txt"))
	}
	// clicking off the input row is a no-op.
	m.PromptMouseClick(textX+2, inputRow+1)
	if m.prompt.pos != len("a.txt") {
		t.Fatalf("click off input row should not move cursor, pos = %d", m.prompt.pos)
	}
}

func TestRenameEscCancels(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j")) // a.txt
	m, cmd := m.Update(RenameMsg{})
	m, _ = pumpScans(m, cmd)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.Prompting() {
		t.Fatal("esc should cancel the rename prompt")
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("a.txt must survive a cancelled rename: %v", err)
	}
}

func TestRenameToExistingNameErrors(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j")) // a.txt
	m, cmd := m.Update(RenameMsg{})
	m, _ = pumpScans(m, cmd)
	for range "a.txt" {
		m, _ = send(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	m, _ = send(m, key("b.txt"), key("enter"))
	if m.err == nil {
		t.Fatal("expected an error renaming onto an existing name")
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("a.txt should be untouched: %v", err)
	}
}

func TestRenameRootIsNoOp(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = m.Update(RenameMsg{}) // cursor starts on root
	if m.Prompting() {
		t.Fatal("renaming the root should be a no-op")
	}
}

func TestDeleteCancelKeepsFile(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j")) // a.txt
	m, cmd := m.Update(DeleteMsg{})
	m, _ = pumpScans(m, cmd)
	m, _ = send(m, key("n")) // decline
	if m.Prompting() {
		t.Fatal("prompt should close on decline")
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("a.txt must survive a cancelled delete: %v", err)
	}
}

func TestNewFileEscCancels(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, cmd := m.Update(NewFileMsg{})
	m, _ = pumpScans(m, cmd)
	m, _ = send(m, key("ab"))
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.Prompting() {
		t.Fatal("esc should cancel the name prompt")
	}
	if _, err := os.Stat(filepath.Join(root, "ab")); !os.IsNotExist(err) {
		t.Fatal("no file should be created on cancel")
	}
}

func TestUndoEmptyIsNoOp(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = m.Update(UndoMsg{})
	if m.Prompting() {
		t.Fatal("undo with empty stack must not open a prompt")
	}
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

func TestMouseSingleClickOnlySelects(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 30, 20)
	m.SetFocused(true)
	// rows: root(0) sub(1) a.txt(2) b.txt(3); a single click on a.txt selects it
	// without opening it.
	m, cmd := clickAt(m, 5, 2, 0)
	if m.current().name != "a.txt" {
		t.Fatalf("cursor on %q want a.txt", m.current().name)
	}
	if cmd != nil {
		t.Fatalf("single click must not open a file, got %#v", cmd())
	}
	// Two slow clicks (past the double-click window) still only select.
	m, cmd = clickAt(m, 5, 2, doubleClickWindow+time.Millisecond)
	if cmd != nil {
		t.Fatalf("slow second click must not open a file, got %#v", cmd())
	}
}

func TestMouseDoubleClickOpensFile(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 30, 20)
	m.SetFocused(true)
	m, _ = clickAt(m, 5, 2, 0)
	m, cmd := clickAt(m, 5, 2, doubleClickWindow/2)
	if cmd == nil {
		t.Fatal("double-clicking a file should emit an open command")
	}
	if msg, ok := cmd().(OpenFileMsg); !ok || msg.Path != filepath.Join(root, "a.txt") {
		t.Fatalf("open msg = %#v", cmd())
	}
}

func TestMouseDoubleClickTogglesDir(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 30, 20)
	m.SetFocused(true)
	// double-click "sub" (y=1, off the caret) expands it; c.txt appears beneath.
	m, _ = clickAt(m, 5, 1, 0)
	m, c := clickAt(m, 5, 1, doubleClickWindow/2)
	m, _ = pumpScans(m, c)
	if got := names(m); len(got) != 5 || got[2] != "c.txt" {
		t.Fatalf("after double-click rows = %v", names(m))
	}
	// a second double-click collapses.
	m, _ = clickAt(m, 5, 1, time.Second)
	m, _ = clickAt(m, 5, 1, doubleClickWindow/2)
	if len(m.rows) != 4 {
		t.Fatalf("after second double-click rows = %v", names(m))
	}
}

func TestMouseCaretClickTogglesDirInstantly(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 30, 20)
	m.SetFocused(true)
	// "sub" sits at depth 1 with indent 2: its caret occupies columns 2-3.
	m, c := clickAt(m, 2, 1, 0)
	m, _ = pumpScans(m, c)
	if got := names(m); len(got) != 5 || got[2] != "c.txt" {
		t.Fatalf("after caret click rows = %v", names(m))
	}
	m, _ = clickAt(m, 2, 1, time.Second)
	if len(m.rows) != 4 {
		t.Fatalf("after second caret click rows = %v", names(m))
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
	m.hover = -1 // no hover → the unfocused cursor stays visible (#1034)
	if k := m.rowKind(2); k != rowCursorIdle {
		t.Fatalf("kind = %d want rowCursorIdle", k)
	}
	m.cursor = 0 // cursor elsewhere → active shows
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
	if msg.NewPane {
		t.Fatal("plain open should leave NewPane false")
	}
}

// TestModifiedOpenRequestsNewPane verifies the "o" action emits an open with the
// NewPane intent set, while a directory yields no command.
func TestModifiedOpenRequestsNewPane(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 30, 20)
	m, _ = send(m, key("j"), key("j")) // onto a.txt
	if m.current().name != "a.txt" {
		t.Fatalf("cursor on %q want a.txt", m.current().name)
	}
	_, cmd := send(m, key("o"))
	if cmd == nil {
		t.Fatal("o on a file should emit a command")
	}
	msg, ok := cmd().(OpenFileMsg)
	if !ok || !msg.NewPane {
		t.Fatalf("want OpenFileMsg{NewPane:true}, got %#v", cmd())
	}
	// On a directory "o" is a no-op.
	m2 := mounted(t, root, 30, 20)
	m2, _ = send(m2, key("k")) // park on the root directory
	if !m2.current().isDir {
		t.Skip("setup: cursor not on a directory")
	}
	if _, cmd := send(m2, key("o")); cmd != nil {
		t.Fatal("o on a directory should be a no-op")
	}
}

func TestRefreshPreservesExpansion(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("l")) // expand sub
	if got := names(m); len(got) != 5 {
		t.Fatalf("setup: sub not expanded: %v", got)
	}
	// A file appears externally; a manual refresh from the root must pick it up
	// without collapsing sub.
	mustWrite(t, filepath.Join(root, "sub", "d.txt"), "d")
	m, _ = send(m, key("k")) // back on root
	m, cmd := m.Update(RefreshMsg{})
	m, _ = pumpScans(m, cmd)
	got := names(m)
	if len(got) != 6 || got[3] != "d.txt" {
		t.Fatalf("refresh lost expansion or the new file: %v", got)
	}
}

func TestPollDetectsExternalChanges(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.autoRefresh = true
	m.pollEvery = time.Millisecond
	cmd := m.startPoll()
	if cmd == nil {
		t.Fatal("startPoll should schedule a poll")
	}
	mustWrite(t, filepath.Join(root, "fresh.txt"), "x")
	msg, ok := cmd().(pollMsg)
	if !ok {
		t.Fatalf("poll cmd returned %#v", msg)
	}
	found := false
	for _, p := range msg.changed {
		if p == root {
			found = true
		}
	}
	if !found {
		t.Fatalf("poll missed the root change: %v", msg.changed)
	}
	m, cmd = m.Update(msg)
	m, _ = pumpScans(m, cmd)
	for _, n := range names(m) {
		if n == "fresh.txt" {
			return
		}
	}
	t.Fatalf("fresh.txt not visible after poll refresh: %v", names(m))
}

func TestSetOpenClearsStaleActive(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt")
	m.SetActive(a)
	m.SetOpen([]string{a, b})
	if !m.IsOpen(a) || !m.IsOpen(b) {
		t.Fatal("both files should be marked open")
	}
	if m.Active() != a {
		t.Fatalf("active = %q want %q", m.Active(), a)
	}
	// a closes: it drops from the open set, and the stale active mark clears.
	m.SetOpen([]string{b})
	if m.IsOpen(a) {
		t.Fatal("a.txt should no longer be open")
	}
	if m.Active() != "" {
		t.Fatalf("stale active = %q want cleared", m.Active())
	}
}

// #373: a prompt whose natural box is wider than the pane must still render
// (truncated), never disappear while it keeps capturing keys.
func TestDeletePromptRendersWiderThanPane(t *testing.T) {
	root := t.TempDir()
	long := "a-very-long-filename-that-overflows.go"
	mustWrite(t, filepath.Join(root, long), "x")
	m := mounted(t, root, 28, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j")) // select the file
	m, cmd := m.Update(DeleteMsg{})
	m, _ = pumpScans(m, cmd)
	if !m.Prompting() {
		t.Fatal("delete confirmation prompt should be open")
	}
	_, _, w, _, ok := m.promptBoxOrigin()
	if !ok {
		t.Fatal("an active prompt must always have a render origin")
	}
	if w > 28 {
		t.Fatalf("prompt box width = %d, must fit pane width 28", w)
	}
	view := m.View()
	if !strings.Contains(view, "…") {
		t.Fatalf("truncated prompt title should end in an ellipsis:\n%s", view)
	}
	if !strings.Contains(view, "[y]es") {
		t.Fatalf("confirm row missing — prompt would capture keys invisibly:\n%s", view)
	}
}

// #373: the rename input windows horizontally so the cursor (at the end of the
// prefilled name) stays visible, and typing still lands at the cursor.
func TestRenamePromptWindowsLongInput(t *testing.T) {
	root := t.TempDir()
	long := "a-very-long-filename-that-overflows.go"
	mustWrite(t, filepath.Join(root, long), "x")
	m := mounted(t, root, 28, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"))
	m, cmd := m.Update(RenameMsg{})
	m, _ = pumpScans(m, cmd)
	if m.prompt == nil || m.prompt.input != long {
		t.Fatalf("rename prompt should be open prefilled with %q", long)
	}
	_, _, w, _, ok := m.promptBoxOrigin()
	if !ok || w > 28 {
		t.Fatalf("prompt box ok=%v width=%d, want rendered and <= pane width 28", ok, w)
	}
	// the window slides to the cursor at the end of the name: the tail is visible.
	if !strings.Contains(m.View(), "overflows.go") {
		t.Fatalf("input window should show the text around the cursor:\n%s", m.View())
	}
	m, _ = send(m, key("z"))
	if m.prompt.input != long+"z" {
		t.Fatalf("input after typing = %q want %q", m.prompt.input, long+"z")
	}
}

// #373: a click on the input row maps through the window offset, so the cursor
// lands on the clicked rune even when the input is horizontally scrolled.
func TestPromptMouseClickWithWindowOffset(t *testing.T) {
	root := t.TempDir()
	long := "a-very-long-filename-that-overflows.go"
	mustWrite(t, filepath.Join(root, long), "x")
	m := mounted(t, root, 28, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"))
	m, cmd := m.Update(RenameMsg{})
	m, _ = pumpScans(m, cmd)
	off, _ := m.promptInputWindow()
	if off == 0 {
		t.Fatal("test setup: expected a scrolled input window")
	}
	bx, by, _, _, _ := m.promptBoxOrigin()
	inputRow := by + 2
	textX := bx + 2 + len(promptInputPrefix)
	m.PromptMouseClick(textX+3, inputRow)
	if want := off + 3; m.prompt.pos != want {
		t.Fatalf("pos after click = %d want %d", m.prompt.pos, want)
	}
}

// TestHiddenOnlyDirNoPanic guards #949: a project whose root holds only
// hidden entries (e.g. just .git) renders a single root row; stepping "into"
// the root must not advance the cursor past the rows slice, and a refresh
// afterwards must not panic in current().
func TestHiddenOnlyDirNoPanic(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := mounted(t, root, 40, 12)
	if len(m.rows) != 1 {
		t.Fatalf("fixture: rows = %d, want only the root", len(m.rows))
	}
	// Root is expanded with one (hidden) child: "l" used to run cursor to 1.
	m, _ = send(m, key("l"))
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (no visible child)", m.cursor)
	}
	// The crash path (#949): refresh goes through current().
	m, cmd := m.Update(RefreshMsg{})
	_, _ = pumpScans(m, cmd)
}

// TestCurrentHealsStaleCursor covers the defensive clamp in current(): a
// cursor beyond the rows slice (however it got there) clamps instead of
// panicking.
func TestCurrentHealsStaleCursor(t *testing.T) {
	m := mounted(t, tree(t), 40, 12)
	m.cursor = len(m.rows) + 5
	n := m.current()
	if n == nil {
		t.Fatal("current must return the clamped row, not nil")
	}
	if m.cursor != len(m.rows)-1 {
		t.Fatalf("cursor = %d, want %d", m.cursor, len(m.rows)-1)
	}
}
