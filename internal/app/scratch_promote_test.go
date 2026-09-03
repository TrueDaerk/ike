package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/scratch"
)

// scratch_promote_test.go covers #2339's way out of the store:
// scratch.promote writes the scratch to a chosen project path, removes the
// store entry and leaves the open tab on the real file.

// promoteApp creates a scratch holding content, opens it, and opens the
// promote prompt for it.
func promoteApp(t *testing.T, content string) Model {
	t.Helper()
	m := dispatch(t, newSized(), NewScratchMsg{Ext: "sct", Content: content})
	m = dispatch(t, m, PromoteScratchMsg{})
	if !m.promoteScratchOpen() {
		t.Fatal("scratch.promote must open the target-path prompt")
	}
	return m
}

// promoteTo types target into the open prompt (replacing the prefill) and
// accepts it.
func promoteTo(m Model, target string) Model {
	m.promoteInput.Set(target)
	m.promoteInput.Cur = len([]rune(target))
	m.renderScratchPromote()
	return drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
}

// TestPromoteScratchWritesFileAndRepointsTab is the headline AC: the content
// lands at the chosen path, the store entry is gone, and the open tab now
// works on the real file — so the next save writes there, not into the store.
func TestPromoteScratchWritesFileAndRepointsTab(t *testing.T) {
	m := promoteApp(t, "keep me\n")
	src := m.promotePath
	if filepath.Base(m.promoteInput.Text) != filepath.Base(src) {
		t.Fatalf("prefill = %q, want the scratch's own name", m.promoteInput.Text)
	}
	target := filepath.Join(t.TempDir(), "pkg", "kept.sct")

	m = promoteTo(m, target)
	if m.promoteScratchOpen() {
		t.Fatal("a successful promote must close the prompt")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "keep me\n" {
		t.Fatalf("promoted file = %q, %v; want the scratch content", data, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("the store entry must be gone, stat err = %v", err)
	}
	if got, err := scratch.List(); err != nil || len(got) != 0 {
		t.Fatalf("store after promote = %v (%v)", got, err)
	}
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	if ed.Path() != target {
		t.Fatalf("open tab path = %q, want %q", ed.Path(), target)
	}
	// Saving now writes to the project file — the store is out of the loop.
	if err := ed.SaveTo(target); err != nil {
		t.Fatalf("saving the promoted buffer = %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("a save must not resurrect the scratch, stat err = %v", err)
	}
}

// TestPromoteScratchFlushesDirtyBuffer: unsaved edits go with the file, so the
// promoted result is what the user sees rather than the last written state.
func TestPromoteScratchFlushesDirtyBuffer(t *testing.T) {
	m := dispatch(t, newSized(), NewScratchMsg{Ext: "sct", Content: "old\n"})
	// Type a line in insert mode, then leave it unsaved.
	m = drainKey(m, tea.KeyPressMsg{Code: 'i', Text: "i"})
	for _, r := range "new " {
		m = drainKey(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.activeWS().Panes.FocusedInstance().Editor().Dirty() {
		t.Fatal("setup: the buffer must be dirty")
	}

	m = dispatch(t, m, PromoteScratchMsg{})
	target := filepath.Join(t.TempDir(), "flushed.sct")
	m = promoteTo(m, target)

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("promoted file must exist: %v", err)
	}
	if !strings.Contains(string(data), "new ") {
		t.Fatalf("promoted content = %q, want the unsaved edit included", data)
	}
}

// TestPromoteScratchRefusesExistingTarget is the no-clobber AC: the prompt
// stays open with the reason, the target keeps its content, the scratch stays.
func TestPromoteScratchRefusesExistingTarget(t *testing.T) {
	m := promoteApp(t, "mine\n")
	src := m.promotePath
	target := filepath.Join(t.TempDir(), "taken.sct")
	if err := os.WriteFile(target, []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m = promoteTo(m, target)
	if !m.promoteScratchOpen() {
		t.Fatal("a refused promote must keep the prompt open for another path")
	}
	if !strings.Contains(m.promoteErr, "file exists") {
		t.Fatalf("promoteErr = %q, want an exists message", m.promoteErr)
	}
	if data, _ := os.ReadFile(target); string(data) != "theirs\n" {
		t.Fatalf("the existing file must be untouched, got %q", data)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("the scratch must survive a refused promote: %v", err)
	}
}

// TestPromoteScratchWriteErrorKeepsStoreEntry: a target that cannot be created
// leaves the store entry and the open tab exactly as they were.
func TestPromoteScratchWriteErrorKeepsStoreEntry(t *testing.T) {
	m := promoteApp(t, "mine\n")
	src := m.promotePath
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	m = promoteTo(m, filepath.Join(blocker, "child.sct"))
	if !m.promoteScratchOpen() || m.promoteErr == "" {
		t.Fatalf("a write error must be reported in the prompt (open=%v err=%q)",
			m.promoteScratchOpen(), m.promoteErr)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("the scratch must survive a failed promote: %v", err)
	}
	if got := m.activeWS().Panes.FocusedInstance().Editor().Path(); got != src {
		t.Fatalf("the tab must still point at the scratch, got %q", got)
	}
}

// TestPromoteScratchEmptyPathRefused: enter on an empty line asks again.
func TestPromoteScratchEmptyPathRefused(t *testing.T) {
	m := promoteApp(t, "mine\n")

	m = promoteTo(m, "   ")
	if !m.promoteScratchOpen() {
		t.Fatal("an empty target must keep the prompt open")
	}
	if !strings.Contains(m.promoteErr, "required") {
		t.Fatalf("promoteErr = %q, want the required-path message", m.promoteErr)
	}
}

// TestPromoteScratchEscapeCancels: nothing moves and the prompt closes.
func TestPromoteScratchEscapeCancels(t *testing.T) {
	m := promoteApp(t, "mine\n")
	src := m.promotePath

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.promoteScratchOpen() {
		t.Fatal("esc must close the prompt")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("a cancelled promote must leave the scratch: %v", err)
	}
}

// TestPromoteRefusesNonScratch: the command only means something for a store
// file, and says so for anything else instead of moving a project file.
func TestPromoteRefusesNonScratch(t *testing.T) {
	m := newSized()
	path := filepath.Join(t.TempDir(), "real.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)
	clearNotices(&m)

	m = dispatch(t, m, PromoteScratchMsg{})
	if m.promoteScratchOpen() {
		t.Fatal("a project file must not open the promote prompt")
	}
	if !containsSubstr(notices(m), "is not a scratch file") {
		t.Fatalf("the refusal must be reported, notices = %v", notices(m))
	}
}

// TestPromoteTargetIsRelativeToProjectRoot: a bare name is taken against the
// project root, an absolute path as typed — the save-as prompt's rule.
func TestPromoteTargetIsRelativeToProjectRoot(t *testing.T) {
	m := newSized()
	root := m.explorer().Root()
	if got, want := m.promoteScratchTarget("sub/notes.md"),
		filepath.Join(root, "sub", "notes.md"); got != want {
		t.Fatalf("relative target = %q, want %q", got, want)
	}
	abs := filepath.Join(t.TempDir(), "notes.md")
	if got := m.promoteScratchTarget(abs); got != abs {
		t.Fatalf("absolute target = %q, want %q", got, abs)
	}
}

// TestScratchManagerPromoteHandsOffSelection: ctrl+p in the manager closes it
// and opens the prompt for the marked scratch, so the manager's row is the
// subject rather than whatever happens to be focused behind it.
func TestScratchManagerPromoteHandsOffSelection(t *testing.T) {
	m := newSized()
	paths := smSeed(t, "sct", "sct")
	m = openManager(t, m)
	m = smSelect(t, m, filepath.Base(paths[0]))

	m = smKey(m, tea.Key{Code: 'p', Mod: tea.ModCtrl})
	if m.scratchManagerOpen() {
		t.Fatal("promote must leave the manager")
	}
	if !m.promoteScratchOpen() {
		t.Fatal("promote must open the target-path prompt")
	}
	if m.promotePath != paths[0] {
		t.Fatalf("prompt subject = %q, want the marked scratch %q", m.promotePath, paths[0])
	}
}
