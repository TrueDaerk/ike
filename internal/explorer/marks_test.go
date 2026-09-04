package explorer

// marks_test.go covers the sticky multi-select (#2166): space toggles a mark
// on the cursor row, the mark survives every cursor motion, esc clears the
// set, and delete/move/copy act on the whole selection with one prompt, one
// undo step and a partial-failure report when an entry fails mid-batch.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// spaceKey is the mark toggle as the terminal delivers it.
func spaceKey() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
}

// escKey clears the selection.
func escKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEscape} }

// markedTree mounts the standard fixture (root, sub, a.txt, b.txt) and marks
// the two files, leaving the cursor on b.txt.
func markedTree(t *testing.T, root string) Model {
	t.Helper()
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j"), spaceKey()) // mark a.txt
	m, _ = send(m, key("j"), spaceKey())           // mark b.txt
	return m
}

func TestSpaceTogglesMarkAndEscClears(t *testing.T) {
	m := selTree(t)
	m, _ = send(m, key("j"), key("j")) // a.txt
	m, _ = send(m, spaceKey())
	if m.markCount() != 1 || !m.marked(m.rows[2].path) {
		t.Fatalf("space must mark the cursor row, marks=%d", m.markCount())
	}
	if k := m.rowKind(2); k != rowSelected {
		t.Fatalf("cursor row kind = %v, want rowSelected (cursor outranks the mark)", k)
	}
	// The mark survives cursor movement and shows the muted range recipe once
	// the cursor is elsewhere.
	m, _ = send(m, key("j"))
	if m.markCount() != 1 {
		t.Fatalf("a mark must survive cursor motion, marks=%d", m.markCount())
	}
	if k := m.rowKind(2); k != rowMarked {
		t.Fatalf("marked row kind = %v, want rowMarked", k)
	}
	// A hover sweeping over it does not win.
	m.hover = 2
	if k := m.rowKind(2); k != rowMarked {
		t.Fatalf("hovered marked row kind = %v, want rowMarked", k)
	}
	m.hover = -1
	// Space on the same row again unmarks it.
	m, _ = send(m, key("k"), spaceKey())
	if m.markCount() != 0 {
		t.Fatalf("space must toggle the mark off, marks=%d", m.markCount())
	}
	// Two marks, then esc clears the whole set without moving the cursor.
	m, _ = send(m, spaceKey(), key("j"), spaceKey())
	if m.markCount() != 2 {
		t.Fatalf("marks = %d, want 2", m.markCount())
	}
	cur := m.cursor
	m, _ = send(m, escKey())
	if m.markCount() != 0 {
		t.Fatalf("esc must clear the marks, got %d", m.markCount())
	}
	if m.cursor != cur {
		t.Fatalf("esc must not move the cursor: %d -> %d", cur, m.cursor)
	}
}

func TestMarkerColumnOnlyWhileMarked(t *testing.T) {
	m := selTree(t)
	plain := m.rowText(m.rows[2])
	m, _ = send(m, key("j"), key("j"), spaceKey())
	marked := m.rowText(m.rows[2])
	if !strings.Contains(marked, markGlyph) {
		t.Fatalf("marked row %q must show the marker", marked)
	}
	if un := m.rowText(m.rows[3]); strings.Contains(un, markGlyph) {
		t.Fatalf("unmarked row %q must not show the marker", un)
	}
	m, _ = send(m, escKey())
	if got := m.rowText(m.rows[2]); got != plain {
		t.Fatalf("with nothing marked the row must render as before: %q != %q", got, plain)
	}
}

func TestRootIsNeverMarkable(t *testing.T) {
	m := selTree(t)
	m, _ = send(m, spaceKey()) // cursor sits on the root
	if m.markCount() != 0 {
		t.Fatalf("the root must not be markable, marks=%d", m.markCount())
	}
}

func TestMarkTargetsSkipNestedChildren(t *testing.T) {
	m := selTree(t)
	m, _ = send(m, key("j"), key("enter")) // expand sub
	m, _ = send(m, spaceKey())             // mark sub
	m, _ = send(m, key("j"), spaceKey())   // mark sub/c.txt
	if m.markCount() != 2 {
		t.Fatalf("marks = %d, want 2", m.markCount())
	}
	ts := m.markTargets()
	if len(ts) != 1 || filepath.Base(ts[0].path) != "sub" {
		t.Fatalf("targets = %v, want just sub (nested child filtered)", ts)
	}
}

