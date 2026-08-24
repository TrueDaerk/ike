package ghissues

// mutations_test.go covers the pane's write side (#2088): the capability
// gate, the two pickers and the diff they apply, close/reopen with and
// without a comment, and the optimistic update's rollback on a rejection.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
)

// mutable is the filled pane with the write factory injected and a triage
// capability probed; sent collects every mutation the pane dispatched.
func mutable(t *testing.T) (*Model, *[]forge.Mutation) {
	t.Helper()
	m := filled(t)
	var sent []forge.Mutation
	m.SetMutate(func(mut forge.Mutation) tea.Cmd {
		sent = append(sent, mut)
		return func() tea.Msg { return forge.MutationMsg{Issue: mut.Issue, Kind: mut.Kind} }
	})
	m.SetRepoMeta(forge.RepoMetaMsg{
		Caps: forge.Capabilities{Triage: true, Push: true},
		Labels: []forge.Label{
			{Name: "bug", Color: "d73a4a"},
			{Name: "feature", Color: "a2eeef"},
			{Name: "size:1d", Color: "8250df"},
		},
		Users: []string{"ada", "dev"},
	})
	return m, &sent
}

// selectIssue puts the list cursor on one issue by number — the default sort
// is newest first, so the fixture's #1 is not row zero.
func selectIssue(t *testing.T, m *Model, number int) {
	t.Helper()
	for i, r := range m.rows {
		if r.idx >= 0 && m.issues[r.idx].Number == number {
			m.cursor = i
			m.clampScroll()
			return
		}
	}
	t.Fatalf("issue #%d is not in the list", number)
}

func TestMutationGateHidesActionsWithoutPermission(t *testing.T) {
	m := filled(t)
	m.SetMutate(func(forge.Mutation) tea.Cmd { return nil })
	// Before a probe answers the actions are listed but disabled.
	hasKey := func(key string) bool {
		for _, a := range m.Actions() {
			if a[0] == key {
				return true
			}
		}
		return false
	}
	if !hasKey("e") {
		t.Fatal("the label action must be discoverable in the menu")
	}
	if strings.Contains(m.View(), "e labels±") {
		t.Fatal("an ungranted action must not be advertised in the footer")
	}
	m.SetRepoMeta(forge.RepoMetaMsg{Caps: forge.Capabilities{}})
	var label string
	for _, a := range m.Actions() {
		if a[0] == "e" {
			label = a[1]
		}
	}
	if !strings.Contains(label, "needs triage permission") {
		t.Fatalf("the menu must name the reason, got %q", label)
	}
	// Pressing the key explains rather than doing nothing.
	m.Update(key("e"))
	if m.LabelEditorOpen() {
		t.Fatal("the picker must not open without permission")
	}
	if !strings.Contains(m.MutationError(), "needs triage permission") {
		t.Fatalf("mutErr = %q", m.MutationError())
	}
	if !strings.Contains(m.View(), "needs triage permission") {
		t.Fatal("the reason must be on screen")
	}
}

func TestMutationActionsListedWithPermission(t *testing.T) {
	m, _ := mutable(t)
	view := m.View()
	for _, hint := range []string{"e labels±", "u assignees", "c close"} {
		if !strings.Contains(view, hint) {
			t.Fatalf("footer must advertise %q: %s", hint, view)
		}
	}
}

