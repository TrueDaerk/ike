package ghissues

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
)

// staleresult_test.go covers the request-tagging race (#2107): fetches resolve
// off the Update loop and out of order, so a listing fetched for a state
// filter the user has already left must never win over the one the active
// filter asked for.

// fetchReq is one recorded call of the injected fetch factory.
type fetchReq struct {
	state forge.IssueState
	gen   int
}

// recordFetches injects a fetch factory that only records what each request
// asked for — nothing runs, so the test decides when (and in which order) the
// answers land.
func recordFetches(m *Model) *[]fetchReq {
	log := &[]fetchReq{}
	m.SetRefresh(func(state forge.IssueState, gen int) tea.Cmd {
		*log = append(*log, fetchReq{state: state, gen: gen})
		return func() tea.Msg { return nil }
	})
	return log
}

// answer is the IssuesMsg one recorded request resolves to, echoing its state
// and generation the way a real fetch echoes them.
func answer(req fetchReq, issues ...forge.Issue) forge.IssuesMsg {
	return forge.IssuesMsg{State: req.state, Gen: req.gen, Issues: issues}
}

// openIssue / closedIssue are minimal listing entries in one state.
func openIssue(n int) forge.Issue {
	return forge.Issue{Number: n, Title: "open " + strconv.Itoa(n), State: "OPEN"}
}

func closedIssue(n int) forge.Issue {
	return forge.Issue{Number: n, Title: "closed " + strconv.Itoa(n), State: "CLOSED"}
}

// listed is the issue numbers the pane currently shows, in row order.
func listed(m *Model) []int {
	var out []int
	for _, idx := range m.visible {
		out = append(out, m.issues[idx].Number)
	}
	return out
}

func TestRapidStateCycleDropsTheSupersededFetch(t *testing.T) {
	m := filled(t)
	log := recordFetches(m)
	m.Update(key("t")) // open → closed
	m.Update(key("t")) // closed → all
	if len(*log) != 2 {
		t.Fatalf("two state changes must dispatch two fetches, got %v", *log)
	}
	closedReq, allReq := (*log)[0], (*log)[1]
	if closedReq.gen == allReq.gen {
		t.Fatalf("every fetch needs its own generation, got %v", *log)
	}
	// The fetch the active filter asked for happens to finish first.
	m.SetResult(answer(allReq, openIssue(1), closedIssue(2)))
	if got := listed(m); len(got) != 2 {
		t.Fatalf("the all listing = %v, want both issues", got)
	}
	// The fetch for the filter the user already left finishes after it. It
	// answers "closed", the pane shows "all" — applying it would drop the
	// open issue from a listing that says it shows everything.
	m.SetResult(answer(closedReq, closedIssue(2)))
	if got := listed(m); len(got) != 2 {
		t.Errorf("listing = %v after the superseded fetch landed, want the all listing kept", got)
	}
	if m.loading {
		t.Error("the newest fetch already answered, so nothing is pending")
	}
}

func TestSupersedingFetchIsAppliedAfterTheStaleOne(t *testing.T) {
	m := filled(t)
	log := recordFetches(m)
	m.Update(key("t")) // open → closed
	m.Update(key("t")) // closed → all
	closedReq, allReq := (*log)[0], (*log)[1]
	// The out-of-order half: the stale answer lands first and is dropped, so
	// the pane must still be waiting rather than showing it.
	m.SetResult(answer(closedReq, closedIssue(2)))
	if got := listed(m); len(got) != 0 {
		t.Fatalf("listing = %v while only the superseded fetch answered, want none", got)
	}
	if !m.loading {
		t.Error("dropping a superseded answer must leave the pending fetch pending")
	}
	m.SetResult(answer(allReq, openIssue(1), closedIssue(2)))
	if got := listed(m); len(got) != 2 {
		t.Errorf("listing = %v, want the superseding fetch applied", got)
	}
	if m.loading {
		t.Error("the awaited answer must clear the loading state")
	}
}

