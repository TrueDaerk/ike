package explorer

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// onEntry mounts the tree and parks the cursor on name, so a duplicate test
// reads as "stand here, press cmd+d".
func onEntry(t *testing.T, root, name string) Model {
	t.Helper()
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	path := filepath.Join(root, name)
	for i, r := range m.rows {
		if r.path == path {
			m.cursor = i
			return m
		}
	}
	t.Fatalf("row %q not found", name)
	return m
}

// TestDuplicateCopiesAndOpensRename is the whole gesture of #2697: the copy
// lands next to the original, the cursor moves onto it, and the rename prompt
// is already open on the copy with its stem preselected.
func TestDuplicateCopiesAndOpensRename(t *testing.T) {
	root := tree(t)
	m := onEntry(t, root, "a.txt")

	m, cmd := m.Update(DuplicateMsg{})
	m, _ = pumpScans(m, cmd)

	dest := filepath.Join(root, "a-copy.txt")
	if data, err := os.ReadFile(dest); err != nil || string(data) != "a" {
		t.Fatalf("copy = %q, %v; want the source's content", data, err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("the original must stay: %v", err)
	}
	if got := m.cursorPath(); got != dest {
		t.Fatalf("cursor = %q, want the copy %q", got, dest)
	}
	p := m.prompt
	if p == nil || p.kind != promptInput {
		t.Fatal("a duplicate must open the rename prompt on the copy")
	}
	if p.input.Text != "a-copy.txt" {
		t.Fatalf("prompt input = %q, want the copy's name", p.input.Text)
	}
	if p.anchor != dest {
		t.Fatalf("prompt anchor = %q, want the copy %q", p.anchor, dest)
	}
	// The stem is preselected, JetBrains-style (#1047): typing replaces
	// "a-copy" and keeps ".txt".
	if p.selStart != 0 || p.selEnd != len("a-copy") {
		t.Fatalf("preselection = [%d,%d), want the stem [0,%d)", p.selStart, p.selEnd, len("a-copy"))
	}
}

// TestDuplicateRenameHandOffAcceptsTypedName: typing over the preselected stem
// and pressing enter renames the copy — the gesture the prompt exists for.
func TestDuplicateRenameHandOffAcceptsTypedName(t *testing.T) {
	root := tree(t)
	m := onEntry(t, root, "a.txt")

	m, cmd := m.Update(DuplicateMsg{})
	m, _ = pumpScans(m, cmd)
	m, _ = send(m, key("draft"), key("enter"))

	if _, err := os.Stat(filepath.Join(root, "draft.txt")); err != nil {
		t.Fatalf("the typed name must rename the copy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a-copy.txt")); err == nil {
		t.Fatal("the -copy name must be gone after the rename")
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("the original must stay: %v", err)
	}
}

// TestDuplicateEscKeepsCopyName: esc cancels the rename, not the duplicate —
// the copy is already on disk and keeps its "-copy" name.
func TestDuplicateEscKeepsCopyName(t *testing.T) {
	root := tree(t)
	m := onEntry(t, root, "a.txt")

	m, cmd := m.Update(DuplicateMsg{})
	m, _ = pumpScans(m, cmd)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if m.prompt != nil {
		t.Fatal("esc must close the rename prompt")
	}
	if data, err := os.ReadFile(filepath.Join(root, "a-copy.txt")); err != nil || string(data) != "a" {
		t.Fatalf("the copy must survive esc: %q, %v", data, err)
	}
}

// TestDuplicateNumbersTakenNames: repeated duplicates of the same entry walk
// "-copy", "-copy-2", "-copy-3" and never overwrite what is already there.
func TestDuplicateNumbersTakenNames(t *testing.T) {
	root := tree(t)
	for i, want := range []string{"a-copy.txt", "a-copy-2.txt", "a-copy-3.txt"} {
		m := onEntry(t, root, "a.txt")
		m, cmd := m.Update(DuplicateMsg{})
		m, _ = pumpScans(m, cmd)
		if m.err != nil {
			t.Fatalf("duplicate %d failed: %v", i+1, m.err)
		}
		if data, err := os.ReadFile(filepath.Join(root, want)); err != nil || string(data) != "a" {
			t.Fatalf("duplicate %d = %q, %v; want %s with the source's content", i+1, data, err, want)
		}
	}
	// Nothing was clobbered on the way: every name is still there.
	for _, name := range []string{"a.txt", "a-copy.txt", "a-copy-2.txt", "a-copy-3.txt"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("%s must survive the suffix walk: %v", name, err)
		}
	}
}

