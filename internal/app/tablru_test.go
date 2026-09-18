package app

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
)

// tablru_test.go is the regression suite of #2640: the tab limit must close the
// tab the user really used least — through the file-open path, across a restart
// and without pinned tabs spending a slot of the limit.

// openedTabs lists the pane's document tab paths by base name, deferred tabs
// (#2177) included — a restored tab still counts as open.
func openedTabs(inst *pane.Instance) []string {
	var out []string
	for i := 0; i < inst.TabCount(); i++ {
		if inst.TabEditor(i) != nil {
			out = append(out, filepath.Base(inst.TabPath(i)))
		}
	}
	return out
}

// hasTab reports whether the pane holds a tab for path.
func hasTab(inst *pane.Instance, path string) bool { return inst.TabForPath(path) >= 0 }

// TestTabLimitEvictsTheTrueLRUNotAFixedSlot reproduces the report: with limit 5
// and tabs a..e, using a and c and then opening f must close b — the oldest
// untouched tab — and every further open must close the next-oldest one instead
// of recycling the same slot.
func TestTabLimitEvictsTheTrueLRUNotAFixedSlot(t *testing.T) {
	dir := t.TempDir()
	var p []string
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		p = append(p, writeTemp(t, dir, n+".txt", n+"\n"))
	}
	m := newSized()
	withTabLimit(t, 5)
	m = openApp2(t, m, p[0], p[1], p[2], p[3], p[4]) // a..e

	inst := m.activeWS().Panes.FocusedInstance()
	if got := inst.TabCount(); got != 5 {
		t.Fatalf("five tabs must fit the limit, got %v", openedTabs(inst))
	}
	// The user works in a, then in c — the file-open path (palette @) is how
	// they get there, and it must stamp recency.
	m = openApp2(t, m, p[0], p[2])

	m = openApp2(t, m, p[5]) // open f: b is the oldest untouched tab
	inst = m.activeWS().Panes.FocusedInstance()
	if hasTab(inst, p[1]) {
		t.Fatalf("b is the least recently used tab and must be evicted, tabs = %v", openedTabs(inst))
	}
	m = openApp2(t, m, p[6]) // open g: d is next
	inst = m.activeWS().Panes.FocusedInstance()
	if hasTab(inst, p[3]) {
		t.Fatalf("d must be evicted next, tabs = %v", openedTabs(inst))
	}
	m = openApp2(t, m, p[7]) // open h: e is next
	inst = m.activeWS().Panes.FocusedInstance()
	if hasTab(inst, p[4]) {
		t.Fatalf("e must be evicted next, tabs = %v", openedTabs(inst))
	}
	// The tabs the user actually used survived all three opens.
	if !hasTab(inst, p[0]) || !hasTab(inst, p[2]) {
		t.Fatalf("the used tabs a and c must survive, tabs = %v", openedTabs(inst))
	}
	if got := inst.TabCount(); got != 5 {
		t.Fatalf("the pane must stay at the limit, tabs = %v", openedTabs(inst))
	}
}

// TestTabLimitAfterRestoreDoesNotRecycleOneSlot: the recency order rides along
// in the session, so a restarted pane still evicts by real recency — and two
// consecutive opens evict two different tabs instead of the same slot twice.
func TestTabLimitAfterRestoreEvictsByPersistedRecency(t *testing.T) {
	conf := t.TempDir()
	dir := t.TempDir()
	var p []string
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		p = append(p, writeTemp(t, dir, n+".txt", n+"\n"))
	}
	m := fixedDirApp(t, conf)
	m = openApp2(t, m, p[0], p[1], p[2], p[3], p[4]) // a..e, default limit 5
	// A tab switch persists the layout — and with it the use order: d, then e
	// back on top, leaving a the least recently used tab of the session.
	m = dispatch(t, m, TabSelectMsg{Index: 3})
	m = dispatch(t, m, TabSelectMsg{Index: 4})

	// Restart: the strip comes back as deferred tabs (#2177) carrying the
	// recorded use order — a..e, a the least recently used.
	key := m.activeWS().Panes.Focused()
	m2 := fixedDirApp(t, conf)
	inst := m2.activeWS().Panes.Get(key)
	if inst == nil || inst.TabCount() != 5 {
		t.Fatalf("the pane must restore with five tabs, got %v", openedTabs(inst))
	}
	m2 = openApp2(t, m2, p[2]) // only c is used before the next open

	m2 = openApp2(t, m2, p[5]) // open f
	inst = m2.activeWS().Panes.Get(key)
	if !hasTab(inst, p[2]) || !hasTab(inst, p[5]) {
		t.Fatalf("the used and the opened tab must survive, tabs = %v", openedTabs(inst))
	}
	if hasTab(inst, p[0]) {
		t.Fatalf("a is the least recently used tab and must be evicted, tabs = %v", openedTabs(inst))
	}
	m2 = openApp2(t, m2, p[6]) // open g: a different tab must go this time
	inst = m2.activeWS().Panes.Get(key)
	if hasTab(inst, p[1]) {
		t.Fatalf("the second open must evict b, not recycle one slot, tabs = %v", openedTabs(inst))
	}
	if !hasTab(inst, p[2]) || !hasTab(inst, p[5]) || !hasTab(inst, p[6]) {
		t.Fatalf("c, f and g must all be open, tabs = %v", openedTabs(inst))
	}
	// Evicted restored tabs still land in the reopen ring (#158).
	if len(m2.closedTabs) < 2 {
		t.Fatalf("evicted restored tabs must stay reopenable, ring = %+v", m2.closedTabs)
	}
}

