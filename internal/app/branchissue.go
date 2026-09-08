package app

import (
	"regexp"
	"strconv"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/config"
	"ike/internal/forge"
	"ike/internal/host"
)

// branchissue.go is the status line's branch-issue segment and its command
// (#2544). Work in this repository happens on issue/<number> branches (see
// the change workflow), and the branch name alone says which number but not
// which issue — reading the title meant opening the Issues tool window for a
// lookup and closing it again. The segment puts "#2544 <title>" on the bar,
// and one click on it opens that issue's detail.
//
// The title is never fetched for the segment: it is read out of the listing
// the Issues window and the background poll already produce (#2085, #2108),
// so an opted-in session costs the forge nothing extra and the label refreshes
// with the poll. A number the listing does not carry renders bare ("#2544"),
// which is still the honest answer — and clicking it still works, because the
// pane fetches on open and the reveal waits for that listing.

// branchIssueTitleMax caps the title in the segment. The bar has one line for
// everything; past ~34 cells an issue title crowds out the segments that say
// what the editor is doing, and the head of a title is what identifies it.
const branchIssueTitleMax = 34

// branchIssueRE caches the compiled branch pattern for the configured string.
// The segment renders every frame and the pattern changes about once per
// config reload, so compiling per frame would be pure waste; the mutex is
// there because a plugin host goroutine may render off the UI loop.
var branchIssueRE struct {
	sync.Mutex
	pattern string
	re      *regexp.Regexp
}

// branchIssuePattern reads statusline.branch_issue_pattern live from the
// config (like the project-time segment), falling back to the default for a
// model built without one.
func branchIssuePattern() string {
	c := config.Get()
	if c == nil || c.StatusLine.BranchIssuePattern == "" {
		return config.DefaultBranchIssuePattern
	}
	return c.StatusLine.BranchIssuePattern
}

// branchIssueSegmentOn reads the opt-in switch live, so a settings flip
// applies without a restart.
func branchIssueSegmentOn() bool {
	c := config.Get()
	return c != nil && c.StatusLine.BranchIssue == "on"
}

// branchIssueNumber extracts the issue number a branch name carries under
// pattern, 0 when it carries none. An invalid pattern resolves nothing rather
// than falling back to another one: the config validator and the settings
// form both refuse such a pattern, so the only way to get here with one is a
// hand-edited file, and silently matching something else would be a lie.
func branchIssueNumber(branch, pattern string) int {
	if branch == "" || pattern == "" {
		return 0
	}
	branchIssueRE.Lock()
	if branchIssueRE.pattern != pattern || branchIssueRE.re == nil {
		re, err := regexp.Compile(pattern)
		branchIssueRE.pattern, branchIssueRE.re = pattern, re
		if err != nil {
			branchIssueRE.re = nil
		}
	}
	re := branchIssueRE.re
	branchIssueRE.Unlock()
	if re == nil || re.NumSubexp() < 1 {
		return 0
	}
	mm := re.FindStringSubmatch(branch)
	if mm == nil {
		return 0
	}
	n, err := strconv.Atoi(mm[1])
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// branchIssue returns the issue number of the checked-out branch, 0 outside a
// git repository, on a detached HEAD and on a branch the pattern misses.
func (m Model) branchIssue() int {
	if m.vcs == nil || m.vcs.snap == nil || m.vcs.snap.Branch == "" {
		return 0
	}
	snap := m.vcs.snap
	return branchIssueNumber(snap.Branch, branchIssuePattern())
}

// branchIssueSegment renders the segment: "#2544 status line: show the issue…"
// with the title from the listing cache, or the bare "#2544" while no listing
// carries it (a cold start with polling off, or an issue outside the fetched
// page). Hidden entirely when the setting is off or the branch names no issue.
func (m Model) branchIssueSegment() string {
	if !branchIssueSegmentOn() {
		return ""
	}
	n := m.branchIssue()
	if n == 0 {
		return ""
	}
	s := "#" + strconv.Itoa(n)
	if title := m.forgeTitles[n]; title != "" {
		s += " " + ansi.Truncate(title, branchIssueTitleMax, "…")
	}
	return s
}

// rememberIssueTitles folds one listing's titles into the segment's lookup
// map. Every listing passes through here — background poll, the pane's own
// fetch and the persisted snapshot alike — so the segment follows a title
// edit on the forge without a lookup of its own. Titles of issues that left
// the listing are kept: the map is bounded by what the repository's listings
// hold, and dropping the current branch's title on a state-filtered fetch
// would make the segment flicker between title and bare number.
func (m *Model) rememberIssueTitles(issues []forge.Issue) {
	if m.forgeTitles == nil || len(issues) == 0 {
		return
	}
	for _, is := range issues {
		if is.Title != "" {
			m.forgeTitles[is.Number] = is.Title
		}
	}
}

// branchIssueSeedCmd asks the persisted listing cache (#2108) for the current
// branch's title once, from the settled pass. Without it an opted-in session
// whose Issues window was never opened would show the bare number until the
// first background poll lands — and show it forever with polling switched
// off. It runs at most once per branch number: the flag is only reset by
// checking out a different issue branch.
func (m *Model) branchIssueSeedCmd() tea.Cmd {
	if !branchIssueSegmentOn() {
		return nil
	}
	n := m.branchIssue()
	if n == 0 || n == m.branchIssueSeeded || m.forgeTitles[n] != "" {
		return nil
	}
	m.branchIssueSeeded = n
	return forge.LoadCacheCmd(".")
}

// IssuesOpenCurrentBranchMsg runs issues.openCurrentBranch — also what a left
// click on the branch-issue segment dispatches (#1128).
type IssuesOpenCurrentBranchMsg struct{}

// openCurrentBranchIssue opens the Issues tool window on the detail of the
// issue the current branch belongs to, the same way the forge event dialog
// lands on an event's issue: reveal it when the pane already holds the
// listing, otherwise remember the number and reveal as soon as the pane's
// first fetch lands. It works whether or not the segment is switched on — the
// branch is what it is; the setting only decides whether the bar says so.
func (m *Model) openCurrentBranchIssue() tea.Cmd {
	n := m.branchIssue()
	if n == 0 {
		branch := "HEAD"
		if m.vcs != nil && m.vcs.snap != nil && m.vcs.snap.Branch != "" {
			branch = m.vcs.snap.Branch
		}
		m.host.Notify(host.Warn, "no issue branch: "+branch+" does not match "+branchIssuePattern())
		return nil
	}
	cmd := m.showIssuesPanel()
	if p := m.issuesPanel(); p != nil && p.Reveal(n) {
		// The reveal opened the detail: fetch its timeline (#2084).
		return tea.Batch(cmd, p.PendingTimelineCmd())
	}
	m.forgeReveal = n
	return cmd
}
