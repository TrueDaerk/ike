package app

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"ike/internal/host"
	"ike/internal/pane"
)

// tabsession_test.go covers the per-tab half of session restore (#2177): the
// caret and viewport framing of every tab round-trip beside the tab list, the
// tabs that are not active restore lazily, and files gone since the save are
// skipped with one summary notice.

// numberedLines builds n lines of "L0…Ln-1", long enough to scroll.
func numberedLines(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString("L")
		b.WriteString(strconv.Itoa(i))
		b.WriteByte('\n')
	}
	return b.String()
}

// openAll opens every path in m through the ordinary open funnel, in order.
func openAll(t *testing.T, m Model, paths ...string) Model {
	t.Helper()
	for _, p := range paths {
		tm, _ := m.openPath(p, false)
		m = tm.(Model)
	}
	return m
}

func TestPerTabCursorAndScrollRoundTrip(t *testing.T) {
	conf, dir := t.TempDir(), t.TempDir()
	a := writeTemp(t, dir, "a.txt", numberedLines(60))
	b := writeTemp(t, dir, "b.txt", numberedLines(60))
	c := writeTemp(t, dir, "c.txt", numberedLines(60))

	m := fixedDirApp(t, conf)
	m = openAll(t, m, a, b, c)
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)

	// A distinct caret per tab, each parked below a sticky framing: scroll
	// deep first, then move back up, so Top is not derivable from the cursor.
	want := map[string][2]int{a: {40, 0}, b: {30, 1}, c: {20, 2}}
	tops := map[string]int{}
	for i, p := range []string{a, b, c} {
		m = dispatch(t, m, TabSelectMsg{Index: i})
		ed := inst.Editor()
		ed.SetCursor(50, 0)
		ed.SetCursor(want[p][0], want[p][1])
		tops[p], _ = ed.ScrollOffset()
		if tops[p] == 0 {
			t.Fatalf("%s: the test needs a non-zero framing to prove anything", p)
		}
	}
	m = dispatch(t, m, TabSelectMsg{Index: 1}) // quit with b active
	if _, cmd := m.quit(); cmd == nil {
		t.Fatal("quit should return a command")
	}

	m2 := fixedDirApp(t, conf)
	inst2 := m2.activeWS().Panes.Get(key)
	if inst2 == nil || inst2.TabCount() != 3 {
		t.Fatalf("want the 3-tab pane back under %q", key)
	}
	if inst2.ActiveTab() != 1 || inst2.TabPath(1) != b {
		t.Fatalf("active tab = %d (%q), want 1 (%q)", inst2.ActiveTab(), inst2.TabPath(1), b)
	}
	for i, p := range []string{a, b, c} {
		m2 = dispatch(t, m2, TabSelectMsg{Index: i})
		ed := inst2.Editor()
		if ed == nil || ed.Path() != p {
			t.Fatalf("tab %d must show %q", i, p)
		}
		if line, col := ed.CursorPos(); line != want[p][0] || col != want[p][1] {
			t.Fatalf("%s: restored cursor = (%d,%d), want %v", p, line, col, want[p])
		}
		if top, _ := ed.ScrollOffset(); top != tops[p] {
			t.Fatalf("%s: restored framing top = %d, want %d", p, top, tops[p])
		}
	}
}

func TestInactiveRestoredTabsLoadLazily(t *testing.T) {
	conf, dir := t.TempDir(), t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	c := writeTemp(t, dir, "c.txt", "ccc\n")

	m := fixedDirApp(t, conf)
	m = openAll(t, m, a, b, c) // c stays active
	key := m.activeWS().Panes.Focused()

	m2 := fixedDirApp(t, conf)
	inst := m2.activeWS().Panes.Get(key)
	for i, p := range []string{a, b} {
		if _, deferred := inst.TabDeferredView(i); !deferred {
			t.Fatalf("tab %d (%q) must restore deferred, not read at startup", i, p)
		}
		if ed := inst.TabEditor(i); ed == nil || ed.HasFile() {
			t.Fatalf("tab %d (%q) must hold no document before activation", i, p)
		}
		if inst.TabPath(i) != p {
			t.Fatalf("a deferred tab must still know its file, got %q", inst.TabPath(i))
		}
	}
	if _, deferred := inst.TabDeferredView(2); deferred {
		t.Fatal("the active tab must be read at startup — it is on screen")
	}

	// Activating one reads exactly that file; the other stays deferred.
	m2 = dispatch(t, m2, TabSelectMsg{Index: 0})
	if ed := inst.TabEditor(0); ed == nil || ed.Path() != a || !strings.Contains(ed.Text(), "aaa") {
		t.Fatal("activating a deferred tab must load its file")
	}
	if _, deferred := inst.TabDeferredView(1); !deferred {
		t.Fatal("activating one tab must not read the whole strip")
	}
}

