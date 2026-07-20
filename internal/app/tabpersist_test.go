package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
)

// tabpersist_test.go covers per-pane tab persistence (#160): layout.json
// round-trips the ordered tab list + active index, legacy files restore as
// single-tab panes, and files missing on restore are skipped gracefully.

// fixedDirApp builds a sized app bound to dir as its state store.
func fixedDirApp(t *testing.T, dir string) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", dir)
	m := New()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return tm.(Model)
}

func TestTabListRoundTripsThroughLayout(t *testing.T) {
	conf := t.TempDir()
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	c := writeTemp(t, dir, "c.txt", "ccc\n")

	m := fixedDirApp(t, conf)
	for _, p := range []string{a, b, c} {
		tm, _ := m.openPath(p, false)
		m = tm.(Model)
	}
	m = dispatch(t, m, TabSelectMsg{Index: 1}) // persist with b active
	key := m.activeWS().Panes.Focused()

	m2 := fixedDirApp(t, conf)
	inst := m2.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindEditor {
		t.Fatal("the editor pane must restore under its saved key")
	}
	if inst.TabCount() != 3 {
		t.Fatalf("want 3 restored tabs, got %d", inst.TabCount())
	}
	for i, want := range []string{a, b, c} {
		if got := inst.TabEditor(i).Path(); got != want {
			t.Fatalf("tab %d = %q, want %q (order must survive)", i, got, want)
		}
	}
	if inst.ActiveTab() != 1 || inst.Editor().Path() != b {
		t.Fatalf("the active tab must survive, active=%d %q", inst.ActiveTab(), inst.Editor().Path())
	}
}

func TestLegacyLayoutRestoresSingleTab(t *testing.T) {
	conf := t.TempDir()
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")

	// A pre-#160 layout file: identity with a bare path, no tabs field.
	tree := json.RawMessage(`{"orient":"h","ratio":0.3,"a":{"pane":"explorer"},"b":{"pane":"editor"}}`)
	data, err := json.Marshal(persistedLayout{Tree: tree, Panes: map[string]paneIdentity{
		"explorer": {Kind: "explorer"},
		"editor":   {Kind: "editor", Path: a},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conf, "layout.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	m := fixedDirApp(t, conf)
	inst := m.activeWS().Panes.Get("editor")
	if inst == nil || inst.TabCount() != 1 || inst.Editor().Path() != a {
		t.Fatal("a legacy single-document identity must restore as one tab")
	}
}

func TestMissingFilesSkippedOnRestore(t *testing.T) {
	conf := t.TempDir()
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	c := writeTemp(t, dir, "c.txt", "ccc\n")

	m := fixedDirApp(t, conf)
	for _, p := range []string{a, b, c} {
		tm, _ := m.openPath(p, false)
		m = tm.(Model)
	}
	key := m.activeWS().Panes.Focused() // c active, persisted by the open flow

	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	m2 := fixedDirApp(t, conf)
	inst := m2.activeWS().Panes.Get(key)
	if inst.TabCount() != 2 {
		t.Fatalf("the vanished file must be skipped, got %d tabs", inst.TabCount())
	}
	if inst.TabEditor(0).Path() != a || inst.TabEditor(1).Path() != c {
		t.Fatal("surviving tabs must keep their order")
	}
	if inst.Editor().Path() != c {
		t.Fatalf("the saved active tab must stay active despite the gap, got %q", inst.Editor().Path())
	}
}

func TestAllFilesMissingRestoresScratchPane(t *testing.T) {
	conf := t.TempDir()
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")

	m := fixedDirApp(t, conf)
	for _, p := range []string{a, b} {
		tm, _ := m.openPath(p, false)
		m = tm.(Model)
	}
	key := m.activeWS().Panes.Focused()
	os.Remove(a)
	os.Remove(b)

	m2 := fixedDirApp(t, conf)
	inst := m2.activeWS().Panes.Get(key)
	if inst.TabCount() != 1 || inst.Editor().HasFile() {
		t.Fatal("a pane whose files all vanished must restore as one scratch tab")
	}
}

func TestSharedDocumentRestoresAcrossTabsAndPanes(t *testing.T) {
	conf := t.TempDir()
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")

	m := fixedDirApp(t, conf)
	tm, _ := m.openPath(a, false)
	m = tm.(Model)
	tm, _ = m.openPath(b, false) // pane 1: tabs a, b
	m = tm.(Model)
	tm, _ = m.openPath(a, true) // pane 2: a again, shared
	m = tm.(Model)

	m2 := fixedDirApp(t, conf)
	views := m2.editorViewsForPath(a)
	if len(views) != 2 {
		t.Fatalf("want the shared file restored in 2 views, got %d", len(views))
	}
	if !views[0].SharesBufferWith(views[1]) {
		t.Fatal("restored views of one file must share the document, not diverge")
	}
}
