package app

import (
	"os"

	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"

	"ike/internal/editor"
	"ike/internal/pane"
	"ike/internal/vcs"
)

// mergeKeyOf returns the key of the first merge pane, or "".
func mergeKeyOf(m Model) string {
	for _, key := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindMerge {
			return key
		}
	}
	return ""
}

// openMergeView drives the stage-fetch answer into a sized model and returns
// it with the merge pane open (1 conflict: ours vs theirs over base "x").
func openMergeView(t *testing.T, m Model) (Model, string) {
	t.Helper()
	path := writeTempFile(t, "conflicted.txt", "a\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> other\nz\n")
	tm, _ := m.Update(vcs.MergeStagesMsg{
		Path:   path,
		Base:   "a\nx\nz\n",
		Ours:   "a\nours\nz\n",
		Theirs: "a\ntheirs\nz\n",
	})
	m = tm.(Model)
	key := mergeKeyOf(m)
	if key == "" {
		t.Fatal("merge stages should open a merge pane")
	}
	return m, key
}

// TestMergeStagesOpenPane guards the entry wiring (#1478): the fetched
// stages open a focused merge view seeded with the auto-merge.
func TestMergeStagesOpenPane(t *testing.T) {
	m := newSized()
	m, key := openMergeView(t, m)
	if m.activeWS().Panes.Focused() != key {
		t.Fatalf("the merge pane should take focus, got %q", m.activeWS().Panes.Focused())
	}
	mg := m.activeWS().Panes.Get(key).Merge()
	if mg.Unresolved() != 1 || mg.Total() != 1 {
		t.Fatalf("unresolved=%d total=%d, want 1/1", mg.Unresolved(), mg.Total())
	}
	if v := m.render(); !strings.Contains(v, "MERGE") {
		t.Fatal("the statusline should show the MERGE segment")
	}
}

// TestMergeAcceptResolves drives the palette accept through the ActionMsg
// routing: next-conflict + accept-ours resolves the block.
func TestMergeAcceptResolves(t *testing.T) {
	m := newSized()
	m, key := openMergeView(t, m)
	tm, _ := m.Update(editor.ActionMsg{Action: "merge_next_conflict"})
	m = tm.(Model)
	tm, _ = m.Update(editor.ActionMsg{Action: "merge_accept_ours"})
	m = tm.(Model)
	mg := m.activeWS().Panes.Get(key).Merge()
	if mg.Unresolved() != 0 {
		t.Fatalf("unresolved=%d after accept, want 0", mg.Unresolved())
	}
	text := mg.Editor().Text()
	if !strings.Contains(text, "ours") || strings.Contains(text, "theirs") || strings.Contains(text, "<<<<<<<") {
		t.Fatalf("accept ours left %q", text)
	}
}

// TestMergeApplyBlockedUnresolved refuses to save/stage while conflicts
// remain.
func TestMergeApplyBlockedUnresolved(t *testing.T) {
	m := newSized()
	m, key := openMergeView(t, m)
	tm, _ := m.Update(MergeApplyMsg{})
	m = tm.(Model)
	if mergeKeyOf(m) != key {
		t.Fatal("apply with unresolved conflicts must keep the view open")
	}
	found := false
	for _, n := range m.toasts {
		if strings.Contains(n.text, "unresolved") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the unresolved-conflicts notification")
	}
}

// TestMergeApplySavesAndCloses writes the resolved result to the file and
// closes the view.
func TestMergeApplySavesAndCloses(t *testing.T) {
	m := newSized()
	m, key := openMergeView(t, m)
	path := m.activeWS().Panes.Get(key).Merge().Path()
	tm, _ := m.Update(editor.ActionMsg{Action: "merge_next_conflict"})
	m = tm.(Model)
	tm, _ = m.Update(editor.ActionMsg{Action: "merge_accept_theirs"})
	m = tm.(Model)
	tm, _ = m.Update(MergeApplyMsg{})
	m = tm.(Model)
	if mergeKeyOf(m) != "" {
		t.Fatal("apply should close the merge view")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); !strings.Contains(got, "theirs") || strings.Contains(got, "<<<<<<<") {
		t.Fatalf("saved result = %q", got)
	}
}

// TestMergeCloseGuard warns before closing a view with unresolved conflicts;
// d discards, esc keeps it open.
func TestMergeCloseGuard(t *testing.T) {
	m := newSized()
	m, key := openMergeView(t, m)
	tm, _ := m.Update(ClosePaneMsg{})
	m = tm.(Model)
	if !m.mergeClosePromptOpen() {
		t.Fatal("closing with unresolved conflicts must open the guard")
	}
	if mergeKeyOf(m) != key {
		t.Fatal("the pane must stay open while the guard is up")
	}
	// esc cancels.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.mergeClosePromptOpen() || mergeKeyOf(m) != key {
		t.Fatal("esc must cancel the close")
	}
	// d discards.
	tm, _ = m.Update(ClosePaneMsg{})
	m = tm.(Model)
	m = drainKey(m, tea.KeyPressMsg{Code: 'd', Text: "d"})
	if mergeKeyOf(m) != "" {
		t.Fatal("d must close the merge view")
	}
}
