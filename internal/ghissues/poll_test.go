package ghissues

import (
	"errors"
	"strings"
	"testing"

	"ike/internal/forge"
)

// poll_test.go covers what a background poll result (#2085) must not do to a
// user mid-interaction: move the cursor, drop the filters, blank the
// linked-PR column over a partial failure, or clear a pending manual refresh.

// baseIssues is the listing filled(t) seeded, re-delivered unchanged.
func baseIssues() []forge.Issue {
	return []forge.Issue{
		{Number: 1, Title: "explorer crash on rename", URL: "https://e/1",
			Labels: []forge.Label{{Name: "bug", Color: "d73a4a"}}, Assignees: []string{"dev"}},
		{Number: 2, Title: "add markdown preview", URL: "https://e/2",
			Labels: []forge.Label{{Name: "feature", Color: "a2eeef"}}, Body: "## Task\nRender it."},
		{Number: 3, Title: "explorer icons", URL: "https://e/3",
			Labels: []forge.Label{{Name: "feature", Color: "a2eeef"}}},
	}
}

// pollResult wraps a listing as one background poll would deliver it.
func pollResult(issues []forge.Issue, prs []forge.PR) forge.IssuesMsg {
	return forge.IssuesMsg{Issues: issues, PRs: prs, Poll: true}
}

func TestPollKeepsCursorOnTheSelectedIssue(t *testing.T) {
	m := filled(t)
	m.Update(key("j")) // cursor on issue 2
	if m.Cursor() != 1 || m.Selected().Number != 2 {
		t.Fatalf("setup: cursor=%d issue=%d, want row 1 / issue 2", m.Cursor(), m.Selected().Number)
	}
	// A newer issue lands at the top of the listing, shifting every row down.
	m.SetResult(pollResult([]forge.Issue{
		{Number: 4, Title: "brand new"},
		{Number: 1, Title: "explorer crash on rename"},
		{Number: 2, Title: "add markdown preview"},
		{Number: 3, Title: "explorer icons"},
	}, nil))
	if m.Selected() == nil || m.Selected().Number != 2 {
		t.Fatalf("selection = %v, want issue 2 to survive the refresh", m.Selected())
	}
	if m.Cursor() != 2 {
		t.Errorf("cursor = %d, want row 2 (issue 2 moved down by one)", m.Cursor())
	}
}

func TestPollKeepsFuzzyAndLabelFilters(t *testing.T) {
	m := filled(t)
	m.Update(key("l")) // first label: "bug"
	if m.LabelFilter() != "bug" {
		t.Fatalf("setup: label filter = %q, want bug", m.LabelFilter())
	}
	m.Update(key("/"))
	m.Update(key("e"))
	if !m.Filtering() || m.Filter() != "e" {
		t.Fatalf("setup: filtering=%v pattern=%q", m.Filtering(), m.Filter())
	}
	m.SetResult(pollResult([]forge.Issue{
		{Number: 4, Title: "brand new", Labels: []forge.Label{{Name: "bug"}}},
		{Number: 1, Title: "explorer crash on rename", Labels: []forge.Label{{Name: "bug"}}},
		{Number: 2, Title: "add markdown preview", Labels: []forge.Label{{Name: "feature"}}},
	}, nil))
	if !m.Filtering() {
		t.Error("the open filter line must survive a background refresh")
	}
	if m.Filter() != "e" {
		t.Errorf("pattern = %q, want it kept", m.Filter())
	}
	if m.LabelFilter() != "bug" {
		t.Errorf("label filter = %q, want bug kept", m.LabelFilter())
	}
	// Both filters still gate: the "feature" issue is out on the label, and
	// only the two "bug" ones with an "e" survive the pattern.
	if m.Visible() != 2 {
		t.Errorf("visible = %d, want the 2 bug issues matching \"e\"", m.Visible())
	}
}

func TestPollDoesNotClearAPendingManualRefresh(t *testing.T) {
	m := filled(t)
	m.MarkLoading()
	m.SetResult(pollResult([]forge.Issue{{Number: 1, Title: "one"}}, nil))
	if !m.loading {
		t.Error("a poll landing mid-'r' must leave the manual refresh pending")
	}
	m.SetResult(forge.IssuesMsg{Issues: []forge.Issue{{Number: 1, Title: "one"}}})
	if m.loading {
		t.Error("the foreground result should clear the loading state")
	}
}

func TestPartialFailureKeepsThePRColumn(t *testing.T) {
	m := filled(t)
	if len(m.prs) != 1 {
		t.Fatalf("setup: %d PRs, want 1", len(m.prs))
	}
	m.SetResult(forge.IssuesMsg{
		Issues: []forge.Issue{{Number: 1, Title: "explorer crash on rename"}},
		PRErr:  errors.New("gh pr list failed"),
		Poll:   true,
	})
	if len(m.prs) != 1 {
		t.Errorf("PRs = %d, want the last known listing kept over a PR-listing blip", len(m.prs))
	}
}

func TestPollDropsTheDetailCacheOfTheOpenIssue(t *testing.T) {
	m := filled(t)
	m.Update(key("j")) // issue 2, the one with a body
	m.Update(key("enter"))
	if !m.DetailOpen() {
		t.Fatal("setup: detail view should be open")
	}
	m.detailFor, m.detailLines = 2, []string{"stale"}
	m.SetResult(pollResult([]forge.Issue{
		{Number: 1, Title: "explorer crash on rename"},
		{Number: 2, Title: "add markdown preview", Body: "## Task\nEdited on the forge."},
		{Number: 3, Title: "explorer icons"},
	}, nil))
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
	m.Update(key("j")) // issue 2, the one with a body
	m.Update(key("enter"))
	m.detailLines = []string{"rendered"}
	m.SetResult(pollResult(baseIssues(), nil))
	if m.detailLines == nil {
		t.Error("an unchanged body must not be re-rendered every poll")
	}
}

func TestPollKeepsTheDetailScrollOffset(t *testing.T) {
	m := filled(t)
	m.Update(key("j")) // issue 2
	m.Update(key("enter"))
	body := strings.Repeat("a long line of prose\n", 60)
	m.SetResult(pollResult([]forge.Issue{
		{Number: 1, Title: "explorer crash on rename"},
		{Number: 2, Title: "add markdown preview", Body: body},
		{Number: 3, Title: "explorer icons"},
	}, nil))
	m.View() // render the detail once so there are lines to scroll
	m.detailTop = 5
	// A poll bringing an edited body re-renders it — the offset must survive,
	// or a long issue would jump back to line one every interval.
	m.SetResult(pollResult([]forge.Issue{
		{Number: 1, Title: "explorer crash on rename"},
		{Number: 2, Title: "add markdown preview", Body: body + "one more paragraph.\n"},
		{Number: 3, Title: "explorer icons"},
	}, nil))
	m.View()
	if m.detailTop != 5 {
		t.Errorf("detailTop = %d after a background re-render, want the 5 the user scrolled to", m.detailTop)
	}
}

func TestClosedIssueLeavesTheCursorClamped(t *testing.T) {
	m := filled(t)
	m.Update(key("j"))
	m.Update(key("j")) // issue 3, the last row
	if m.Selected().Number != 3 {
		t.Fatalf("setup: selected %d, want 3", m.Selected().Number)
	}
	m.SetResult(pollResult([]forge.Issue{{Number: 1, Title: "explorer crash on rename"}}, nil))
	if m.Cursor() != 0 || m.Selected().Number != 1 {
		t.Errorf("cursor=%d selected=%v, want the clamped first row", m.Cursor(), m.Selected())
	}
}
