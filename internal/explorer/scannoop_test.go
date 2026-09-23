package explorer

import (
	"path/filepath"
	"testing"

	"ike/internal/watch"
)

// rescan runs one watcher-driven refresh of dir and applies its result.
func rescan(t *testing.T, m Model, dir string) Model {
	t.Helper()
	m, cmd := m.Update(watch.EventMsg{Kind: watch.DirChanged, Path: dir})
	if cmd == nil {
		t.Fatal("the directory event must launch a rescan")
	}
	msg := cmd()
	sd, ok := msg.(ScanDoneMsg)
	if !ok {
		t.Fatalf("rescan produced %T, want ScanDoneMsg", msg)
	}
	m, _ = m.Update(sd)
	return m
}

// TestLastScanNoop (#2693): a rescan that lists exactly what the node holds
// is a no-op the app may skip the frame for; an added or removed entry or a
// pending cursor snap is not.
func TestLastScanNoop(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	if m.LastScanNoop() {
		t.Fatal("the first scan of a node loads it: never a no-op")
	}
	m = rescan(t, m, root)
	if !m.LastScanNoop() {
		t.Fatal("a rescan over an unchanged listing must be a no-op")
	}
	before := names(m)

	mustWrite(t, filepath.Join(root, "d.txt"), "d")
	m = rescan(t, m, root)
	if m.LastScanNoop() {
		t.Fatal("a rescan that found a new entry is a change")
	}
	if got := names(m); len(got) != len(before)+1 {
		t.Fatalf("rows after the new entry = %v, want one more than %v", got, before)
	}
	m = rescan(t, m, root)
	if !m.LastScanNoop() {
		t.Fatal("the following rescan lists the same entries again: a no-op")
	}

	// A deliberate cursor snap armed while the scan is in flight (a file op
	// landing on the same directory) means the rebuild may move the cursor
	// and reframe: not a no-op even over an unchanged listing.
	m, cmd := m.Update(watch.EventMsg{Kind: watch.DirChanged, Path: root})
	sd := cmd().(ScanDoneMsg)
	m.snapCursorTo(filepath.Join(root, "b.txt"))
	m, _ = m.Update(sd)
	if m.LastScanNoop() {
		t.Fatal("a rescan applying a deliberate selection snap is a change")
	}
}
