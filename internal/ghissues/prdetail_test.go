package ghissues

// prdetail_test.go covers the PR detail view and its write side (#2089): the
// detail rendering with per-check status and the linked issue, the push
// capability gate, the two-stage merge/close dialog, the forge-reason error
// surface, and the post-merge cleanup offer.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
)

// prActive is the filled pane on the PR tab with both PR factories injected
// and a push capability probed; sent collects every action dispatched.
func prActive(t *testing.T) (*Model, *[]forge.PRAction) {
	t.Helper()
	m := filled(t)
	m.SetPRDetailFetch(func(pr int) tea.Cmd { return func() tea.Msg { return nil } })
	var sent []forge.PRAction
	m.SetPRAction(func(act forge.PRAction) tea.Cmd {
		sent = append(sent, act)
		return func() tea.Msg { return forge.PRActionMsg{PR: act.PR, Kind: act.Kind} }
	})
	m.SetRepoMeta(forge.RepoMetaMsg{Caps: forge.Capabilities{Triage: true, Push: true}})
	m.Update(key("tab"))
	return m, &sent
}

func TestPRDetailRendersFetchedData(t *testing.T) {
	m, _ := prActive(t)
	m.SetSize(90, 30) // tall enough that the check list is above the fold
	m.Update(key("enter"))
	if !m.PRDetailOpen() {
		t.Fatal("enter must open the PR detail")
	}
	if !strings.Contains(m.View(), "fetching the pull request") {
		t.Fatal("the loading state must render")
	}
	m.SetPRDetailResult(forge.PRDetailMsg{Number: 9, Detail: forge.PRDetail{
		PR: forge.PR{Number: 9, Title: "fix the explorer crash", State: "OPEN",
			HeadRef: "issue/1-explorer-crash", Author: "ada", Review: "APPROVED"},
		Body:        "## Fix\nDone.\n\nCloses #1",
		BaseRef:     "main",
		Mergeable:   "mergeable",
		MergeMethod: "merge",
		CheckRuns: []forge.CheckRun{
			{Name: "build", State: forge.ChecksPassing},
			{Name: "lint", State: forge.ChecksFailing},
		},
	}})
	view := m.View()
	for _, want := range []string{
		"issue/1-explorer-crash → main", // branches in the meta line
		"mergeable",
		"── checks ──",
		"✓ build",
		"✗ lint",
		"Closes #1 — explorer crash on rename", // linked issue with its title
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("detail must show %q:\n%s", want, view)
		}
	}
}

func TestPRDetailDropsStaleAnswer(t *testing.T) {
	m, _ := prActive(t)
	m.Update(key("enter")) // PR #9
	m.SetPRDetailResult(forge.PRDetailMsg{Number: 8, Detail: forge.PRDetail{
		PR: forge.PR{Number: 8}, Body: "stale",
	}})
	if m.prd != nil {
		t.Fatal("an answer for another PR must be dropped")
	}
}

func TestPRActionGateWithoutPermission(t *testing.T) {
	m := filled(t)
	m.SetPRAction(func(forge.PRAction) tea.Cmd { return nil })
	m.SetRepoMeta(forge.RepoMetaMsg{Caps: forge.Capabilities{Triage: true}})
	m.Update(key("tab"))
	if strings.Contains(m.View(), "M merge") {
		t.Fatal("an ungranted action must not be advertised in the footer")
	}
	var label string
	for _, a := range m.Actions() {
		if a[0] == "M" {
			label = a[1]
		}
	}
	if !strings.Contains(label, "needs push permission") {
		t.Fatalf("the menu must name the reason, got %q", label)
	}
	m.Update(key("M"))
	if m.PRActionDialogOpen() {
		t.Fatal("the dialog must not open without permission")
	}
	if !strings.Contains(m.MutationError(), "needs push permission") {
		t.Fatalf("mutErr = %q", m.MutationError())
	}
}

func TestMergeDialogTwoStagesAndDispatch(t *testing.T) {
	m, sent := prActive(t)
	m.Update(key("M"))
	if !m.PRActionDialogOpen() {
		t.Fatal("M must open the merge dialog")
	}
	// Stage 0: the optional comment.
	press(m, "l", "g", "t", "m")
	if !strings.Contains(m.View(), "Merge #9 with a comment") {
		t.Fatalf("the comment stage must name the PR:\n%s", m.View())
	}
	m.Update(key("enter"))
	// Stage 1: the confirm names PR, branches and method; nothing sent yet.
	view := m.View()
	if !strings.Contains(view, "merge #9: issue/1-explorer-crash") || !strings.Contains(view, "method: merge") {
		t.Fatalf("the confirm must name PR, branch and method:\n%s", view)
	}
	if len(*sent) != 0 {
		t.Fatal("nothing may be dispatched before the confirm")
	}
	cmd := m.Update(key("enter"))
	if cmd == nil || len(*sent) != 1 {
		t.Fatalf("the confirm must dispatch exactly one action, sent = %v", *sent)
	}
	act := (*sent)[0]
	if act.PR != 9 || act.Kind != forge.PRMerge || act.Method != "merge" || act.Comment != "lgtm" {
		t.Fatalf("action = %+v", act)
	}
	if !m.MutationBusy() {
		t.Fatal("the in-flight state must show")
	}
}

