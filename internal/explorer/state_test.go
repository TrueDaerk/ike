package explorer

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// buildTree lays out a small project: root/{a/{a1.txt}, b/, .hidden/, top.txt}.
func buildTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"a", "b", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"top.txt", filepath.Join("a", "a1.txt")} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func rowPaths(m Model) map[string]bool {
	out := map[string]bool{}
	for _, n := range m.rows {
		out[n.path] = true
	}
	return out
}

// TestRestoreExpandsAndPositions verifies a saved State re-expands directories,
// re-applies show-hidden, and parks the cursor on the saved path.
func TestRestoreExpandsAndPositions(t *testing.T) {
	root := buildTree(t)
	m := New(root)
	subA := filepath.Join(root, "a")
	a1 := filepath.Join(subA, "a1.txt")

	m.Restore(State{Expanded: []string{subA}, ShowHidden: true, Cursor: a1})

	rows := rowPaths(m)
	if !rows[a1] {
		t.Fatalf("expanded child a1.txt not visible after restore; rows=%v", rows)
	}
	if !rows[filepath.Join(root, ".hidden")] {
		t.Fatal("show_hidden=true should reveal .hidden")
	}
	if n := m.currentConst(); n == nil || n.path != a1 {
		t.Fatalf("cursor not on saved path; got %v", n)
	}
}

// TestSnapshotRoundTrip checks Snapshot captures exactly what Restore re-applies.
func TestSnapshotRoundTrip(t *testing.T) {
	root := buildTree(t)
	subA := filepath.Join(root, "a")

	m := New(root)
	m.Restore(State{Expanded: []string{subA}, ShowHidden: true, Cursor: subA})

	s := m.Snapshot()
	if !s.ShowHidden {
		t.Fatal("snapshot lost show_hidden")
	}
	if s.Cursor != subA {
		t.Fatalf("snapshot cursor = %q, want %q", s.Cursor, subA)
	}
	if len(s.Expanded) != 1 || s.Expanded[0] != subA {
		t.Fatalf("snapshot expanded = %v, want [%q]", s.Expanded, subA)
	}

	// Re-restoring the snapshot into a fresh explorer reproduces the view.
	m2 := New(root)
	m2.Restore(s)
	if !rowPaths(m2)[filepath.Join(subA, "a1.txt")] {
		t.Fatal("round-tripped snapshot did not re-expand a")
	}
}

// TestRestoreSkipsMissingDirs ensures a stale expanded path is ignored, not fatal.
func TestRestoreSkipsMissingDirs(t *testing.T) {
	root := buildTree(t)
	m := New(root)
	m.Restore(State{Expanded: []string{filepath.Join(root, "gone")}})
	// Root children still load; the bogus path is simply skipped.
	if !rowPaths(m)[filepath.Join(root, "top.txt")] {
		t.Fatal("root should still load when an expanded path is missing")
	}
}

// TestRestoreThenSizeKeepsClicksAligned guards the session-restore click bug:
// Restore runs while the pane has no size (height 0), so its clampScroll parks a
// large offset; once the real size arrives the offset must be pulled back into
// the renderable range, otherwise View shows one page while MouseClick reads a
// stale offset and selects a row far below the one clicked.
func TestRestoreThenSizeKeepsClicksAligned(t *testing.T) {
	root := t.TempDir()
	var dirs []string
	for i := 0; i < 40; i++ {
		d := filepath.Join(root, "dir"+strconv.Itoa(i))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "f.txt"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, d)
	}
	m := New(root)
	// Restore (height still 0) with the cursor near the end of the expanded tree.
	m.Restore(State{Expanded: dirs, Cursor: dirs[len(dirs)-1]})
	m.SetSize(30, 20)

	_, textH, _, _, _ := m.viewport()
	if maxOff := len(m.rows) - textH; m.offset > maxOff {
		t.Fatalf("offset %d exceeds maxOff %d after sizing — clicks will desync", m.offset, maxOff)
	}
	// The row clicked at the top of the viewport must be the row rendered there.
	want := m.rows[m.offset].name
	mc, _ := m.MouseClick(0, 0)
	if got := mc.rows[mc.cursor].name; got != want {
		t.Fatalf("click top row selected %q, want the rendered top row %q", got, want)
	}
}

// TestInitSkipsScanAfterRestore guards the Init/Restore interplay: once Restore
// has loaded the root synchronously, Init must not issue an async re-scan that
// would discard the restored expansion.
func TestInitSkipsScanAfterRestore(t *testing.T) {
	root := buildTree(t)
	m := New(root)
	m.autoRefresh = false // the poll Cmd would otherwise mask the nil check
	if m.Init() == nil {
		t.Fatal("fresh explorer should scan on Init")
	}
	m.Restore(State{})
	if m.Init() != nil {
		t.Fatal("Init must not re-scan after Restore loaded the root")
	}
}
