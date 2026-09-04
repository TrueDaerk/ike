package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
	"ike/internal/vcs"
)

// TestDiffReopenFocusesExisting guards #509: opening the same diff again
// focuses the existing pane instead of splitting a duplicate.
func TestDiffReopenFocusesExisting(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	left := filepath.Join(dir, "l.txt")
	right := filepath.Join(dir, "r.txt")
	os.WriteFile(left, []byte("a\n"), 0o644)
	os.WriteFile(right, []byte("b\n"), 0o644)

	m := newSized()
	m.openDiffPane(left, right)
	key := m.activeWS().Panes.Focused()
	m.setFocus(pane.ExplorerKey)
	count := len(m.activeWS().Panes.Keys())

	m.openDiffPane(left, right)
	if len(m.activeWS().Panes.Keys()) != count {
		t.Fatal("re-open must not create a second pane")
	}
	if m.activeWS().Panes.Focused() != key {
		t.Fatalf("focus = %q, want the existing diff %q", m.activeWS().Panes.Focused(), key)
	}

	// A HEAD diff of the same file also dedupes.
	m.vcs.snap = vcs.NewSnapshot(dir, map[string]vcs.FileStatus{"r.txt": vcs.StatusModified})
	out, _ := m.Update(vcs.HeadDiffMsg{Path: right, Head: "old\n"})
	m2 := out.(Model)
	headKey := m2.activeWS().Panes.Focused()
	m2.setFocus(pane.ExplorerKey)
	count = len(m2.activeWS().Panes.Keys())
	out, _ = m2.Update(vcs.HeadDiffMsg{Path: right, Head: "old\n"})
	m2 = out.(Model)
	if len(m2.activeWS().Panes.Keys()) != count || m2.activeWS().Panes.Focused() != headKey {
		t.Fatalf("head diff re-open: panes=%d focus=%q want %q", len(m2.activeWS().Panes.Keys()), m2.activeWS().Panes.Focused(), headKey)
	}
}

// TestDiffSingleWindowRetargets guards #513: opening a different diff reuses
// the one diff pane by default; diff.windows = "multi" restores splitting.
func TestDiffSingleWindowRetargets(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	c := filepath.Join(dir, "c.txt")
	for _, p := range []string{a, b, c} {
		os.WriteFile(p, []byte(p+"\n"), 0o644)
	}

	m := newSized()
	m.openDiffPane(a, b)
	key := m.activeWS().Panes.Focused()
	count := len(m.activeWS().Panes.Keys())

	// A different pair retargets the same pane.
	m.openDiffPane(a, c)
	if len(m.activeWS().Panes.Keys()) != count || m.activeWS().Panes.Focused() != key {
		t.Fatalf("second diff split a new pane (panes=%d focus=%q)", len(m.activeWS().Panes.Keys()), m.activeWS().Panes.Focused())
	}
	if got := m.activeWS().Panes.Get(key).Diff().RightPath(); got != c {
		t.Fatalf("retarget right = %q, want %q", got, c)
	}
	// A HEAD diff also lands in the slot, flipping revs/titles.
	m.vcs.snap = vcs.NewSnapshot(dir, map[string]vcs.FileStatus{"b.txt": vcs.StatusModified})
	out, _ := m.Update(vcs.HeadDiffMsg{Path: b, Head: "old\n"})
	m = out.(Model)
	if len(m.activeWS().Panes.Keys()) != count {
		t.Fatal("head diff split a new pane")
	}
	if lr, _ := m.activeWS().Panes.Get(key).Diff().Revs(); lr != "HEAD" {
		t.Fatalf("retarget revs = %q", lr)
	}
}

// TestDiffMultiWindowConfigOpensSecondViewer guards diff.windows = "multi"
// under the focused placement (#2507): the second diff opens a second viewer
// — a tab beside the first — instead of retargeting the one slot.
func TestDiffMultiWindowConfigOpensSecondViewer(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	c := filepath.Join(dir, "c.txt")
	for _, p := range []string{a, b, c} {
		os.WriteFile(p, []byte(p+"\n"), 0o644)
	}
	m := NewWith(registry.New(), host.MapConfig{"diff.windows": "multi"})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	m.openDiffPane(a, b)
	m.openDiffPane(a, c)
	if n := countDiffViewers(m); n != 2 {
		t.Fatalf("multi mode must open a second viewer (diffs=%d)", n)
	}
	if inst := m.focusedContent(); inst == nil || inst.Diff().RightPath() != c {
		t.Fatal("the second diff must be the focused one")
	}
}

