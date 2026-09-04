package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/backup"
	"ike/internal/pane"
)

// TestPanelReturnTargetFallbackChain guards the toggle-off chain every tool
// window shares (#2463): the remembered pane when it is still part of the
// layout, otherwise the active editor, otherwise the explorer.
func TestPanelReturnTargetFallbackChain(t *testing.T) {
	m := problemsApp(t)
	editorKey := m.activeEditorKey()
	if editorKey == "" {
		t.Fatal("setup: expected an editor pane")
	}

	// Remembered pane wins while it exists.
	m.setPanelReturn(pane.ProblemsKey, editorKey)
	if got := m.panelReturnTarget(pane.ProblemsKey); got != editorKey {
		t.Fatalf("remembered target = %q, want %q", got, editorKey)
	}

	// A pane that is gone (or was never remembered) falls back to the active
	// editor.
	m.setPanelReturn(pane.ProblemsKey, "pane-that-never-existed")
	if got := m.panelReturnTarget(pane.ProblemsKey); got != editorKey {
		t.Fatalf("stale target = %q, want the active editor %q", got, editorKey)
	}
	m.setPanelReturn(pane.ProblemsKey, "")
	if got := m.panelReturnTarget(pane.ProblemsKey); got != editorKey {
		t.Fatalf("empty target = %q, want the active editor %q", got, editorKey)
	}

	// Without any editor left the explorer is the last resort.
	for _, key := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
			m.activeWS().Panes.Close(key)
		}
	}
	if got := m.panelReturnTarget(pane.ProblemsKey); got != pane.ExplorerKey {
		t.Fatalf("last-resort target = %q, want %q", got, pane.ExplorerKey)
	}
}

// TestSetPanelReturnLazyMap: the return-focus map is created on demand, so a
// Model that never opened a tool window carries none.
func TestSetPanelReturnLazyMap(t *testing.T) {
	var m Model
	if m.panelReturnFocus != nil {
		t.Fatal("a fresh Model must not allocate the return-focus map")
	}
	m.setPanelReturn(pane.VCSKey, "editor-1")
	if m.panelReturnFocus[pane.VCSKey] != "editor-1" {
		t.Fatalf("remembered = %q, want %q", m.panelReturnFocus[pane.VCSKey], "editor-1")
	}
}

// TestTogglePanelStateMachine drives the three branches over a fake pane key:
// open (running the opener once), focus, and return-focus.
func TestTogglePanelStateMachine(t *testing.T) {
	m := problemsApp(t)
	before := m.activeWS().Panes.Focused()
	opens := 0
	open := func() tea.Cmd {
		opens++
		m.openProblemsPanel()
		return nil
	}

	m.togglePanel(pane.ProblemsKey, open)
	if opens != 1 || m.activeWS().Panes.Focused() != pane.ProblemsKey {
		t.Fatalf("first toggle must open and focus (opens=%d focus=%q)", opens, m.activeWS().Panes.Focused())
	}
	m.togglePanel(pane.ProblemsKey, open)
	if opens != 1 || m.activeWS().Panes.Focused() != before {
		t.Fatalf("second toggle must return focus to %q, got %q (opens=%d)", before, m.activeWS().Panes.Focused(), opens)
	}
	m.togglePanel(pane.ProblemsKey, open)
	if opens != 1 || m.activeWS().Panes.Focused() != pane.ProblemsKey {
		t.Fatalf("third toggle must refocus the open pane (opens=%d focus=%q)", opens, m.activeWS().Panes.Focused())
	}
}

// TestTogglePanelWithFocusHook: the focus branch runs the hook, the open and
// return-focus branches do not.
func TestTogglePanelWithFocusHook(t *testing.T) {
	m := problemsApp(t)
	hooks := 0
	hook := func() tea.Cmd { hooks++; return nil }
	open := func() tea.Cmd { m.openProblemsPanel(); return nil }

	m.togglePanelWith(pane.ProblemsKey, open, hook)
	if hooks != 0 {
		t.Fatalf("opening must not run the focus hook (hooks=%d)", hooks)
	}
	m.togglePanelWith(pane.ProblemsKey, open, hook) // toggles off
	if hooks != 0 {
		t.Fatalf("returning focus must not run the focus hook (hooks=%d)", hooks)
	}
	m.togglePanelWith(pane.ProblemsKey, open, hook) // refocuses
	if hooks != 1 {
		t.Fatalf("refocusing must run the focus hook once (hooks=%d)", hooks)
	}
}