// TestPinnedTabsDoNotCountTowardTabLimit: with limit 5 and three pinned tabs,
// five unpinned tabs coexist beside them; the sixth unpinned open evicts the
// least recently used unpinned tab and never a pinned one.
func TestPinnedTabsDoNotCountTowardTabLimit(t *testing.T) {
	dir := t.TempDir()
	var pins, files []string
	for _, n := range []string{"p1", "p2", "p3"} {
		pins = append(pins, writeTemp(t, dir, n+".txt", n+"\n"))
	}
	for _, n := range []string{"u1", "u2", "u3", "u4", "u5", "u6"} {
		files = append(files, writeTemp(t, dir, n+".txt", n+"\n"))
	}
	m := newSized()
	withTabLimit(t, 5)
	for _, p := range pins {
		m = openApp2(t, m, p)
		m = dispatch(t, m, TabTogglePinMsg{}) // pin the freshly opened tab
	}
	m = openApp2(t, m, files[0], files[1], files[2], files[3], files[4])

	inst := m.activeWS().Panes.FocusedInstance()
	if got := inst.TabCount(); got != 8 {
		t.Fatalf("3 pinned + 5 unpinned tabs must coexist, tabs = %v", openedTabs(inst))
	}
	m = openApp2(t, m, files[5]) // the sixth unpinned open
	inst = m.activeWS().Panes.FocusedInstance()
	if hasTab(inst, files[0]) {
		t.Fatalf("the LRU unpinned tab must be evicted, tabs = %v", openedTabs(inst))
	}
	for _, p := range pins {
		if !hasTab(inst, p) {
			t.Fatalf("pinned tabs must never be evicted, tabs = %v", openedTabs(inst))
		}
	}
	if got := inst.TabCount(); got != 8 {
		t.Fatalf("the pane must hold 3 pinned + 5 unpinned tabs, tabs = %v", openedTabs(inst))
	}
}

// TestCloseTabClosesPinnedActiveTab: pinning exempts a tab from the automatic
// closes only — cmd+w on the pinned active tab still closes it (#1172).
func TestCloseTabClosesPinnedActiveTab(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "a\n")
	b := writeTemp(t, dir, "b.txt", "b\n")
	m := newSized()
	m = openApp2(t, m, a, b)
	m = dispatch(t, m, TabTogglePinMsg{}) // pin b, the active tab
	inst := m.activeWS().Panes.FocusedInstance()
	if !inst.TabPinned(inst.ActiveTab()) {
		t.Fatal("precondition: the active tab must be pinned")
	}
	m = dispatch(t, m, CloseTabMsg{})

	inst = m.activeWS().Panes.FocusedInstance()
	if hasTab(inst, b) {
		t.Fatalf("an explicit close must close the pinned tab, tabs = %v", openedTabs(inst))
	}
	if !hasTab(inst, a) {
		t.Fatalf("the remaining tab must stay open, tabs = %v", openedTabs(inst))
	}
}

// TestBatchClosesSkipPinnedTabs covers the #2538 scopes beyond Close Others:
// Close Left, Close Right, Close Unmodified and Close All all keep pinned tabs.
func TestBatchClosesSkipPinnedTabs(t *testing.T) {
	// run pins the tab at pinAt, activates activeAt, dispatches the batch
	// close and expects the pinned tab to still be there.
	run := func(t *testing.T, pinAt, activeAt int, msg tea.Msg) {
		dir := t.TempDir()
		a := writeTemp(t, dir, "a.txt", "a\n")
		b := writeTemp(t, dir, "b.txt", "b\n")
		c := writeTemp(t, dir, "c.txt", "c\n")
		paths := []string{a, b, c}
		m := newSized()
		m = openApp2(t, m, a, b, c)
		m = dispatch(t, m, TabSelectMsg{Index: pinAt})
		m = dispatch(t, m, TabTogglePinMsg{})
		m = dispatch(t, m, TabSelectMsg{Index: activeAt})
		m = dispatch(t, m, msg)

		inst := m.activeWS().Panes.FocusedInstance()
		if inst == nil || inst.Kind() != pane.KindEditor {
			t.Fatalf("%T must leave the pane holding the pinned tab", msg)
		}
		if !hasTab(inst, paths[pinAt]) {
			t.Fatalf("%T must keep the pinned tab, tabs = %v", msg, openedTabs(inst))
		}
	}
	t.Run("closeLeft", func(t *testing.T) { run(t, 0, 2, TabCloseSideMsg{Delta: -1}) })
	t.Run("closeRight", func(t *testing.T) { run(t, 2, 0, TabCloseSideMsg{Delta: 1}) })
	t.Run("closeUnmodified", func(t *testing.T) { run(t, 0, 2, TabCloseUnmodifiedMsg{}) })
	t.Run("closeAll", func(t *testing.T) { run(t, 0, 2, TabCloseAllMsg{}) })
}
