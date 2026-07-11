package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/keymap"
	"ike/internal/pane"
)

// tabs_test.go covers the per-pane tab model (#156): opening files into the
// focused pane's tab list, activating existing tabs, shared documents across
// tabs, and tab-aware close semantics.

// writeTemp creates a file with content and returns its path.
func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// openApp opens each path in order in the focused pane and returns the model.
func openApp(t *testing.T, paths ...string) Model {
	t.Helper()
	m := newSized()
	for _, p := range paths {
		tm, _ := m.openPath(p, false)
		m = tm.(Model)
	}
	return m
}

func TestOpenSecondFileAppendsTab(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	m := openApp(t, a, b)

	inst := m.panes.FocusedInstance()
	if inst.Kind() != pane.KindEditor || inst.TabCount() != 2 {
		t.Fatalf("want 2 tabs in the focused editor, got %d", inst.TabCount())
	}
	if inst.Editor().Path() != b {
		t.Fatalf("active tab = %q, want %q", inst.Editor().Path(), b)
	}
	if inst.TabEditor(0).Path() != a {
		t.Fatalf("first tab = %q, want %q", inst.TabEditor(0).Path(), a)
	}
}

func TestOpenExistingFileActivatesTab(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	m := openApp(t, a, b, a)

	inst := m.panes.FocusedInstance()
	if inst.TabCount() != 2 {
		t.Fatalf("re-opening an open file must not add a tab, got %d", inst.TabCount())
	}
	if inst.Editor().Path() != a || inst.ActiveTab() != 0 {
		t.Fatalf("re-open must activate the existing tab, active=%d %q",
			inst.ActiveTab(), inst.Editor().Path())
	}
}

func TestOpenInNewPaneKeepsSplitBehavior(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	m := openApp(t, a)
	tm, _ := m.openPath(b, true)
	m = tm.(Model)

	editors := 0
	for _, key := range m.panes.Keys() {
		if inst := m.panes.Get(key); inst.Kind() == pane.KindEditor {
			editors++
			if inst.TabCount() != 1 {
				t.Fatalf("pane %s: open-in-new-pane must not grow tab lists, tabs=%d",
					key, inst.TabCount())
			}
		}
	}
	if editors != 2 {
		t.Fatalf("want 2 editor panes after a split open, got %d", editors)
	}
}

func TestSyncReachesBackgroundTab(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\ntwo\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	// Pane 1: tab a (background) + tab b (active); pane 2: a again, shared.
	m := openApp(t, a, b)
	tm, _ := m.openPath(a, true)
	m = tm.(Model)

	background := m.panes.Get("editor").TabEditor(0)
	if background.Path() != a {
		t.Fatalf("setup: first tab of pane 1 should hold %q", a)
	}
	if !background.SharesBufferWith(m.activeEditor()) {
		t.Fatal("a background tab must share the document with the new pane")
	}

	for _, k := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"},
		{Code: 'X', Text: "X"},
		{Code: tea.KeyEscape},
	} {
		tm, _ := m.Update(k) // edits land in the focused second pane
		m = tm.(Model)
	}
	deliverSync(&m, a, m.activeEditorKey())

	if !strings.Contains(background.Text(), "Xone") {
		t.Fatalf("background tab missing the shared edit: %q", background.Text())
	}
	if !background.Dirty() {
		t.Fatal("background tab must mirror the dirty flag")
	}
}

func TestCloseTabKeepsPaneAndActivatesNeighbour(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	m := openApp(t, a, b)
	key := m.panes.Focused()

	m.CloseFocused()

	inst := m.panes.Get(key)
	if inst == nil {
		t.Fatal("closing one of two tabs must keep the pane alive")
	}
	if inst.TabCount() != 1 || inst.Editor().Path() != a {
		t.Fatalf("want the remaining tab %q active, got %d tabs, %q",
			a, inst.TabCount(), inst.Editor().Path())
	}

	// The pane holds one tab now: the next close removes the pane itself.
	m.CloseFocused()
	if m.panes.Has(key) {
		t.Fatal("closing the last tab must close the pane")
	}
}

func TestCloseTabViaKeymap(t *testing.T) {
	oldGOOS := keymap.GOOS
	keymap.GOOS = "darwin"
	defer func() { keymap.GOOS = oldGOOS }()

	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	m := openApp(t, a, b)
	key := m.panes.Focused()

	m = drainKey(m, tea.KeyPressMsg{Code: 'w', Mod: tea.ModSuper})

	inst := m.panes.Get(key)
	if inst == nil || inst.TabCount() != 1 || inst.Editor().Path() != a {
		t.Fatal("cmd+w on a multi-tab pane must close only the active tab")
	}
}