// TestLazyLoadIsWiredLikeAnOpen checks the bookkeeping behind a lazy load: the
// registry records the freshly read file so the update pass can give it the
// wiring an explicit open gives — highlighting, marks, hooks — and the record
// is consumed there rather than piling up.
func TestLazyLoadIsWiredLikeAnOpen(t *testing.T) {
	conf, dir := t.TempDir(), t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")

	m := fixedDirApp(t, conf)
	m = openAll(t, m, a, b)
	key := m.activeWS().Panes.Focused()
	m.quit()

	m2 := fixedDirApp(t, conf)
	if pending := m2.activeWS().Panes.TakeLoaded(); len(pending) != 0 {
		t.Fatalf("the restore's own loads are wired by Init, got %v", pending)
	}
	m2.activeWS().Panes.Get(key).MaterializeTab(0)
	if got := m2.activeWS().Panes.TakeLoaded(); len(got) != 1 || got[0] != a {
		t.Fatalf("a lazy load must record its file for wiring, got %v", got)
	}

	m3 := fixedDirApp(t, conf)
	m3 = dispatch(t, m3, TabSelectMsg{Index: 0})
	if pending := m3.activeWS().Panes.TakeLoaded(); len(pending) != 0 {
		t.Fatalf("the update pass must drain the record, got %v", pending)
	}
}

// TestDeferredTabSurvivesAnUntouchedRestart guards the round trip a lazy
// restore makes possible: quitting without ever activating a restored tab
// must persist it — path, position in the strip, and caret — unchanged.
func TestDeferredTabSurvivesAnUntouchedRestart(t *testing.T) {
	conf, dir := t.TempDir(), t.TempDir()
	a := writeTemp(t, dir, "a.txt", numberedLines(60))
	b := writeTemp(t, dir, "b.txt", numberedLines(60))

	m := fixedDirApp(t, conf)
	m = openAll(t, m, a, b)
	key := m.activeWS().Panes.Focused()
	m = dispatch(t, m, TabSelectMsg{Index: 0})
	m.activeWS().Panes.Get(key).Editor().SetCursor(33, 2)
	m = dispatch(t, m, TabSelectMsg{Index: 1}) // a is inactive at quit
	m.quit()

	// Second launch: never touch a, quit again.
	m2 := fixedDirApp(t, conf)
	if _, deferred := m2.activeWS().Panes.Get(key).TabDeferredView(0); !deferred {
		t.Fatal("the inactive tab must come back deferred")
	}
	m2.quit()

	m3 := fixedDirApp(t, conf)
	inst := m3.activeWS().Panes.Get(key)
	if inst.TabCount() != 2 || inst.TabPath(0) != a {
		t.Fatalf("an untouched deferred tab must persist, got %d tabs", inst.TabCount())
	}
	m3 = dispatch(t, m3, TabSelectMsg{Index: 0})
	if line, col := inst.Editor().CursorPos(); line != 33 || col != 2 {
		t.Fatalf("caret through two restarts = (%d,%d), want (33,2)", line, col)
	}
}

