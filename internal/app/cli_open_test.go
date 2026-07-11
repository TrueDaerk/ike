package app

import (
	"testing"

	"ike/internal/cli"
	"ike/internal/pane"
)

// cli_open_test.go covers OpenCLITargets (Roadmap 0270, #343): tabs in
// argument order, first target focused with the cursor placed, clamping,
// and the missing-path fallback buffer.

func TestCLITargetsOpenAsTabsFirstFocused(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\nbbb\nccc\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	m := newSized()

	m = m.OpenCLITargets([]cli.Target{{Path: a, Line: 2}, {Path: b}})

	inst := m.panes.FocusedInstance()
	if inst.Kind() != pane.KindEditor || inst.TabCount() != 2 {
		t.Fatalf("want 2 tabs, got %d", inst.TabCount())
	}
	// Tab order follows argument order; the first target is active.
	if got := inst.TabEditor(0).Path(); got != a {
		t.Fatalf("tab 0 = %q, want %q", got, a)
	}
	if got := inst.TabEditor(1).Path(); got != b {
		t.Fatalf("tab 1 = %q, want %q", got, b)
	}
	if got := inst.Editor().Path(); got != a {
		t.Fatalf("active tab = %q, want first target %q", got, a)
	}
	// CLI line 2 (1-based) lands the cursor on editor line 1 (0-based).
	if line, _ := inst.Editor().CursorPos(); line != 1 {
		t.Fatalf("cursor line = %d, want 1", line)
	}
}

func TestCLITargetLineColPlacementAndClamp(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "alpha\nbravo charlie\n")
	m := newSized()

	m = m.OpenCLITargets([]cli.Target{{Path: a, Line: 2, Col: 7}})
	line, col := m.panes.FocusedInstance().Editor().CursorPos()
	if line != 1 || col != 6 {
		t.Fatalf("cursor = %d,%d, want 1,6", line, col)
	}

	// Out-of-range line clamps to the last line; unset line/col stay at 0,0.
	m = newSized()
	m = m.OpenCLITargets([]cli.Target{{Path: a, Line: 99}})
	if line, _ := m.panes.FocusedInstance().Editor().CursorPos(); line != 1 {
		t.Fatalf("clamped line = %d, want 1", line)
	}
	m = newSized()
	m = m.OpenCLITargets([]cli.Target{{Path: a}})
	line, col = m.panes.FocusedInstance().Editor().CursorPos()
	if line != 0 || col != 0 {
		t.Fatalf("unset position = %d,%d, want 0,0", line, col)
	}
}

func TestCLITargetMissingPathOpensUnsavedBuffer(t *testing.T) {
	dir := t.TempDir()
	missing := dir + "/new.txt"
	m := newSized()

	m = m.OpenCLITargets([]cli.Target{{Path: missing}})
	ed := m.panes.FocusedInstance().Editor()
	if !ed.HasFile() || ed.Path() != canonicalPath(missing) {
		t.Fatalf("path = %q, want %q", ed.Path(), canonicalPath(missing))
	}
	if ed.Text() != "" || ed.Dirty() {
		t.Fatalf("missing path must open empty and unmodified (text %q dirty %v)", ed.Text(), ed.Dirty())
	}
}

func TestCLITargetsZeroIsNoop(t *testing.T) {
	m := newSized()
	before := m.panes.Focused()
	m = m.OpenCLITargets(nil)
	if m.panes.Focused() != before {
		t.Fatal("zero targets must not change focus")
	}
}
