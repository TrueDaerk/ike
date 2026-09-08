package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/forge"
	"ike/internal/pane"
	"ike/internal/vcs"
)

// branchIssueApp opens a wide app on branch, with the branch-issue segment
// (#2544) switched on and pattern set to the given one (empty = the default).
func branchIssueApp(t *testing.T, branch, pattern string, on bool) Model {
	t.Helper()
	dir := t.TempDir()
	m := openApp(t, writeTemp(t, dir, "a.txt", "aaa\n"))
	// The temp path in the file segment is long; widen the bar so the
	// overflow guard cannot drop the segment under test.
	out, _ := m.Update(tea.WindowSizeMsg{Width: 400, Height: 30})
	m = out.(Model)
	// The first-start LSP onboarding dialog (#301) opens where a registered
	// language's server is missing and swallows input before it reaches the
	// mouse router; a no-op everywhere else.
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	m.vcs.snap = &vcs.Snapshot{Root: dir, Branch: branch}

	// The opt-in is read live from the config, so it is set after the app has
	// started — opening one reloads the config from disk.
	orig := config.Get()
	t.Cleanup(func() { config.Set(orig) })
	c, _ := config.Load(config.Options{})
	if on {
		c.StatusLine.BranchIssue = "on"
	}
	if pattern != "" {
		c.StatusLine.BranchIssuePattern = pattern
	}
	config.Set(c)
	return m
}

// TestBranchIssuePatternMatch guards the number extraction: the default
// pattern reads issue/<n>, a custom one reads whatever it captures, and
// everything else resolves to nothing rather than to a guess.
func TestBranchIssuePatternMatch(t *testing.T) {
	cases := []struct {
		branch, pattern string
		want            int
	}{
		{"issue/2544", "", 2544},
		{"issue/2544-status-line-show-the-issue", "", 2544},
		{"main", "", 0},
		{"issue/abc", "", 0},
		{"feature/issue/2544", "", 0}, // the default pattern is anchored
		{"", "", 0},
		{"issue/0", "", 0}, // no forge numbers issues 0
		{"feature/ISSUE-12-title", `^feature/ISSUE-(\d+)`, 12},
		{"issue/2544", `^issue/\d+`, 0},  // no capture group: nothing to read
		{"issue/2544", `^issue/(\d+`, 0}, // does not compile
		{"bugfix/77", `bugfix/(\d+)$`, 77},
	}
	for _, c := range cases {
		pattern := c.pattern
		if pattern == "" {
			pattern = config.DefaultBranchIssuePattern
		}
		if got := branchIssueNumber(c.branch, pattern); got != c.want {
			t.Errorf("branchIssueNumber(%q, %q) = %d, want %d", c.branch, pattern, got, c.want)
		}
	}
}

// TestBranchIssueSegmentOptIn guards the opt-in: nothing renders while the
// setting is off, and nothing renders on a branch the pattern misses.
func TestBranchIssueSegmentOptIn(t *testing.T) {
	m := branchIssueApp(t, "issue/2544", "", false)
	if s := m.branchIssueSegment(); s != "" {
		t.Fatalf("segment off must render nothing, got %q", s)
	}
	m = branchIssueApp(t, "main", "", true)
	if s := m.branchIssueSegment(); s != "" {
		t.Fatalf("a non-issue branch must render nothing, got %q", s)
	}
}

// TestBranchIssueSegmentCacheMiss guards the cold path: an issue no listing
// carries renders the bare number — the honest answer — and the title appears
// as soon as a listing lands.
func TestBranchIssueSegmentCacheMiss(t *testing.T) {
	m := branchIssueApp(t, "issue/2544-status-line", "", true)
	if got := m.branchIssueSegment(); got != "#2544" {
		t.Fatalf("cache miss must render the bare number, got %q", got)
	}
	m.rememberIssueTitles([]forge.Issue{{Number: 2544, Title: "status line: show the issue"}})
	if got, want := m.branchIssueSegment(), "#2544 status line: show the issue"; got != want {
		t.Fatalf("segment = %q, want %q", got, want)
	}
	// A title longer than the cap is truncated, never wrapped onto the bar.
	m.rememberIssueTitles([]forge.Issue{{Number: 2544, Title: strings.Repeat("x", 80)}})
	if got := m.branchIssueSegment(); len([]rune(got)) > branchIssueTitleMax+len("#2544 ") {
		t.Fatalf("segment %q is not truncated to the cap", got)
	}
}

// TestBranchIssueTitlesFromListing guards the no-extra-request promise: the
// titles come out of the listings the app already routes, poll and cached
// snapshot alike.
func TestBranchIssueTitlesFromListing(t *testing.T) {
	m := branchIssueApp(t, "issue/2544", "", true)
	out, _ := m.Update(forge.IssuesMsg{Poll: true, Issues: []forge.Issue{{Number: 2544, Title: "from the poll"}}})
	m = out.(Model)
	if got, want := m.branchIssueSegment(), "#2544 from the poll"; got != want {
		t.Fatalf("after a poll listing: segment = %q, want %q", got, want)
	}
	out, _ = m.Update(forge.CachedListingMsg{Issues: []forge.Issue{{Number: 2544, Title: "from the cache"}}})
	m = out.(Model)
	if got, want := m.branchIssueSegment(), "#2544 from the cache"; got != want {
		t.Fatalf("after a cached listing: segment = %q, want %q", got, want)
	}
}

// TestBranchIssueClickOpensIssue guards the click routing (#1128): a left
// press on the segment dispatches issues.openCurrentBranch, whose handler
// opens the Issues window on the branch's issue.
func TestBranchIssueClickOpensIssue(t *testing.T) {
	m := branchIssueApp(t, "issue/2544", "", true)
	m.rememberIssueTitles([]forge.Issue{{Number: 2544, Title: "status line"}})
	x := -1
	for i := 0; i < m.width; i++ {
		if m.statusSegmentAt(i) == "branchissue" {
			x = i
			break
		}
	}
	if x < 0 {
		t.Fatal("setup: no branch-issue segment on the status row")
	}
	out, cmd := m.Update(tea.MouseClickMsg{X: x, Y: m.height - 1, Button: tea.MouseLeft})
	m = out.(Model)
	found := false
	for _, msg := range cmdMsgs(cmd) {
		if _, ok := msg.(IssuesOpenCurrentBranchMsg); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("clicking the branch-issue segment must dispatch issues.openCurrentBranch")
	}
	out, _ = m.Update(IssuesOpenCurrentBranchMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.IssuesKey) {
		t.Fatal("issues.openCurrentBranch must open the Issues window")
	}
	if m.forgeReveal != 2544 {
		t.Fatalf("the issue must be revealed once the listing lands, forgeReveal = %d", m.forgeReveal)
	}
}

// TestBranchIssueCommandOffBranch guards the no-match path of the command:
// it says why nothing opened instead of opening an arbitrary issue.
func TestBranchIssueCommandOffBranch(t *testing.T) {
	m := branchIssueApp(t, "main", "", true)
	out, _ := m.Update(IssuesOpenCurrentBranchMsg{})
	m = out.(Model)
	if m.forgeReveal != 0 {
		t.Fatalf("a branch with no issue must reveal nothing, forgeReveal = %d", m.forgeReveal)
	}
	if m.activeWS().Panes.Has(pane.IssuesKey) {
		t.Fatal("a branch with no issue must not open the Issues window")
	}
}
