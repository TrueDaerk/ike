package explorer

// clipboard_test.go covers the file clipboard (#2660): cmd+c/cmd+x fill it,
// cmd+v drops it into the cursor's directory, a name conflict routes through
// the ordinary name prompt, and a multi-select paste is one undo step.

import (
	"os"
	"path/filepath"
	"testing"
)

// cursorOn puts the cursor on the row with the given name — the paste target
// is derived from it (targetDir).
func cursorOn(t *testing.T, m Model, name string) Model {
	t.Helper()
	for i, n := range m.rows {
		if n.name == name {
			m.cursor = i
			return m
		}
	}
	t.Fatalf("no row named %q in %v", name, names(m))
	return m
}

// setPromptText replaces the open prompt's text, the way typing would.
func setPromptText(t *testing.T, m Model, s string) Model {
	t.Helper()
	if m.prompt == nil {
		t.Fatal("no prompt open")
	}
	m.prompt.input.Text = s
	m.prompt.input.Cur = len([]rune(s))
	return m
}

// TestClipCopyPasteFile: copy + paste puts a copy in the target directory and
// leaves both the source and the clipboard in place.
func TestClipCopyPasteFile(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j")) // a.txt
	m, _ = m.Update(ClipCopyMsg{})
	if m.ClipCount() != 1 || m.ClipCut() {
		t.Fatalf("clipboard = %d entries cut=%v, want 1 copy", m.ClipCount(), m.ClipCut())
	}

	m = cursorOn(t, m, "sub")
	m, cmd := m.Update(ClipPasteMsg{})
	m, _ = pumpScans(m, cmd)

	if data, err := os.ReadFile(filepath.Join(root, "sub", "a.txt")); err != nil || string(data) != "a" {
		t.Fatalf("paste must create the copy, got %q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("a copy must leave the source alone: %v", err)
	}
	if m.ClipCount() != 1 {
		t.Fatalf("a copy keeps the clipboard, got %d entries", m.ClipCount())
	}
}

// TestClipCopyPasteDirTree: a directory is pasted with its whole subtree.
func TestClipCopyPasteDirTree(t *testing.T) {
	root := tree(t)
	mustWrite(t, filepath.Join(root, "sub", "deep.txt"), "deep")
	if err := os.Mkdir(filepath.Join(root, "dst"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m = cursorOn(t, m, "sub")
	m, _ = m.Update(ClipCopyMsg{})

	m = cursorOn(t, m, "dst")
	m, cmd := m.Update(ClipPasteMsg{})
	m, _ = pumpScans(m, cmd)

	if data, err := os.ReadFile(filepath.Join(root, "dst", "sub", "deep.txt")); err != nil || string(data) != "deep" {
		t.Fatalf("the subtree must be copied, got %q err=%v", data, err)
	}
}

// TestClipCutPasteMovesAndClears: a cut+paste moves the entry, announces the
// move so open editors follow it, and empties the clipboard.
func TestClipCutPasteMovesAndClears(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j")) // a.txt
	m, _ = m.Update(ClipCutMsg{})
	if !m.ClipCut() {
		t.Fatal("cmd+x must arm cut mode")
	}

	m = cursorOn(t, m, "sub")
	m, cmd := m.Update(ClipPasteMsg{})
	moved := findMoved(t, m, cmd)

	if _, err := os.Stat(filepath.Join(root, "sub", "a.txt")); err != nil {
		t.Fatalf("the cut entry must land in the target: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err == nil {
		t.Fatal("the source must be gone after a cut+paste")
	}
	if moved.New != filepath.Join(root, "sub", "a.txt") {
		t.Fatalf("a cut+paste must announce the move, got %+v", moved)
	}
	if m.ClipCount() != 0 {
		t.Fatalf("a cut empties the clipboard, got %d entries", m.ClipCount())
	}
}

// TestClipCutPasteSameDirIsNoop: dropping a cut back where it came from
// reports a status note instead of failing.
func TestClipCutPasteSameDirIsNoop(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j")) // a.txt; its directory is the root
	m, _ = m.Update(ClipCutMsg{})
	m, cmd := m.Update(ClipPasteMsg{})
	m, _ = pumpScans(m, cmd)

	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("the entry must stay put: %v", err)
	}
	if m.err != nil {
		t.Fatalf("a same-directory cut is not a failure, got %v", m.err)
	}
	if m.prompt == nil || !m.prompt.info {
		t.Fatalf("a same-directory cut must report a status note, prompt=%+v", m.prompt)
	}
}

// TestClipPasteEmptyClipboard: nothing on the clipboard says so and changes
// nothing.
func TestClipPasteEmptyClipboard(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	before := names(m)

	m, cmd := m.Update(ClipPasteMsg{})
	if cmd != nil {
		t.Fatal("an empty clipboard must not run a paste")
	}
	if m.prompt == nil || !m.prompt.info {
		t.Fatalf("an empty clipboard must report a status note, prompt=%+v", m.prompt)
	}
	if len(names(m)) != len(before) {
		t.Fatal("an empty paste must not touch the tree")
	}
}

// TestClipPasteConflictPrompt: a taken name opens the name prompt prefilled
// with it; re-entering the same name keeps the prompt open with a reason, and
// a free name completes the paste.
func TestClipPasteConflictPrompt(t *testing.T) {
	root := tree(t)
	mustWrite(t, filepath.Join(root, "sub", "a.txt"), "existing")
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j")) // a.txt
	m, _ = m.Update(ClipCopyMsg{})

	m = cursorOn(t, m, "sub")
	m, cmd := m.Update(ClipPasteMsg{})
	m, _ = pumpScans(m, cmd)
	if m.prompt == nil || m.prompt.input.Text != "a.txt" {
		t.Fatalf("a conflict must prefill the name prompt, prompt=%+v", m.prompt)
	}

	// The same (still conflicting) name is rejected with a reason.
	m, _ = send(m, key("enter"))
	if m.prompt == nil || m.prompt.note == "" {
		t.Fatalf("a conflicting name must keep the prompt open with a reason, prompt=%+v", m.prompt)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "sub", "a.txt")); string(data) != "existing" {
		t.Fatalf("the existing entry must be untouched, got %q", data)
	}

	// A free name completes the paste.
	m = setPromptText(t, m, "a-copy.txt")
	m, cmd = m.Update(key("enter"))
	m, _ = pumpScans(m, cmd)
	if m.prompt != nil {
		t.Fatalf("a free name must close the prompt, got %+v", m.prompt)
	}
	if data, err := os.ReadFile(filepath.Join(root, "sub", "a-copy.txt")); err != nil || string(data) != "a" {
		t.Fatalf("the copy must use the new name, got %q err=%v", data, err)
	}
}

// TestClipPasteConflictEscSkipsEntry: esc on a conflict skips that entry and
// the rest of the batch still runs.
func TestClipPasteConflictEscSkipsEntry(t *testing.T) {
	root := tree(t)
	mustWrite(t, filepath.Join(root, "sub", "a.txt"), "existing")
	m := markedTree(t, root) // a.txt and b.txt marked
	m, _ = m.Update(ClipCopyMsg{})
	if m.ClipCount() != 2 {
		t.Fatalf("clipboard = %d entries, want the two marked ones", m.ClipCount())
	}

	m = cursorOn(t, m, "sub")
	m, cmd := m.Update(ClipPasteMsg{})
	m, _ = pumpScans(m, cmd)
	if m.prompt == nil {
		t.Fatal("the conflicting entry must open a prompt")
	}
	m, cmd = m.Update(escKey())
	m, _ = pumpScans(m, cmd)

	if data, _ := os.ReadFile(filepath.Join(root, "sub", "a.txt")); string(data) != "existing" {
		t.Fatalf("the skipped entry must not be pasted, got %q", data)
	}
	if _, err := os.Stat(filepath.Join(root, "sub", "b.txt")); err != nil {
		t.Fatalf("the rest of the batch must still run: %v", err)
	}
}

// TestClipMultiSelectPasteUndoneInOneStep: a marked multi-select pastes as one
// batch and one explorer.undo takes all of it back.
func TestClipMultiSelectPasteUndoneInOneStep(t *testing.T) {
	root := tree(t)
	m := markedTree(t, root) // a.txt and b.txt marked
	m, _ = m.Update(ClipCopyMsg{})

	m = cursorOn(t, m, "sub")
	m, cmd := m.Update(ClipPasteMsg{})
	m, _ = pumpScans(m, cmd)
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(root, "sub", name)); err != nil {
			t.Fatalf("%s must be pasted: %v", name, err)
		}
	}
	if len(m.ops) != 1 {
		t.Fatalf("a batch paste must be one undo step, got %d", len(m.ops))
	}

	m, cmd = m.Update(UndoMsg{})
	m, _ = pumpScans(m, cmd)
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(root, "sub", name)); err == nil {
			t.Fatalf("one undo must remove %s again", name)
		}
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("the sources must survive the undo of a copy: %v", err)
		}
	}
}

// TestClipCutMultiSelectMovesAll: cut over a multi-select moves every entry in
// one batch and one undo walks them back.
func TestClipCutMultiSelectMovesAll(t *testing.T) {
	root := tree(t)
	m := markedTree(t, root)
	m, _ = m.Update(ClipCutMsg{})

	m = cursorOn(t, m, "sub")
	m, cmd := m.Update(ClipPasteMsg{})
	m, _ = pumpScans(m, cmd)
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(root, "sub", name)); err != nil {
			t.Fatalf("%s must be moved: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			t.Fatalf("%s must be gone from the source directory", name)
		}
	}

	m, cmd = m.Update(UndoMsg{})
	m, _ = pumpScans(m, cmd)
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("one undo must move %s back: %v", name, err)
		}
	}
}