func TestExternallyDeletedFileClosesItsTab(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	m := openApp(t, a, b)
	key := m.panes.Focused()

	m.closeEditorsForPath(a, false)

	inst := m.panes.Get(key)
	if inst == nil {
		t.Fatal("a pane with a surviving tab must stay open")
	}
	if inst.TabCount() != 1 || inst.Editor().Path() != b {
		t.Fatalf("want only %q left, got %d tabs, %q", b, inst.TabCount(), inst.Editor().Path())
	}
	if got := m.editorKeysForPath(a); len(got) != 0 {
		t.Fatalf("no editor may still claim the deleted file, got %v", got)
	}
}

// dirtyActive makes the focused editor's active tab dirty by deleting a char.
func dirtyActive(t *testing.T, m Model) Model {
	t.Helper()
	m = drainKey(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if !m.panes.FocusedInstance().Editor().Dirty() {
		t.Fatal("setup: active editor should be dirty")
	}
	return m
}

func TestCloseGuardPromptsOnDirtyTab(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	m := openApp(t, a, b)
	key := m.panes.Focused()
	m = dirtyActive(t, m)

	tm, _ := m.Update(CloseTabMsg{})
	m = tm.(Model)
	if !m.closePromptOpen() {
		t.Fatal("closing a dirty tab must open the unsaved-changes guard (#259)")
	}
	if m.panes.Get(key).TabCount() != 2 {
		t.Fatal("the tab must stay open while the guard is up")
	}

	// esc cancels: prompt gone, tab still open and still dirty.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.closePromptOpen() || m.panes.Get(key).TabCount() != 2 {
		t.Fatal("esc must cancel the close and keep the tab")
	}
	if !m.panes.Get(key).Editor().Dirty() {
		t.Fatal("esc must not touch the buffer")
	}
}

func TestCloseGuardDiscardCloses(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	m := openApp(t, a, b)
	key := m.panes.Focused()
	m = dirtyActive(t, m)

	tm, _ := m.Update(CloseTabMsg{})
	m = tm.(Model)
	m = drainKey(m, tea.KeyPressMsg{Code: 'd', Text: "d"})
	if m.closePromptOpen() {
		t.Fatal("d must dismiss the guard")
	}
	if got := m.panes.Get(key).TabCount(); got != 1 {
		t.Fatalf("d must close the tab, got %d tabs", got)
	}
	if data, _ := os.ReadFile(b); string(data) != "bbb\n" {
		t.Fatalf("discard must not write the file, got %q", data)
	}
}

func TestCloseGuardSaveCloses(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	m := openApp(t, a, b)
	key := m.panes.Focused()
	m = dirtyActive(t, m)

	tm, _ := m.Update(CloseTabMsg{})
	m = tm.(Model)
	m = drainKey(m, tea.KeyPressMsg{Code: 's', Text: "s"})
	if got := m.panes.Get(key).TabCount(); got != 1 {
		t.Fatalf("s must save and close, got %d tabs", got)
	}
	if data, _ := os.ReadFile(b); string(data) != "bb\n" {
		t.Fatalf("s must write the edited content, got %q", data)
	}
}

func TestCloseGuardSaveFailureKeepsTab(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	m := openApp(t, a, b)
	key := m.panes.Focused()
	m = dirtyActive(t, m)
	if err := os.Chmod(b, 0o444); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(b, 0o644)

	tm, _ := m.Update(CloseTabMsg{})
	m = tm.(Model)
	m = drainKey(m, tea.KeyPressMsg{Code: 's', Text: "s"})
	if got := m.panes.Get(key).TabCount(); got != 2 {
		t.Fatalf("a failed save must keep the tab open, got %d tabs", got)
	}
}

func TestCloseGuardForceSkipsPrompt(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	m := openApp(t, a, b)
	key := m.panes.Focused()
	m = dirtyActive(t, m)

	tm, _ := m.Update(editor.CloseMsg{Force: true}) // :q!
	m = tm.(Model)
	if m.closePromptOpen() {
		t.Fatal(":q! must not open the guard")
	}
	if got := m.panes.Get(key).TabCount(); got != 1 {
		t.Fatalf(":q! must close the tab, got %d tabs", got)
	}
}