// TestDuplicateSkipsOccupiedNameOfAnyKind: a taken "-copy" name is taken even
// when it is a directory or a dangling symlink, which os.Stat alone would miss.
func TestDuplicateSkipsOccupiedNameOfAnyKind(t *testing.T) {
	root := tree(t)
	if err := os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(root, "a-copy.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	m := onEntry(t, root, "a.txt")

	m, cmd := m.Update(DuplicateMsg{})
	m, _ = pumpScans(m, cmd)
	if m.err != nil {
		t.Fatalf("duplicate failed: %v", m.err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "a-copy-2.txt")); err != nil || string(data) != "a" {
		t.Fatalf("copy = %q, %v; want a-copy-2.txt beside the dangling link", data, err)
	}
	info, err := os.Lstat(filepath.Join(root, "a-copy.txt"))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the dangling link must be untouched: %v", err)
	}
}

// TestDuplicateDirectoryRecursesAndKeepsSymlinks: a folder duplicate copies the
// whole subtree, keeps links as links, and takes its suffix behind the whole
// name (a directory has no extension to protect).
func TestDuplicateDirectoryRecursesAndKeepsSymlinks(t *testing.T) {
	root := tree(t)
	nested := filepath.Join(root, "sub", "deep")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(nested, "d.txt"), "d")
	if err := os.Symlink("c.txt", filepath.Join(root, "sub", "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	m := onEntry(t, root, "sub")

	m, cmd := m.Update(DuplicateMsg{})
	m, _ = pumpScans(m, cmd)

	dest := filepath.Join(root, "sub-copy")
	if data, err := os.ReadFile(filepath.Join(dest, "deep", "d.txt")); err != nil || string(data) != "d" {
		t.Fatalf("nested file = %q, %v; want the recursive copy", data, err)
	}
	info, err := os.Lstat(filepath.Join(dest, "link.txt"))
	if err != nil {
		t.Fatalf("the symlink must be copied: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("a symlink must be copied as a link, not followed")
	}
	if m.prompt == nil || m.prompt.input.Text != "sub-copy" {
		t.Fatalf("rename prompt = %#v, want it open on sub-copy", m.prompt)
	}
	// A folder name has nothing to protect, so the whole name is preselected.
	if m.prompt.selEnd != len("sub-copy") {
		t.Fatalf("preselection ends at %d, want the whole folder name", m.prompt.selEnd)
	}
}

// TestDuplicateUndoRemovesOnlyTheCopy: the duplicate is one undo step (the
// copy's opCreate), and it leaves the original alone.
func TestDuplicateUndoRemovesOnlyTheCopy(t *testing.T) {
	root := tree(t)
	m := onEntry(t, root, "a.txt")

	m, cmd := m.Update(DuplicateMsg{})
	m, _ = pumpScans(m, cmd)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	m, cmd = m.Update(UndoMsg{})
	pumpScans(m, cmd)
	if _, err := os.Stat(filepath.Join(root, "a-copy.txt")); err == nil {
		t.Fatal("undo must remove the copy")
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("undo must leave the original alone: %v", err)
	}
}

// TestDuplicateRootIsNoOp: the project root has no sibling slot, so cmd+d on
// it does nothing at all — no copy, no prompt.
func TestDuplicateRootIsNoOp(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m.cursor = 0

	m, cmd := m.Update(DuplicateMsg{})
	m, _ = pumpScans(m, cmd)
	if m.prompt != nil {
		t.Fatalf("the root must not open a prompt: %#v", m.prompt)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), filepath.Base(root)+"-copy")); err == nil {
		t.Fatal("the root must not be duplicated")
	}
}

// TestDuplicateDestNames pins the naming rules the copy and the numbering
// share: the suffix goes before a file's extension, behind a directory's name
// and behind an extension-only name like ".env".
func TestDuplicateDestNames(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), "a")
	mustWrite(t, filepath.Join(root, ".env"), "e")
	if err := os.Mkdir(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		isDir bool
		want  string
	}{
		{"a.txt", false, "a-copy.txt"},
		{".env", false, ".env-copy"},
		{"pkg", true, "pkg-copy"},
	} {
		got, err := duplicateDest(filepath.Join(root, tc.name), tc.isDir)
		if err != nil {
			t.Fatalf("duplicateDest(%q): %v", tc.name, err)
		}
		if got != filepath.Join(root, tc.want) {
			t.Errorf("duplicateDest(%q) = %q, want %q", tc.name, got, tc.want)
		}
		// The first candidate is exactly the f5 prompt's prefill, so both
		// commands spell a duplicate the same way.
		if base := CopyDest(filepath.Join(root, tc.name), tc.isDir); base != filepath.Join(root, tc.want) {
			t.Errorf("CopyDest(%q) = %q, want %q", tc.name, base, tc.want)
		}
	}
}

// TestDuplicateInScratchIsNoOp: the Scratches section is flat and owns its own
// operations (#1963), so a duplicate there does nothing rather than half-work.
func TestDuplicateInScratchIsNoOp(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m.scrCursor = 0

	m, cmd := m.Update(DuplicateMsg{})
	if cmd != nil {
		t.Fatal("a duplicate in the scratch section must do nothing")
	}
	if m.prompt != nil {
		t.Fatalf("no prompt expected: %#v", m.prompt)
	}
}