func TestStateChangeClearsTheListingUntilTheRefetchLands(t *testing.T) {
	m := filled(t)
	prs := len(m.prs)
	log := recordFetches(m)
	m.Update(key("t")) // open → closed
	if got := listed(m); len(got) != 0 {
		t.Errorf("listing = %v right after the state change, want it cleared — the rows were fetched for the previous filter", got)
	}
	if !m.loading {
		t.Error("a state-changing refetch must show as loading")
	}
	if !strings.Contains(m.View(), "fetching issues") {
		t.Error("the cleared list must render the loading indicator, not an empty listing")
	}
	if len(m.prs) != prs {
		t.Errorf("PRs = %d, want the %d kept — they are fetched in every state and split client-side", len(m.prs), prs)
	}
	m.SetResult(answer((*log)[0], closedIssue(2)))
	if got := listed(m); len(got) != 1 || got[0] != 2 {
		t.Errorf("listing = %v, want the closed issue the refetch brought", got)
	}
}

func TestPollDuringAStateChangeIsDropped(t *testing.T) {
	m := filled(t)
	log := recordFetches(m)
	m.Update(key("t")) // open → closed; the refetch is in flight
	// The background poll always fetches the open listing (#2085). Landing on
	// a closed filter it would refill the pane with open issues the state gate
	// then hides one by one.
	m.SetResult(pollResult(issuesOf(), nil))
	if got := listed(m); len(got) != 0 {
		t.Errorf("listing = %v after an open-state poll landed on a closed filter, want it dropped", got)
	}
	if len(m.issues) != 0 {
		t.Errorf("issues = %d, want the poll's open listing kept out of the pane entirely", len(m.issues))
	}
	if !m.loading {
		t.Error("a poll must not clear the state change's pending refetch")
	}
	m.SetResult(answer((*log)[0], closedIssue(2)))
	if got := listed(m); len(got) != 1 || got[0] != 2 {
		t.Errorf("listing = %v, want only what the closed filter asked for", got)
	}
}

func TestPollMatchingTheFilterStillApplies(t *testing.T) {
	m := filled(t)
	recordFetches(m)
	press(m, "t", "t", "t") // open → closed → all → open again
	if m.StateFilter() != FilterOpen {
		t.Fatalf("setup: state = %v, want open", m.StateFilter())
	}
	m.SetResult(pollResult(issuesOf(), nil))
	if got := listed(m); len(got) != 3 {
		t.Errorf("listing = %v, want the poll applied normally once the filter is back on open", got)
	}
}

func TestSupersededFetchStillSurfacesItsError(t *testing.T) {
	m := filled(t)
	log := recordFetches(m)
	m.Update(key("t")) // open → closed
	m.Update(key("t")) // closed → all
	m.SetResult(forge.IssuesMsg{
		State: (*log)[0].state, Gen: (*log)[0].gen, Err: errFake("dial tcp: timeout"),
	})
	if m.errMsg == "" {
		t.Error("a dead network is worth recording whichever state filter asked for the fetch")
	}
	if !m.loading {
		t.Error("the newest fetch is still in flight, so the pane stays loading")
	}
	// The pending fetch owns the visible slot until it answers — the pane says
	// "fetching…", not "fetch failed", while one is still on its way.
	if strings.Contains(m.View(), "fetch failed") {
		t.Error("a superseded fetch's error must not preempt the pending one's loading indicator")
	}
	m.SetResult(answer((*log)[1], openIssue(1)))
	if m.errMsg != "" || strings.Contains(m.View(), "fetch failed") {
		t.Error("the answer that did arrive must clear the error")
	}
}

func TestSingleFetchPathIsUntagged(t *testing.T) {
	// The normal path is unchanged: one 'r', one answer, applied. An untagged
	// result (Gen 0 — a caller that does not count its requests) is never read
	// as stale.
	m := filled(t)
	log := recordFetches(m)
	m.Update(key("r"))
	if len(*log) != 1 {
		t.Fatalf("'r' must dispatch exactly one fetch, got %v", *log)
	}
	m.SetResult(answer((*log)[0], openIssue(1), openIssue(2)))
	if got := listed(m); len(got) != 2 {
		t.Fatalf("listing = %v, want the refresh applied", got)
	}
	m.SetResult(forge.IssuesMsg{State: forge.IssuesOpen, Issues: []forge.Issue{openIssue(3)}})
	if got := listed(m); len(got) != 1 || got[0] != 3 {
		t.Errorf("listing = %v, want an untagged result applied verbatim", got)
	}
}