func TestMarkSurvivesCollapse(t *testing.T) {
	m := selTree(t)
	m, _ = send(m, key("j"), key("enter"), key("j"), spaceKey()) // mark sub/c.txt
	m, _ = send(m, key("k"), key("enter"))                       // collapse sub again
	if m.markCount() != 1 {
		t.Fatalf("a mark under a collapsed dir must survive, marks=%d", m.markCount())
	}
	ts := m.markTargets()
	if len(ts) != 1 || filepath.Base(ts[0].path) != "c.txt" {
		t.Fatalf("targets = %v, want sub/c.txt", ts)
	}
}

func TestPruneMarksDropsVanishedEntries(t *testing.T) {
	root := tree(t)
	m := markedTree(t, root)
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	m, cmd := m.Update(RefreshMsg{})
	m, _ = pumpScans(m, cmd)
	if m.markCount() != 1 {
		t.Fatalf("a vanished entry's mark must be pruned, marks=%d", m.markCount())
	}
}

func TestBulkDeleteListsTargetsAndUndoesAsOne(t *testing.T) {
	root := tree(t)
	m := markedTree(t, root)
	m, cmd := m.Update(DeleteMsg{})
	m, _ = pumpScans(m, cmd)
	if !m.Prompting() {
		t.Fatal("a marked delete must open one confirm prompt")
	}
	if got := m.prompt.title; !strings.Contains(got, "2 entries") {
		t.Fatalf("prompt title = %q, want the entry count", got)
	}
	joined := strings.Join(m.prompt.lines, "\n")
	for _, want := range []string{"a.txt", "b.txt"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("prompt must list %s, got %q", want, joined)
		}
	}
	m, _ = send(m, key("y"))
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(root, f)); !os.IsNotExist(err) {
			t.Fatalf("%s should be deleted, err=%v", f, err)
		}
	}
	if m.markCount() != 0 {
		t.Fatalf("the selection must clear after the operation, marks=%d", m.markCount())
	}
	if len(m.ops) != 1 || len(m.ops[0].batch) != 2 {
		t.Fatalf("want ONE batch op of 2 subs, got ops=%d", len(m.ops))
	}
	m, cmd = m.Update(UndoMsg{})
	m, _ = pumpScans(m, cmd)
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Fatalf("%s should be restored by a single undo: %v", f, err)
		}
	}
}

func TestBulkMoveIntoDirectory(t *testing.T) {
	root := tree(t)
	m := markedTree(t, root)
	m, cmd := m.Update(MoveSelectionMsg{})
	m, _ = pumpScans(m, cmd)
	if !m.Prompting() || m.prompt.kind != promptInput {
		t.Fatal("a marked move must open one target-directory prompt")
	}
	if got := m.prompt.title; !strings.Contains(got, "2 entries") {
		t.Fatalf("prompt title = %q, want the entry count", got)
	}
	m = typePrompt(m, "sub")
	m, cmd = m.Update(key("enter"))
	m, _ = pumpScans(m, cmd)
	if m.Prompting() {
		t.Fatalf("the move must not leave a dialog open: %q", m.prompt.input.Text)
	}
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(root, "sub", f)); err != nil {
			t.Fatalf("sub/%s should exist after the move: %v", f, err)
		}
		if _, err := os.Stat(filepath.Join(root, f)); !os.IsNotExist(err) {
			t.Fatalf("%s should have left the root, err=%v", f, err)
		}
	}
	if m.markCount() != 0 {
		t.Fatalf("the selection must clear after the move, marks=%d", m.markCount())
	}
	if len(m.ops) != 1 || len(m.ops[0].batch) != 2 {
		t.Fatalf("want ONE batch op of 2 subs, got %+v", m.ops)
	}
	// One undo moves both back.
	m, cmd = m.Update(UndoMsg{})
	m, _ = pumpScans(m, cmd)
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Fatalf("%s should be back in the root: %v", f, err)
		}
	}
	// …and one redo moves them in again.
	m, cmd = m.Update(RedoMsg{})
	pumpScans(m, cmd)
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(root, "sub", f)); err != nil {
			t.Fatalf("sub/%s should be back after redo: %v", f, err)
		}
	}
}