// TestDiffMultiWindowSplitPlacementSplits: with diff.placement = "split" the
// multi mode carves off a second pane, exactly as before #2507.
func TestDiffMultiWindowSplitPlacementSplits(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	c := filepath.Join(dir, "c.txt")
	for _, p := range []string{a, b, c} {
		os.WriteFile(p, []byte(p+"\n"), 0o644)
	}
	m := NewWith(registry.New(), host.MapConfig{"diff.windows": "multi", "diff.placement": "split"})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	m.openDiffPane(a, b)
	count := len(m.activeWS().Panes.Keys())
	m.openDiffPane(a, c)
	if len(m.activeWS().Panes.Keys()) != count+1 {
		t.Fatalf("multi mode must split (panes=%d)", len(m.activeWS().Panes.Keys()))
	}
}

// countDiffViewers counts every open diff — dedicated pane or content tab.
func countDiffViewers(m Model) int {
	n := 0
	m.contentInstances(func(_ string, _ int, c *pane.Instance) bool {
		if c.Kind() == pane.KindDiff {
			n++
		}
		return true
	})
	return n
}

// TestDiffF7StepsHunks guards #495: F7 / shift+F7 drive the focused diff
// pane's hunk navigation through the diff-scoped default bindings.
func TestDiffF7StepsHunks(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	left := filepath.Join(dir, "l.txt")
	right := filepath.Join(dir, "r.txt")
	os.WriteFile(left, []byte("a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\n"), 0o644)
	os.WriteFile(right, []byte("A\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nL\n"), 0o644)

	m := newSized()
	m.openDiffPane(left, right)
	inst := m.activeWS().Panes.FocusedInstance()
	if inst.Kind() != pane.KindDiff || inst.Diff().HunkCount() != 2 {
		t.Fatalf("setup: kind=%v hunks=%d", inst.Kind(), inst.Diff().HunkCount())
	}

	// The chord resolves to a command whose Run dispatches the step message;
	// run the returned command tree (a batch since #679) like the program
	// loop would.
	press := func(k tea.KeyPressMsg) {
		t.Helper()
		m = drainKey(m, k)
	}

	press(tea.KeyPressMsg{Code: tea.KeyF7})
	if got := m.activeWS().Panes.FocusedInstance().Diff().CurrentHunk(); got != 0 {
		t.Fatalf("after F7: hunk = %d, want 0", got)
	}
	press(tea.KeyPressMsg{Code: tea.KeyF7})
	if got := m.activeWS().Panes.FocusedInstance().Diff().CurrentHunk(); got != 1 {
		t.Fatalf("after F7 F7: hunk = %d, want 1", got)
	}
	press(tea.KeyPressMsg{Code: tea.KeyF7, Mod: tea.ModShift})
	if got := m.activeWS().Panes.FocusedInstance().Diff().CurrentHunk(); got != 0 {
		t.Fatalf("after shift+F7: hunk = %d, want 0", got)
	}
}

