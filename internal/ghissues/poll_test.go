package ghissues

import (
	"errors"
	"strings"
	"testing"
	"time"

	"ike/internal/forge"
)

// poll_test.go covers what a background poll result (#2085) must not do to a
// user mid-interaction: move the cursor, drop the filters, blank the
// linked-PR column over a partial failure, or clear a pending manual refresh.

// pollResult wraps a listing as one background poll would deliver it. The
// state is echoed the way a real fetch echoes it, so the pane does not read
// the result as an answer to a different state filter.
func pollResult(issues []forge.Issue, prs []forge.PR) forge.IssuesMsg {
	return forge.IssuesMsg{State: forge.IssuesOpen, Issues: issues, PRs: prs, Poll: true}
}

// issuesOf is filled(t)'s three issues, re-delivered with their sort keys
// intact so the default created-desc order is stable across a poll. Any issue
// passed in is appended, letting one test add a newer one.
func issuesOf(extra ...forge.Issue) []forge.Issue {
	base := []forge.Issue{
		{Number: 1, Title: "explorer crash on rename", URL: "https://e/1", State: "OPEN",
			Author: "ada", CreatedAt: fixedNow.Add(-72 * time.Hour), UpdatedAt: fixedNow.Add(-time.Hour),
			Labels: []forge.Label{{Name: "bug", Color: "d73a4a"}}, Assignees: []string{"dev"}},
		{Number: 2, Title: "add markdown preview", URL: "https://e/2", State: "OPEN",
			Author: "bo", CreatedAt: fixedNow.Add(-24 * time.Hour), UpdatedAt: fixedNow.Add(-48 * time.Hour),
			Labels: []forge.Label{{Name: "feature", Color: "a2eeef"}}, Body: "## Task\nRender it."},
		{Number: 3, Title: "explorer icons", URL: "https://e/3", State: "OPEN",
			Author: "cy", CreatedAt: fixedNow.Add(-240 * time.Hour), UpdatedAt: fixedNow.Add(-240 * time.Hour),
			Labels: []forge.Label{{Name: "feature", Color: "a2eeef"}}},
	}
	return append(base, extra...)
}

func TestPollKeepsCursorOnTheSelectedIssue(t *testing.T) {
	m := filled(t)
	selectIssue(t, m, 2)
	before := m.Cursor()
	// A newer issue lands above the selection in the created-desc order,
	// shifting every row below it down.
	m.SetResult(pollResult(issuesOf(forge.Issue{
		Number: 4, Title: "brand new", State: "OPEN",
		CreatedAt: fixedNow.Add(-time.Hour), UpdatedAt: fixedNow.Add(-time.Hour),
	}), nil))
	if m.Selected() == nil || m.Selected().Number != 2 {
		t.Fatalf("selection = %v, want issue 2 to survive the refresh", m.Selected())
	}
	if m.Cursor() != before+1 {
		t.Errorf("cursor = %d, want %d — the row moved down, so a cursor kept by index would show the wrong issue", m.Cursor(), before+1)
	}
}

func TestPollKeepsFuzzyAndLabelFilters(t *testing.T) {
	m := filled(t)
	m.labelSel["bug"] = true
	m.applyFilter()
	m.Update(key("/"))
	m.Update(key("e"))
	if !m.Filtering() || m.Filter() != "e" {
		t.Fatalf("setup: filtering=%v pattern=%q", m.Filtering(), m.Filter())
	}
	visible := m.Visible()
	m.SetResult(pollResult(issuesOf(), nil))
	if !m.Filtering() {
		t.Error("the open filter line must survive a background refresh")
	}
	if m.Filter() != "e" {
		t.Errorf("pattern = %q, want it kept", m.Filter())
	}
	if got := m.LabelFilter(); len(got) != 1 || got[0] != "bug" {
		t.Errorf("label filter = %q, want [bug] kept", got)
	}
	if m.Visible() != visible {
		t.Errorf("visible = %d, want the %d both filters gated before the poll", m.Visible(), visible)
	}
}

func TestPollDoesNotClearAPendingManualRefresh(t *testing.T) {
	m := filled(t)
	m.MarkLoading()
	m.SetResult(pollResult(issuesOf(), nil))
	if !m.loading {
		t.Error("a poll landing mid-'r' must leave the manual refresh pending")
	}
	m.SetResult(forge.IssuesMsg{State: forge.IssuesOpen, Issues: issuesOf()})
	if m.loading {
		t.Error("the foreground result should clear the loading state")
	}
}

func TestPartialFailureKeepsThePRColumn(t *testing.T) {
	m := filled(t)
	before := len(m.prs)
	if before == 0 {
		t.Fatal("setup: the fixture should carry pull requests")
	}
	m.SetResult(forge.IssuesMsg{
		State:  forge.IssuesOpen,
		Issues: issuesOf(),
		PRErr:  errors.New("gh pr list failed"),
		Poll:   true,
	})
	if len(m.prs) != before {
		t.Errorf("PRs = %d, want the last known listing (%d) kept over a PR-listing blip", len(m.prs), before)
	}
}

func TestPollDropsTheDetailCacheOfTheOpenIssue(t *testing.T) {
	m := filled(t)
	selectIssue(t, m, 2)
	m.openDetail()
	if !m.DetailOpen() {
		t.Fatal("setup: detail view should be open")
	}
	m.detailFor, m.detailLines = 2, []string{"stale"}
	edited := issuesOf()
	edited[1].Body = "## Task\nEdited on the forge."
	m.SetResult(pollResult(edited, nil))
	if !m.DetailOpen() {
		t.Error("the detail view must stay open across a background refresh")
	}
	if m.detailLines != nil {
		t.Error("an edited body must drop the rendered cache so the edit shows")
	}
	if m.detailFor != 2 {
		t.Error("the cache is dropped by clearing the lines, not the issue number — the number is what keeps the scroll offset")
	}
}

func TestPollKeepsTheDetailCacheOfAnUnchangedBody(t *testing.T) {
	m := filled(t)
	selectIssue(t, m, 2)
	m.openDetail()
	m.detailFor, m.detailLines = 2, []string{"rendered"}
	m.SetResult(pollResult(issuesOf(), nil))
	if m.detailLines == nil {
		t.Error("an unchanged body must not be re-rendered every poll")
	}
}

func TestPollKeepsTheDetailScrollOffset(t *testing.T) {
	m := filled(t)
	selectIssue(t, m, 2)
	m.openDetail()
	body := strings.Repeat("a long line of prose\n", 60)
	long := issuesOf()
	long[1].Body = body
	m.SetResult(pollResult(long, nil))
	m.View() // render the detail once so there are lines to scroll
	m.detailTop = 5
	// A poll bringing an edited body re-renders it — the offset must survive,
	// or a long issue would jump back to line one every interval.
	edited := issuesOf()
	edited[1].Body = body + "one more paragraph.\n"
	m.SetResult(pollResult(edited, nil))
	m.View()
	if m.detailTop != 5 {
		t.Errorf("detailTop = %d after a background re-render, want the 5 the user scrolled to", m.detailTop)
	}
}

func TestClosedIssueLeavesTheCursorClamped(t *testing.T) {
	m := filled(t)
	selectIssue(t, m, 3)
	m.SetResult(pollResult(issuesOf()[:1], nil))
	if m.Cursor() != 0 || m.Selected() == nil || m.Selected().Number != 1 {
		t.Errorf("cursor=%d selected=%v, want the clamped first row", m.Cursor(), m.Selected())
	}
}