func TestMergeDialogEscCancels(t *testing.T) {
	m, sent := prActive(t)
	press(m, "M", "enter", "esc")
	if m.PRActionDialogOpen() || len(*sent) != 0 {
		t.Fatalf("esc must cancel without dispatching, sent = %v", *sent)
	}
}

func TestCloseDialogDispatchesClose(t *testing.T) {
	m, sent := prActive(t)
	press(m, "c", "enter", "enter")
	if len(*sent) != 1 || (*sent)[0].Kind != forge.PRClose || (*sent)[0].Method != "" {
		t.Fatalf("sent = %v", *sent)
	}
}

func TestPRActionRejectionShowsForgeReason(t *testing.T) {
	m, _ := prActive(t)
	press(m, "M", "enter", "enter")
	m.SetPRActionResult(forge.PRActionMsg{PR: 9, Kind: forge.PRMerge,
		Err: errFake("gh: Base branch requires 1 approving review")})
	if !strings.Contains(m.MutationError(), "Base branch requires 1 approving review") {
		t.Fatalf("mutErr = %q", m.MutationError())
	}
	if !strings.Contains(m.View(), "Base branch requires") {
		t.Fatal("the forge's reason must be on screen")
	}
	if m.CleanupOfferOpen() {
		t.Fatal("a failed merge must not offer the cleanup")
	}
}

func TestMergeSuccessRefreshesAndOffersCleanup(t *testing.T) {
	m, _ := prActive(t)
	refreshed := 0
	m.SetRefresh(func(forge.IssueState) tea.Cmd {
		refreshed++
		return func() tea.Msg { return nil }
	})
	press(m, "M", "enter", "enter")
	cmd := m.SetPRActionResult(forge.PRActionMsg{PR: 9, Kind: forge.PRMerge})
	if cmd == nil || refreshed != 1 {
		t.Fatal("a successful merge must refetch the listing (issues included)")
	}
	if !m.CleanupOfferOpen() {
		t.Fatal("a successful merge must offer the branch cleanup")
	}
	if !strings.Contains(m.View(), "issue/1-explorer-crash") {
		t.Fatal("the offer must name the branch")
	}
	// Accepting emits the request for the app to run.
	ccmd := m.Update(key("enter"))
	if ccmd == nil {
		t.Fatal("accepting the offer must emit the cleanup request")
	}
	msg, ok := ccmd().(CleanupRequestMsg)
	if !ok || msg.Branch != "issue/1-explorer-crash" {
		t.Fatalf("msg = %#v", msg)
	}
	if m.CleanupOfferOpen() {
		t.Fatal("the offer must close after accepting")
	}
}

func TestCleanupOfferDeclined(t *testing.T) {
	m, _ := prActive(t)
	press(m, "M", "enter", "enter")
	m.SetPRActionResult(forge.PRActionMsg{PR: 9, Kind: forge.PRMerge})
	cmd := m.Update(key("esc"))
	if cmd != nil || m.CleanupOfferOpen() {
		t.Fatal("esc must decline without emitting anything")
	}
}

func TestCloseSuccessDoesNotOfferCleanup(t *testing.T) {
	m, _ := prActive(t)
	m.SetRefresh(func(forge.IssueState) tea.Cmd { return func() tea.Msg { return nil } })
	press(m, "c", "enter", "enter")
	m.SetPRActionResult(forge.PRActionMsg{PR: 9, Kind: forge.PRClose})
	if m.CleanupOfferOpen() {
		t.Fatal("a close must not offer the branch cleanup")
	}
}

func TestPRActionRefusesNonOpenPR(t *testing.T) {
	m, sent := prActive(t)
	m.Update(key("t")) // state filter → closed: the list shows the MERGED #8
	if pr := m.SelectedPR(); pr == nil || pr.Number != 8 {
		t.Fatalf("selected = %+v", m.SelectedPR())
	}
	m.Update(key("M"))
	if m.PRActionDialogOpen() || len(*sent) != 0 {
		t.Fatal("a merged PR must not open the dialog")
	}
	if !strings.Contains(m.MutationError(), "already merged") {
		t.Fatalf("mutErr = %q", m.MutationError())
	}
}