func TestMissingRestoredFilesNotifyOnce(t *testing.T) {
	conf, dir := t.TempDir(), t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	c := writeTemp(t, dir, "c.txt", "ccc\n")

	m := fixedDirApp(t, conf)
	m = openAll(t, m, a, b, c)
	key := m.activeWS().Panes.Focused()
	m.quit()

	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	m2 := fixedDirApp(t, conf)
	inst := m2.activeWS().Panes.Get(key)
	if inst.TabCount() != 1 || inst.TabPath(0) != c {
		t.Fatalf("only the surviving file must reopen, got %d tabs", inst.TabCount())
	}
	// The construction's first Update already drained the queue into the
	// notification history, which is where a startup notice ends up.
	warns := 0
	for _, e := range m2.history {
		if e.sev == host.Warn && strings.Contains(e.text, "session restore") {
			warns++
			if !strings.Contains(e.text, "2 files") {
				t.Fatalf("the notice must summarize the whole restore, got %q", e.text)
			}
		}
	}
	if warns != 1 {
		t.Fatalf("want exactly one summary notice for both missing files, got %d", warns)
	}
}

// TestMissingDeferredFileLeavesTheStripIntact covers the file that vanishes
// *between* restore and activation — too late for the stat sweep: the tab
// falls back to an empty scratch slot instead of showing stale content, and
// the neighbouring tabs keep working.
func TestMissingDeferredFileLeavesTheStripIntact(t *testing.T) {
	conf, dir := t.TempDir(), t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")

	m := fixedDirApp(t, conf)
	m = openAll(t, m, a, b)
	key := m.activeWS().Panes.Focused()
	m.quit()

	m2 := fixedDirApp(t, conf)
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	inst := m2.activeWS().Panes.Get(key)
	m2 = dispatch(t, m2, TabSelectMsg{Index: 0})
	if ed := inst.TabEditor(0); ed == nil || ed.HasFile() {
		t.Fatal("a deferred tab whose file vanished must fall back to an empty tab")
	}
	m2 = dispatch(t, m2, TabSelectMsg{Index: 1})
	if ed := inst.TabEditor(1); ed == nil || ed.Path() != b {
		t.Fatal("the rest of the strip must be unaffected")
	}
}

// TestDeferredTabActivatesInsteadOfDuplicating: opening a file that a
// deferred tab already holds must land on that tab — the strip is the
// document's slot whether or not its bytes are in memory yet.
func TestDeferredTabActivatesInsteadOfDuplicating(t *testing.T) {
	conf, dir := t.TempDir(), t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")

	m := fixedDirApp(t, conf)
	m = openAll(t, m, a, b)
	key := m.activeWS().Panes.Focused()
	m.quit()

	m2 := fixedDirApp(t, conf)
	inst := m2.activeWS().Panes.Get(key)
	tm, _ := m2.openPath(a, false)
	m2 = tm.(Model)
	if inst.TabCount() != 2 {
		t.Fatalf("opening a deferred file must reuse its tab, got %d tabs", inst.TabCount())
	}
	if inst.ActiveTab() != 0 || inst.Editor() == nil || inst.Editor().Path() != a {
		t.Fatal("the deferred tab must become the active, loaded one")
	}
	if _, ok := inst.TabDeferredView(0); ok {
		t.Fatal("the tab must have materialized")
	}
}

// TestPaneViewsSnapshotKeepsEveryTab checks the session payload itself: one
// entry per document tab of every pane, deferred ones included.
func TestPaneViewsSnapshotKeepsEveryTab(t *testing.T) {
	conf, dir := t.TempDir(), t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")

	m := fixedDirApp(t, conf)
	m = openAll(t, m, a, b)
	key := m.activeWS().Panes.Focused()
	m.quit()

	m2 := fixedDirApp(t, conf)
	views := m2.snapshotSession().Panes[key]
	if len(views) != 2 || views[0].Path != a || views[1].Path != b {
		t.Fatalf("the snapshot must list every tab in order, got %+v", views)
	}
	if idx := tabViewIndex(map[string][]tabView{key: views}); len(idx[key]) != 2 {
		t.Fatal("the index must key each pane's views by path")
	}
	if m2.activeWS().Panes.Get(key).Kind() != pane.KindEditor {
		t.Fatal("the pane must still be an editor pane")
	}
}