func TestLabelPickerAppliesDiff(t *testing.T) {
	m, sent := mutable(t)
	selectIssue(t, m, 1)
	// Issue #1 carries "bug"; the rows are the repository's three labels.
	m.Update(key("e"))
	if !m.LabelEditorOpen() {
		t.Fatal("e must open the label editor")
	}
	if got := m.EditSelection(); len(got) != 1 || got[0] != "bug" {
		t.Fatalf("the issue's own labels must be preselected, got %v", got)
	}
	press(m, "space")          // toggles "bug" off (the cursor starts on it)
	press(m, "down", "down")   // → size:1d
	press(m, "space", "enter") //   add it and apply
	if m.LabelEditorOpen() {
		t.Fatal("enter must close the editor")
	}
	if len(*sent) != 1 {
		t.Fatalf("mutations = %+v, want one", *sent)
	}
	mut := (*sent)[0]
	if mut.Issue != 1 || mut.Kind != forge.MutateLabels {
		t.Fatalf("mutation = %+v", mut)
	}
	if len(mut.AddLabels) != 1 || mut.AddLabels[0] != "size:1d" {
		t.Fatalf("add = %v", mut.AddLabels)
	}
	if len(mut.RemoveLabels) != 1 || mut.RemoveLabels[0] != "bug" {
		t.Fatalf("remove = %v", mut.RemoveLabels)
	}
	// The optimistic update is already visible.
	if is := m.Selected(); len(is.Labels) != 1 || is.Labels[0].Name != "size:1d" {
		t.Fatalf("optimistic labels = %+v", m.Selected().Labels)
	}
}

func TestLabelPickerNoDiffSendsNothing(t *testing.T) {
	m, sent := mutable(t)
	press(m, "e", "enter")
	if len(*sent) != 0 {
		t.Fatalf("an unchanged selection must not write: %+v", *sent)
	}
	press(m, "e", "esc")
	if len(*sent) != 0 || m.LabelEditorOpen() {
		t.Fatalf("esc must cancel: %+v", *sent)
	}
}

func TestAssigneePickerReplacesTheSet(t *testing.T) {
	m, sent := mutable(t)
	selectIssue(t, m, 1)
	m.Update(key("u"))
	if !m.AssigneeEditorOpen() {
		t.Fatal("u must open the assignee editor")
	}
	if got := m.EditSelection(); len(got) != 1 || got[0] != "dev" {
		t.Fatalf("current assignees must be preselected, got %v", got)
	}
	press(m, "backspace") // clear everyone
	press(m, "enter")
	if len(*sent) != 1 {
		t.Fatalf("mutations = %+v", *sent)
	}
	mut := (*sent)[0]
	if !mut.SetAssignees || len(mut.Assignees) != 0 {
		t.Fatalf("clearing must send an explicit empty set: %+v", mut)
	}
	if len(m.Selected().Assignees) != 0 {
		t.Fatalf("optimistic assignees = %v", m.Selected().Assignees)
	}
}

func TestCloseAndReopen(t *testing.T) {
	m, sent := mutable(t)
	selectIssue(t, m, 1)
	m.Update(key("c"))
	if len(*sent) != 1 || (*sent)[0].State != "closed" {
		t.Fatalf("c on an open issue must close it: %+v", *sent)
	}
	if m.Selected() != nil && m.Selected().Number == 1 {
		t.Fatal("the closed issue must leave the open listing optimistically")
	}
	// With the state filter on all, the closed issue is selectable again and
	// 'c' reads as reopen.
	m.state = FilterAll
	m.applyFilter()
	selectIssue(t, m, 1)
	if m.stateVerb() != "reopen" {
		t.Fatalf("verb = %q, want reopen", m.stateVerb())
	}
	m.Update(key("c"))
	if len(*sent) != 2 || (*sent)[1].State != "open" {
		t.Fatalf("c on a closed issue must reopen it: %+v", *sent)
	}
}

func TestCloseWithComment(t *testing.T) {
	m, sent := mutable(t)
	selectIssue(t, m, 1)
	m.Update(key("C"))
	if !m.CommentPromptOpen() {
		t.Fatal("C must open the comment prompt")
	}
	press(m, "d", "o", "n", "e")
	m.Update(key("enter"))
	if m.CommentPromptOpen() {
		t.Fatal("enter must close the prompt")
	}
	if len(*sent) != 1 {
		t.Fatalf("mutations = %+v", *sent)
	}
	mut := (*sent)[0]
	if mut.Comment != "done" || mut.State != "closed" {
		t.Fatalf("mutation = %+v", mut)
	}
}

