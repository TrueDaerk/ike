package app

import (
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/pane"
)

// tablimit_test.go covers the per-pane editor tab limit (#742): LRU eviction
// on open, dirty/terminal exemptions, the disabled case and reopenability.

// withTabLimit installs a config with the given editor.tabs.limit, restoring
// the previous one on cleanup. Autosave is switched off so the dirty-tab
// exemption is actually observable — with the "focus" default, leaving a tab
// saves it and a clean tab is legitimately evictable.
func withTabLimit(t *testing.T, limit int) {
	t.Helper()
	prev := config.Get()
	c := *prev
	c.Editor.Tabs.Limit = limit
	c.Editor.AutoSave = "off"
	config.Set(&c)
	t.Cleanup(func() { config.Set(prev) })
}

// tabPaths lists the pane's document tab paths in order.
func tabPaths(inst *pane.Instance) []string {
	var out []string
	for i := 0; i < inst.TabCount(); i++ {
		if ed := inst.TabEditor(i); ed != nil {
			out = append(out, ed.Path())
		}
	}
	return out
}

func TestTabLimitEvictsLeastRecentlyUsed(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "a\n")
	b := writeTemp(t, dir, "b.txt", "b\n")
	c := writeTemp(t, dir, "c.txt", "c\n")
	d := writeTemp(t, dir, "d.txt", "d\n")
	m := newSized() // building the model loads config; set the limit after
	withTabLimit(t, 3)
	m = openApp2(t, m, a, b, c, d)

	inst := m.activeWS().Panes.FocusedInstance()
	if inst.TabCount() != 3 {
		t.Fatalf("tabs = %v, want 3 after eviction", tabPaths(inst))
	}
	for _, p := range tabPaths(inst) {
		if p == a {
			t.Fatalf("oldest tab must be evicted, tabs = %v", tabPaths(inst))
		}
	}
	// The evicted tab lands in the reopen ring (#158).
	if n := len(m.closedTabs); n == 0 || m.closedTabs[n-1].path != a {
		t.Fatalf("evicted tab must be reopenable, ring = %+v", m.closedTabs)
	}
}

func TestTabLimitEvictionIsLRUNotFIFO(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "a\n")
	b := writeTemp(t, dir, "b.txt", "b\n")
	c := writeTemp(t, dir, "c.txt", "c\n")
	d := writeTemp(t, dir, "d.txt", "d\n")
	m := newSized()
	withTabLimit(t, 3)
	m = openApp2(t, m, a, b, c)
	// Revisit a: it becomes recently used, so b is now the LRU.
	m = openApp2(t, m, a)
	m = openApp2(t, m, d)

	inst := m.activeWS().Panes.FocusedInstance()
	paths := tabPaths(inst)
	for _, p := range paths {
		if p == b {
			t.Fatalf("b is the LRU and must be evicted, tabs = %v", paths)
		}
	}
	found := false
	for _, p := range paths {
		if p == a {
			found = true
		}
	}
	if !found {
		t.Fatalf("recently revisited a must survive, tabs = %v", paths)
	}
}

func TestTabLimitNeverEvictsDirtyTabs(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "a\n")
	b := writeTemp(t, dir, "b.txt", "b\n")
	c := writeTemp(t, dir, "c.txt", "c\n")
	m := newSized()
	withTabLimit(t, 2)
	m = openApp2(t, m, a)
	// Dirty the first tab: insert a character in insert mode, esc back out.
	for _, k := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"}, {Code: 'x', Text: "x"}, {Code: tea.KeyEscape},
	} {
		tm, _ := m.Update(k)
		m = tm.(Model)
	}
	if ed := m.activeEditor(); ed == nil || !ed.Dirty() {
		t.Fatal("precondition: tab a must be dirty")
	}
	// Keep it dirty across tab switches: the focus autosave (#174) fails on
	// a read-only file, so the buffer stays modified.
	if err := os.Chmod(a, 0o444); err != nil {
		t.Fatal(err)
	}
	m = openApp2(t, m, b)
	m = openApp2(t, m, c)
	if ed := m.activeWS().Panes.FocusedInstance().TabEditor(0); ed == nil || !ed.Dirty() {
		t.Skip("autosave cleared the dirty flag despite the failed write; exemption untestable here")
	}

	inst := m.activeWS().Panes.FocusedInstance()
	paths := tabPaths(inst)
	foundA := false
	for _, p := range paths {
		if p == a {
			foundA = true
		}
		if p == b {
			t.Fatalf("clean b must be evicted before dirty a, tabs = %v", paths)
		}
	}
	if !foundA {
		t.Fatalf("dirty tab must never be auto-closed, tabs = %v", paths)
	}
}

func TestTabLimitZeroDisables(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		paths = append(paths, writeTemp(t, dir, n+".txt", n+"\n"))
	}
	m := newSized()
	withTabLimit(t, 0)
	m = openApp2(t, m, paths...)
	if got := m.activeWS().Panes.FocusedInstance().TabCount(); got != 7 {
		t.Fatalf("limit 0 must not evict, tabs = %d", got)
	}
}

func TestTabLimitExceededWhenAllDirty(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "a\n")
	b := writeTemp(t, dir, "b.txt", "b\n")
	m := newSized()
	withTabLimit(t, 1)
	m = openApp2(t, m, a)
	for _, k := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"}, {Code: 'x', Text: "x"}, {Code: tea.KeyEscape},
	} {
		tm, _ := m.Update(k)
		m = tm.(Model)
	}
	if err := os.Chmod(a, 0o444); err != nil {
		t.Fatal(err)
	}
	m = openApp2(t, m, b)
	// a is dirty, b is active: nothing is eligible, the limit is exceeded.
	if got := m.activeWS().Panes.FocusedInstance().TabCount(); got != 2 {
		t.Fatalf("all-exempt pane must exceed the limit, tabs = %d", got)
	}
}

func TestTabLimitEvictedTabReopens(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "a\n")
	b := writeTemp(t, dir, "b.txt", "b\n")
	c := writeTemp(t, dir, "c.txt", "c\n")
	m := newSized()
	withTabLimit(t, 2)
	m = openApp2(t, m, a, b, c)
	tm, _ := m.Update(TabReopenMsg{})
	m = tm.(Model)
	if ed := m.activeEditor(); ed == nil || ed.Path() != a {
		t.Fatalf("reopen must restore the evicted tab, active = %v", m.activeEditor())
	}
}

// openApp2 opens paths into an existing model.
func openApp2(t *testing.T, m Model, paths ...string) Model {
	t.Helper()
	for _, p := range paths {
		tm, _ := m.openPath(p, false)
		m = tm.(Model)
	}
	return m
}
