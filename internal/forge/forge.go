// Package forge talks to the project's code forge (#1934, #2083). It fetches
// the issues and pull requests of the current repository, derives issue
// branch names, and drives the "start work on an issue" flow of the change
// workflow (branch issue/<number>-<slug> off an up-to-date default branch).
//
// The operation surface is the Forge interface (backend.go), with one
// binding per forge: GitHub through the gh CLI (gh.go), Gitea/Forgejo
// through the tea CLI's login plus the Gitea REST API (tea.go). detect.go
// picks the binding by the origin remote's host and caches the result per
// workspace root; a repository neither binding serves resolves to an
// explanatory setup message.
//
// Like internal/vcs, nothing here runs from Update: every subprocess (and
// HTTP) call is wrapped in a tea.Cmd with a timeout, resolving to a message
// carrying the result or the error. Parsing works on JSON output only —
// gh's --json, the Gitea REST responses — never on a human-readable
// rendering. The types are forge-agnostic; each binding maps its forge's
// shapes onto them.
package forge

import (
	"strconv"
	"strings"
	"time"
)

// Label is one issue label: its name and the forge-assigned color as a bare
// rrggbb hex string (no leading '#'), the way the GitHub API reports it.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Issue is one issue of the repository. State carries the forge's own
// vocabulary folded to upper case ("OPEN"/"CLOSED") so a listing fetched with
// IssuesAll can still be split per state in the pane; Author is the login
// that opened it, and the two timestamps back the issues window's age column
// and its sort orders (#2090). Backends that do not report a field leave it
// zero — every consumer degrades to hiding the column.
type Issue struct {
	Number    int
	Title     string
	Body      string
	URL       string
	State     string
	Author    string
	CreatedAt time.Time
	UpdatedAt time.Time
	Labels    []Label
	Assignees []string
}

// CheckState summarizes a pull request's CI rollup.
type CheckState int

const (
	// ChecksNone means the PR reports no check runs at all.
	ChecksNone CheckState = iota
	// ChecksPending means at least one check is still queued or running.
	ChecksPending
	// ChecksPassing means every reported check concluded successfully.
	ChecksPassing
	// ChecksFailing means at least one check concluded unsuccessfully.
	ChecksFailing
)

// PR is one pull request, reduced to what the issues window shows: its state
// (OPEN/MERGED/CLOSED), the branch it comes from, the CI rollup, and — for
// the PR tab's own list (#2090) — author, review decision and timestamps.
// Review is the forge's review rollup in upper case ("APPROVED",
// "CHANGES_REQUESTED", "REVIEW_REQUIRED"); backends that do not report one
// leave it empty and the column stays blank.
type PR struct {
	Number    int
	Title     string
	State     string
	URL       string
	HeadRef   string
	Author    string
	Review    string
	CreatedAt time.Time
	UpdatedAt time.Time
	Checks    CheckState
}

// CheckRun is one named CI check of a pull request with its folded state —
// the per-check row the PR detail view lists (#2089), where the listing only
// carries the rollup.
type CheckRun struct {
	Name  string
	State CheckState
}

// PRDetail is one pull request in full (#2089): everything the listing's PR
// carries plus the description, the base branch, the per-check CI results,
// the mergeability and the merge method the forge would use. Backends that do
// not report a field leave it zero; the view degrades to hiding the line.
type PRDetail struct {
	PR
	// Body is the PR's description in markdown.
	Body string
	// BaseRef is the branch the PR merges into.
	BaseRef string
	// Mergeable is the forge's mergeability verdict in a neutral vocabulary:
	// "mergeable", "conflicting", "unknown", or "" when not reported.
	Mergeable string
	// MergeMethod is the method a merge would use, from the repository's
	// default/allowed settings: "merge", "squash", "rebase" (Gitea also
	// "rebase-merge"). Defaults to "merge" when the probe cannot tell.
	MergeMethod string
	// CheckRuns are the individual CI checks of the head commit.
	CheckRuns []CheckRun
}

// IssuesMsg carries one refreshed issue/PR listing back into Update.
//
// Setup, when non-empty, is the explanatory unavailable state: gh is not
// installed, or the repository has no GitHub remote — a condition the user
// fixes outside the pane, not a transient failure. Err is a transient fetch
// error (offline, gh auth); the pane keeps whatever it showed before.
type IssuesMsg struct {
	Issues []Issue
	PRs    []PR
	// State is the issue state the listing was fetched for, echoed back so
	// the pane can tell a stale answer from the one its current state filter
	// asked for (#2090).
	State IssueState
	Setup string
	Err   error
}

// StartWorkDoneMsg reports a finished start-work flow: the branch that was
// created and switched to, an optional warning (the fetch failed and the
// branch came off the local default branch), or the error that stopped it.
type StartWorkDoneMsg struct {
	Branch  string
	Warning string
	Err     error
}

// PRForIssue finds the pull request belonging to issue number by the
// repository's branch convention: a PR whose head branch is
// issue/<number>-<slug> (or exactly issue/<number>) is the issue's PR.
// Preference order: open PR first, then merged, then closed — the state the
// user cares about when several branches referenced the issue over time.
func PRForIssue(prs []PR, number int) *PR {
	var best *PR
	rank := func(state string) int {
		switch strings.ToUpper(state) {
		case "OPEN":
			return 0
		case "MERGED":
			return 1
		default:
			return 2
		}
	}
	prefix := branchPrefix(number)
	for i := range prs {
		ref := prs[i].HeadRef
		if ref != prefix && !strings.HasPrefix(ref, prefix+"-") {
			continue
		}
		if best == nil || rank(prs[i].State) < rank(best.State) {
			best = &prs[i]
		}
	}
	return best
}

// branchPrefix is the number-only head of an issue branch name, "issue/12".
func branchPrefix(number int) string {
	return "issue/" + strconv.Itoa(number)
}
