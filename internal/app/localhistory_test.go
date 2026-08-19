package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/pane"
	"ike/internal/ui"
)

// TestLocalHistoryRecordsOnSave: the save-side hook (#1023) stores the
// written bytes, and an identical second save dedupes to one entry.
func TestLocalHistoryRecordsOnSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m = openDirty(t, m, path)
	m = drainKey(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}) // save

	// The emitter forwards EventSave from a goroutine; deliver its message
	// deterministically here.
	tm, _ := m.Update(localHistorySnapshotMsg{path: path})
	m = tm.(Model)
	entries := m.lhStore.List(path)
	if len(entries) != 1 {
		t.Fatalf("List = %d entries after save, want 1", len(entries))
	}
	data, err := m.lhStore.Read(entries[0].Hash)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !strings.HasPrefix(string(data), "Xone") {
		t.Fatalf("snapshot = %q, want the saved content (Xone…)", data)
	}

	// A second save without changes stores nothing new.
	tm, _ = m.Update(localHistorySnapshotMsg{path: path})
	m = tm.(Model)
	if n := len(m.lhStore.List(path)); n != 1 {
		t.Fatalf("List = %d entries after identical save, want 1 (dedupe)", n)
	}
}

// TestLocalHistoryRestoreThroughBuffer: restoring a snapshot rewrites the
// buffer through the edit path — marks it dirty, leaves the file on disk
// untouched, and a single undo reverts it.
func TestLocalHistoryRestoreThroughBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m = openDirty(t, m, path) // buffer: "Xone\ntwo\n", dirty
	m.lhStore.Record(path, []byte("SNAP\n"))

	m.openLocalHistoryPicker()
	if !m.localHistoryOpen() {
		t.Fatal("picker did not open")
	}
	tm, _ := m.updateLocalHistoryPicker(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = tm.(Model)

	ed := m.activeEditor()
	if ed == nil {
		t.Fatal("no active editor")
	}
	if got := ed.Text(); got != "SNAP" {
		t.Fatalf("buffer after restore = %q, want %q (buffer form, no final newline)", got, "SNAP")
	}
	if !ed.Dirty() {
		t.Fatal("restore did not mark the buffer dirty")
	}
	if data, _ := os.ReadFile(path); string(data) != "one\ntwo\n" {
		t.Fatalf("restore touched the file on disk: %q", data)
	}

	// One undo brings the pre-restore content back.
	m = drainKey(m, tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if got := m.activeEditor().Text(); got != "Xone\ntwo" {
		t.Fatalf("buffer after undo = %q, want %q", got, "Xone\ntwo")
	}
}

// TestLocalHistoryEnterOpensDiffPane: enter on a snapshot opens the reusable
// diff pane with the snapshot on the left and the live buffer on the right.
func TestLocalHistoryEnterOpensDiffPane(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m = openDirty(t, m, path)
	m.lhStore.Record(path, []byte("SNAP\n"))

	m.openLocalHistoryPicker()
	if !m.localHistoryOpen() {
		t.Fatal("picker did not open")
	}
	tm, _ := m.updateLocalHistoryPicker(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	if m.localHistoryOpen() {
		t.Fatal("picker still open after enter")
	}
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindDiff {
		t.Fatalf("focused pane after enter = %q, want a diff pane", key)
	}
	if inst.Diff().HunkCount() == 0 {
		t.Fatal("diff pane shows no hunks for differing contents")
	}
}

// TestLocalHistoryPickerNeedsSnapshots: without history the command degrades
// to a notice instead of an empty modal.
func TestLocalHistoryPickerNeedsSnapshots(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	m.openLocalHistoryPicker()
	if m.localHistoryOpen() {
		t.Fatal("picker opened with no snapshots")
	}
}

// TestLocalHistoryPickerRenderFollowsSelection guards #1440: the shell body
// must reflect a selection move made in a LATER model copy (the root model is
// a value model — content bound only at open time renders the open-time
// selection forever).
func TestLocalHistoryPickerRenderFollowsSelection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m = openDirty(t, m, path)
	m.lhStore.Record(path, []byte("SNAP1\n"))
	m.lhStore.Record(path, []byte("SNAP2\n"))

	m.openLocalHistoryPicker()
	if !m.localHistoryOpen() {
		t.Fatal("panel did not open")
	}
	tm, _ := m.updateLocalHistoryPicker(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m2 := tm.(Model)
	body := ansi.Strip(m2.shell.Content().(ui.Content).Render(120))
	lines := strings.Split(body, "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[1], "▍") {
		t.Fatalf("marker not on row 2 after j:\n%s", body)
	}
}

// TestLocalHistoryPanelLayout (#1969): the panel body splits into two panes —
// the snapshot list showing file name and date on the left, the selected
// snapshot's inline git-style diff (+/- markers, @@ hunk header) on the right
// of the column separator.
func TestLocalHistoryPanelLayout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m = openDirty(t, m, path) // buffer: "Xone\ntwo"
	m.lhStore.Record(path, []byte("one\ntwo\n"))

	m.openLocalHistoryPicker()
	if !m.localHistoryOpen() {
		t.Fatal("panel did not open")
	}
	body := ansi.Strip(m.shell.Content().(ui.Content).Render(120))
	first := strings.Split(body, "\n")[0]
	left, right, ok := strings.Cut(first, "│")
	if !ok {
		t.Fatalf("no column separator in row 1: %q", first)
	}
	if !strings.Contains(left, "a.txt") {
		t.Errorf("left pane misses the file name: %q", left)
	}
	if !regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`).MatchString(left) {
		t.Errorf("left pane misses the date: %q", left)
	}
	if !strings.HasPrefix(strings.TrimSpace(right), "@@ ") {
		t.Errorf("right pane row 1 is not a hunk header: %q", right)
	}
	if !strings.Contains(body, "│ - one") || !strings.Contains(body, "│ + Xone") {
		t.Errorf("right pane misses the -/+ diff lines:\n%s", body)
	}
}

// TestLocalHistoryDiffFollowsSelection (#1969): moving the selection in the
// left list recomputes the inline diff on the right immediately — no extra
// step to open a diff.
func TestLocalHistoryDiffFollowsSelection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m = openDirty(t, m, path)               // buffer: "Xone"
	m.lhStore.Record(path, []byte("OLD\n")) // older snapshot (row 2)
	m.lhStore.Record(path, []byte("NEW\n")) // newest snapshot (row 1)

	m.openLocalHistoryPicker()
	body := ansi.Strip(m.shell.Content().(ui.Content).Render(120))
	if !strings.Contains(body, "- NEW") || strings.Contains(body, "- OLD") {
		t.Fatalf("diff at open does not show the newest snapshot:\n%s", body)
	}
	tm, _ := m.updateLocalHistoryPicker(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m2 := tm.(Model)
	body = ansi.Strip(m2.shell.Content().(ui.Content).Render(120))
	if !strings.Contains(body, "- OLD") || strings.Contains(body, "- NEW") {
		t.Fatalf("diff did not follow the selection to the older snapshot:\n%s", body)
	}
}

// TestLocalHistoryDiffNoChanges (#1969): a snapshot identical to the buffer
// renders a notice instead of an empty diff.
func TestLocalHistoryDiffNoChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	m.lhStore.Record(path, []byte("one\n"))

	m.openLocalHistoryPicker()
	body := ansi.Strip(m.shell.Content().(ui.Content).Render(120))
	if !strings.Contains(body, "no changes") {
		t.Fatalf("identical snapshot did not render the no-changes notice:\n%s", body)
	}
}