func TestCommentPromptRejectsEmptyAndCancels(t *testing.T) {
	m, sent := mutable(t)
	press(m, "C", "enter")
	if len(*sent) != 0 {
		t.Fatalf("an empty comment must not close the issue: %+v", *sent)
	}
	if !strings.Contains(m.MutationError(), "empty comment") {
		t.Fatalf("mutErr = %q", m.MutationError())
	}
	press(m, "C", "x", "esc")
	if len(*sent) != 0 || m.CommentPromptOpen() {
		t.Fatalf("esc must cancel: %+v", *sent)
	}
}

func TestFailedMutationRollsBack(t *testing.T) {
	m, _ := mutable(t)
	selectIssue(t, m, 1)
	before := append([]forge.Label(nil), m.Selected().Labels...)
	press(m, "e", "space", "enter") // remove "bug"
	if len(m.Selected().Labels) != 0 {
		t.Fatal("the optimistic update must apply first")
	}
	if !m.MutationBusy() {
		t.Fatal("the write must count as in flight")
	}
	m.SetMutationResult(forge.MutationMsg{
		Issue: 1, Kind: forge.MutateLabels, Err: errFake("HTTP 403: Resource not accessible by token"),
	})
	if m.MutationBusy() {
		t.Fatal("the write is no longer in flight")
	}
	got := m.Selected().Labels
	if len(got) != len(before) || got[0].Name != before[0].Name {
		t.Fatalf("labels = %+v, want the pre-mutation %+v", got, before)
	}
	if !strings.Contains(m.MutationError(), "Resource not accessible") {
		t.Fatalf("the forge's own error must show: %q", m.MutationError())
	}
	if !strings.Contains(m.View(), "label change failed") {
		t.Fatal("the error must be on screen")
	}
}

func TestSuccessfulMutationRefetches(t *testing.T) {
	m, _ := mutable(t)
	refetched := 0
	m.SetRefresh(func(forge.IssueState) tea.Cmd {
		refetched++
		return nil
	})
	selectIssue(t, m, 1)
	press(m, "e", "space", "enter")
	if cmd := m.SetMutationResult(forge.MutationMsg{Issue: 1, Kind: forge.MutateLabels}); cmd != nil {
		cmd()
	}
	if refetched == 0 {
		t.Fatal("a successful mutation must refetch instead of trusting the guess")
	}
	if m.MutationError() != "" {
		t.Fatalf("mutErr = %q", m.MutationError())
	}
}

func TestPickerFallsBackToTheListing(t *testing.T) {
	m := filled(t)
	m.SetMutate(func(forge.Mutation) tea.Cmd { return nil })
	m.SetRepoMeta(forge.RepoMetaMsg{
		Caps: forge.Capabilities{Triage: true},
		Err:  errFake("HTTP 403: labels"),
	})
	m.Update(key("e"))
	if !m.LabelEditorOpen() {
		t.Fatal("a failed label probe must still leave a usable picker")
	}
	if rows := m.editRows(); len(rows) != 2 || rows[0] != "bug" {
		t.Fatalf("fallback rows = %v, want the listing's labels", rows)
	}
}

func TestMetaProbeRunsOnceAndRetriesAfterFailure(t *testing.T) {
	m := filled(t)
	probes := 0
	m.SetMeta(func() tea.Cmd {
		probes++
		return nil
	})
	m.startRefresh()
	m.startRefresh()
	if probes != 1 {
		t.Fatalf("probes = %d, want one per session", probes)
	}
	m.SetRepoMeta(forge.RepoMetaMsg{Err: errFake("gh: not logged in")})
	m.startRefresh()
	if probes != 2 {
		t.Fatalf("probes = %d, a failed probe must be retryable by r", probes)
	}
	m.SetRepoMeta(forge.RepoMetaMsg{Caps: forge.Capabilities{Triage: true}})
	m.startRefresh()
	if probes != 2 {
		t.Fatalf("probes = %d, an answered probe must not repeat", probes)
	}
}
