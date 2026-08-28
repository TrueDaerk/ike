package app

// merge_flow_test.go covers the merge-tool flow integration of #2258: the
// offer raised when a conflicted file is opened in the editor, the save/finish
// offer raised when a merge view's last conflict is resolved, and the
// remaining-conflict counter in the status line. The pane wiring, the apply
// guard and the close guard live in merge_view_test.go (#1478).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/pane"
	"ike/internal/vcs"
)

// conflictedRepo writes a conflicted file into a fake repo root and points the
// model's vcs snapshot at it.
func conflictedRepo(t *testing.T, m Model, name, content string) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m.vcs.snap = vcs.NewSnapshot(dir, map[string]vcs.FileStatus{name: vcs.StatusConflicted})
	return m, path
}

// TestConflictedOpenOffersMergeTool: opening a git-conflicted file raises the
// prominent offer, and enter routes into the stage fetch.
func TestConflictedOpenOffersMergeTool(t *testing.T) {
	m := newSized()
	m, path := conflictedRepo(t, m, "c.txt", "a\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> other\nz\n")
	tm, _ := m.openPathInEditor(path)
	m = tm.(Model)
	if !m.mergeOfferPromptOpen() {
		t.Fatal("a conflicted file must offer the merge tool")
	}
	// enter — the primary option — asks git for the three index stages.
	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	if m.mergeOfferPromptOpen() {
		t.Fatal("answering must close the offer")
	}
	found := false
	for _, msg := range cmdMsgs(cmd) {
		if _, ok := msg.(vcs.MergeStagesMsg); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("accepting the offer must fetch the merge stages")
	}
}

// TestConflictedOpenOfferDismissAndOnce: esc leaves the user in the editor,
// and the offer does not interrupt again for the same file.
func TestConflictedOpenOfferDismissAndOnce(t *testing.T) {
	m := newSized()
	m, path := conflictedRepo(t, m, "c.txt", "a\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> other\nz\n")
	tm, _ := m.openPathInEditor(path)
	m = tm.(Model)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.mergeOfferPromptOpen() {
		t.Fatal("esc must dismiss the offer")
	}
	if mergeKeyOf(m) != "" {
		t.Fatal("a dismissed offer must not open the merge view")
	}
	tm, _ = m.openPathInEditor(path)
	m = tm.(Model)
	if m.mergeOfferPromptOpen() {
		t.Fatal("the offer must interrupt only once per file per session")
	}
}

// TestCleanOpenNoMergeOffer: a file git does not report as conflicted opens
// without any dialog.
func TestCleanOpenNoMergeOffer(t *testing.T) {
	m := newSized()
	dir := t.TempDir()
	path := filepath.Join(dir, "clean.txt")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.vcs.snap = vcs.NewSnapshot(dir, map[string]vcs.FileStatus{"clean.txt": vcs.StatusModified})
	tm, _ := m.openPathInEditor(path)
	m = tm.(Model)
	if m.mergeOfferPromptOpen() {
		t.Fatal("a clean file must not offer the merge tool")
	}
}