// TestShowPanelNeverTogglesOff: showPanel reveals the window and keeps it
// focused, where togglePanel would hand focus back.
func TestShowPanelNeverTogglesOff(t *testing.T) {
	m := problemsApp(t)
	open := func() tea.Cmd { m.openProblemsPanel(); return nil }
	m.showPanel(pane.ProblemsKey, open)
	m.showPanel(pane.ProblemsKey, open)
	if m.activeWS().Panes.Focused() != pane.ProblemsKey {
		t.Fatalf("focus = %q, want the panel to stay focused", m.activeWS().Panes.Focused())
	}
}

// TestEnsurePanelOpensOnceAndKeepsFocus: ensurePanel adds the window when it
// is missing and leaves an existing one alone, focus included.
func TestEnsurePanelOpensOnceAndKeepsFocus(t *testing.T) {
	m := problemsApp(t)
	opens := 0
	open := func() tea.Cmd { opens++; m.openProblemsPanel(); return nil }

	m.ensurePanel(pane.ProblemsKey, open)
	if opens != 1 || !m.activeWS().Panes.Has(pane.ProblemsKey) {
		t.Fatalf("missing panel must open (opens=%d)", opens)
	}
	editorKey := m.activeEditorKey()
	m.setFocus(editorKey)
	m.ensurePanel(pane.ProblemsKey, open)
	if opens != 1 {
		t.Fatalf("an open panel must not re-open (opens=%d)", opens)
	}
	if m.activeWS().Panes.Focused() != editorKey {
		t.Fatalf("ensurePanel must not steal focus, got %q", m.activeWS().Panes.Focused())
	}
}

// TestArmTickGenerationGuard guards the shared debounce scheduler (#2463): one
// armed timer at a time, nothing armed without pending marks, and the tick
// carries the model generation it was armed in.
func TestArmTickGenerationGuard(t *testing.T) {
	m := problemsApp(t)
	m.modelGen = 7
	deb := backup.NewDebouncer(0)
	armed := false

	if cmd := m.armTick(&armed, deb, func(gen int64) tea.Msg { return gen }); cmd != nil || armed {
		t.Fatal("an empty debouncer must arm nothing")
	}
	if cmd := m.armTick(&armed, nil, func(gen int64) tea.Msg { return gen }); cmd != nil || armed {
		t.Fatal("a nil debouncer must arm nothing")
	}

	deb.Mark("a.txt", time.Now())
	cmd := m.armTick(&armed, deb, func(gen int64) tea.Msg { return gen })
	if cmd == nil || !armed {
		t.Fatal("a pending mark must arm one tick")
	}
	if got := cmd(); got != int64(7) {
		t.Fatalf("tick msg = %v, want the model generation 7", got)
	}
	deb.Mark("b.txt", time.Now())
	if again := m.armTick(&armed, deb, func(gen int64) tea.Msg { return gen }); again != nil {
		t.Fatal("an armed side must not stack a second tick")
	}
}

// TestWriteDirtyTabsWalksBackgroundTabs guards the shared save-everything walk
// (#2463): every dirty buffer produces one action, clean ones none.
func TestWriteDirtyTabsWalksBackgroundTabs(t *testing.T) {
	m := problemsApp(t)
	if cmds := m.writeDirtyTabs("write_raw"); len(cmds) != 0 {
		t.Fatalf("a clean workspace must produce no writes, got %d", len(cmds))
	}

	dir := t.TempDir()
	var keys []string
	for _, name := range []string{"a.txt", "b.txt"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		tm, _ := m.openPath(path, false)
		m = tm.(Model)
		keys = append(keys, m.activeEditorKey())
	}
	if len(keys) == 0 {
		t.Fatal("setup: no editors opened")
	}
	// Dirty every tab only once both files are open: opening a file settles
	// the pane's other tabs.
	dirty := 0
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			if ed := inst.TabEditor(i); ed != nil && ed.HasFile() {
				ed.RestoreText("unsaved edits")
				dirty++
			}
		}
	}
	if dirty != 2 {
		t.Fatalf("setup: %d dirty tabs, want 2", dirty)
	}

	cmds := m.writeDirtyTabs("write_raw")
	if len(cmds) != 2 {
		t.Fatalf("two dirty tabs must produce two writes, got %d", len(cmds))
	}
	for _, key := range keys {
		inst := m.activeWS().Panes.Get(key)
		for i := 0; i < inst.TabCount(); i++ {
			if ed := inst.TabEditor(i); ed != nil && ed.Dirty() {
				t.Fatalf("pane %q tab %d still dirty after the raw write", key, i)
			}
		}
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(data) != "unsaved edits\n" {
		t.Fatalf("a.txt = %q, want the buffer text written back", data)
	}
}