func TestBulkCopyKeepsOriginals(t *testing.T) {
	root := tree(t)
	m := markedTree(t, root)
	m, cmd := m.Update(CopySelectionMsg{})
	m, _ = pumpScans(m, cmd)
	if !m.Prompting() || m.prompt.kind != promptInput {
		t.Fatal("a marked copy must open one target-directory prompt")
	}
	m = typePrompt(m, "sub")
	m, cmd = m.Update(key("enter"))
	m, _ = pumpScans(m, cmd)
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(root, "sub", f)); err != nil {
			t.Fatalf("sub/%s should be the copy: %v", f, err)
		}
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Fatalf("%s (the original) must stay put: %v", f, err)
		}
	}
	if got, err := os.ReadFile(filepath.Join(root, "sub", "a.txt")); err != nil || string(got) != "a" {
		t.Fatalf("copied content = %q err=%v, want %q", got, err, "a")
	}
	if len(m.ops) != 1 || len(m.ops[0].batch) != 2 {
		t.Fatalf("want ONE batch op of 2 subs, got %+v", m.ops)
	}
	// Undo removes exactly the copies and leaves the sources alone.
	m, cmd = m.Update(UndoMsg{})
	pumpScans(m, cmd)
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(root, "sub", f)); !os.IsNotExist(err) {
			t.Fatalf("sub/%s should be undone, err=%v", f, err)
		}
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Fatalf("%s (the original) must survive the undo: %v", f, err)
		}
	}
}