// TestDiffArrowsScrollHorizontally guards #1700: the arrow keys reach the
// focused diff pane and shift its shared horizontal offset — no global binding
// steals them — and the mouse seam clamps the same way.
func TestDiffArrowsScrollHorizontally(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	long := strings.Repeat("wxyz ", 60) + "\n"
	left := filepath.Join(dir, "l.txt")
	right := filepath.Join(dir, "r.txt")
	os.WriteFile(left, []byte(long), 0o644)
	os.WriteFile(right, []byte(long), 0o644)

	m := newSized()
	m.openDiffPane(left, right)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if got := m.activeWS().Panes.FocusedInstance().Diff().HOffset(); got != 1 {
		t.Fatalf("after right: offset = %d, want 1", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if got := m.activeWS().Panes.FocusedInstance().Diff().HOffset(); got != 0 {
		t.Fatalf("after left: offset = %d, want 0", got)
	}
	// The horizontal wheel seam (#1700) uses the same clamped setter.
	m.activeWS().Panes.FocusedInstance().Diff().ScrollXBy(-5)
	if got := m.activeWS().Panes.FocusedInstance().Diff().HOffset(); got != 0 {
		t.Fatalf("wheel-left at column 0 should clamp, got %d", got)
	}
	m.activeWS().Panes.FocusedInstance().Diff().ScrollXBy(4)
	if got := m.activeWS().Panes.FocusedInstance().Diff().HOffset(); got != 4 {
		t.Fatalf("wheel-right: offset = %d, want 4", got)
	}
}

// TestDiffReusesEmptyEditor guards #628: opening a diff while the active editor
// is an empty scratch pane takes over that pane in place instead of splitting a
// new one — leaf count stays the same and the empty editor is gone.
func TestDiffReusesEmptyEditor(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	left := filepath.Join(dir, "l.txt")
	right := filepath.Join(dir, "r.txt")
	os.WriteFile(left, []byte("a\n"), 0o644)
	os.WriteFile(right, []byte("b\n"), 0o644)

	m := newSized() // default layout: explorer + one empty editor
	editorKey := m.activeEditorKey()
	if editorKey == "" || !m.activeWS().Panes.Get(editorKey).IsEmptyEditor() {
		t.Fatalf("expected an empty editor pane, got %q", editorKey)
	}
	before := len(layout.Leaves(m.activeWS().Tree))

	m.openDiffPane(left, right)

	if got := len(layout.Leaves(m.activeWS().Tree)); got != before {
		t.Fatalf("diff split a new pane: leaves %d -> %d", before, got)
	}
	if m.activeWS().Panes.Has(editorKey) {
		t.Fatal("the empty editor pane should have been taken over, not kept")
	}
	if k := m.activeWS().Panes.Focused(); m.activeWS().Panes.Get(k) == nil || m.activeWS().Panes.Get(k).Kind() != pane.KindDiff {
		t.Fatalf("focused pane is not the diff (key %q)", k)
	}
}

// TestDiffDoesNotClobberNonEmptyEditor: a file-backed editor is preserved —
// the diff joins it as a content tab (#2507) rather than replacing it, and no
// new leaf appears.
func TestDiffDoesNotClobberNonEmptyEditor(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	f := filepath.Join(dir, "open.txt")
	left := filepath.Join(dir, "l.txt")
	right := filepath.Join(dir, "r.txt")
	os.WriteFile(f, []byte("content\n"), 0o644)
	os.WriteFile(left, []byte("a\n"), 0o644)
	os.WriteFile(right, []byte("b\n"), 0o644)

	m := newSized()
	m.openPath(f, false) // active editor now holds a file
	editorKey := m.activeEditorKey()
	if m.activeWS().Panes.Get(editorKey).IsEmptyEditor() {
		t.Fatal("editor should be file-backed now")
	}
	before := len(layout.Leaves(m.activeWS().Tree))

	m.openDiffPane(left, right)

	if got := len(layout.Leaves(m.activeWS().Tree)); got != before {
		t.Fatalf("diff must not split beside a file-backed editor: leaves %d -> %d", before, got)
	}
	if !m.activeWS().Panes.Has(editorKey) {
		t.Fatal("the file-backed editor pane must be preserved")
	}
	if got := m.activeWS().Panes.Focused(); got != editorKey {
		t.Fatalf("focused pane = %q, want the editor %q", got, editorKey)
	}
	inst := m.activeWS().Panes.Get(editorKey)
	if inst.TabCount() != 2 {
		t.Fatalf("the editor's file tab must survive beside the diff tab (tabs=%d)", inst.TabCount())
	}
	if c := inst.ActiveContent(); c == nil || c.Kind() != pane.KindDiff {
		t.Fatal("the diff tab must be the active one")
	}
	if inst.TabPath(0) != f {
		t.Fatalf("tab 0 = %q, want the open file %q", inst.TabPath(0), f)
	}
}

// TestDiffSplitPlacementSplits guards diff.placement = "split" (#2507): the
// pre-#2507 behaviour, a fresh pane to the right of the file-backed editor.
func TestDiffSplitPlacementSplits(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	f := filepath.Join(dir, "open.txt")
	left := filepath.Join(dir, "l.txt")
	right := filepath.Join(dir, "r.txt")
	os.WriteFile(f, []byte("content\n"), 0o644)
	os.WriteFile(left, []byte("a\n"), 0o644)
	os.WriteFile(right, []byte("b\n"), 0o644)

	m := NewWith(registry.New(), host.MapConfig{"diff.placement": "split"})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	m.openPath(f, false)
	editorKey := m.activeEditorKey()
	before := len(layout.Leaves(m.activeWS().Tree))

	m.openDiffPane(left, right)

	if got := len(layout.Leaves(m.activeWS().Tree)); got != before+1 {
		t.Fatalf("split placement must split: leaves %d -> %d", before, got)
	}
	if !m.activeWS().Panes.Has(editorKey) {
		t.Fatal("the file-backed editor pane must be preserved")
	}
	if inst := m.activeWS().Panes.FocusedInstance(); inst == nil || inst.Kind() != pane.KindDiff {
		t.Fatal("the split diff pane must take focus")
	}
}