// TestMergeFinishOfferSavesAndCloses: resolving the last conflict offers
// save/finish, and enter applies the merge.
func TestMergeFinishOfferSavesAndCloses(t *testing.T) {
	m := newSized()
	m, key := openMergeView(t, m)
	path := m.activeWS().Panes.Get(key).Merge().Path()
	tm, _ := m.Update(editor.ActionMsg{Action: "merge_next_conflict"})
	m = tm.(Model)
	tm, _ = m.Update(editor.ActionMsg{Action: "merge_accept_ours"})
	m = tm.(Model)
	if !m.mergeFinishPromptOpen() {
		t.Fatal("resolving the last conflict must offer save/finish")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mergeFinishPromptOpen() {
		t.Fatal("answering must close the offer")
	}
	if mergeKeyOf(m) != "" {
		t.Fatal("accepting the offer must apply the merge and close the view")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); !strings.Contains(got, "ours") || strings.Contains(got, "<<<<<<<") {
		t.Fatalf("the finished file must be marker-free, got %q", got)
	}
}

// TestMergeFinishOfferDismissRearms: esc keeps the view open, and an undo that
// brings the conflict back re-arms the offer for the next resolution.
func TestMergeFinishOfferDismissRearms(t *testing.T) {
	m := newSized()
	m, key := openMergeView(t, m)
	tm, _ := m.Update(editor.ActionMsg{Action: "merge_next_conflict"})
	m = tm.(Model)
	tm, _ = m.Update(editor.ActionMsg{Action: "merge_accept_ours"})
	m = tm.(Model)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.mergeFinishPromptOpen() || mergeKeyOf(m) != key {
		t.Fatal("esc must dismiss the offer and keep the view open")
	}
	// Undo restores the block: the counter goes back up.
	tm, _ = m.Update(editor.ActionMsg{Action: "undo"})
	m = tm.(Model)
	if n := m.activeWS().Panes.Get(key).Merge().Unresolved(); n != 1 {
		t.Fatalf("undo must restore the conflict, unresolved=%d", n)
	}
	if m.mergeFinishPromptOpen() {
		t.Fatal("an unresolved view must not show the finish offer")
	}
	tm, _ = m.Update(editor.ActionMsg{Action: "merge_accept_theirs"})
	m = tm.(Model)
	if !m.mergeFinishPromptOpen() {
		t.Fatal("resolving again must re-arm the offer")
	}
}

// TestMergeFinishBlockedByLeftoverMarkers: a half-edited block parses as no
// conflict at all, so the block count alone would call the merge finished —
// the marker check refuses it, and no offer is raised either.
func TestMergeFinishBlockedByLeftoverMarkers(t *testing.T) {
	m := newSized()
	m, key := openMergeView(t, m)
	mg := m.activeWS().Panes.Get(key).Merge()
	// Drop the closing marker: complete blocks reach zero, markers do not.
	mg.Editor().RestoreText("a\n<<<<<<< ours\nours\n=======\ntheirs\nz\n")
	if mg.Unresolved() != 0 || mg.MarkerLines() == 0 {
		t.Fatalf("setup: unresolved=%d markers=%d", mg.Unresolved(), mg.MarkerLines())
	}
	tm, _ := m.Update(MergeApplyMsg{})
	m = tm.(Model)
	if mergeKeyOf(m) != key {
		t.Fatal("leftover markers must block the finish and keep the view open")
	}
	found := false
	for _, n := range m.toasts {
		if strings.Contains(n.text, "marker") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the leftover-marker notification")
	}
	if m.mergeFinishPromptOpen() {
		t.Fatal("leftover markers must not raise the finish offer")
	}
}

// TestMergeStatusCounter: the status line tracks the caret's place in the
// cycle, the remaining count, and the all-resolved state.
func TestMergeStatusCounter(t *testing.T) {
	m := newSized()
	m, key := openMergeView(t, m)
	tm, _ := m.Update(editor.ActionMsg{Action: "merge_next_conflict"})
	m = tm.(Model)
	if v := m.render(); !strings.Contains(v, "conflict 1/1") || !strings.Contains(v, "1/1 unresolved") {
		t.Fatalf("status line must show the conflict counter:\n%s", v)
	}
	tm, _ = m.Update(editor.ActionMsg{Action: "merge_accept_both"})
	m = tm.(Model)
	if inst := m.activeWS().Panes.Get(key); inst == nil || inst.Kind() != pane.KindMerge {
		t.Fatal("the merge view must stay open until it is applied")
	}
	// The finish offer is up; the bar behind it reads all-resolved.
	if v := m.render(); !strings.Contains(v, "all resolved") {
		t.Fatalf("status line must report the resolved merge:\n%s", v)
	}
}