func TestBulkCopyRecursesIntoDirectories(t *testing.T) {
	root := tree(t)
	if err := os.Mkdir(filepath.Join(root, "dest"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j")) // rows: root, dest, sub, ... -> sub
	if m.rows[m.cursor].name != "sub" {
		t.Fatalf("cursor on %q, want sub", m.rows[m.cursor].name)
	}
	m, _ = send(m, spaceKey())
	m, cmd := m.Update(CopySelectionMsg{})
	m, _ = pumpScans(m, cmd)
	m = typePrompt(m, "dest")
	m, cmd = m.Update(key("enter"))
	pumpScans(m, cmd)
	if got, err := os.ReadFile(filepath.Join(root, "dest", "sub", "c.txt")); err != nil || string(got) != "c" {
		t.Fatalf("dest/sub/c.txt = %q err=%v, want the copied subtree", got, err)
	}
}

func TestBulkMovePartialFailureReports(t *testing.T) {
	root := tree(t)
	// A file already occupying the target name makes exactly one entry of the
	// batch fail; the other must still move.
	mustWrite(t, filepath.Join(root, "sub", "a.txt"), "blocker")
	m := markedTree(t, root)
	m, cmd := m.Update(MoveSelectionMsg{})
	m, _ = pumpScans(m, cmd)
	m = typePrompt(m, "sub")
	m, cmd = m.Update(key("enter"))
	m, _ = pumpScans(m, cmd)
	if m.prompt == nil || m.prompt.kind != promptNotice {
		t.Fatal("a partial failure must open the error dialog")
	}
	report := m.prompt.input.Text
	for _, want := range []string{"moved 1 of 2", "a.txt", "already exists"} {
		if !strings.Contains(report, want) {
			t.Fatalf("partial-failure report %q must mention %q", report, want)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "sub", "b.txt")); err != nil {
		t.Fatalf("the entry that could move must have moved: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "a.txt")); err != nil || string(got) != "a" {
		t.Fatalf("the failed entry must stay put with its content: %q %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "sub", "a.txt")); err != nil || string(got) != "blocker" {
		t.Fatalf("the blocking file must be untouched: %q %v", got, err)
	}
	// The one entry that made it is still a normal, undoable operation.
	if len(m.ops) != 1 || len(m.ops[0].batch) != 0 {
		t.Fatalf("a one-entry batch must record as a plain op, got %+v", m.ops)
	}
}

func TestBulkDeletePartialFailureReports(t *testing.T) {
	root := tree(t)
	m := markedTree(t, root)
	// Remove one target behind the explorer's back: trashing it must fail
	// while the other entry still goes.
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	m, cmd := m.Update(DeleteMsg{})
	m, _ = pumpScans(m, cmd)
	m, _ = send(m, key("y"))
	if m.prompt == nil || m.prompt.kind != promptNotice {
		t.Fatal("a partial failure must open the error dialog")
	}
	if got := m.prompt.input.Text; !strings.Contains(got, "deleted 1 of 2") {
		t.Fatalf("report = %q, want the partial count", got)
	}
	if _, err := os.Stat(filepath.Join(root, "b.txt")); !os.IsNotExist(err) {
		t.Fatalf("the reachable entry must still be deleted, err=%v", err)
	}
}

func TestBulkCopyPartialFailureReports(t *testing.T) {
	root := tree(t)
	mustWrite(t, filepath.Join(root, "sub", "a.txt"), "blocker")
	m := markedTree(t, root)
	m, cmd := m.Update(CopySelectionMsg{})
	m, _ = pumpScans(m, cmd)
	m = typePrompt(m, "sub")
	m, cmd = m.Update(key("enter"))
	m, _ = pumpScans(m, cmd)
	if m.prompt == nil || m.prompt.kind != promptNotice {
		t.Fatal("a partial failure must open the error dialog")
	}
	if got := m.prompt.input.Text; !strings.Contains(got, "copied 1 of 2") {
		t.Fatalf("report = %q, want the partial count", got)
	}
	if _, err := os.Stat(filepath.Join(root, "sub", "b.txt")); err != nil {
		t.Fatalf("the entry that could copy must exist: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "sub", "a.txt")); err != nil || string(got) != "blocker" {
		t.Fatalf("the blocking file must be untouched: %q %v", got, err)
	}
}

func TestSingleEntryBehaviourUnchangedWithoutMarks(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j")) // a.txt, nothing marked
	m, cmd := m.Update(DeleteMsg{})
	m, _ = pumpScans(m, cmd)
	if got := m.prompt.title; !strings.Contains(got, `"a.txt"`) {
		t.Fatalf("prompt title = %q, want the single-entry wording", got)
	}
	if len(m.prompt.lines) != 0 {
		t.Fatalf("a single-entry prompt must not list targets, got %v", m.prompt.lines)
	}
	m, _ = send(m, key("y"))
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("a.txt should be deleted, err=%v", err)
	}
	if len(m.ops) != 1 || len(m.ops[0].batch) != 0 {
		t.Fatalf("a single delete must record a plain op, got %+v", m.ops)
	}
}

func TestMoveSelectionRejectsNonDirectory(t *testing.T) {
	root := tree(t)
	m := markedTree(t, root)
	m, cmd := m.Update(MoveSelectionMsg{})
	m, _ = pumpScans(m, cmd)
	m = typePrompt(m, "sub/c.txt")
	m, cmd = m.Update(key("enter"))
	m, _ = pumpScans(m, cmd)
	if m.prompt == nil || m.prompt.kind != promptNotice {
		t.Fatal("a non-directory target must open the error dialog")
	}
	if got := m.prompt.input.Text; !strings.Contains(got, "not a directory") {
		t.Fatalf("error = %q, want the not-a-directory message", got)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("nothing may move on a bad target: %v", err)
	}
}

func TestMoveManyMsgMovesTheWholeSelection(t *testing.T) {
	root := tree(t)
	m := markedTree(t, root)
	paths := m.MarkedPaths()
	if len(paths) != 2 {
		t.Fatalf("MarkedPaths = %v, want both files", paths)
	}
	m, cmd := m.Update(MoveManyMsg{Paths: paths, TargetDir: filepath.Join(root, "sub")})
	pumpScans(m, cmd)
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(root, "sub", f)); err != nil {
			t.Fatalf("sub/%s should exist after the batch move: %v", f, err)
		}
	}
}

// typePrompt types text into the open input prompt, one key at a time.
func typePrompt(m Model, text string) Model {
	for _, r := range text {
		m, _ = m.Update(tea.KeyPressMsg{Text: string(r)})
	}
	return m
}
