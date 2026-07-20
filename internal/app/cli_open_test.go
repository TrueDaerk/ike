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

	inst := m.activeWS().Panes.FocusedInstance()
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
	line, col := m.activeWS().Panes.FocusedInstance().Editor().CursorPos()
	if line != 1 || col != 6 {
		t.Fatalf("cursor = %d,%d, want 1,6", line, col)
	}

	// Out-of-range line clamps to the last line; unset line/col stay at 0,0.
	m = newSized()
	m = m.OpenCLITargets([]cli.Target{{Path: a, Line: 99}})
	if line, _ := m.activeWS().Panes.FocusedInstance().Editor().CursorPos(); line != 1 {
		t.Fatalf("clamped line = %d, want 1", line)
	}
	m = newSized()
	m = m.OpenCLITargets([]cli.Target{{Path: a}})
	line, col = m.activeWS().Panes.FocusedInstance().Editor().CursorPos()
	if line != 0 || col != 0 {
		t.Fatalf("unset position = %d,%d, want 0,0", line, col)
	}
}

func TestCLITargetMissingPathOpensUnsavedBuffer(t *testing.T) {
	dir := t.TempDir()
	missing := dir + "/new.txt"
	m := newSized()

	m = m.OpenCLITargets([]cli.Target{{Path: missing}})
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	if !ed.HasFile() || ed.Path() != canonicalPath(missing) {
		t.Fatalf("path = %q, want %q", ed.Path(), canonicalPath(missing))
	}
	if ed.Text() != "" || ed.Dirty() {
		t.Fatalf("missing path must open empty and unmodified (text %q dirty %v)", ed.Text(), ed.Dirty())
	}
}

func TestStdinBufferOpensPathlessDirtyFocused(t *testing.T) {
	m := newSized()
	m = m.OpenStdinBuffer("piped line 1\npiped line 2\n")
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	if ed.HasFile() {
		t.Fatalf("scratch buffer must be pathless, got %q", ed.Path())
	}
	// The buffer normalizes the trailing newline away (same as Load); the
	// save flow re-adds it via insert_final_newline.
	if got := ed.Text(); got != "piped line 1\npiped line 2" {
		t.Fatalf("text = %q", got)
	}
	// Dirty + never-saved: quitting must run the unsaved-changes guard.
	if !ed.Dirty() {
		t.Fatal("scratch buffer must be dirty")
	}
}

func TestStdinBufferAppendsAfterFileTargets(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	m := newSized()
	m = m.OpenCLITargets([]cli.Target{{Path: a}})
	m = m.OpenStdinBuffer("piped\n")

	inst := m.activeWS().Panes.FocusedInstance()
	if inst.TabCount() != 2 {
		t.Fatalf("want 2 tabs (file + scratch), got %d", inst.TabCount())
	}
	// The scratch tab is appended after the file targets and wins focus.
	if ed := inst.Editor(); ed.HasFile() || ed.Text() != "piped" {
		t.Fatalf("active tab must be the scratch buffer (path %q text %q)", ed.Path(), ed.Text())
	}
	if got := inst.TabEditor(0).Path(); got != a {
		t.Fatalf("tab 0 = %q, want %q", got, a)
	}
}

func TestCLITargetsZeroIsNoop(t *testing.T) {
	m := newSized()
	before := m.activeWS().Panes.Focused()
	m = m.OpenCLITargets(nil)
	if m.activeWS().Panes.Focused() != before {
		t.Fatal("zero targets must not change focus")
	}
}
