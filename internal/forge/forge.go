// Package forge talks to the project's code forge (#1934) — GitHub through
// the gh CLI, the way internal/vcs shells out to git. It fetches the open
// issues and pull requests of the current repository, derives issue branch
// names, and drives the "start work on an issue" flow of the change workflow
// (branch issue/<number>-<slug> off an up-to-date default branch).
//
// Like internal/vcs, nothing here runs from Update: every subprocess call is
// wrapped in a tea.Cmd with a timeout, resolving to a message carrying the
// result or the error. Parsing works on gh's --json output, never on its
// human-readable rendering. The types are forge-agnostic so another forge
// (Gitea via tea, say) can produce them later; the gh binding is the one
// implementation shipped.
package forge

import (
	"strconv"
	"strings"
)

// Label is one issue label: its name and the forge-assigned color as a bare
// rrggbb hex string (no leading '#'), the way the GitHub API reports it.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Issue is one open issue of the repository.
type Issue struct {
	Number    int
	Title     string
	Body      string
	URL       string
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
// (OPEN/MERGED/CLOSED), the branch it comes from, and the CI rollup.
type PR struct {
	Number  int
	Title   string
	State   string
	URL     string
	HeadRef string
	Checks  CheckState
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
	Setup  string
	Err    error
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
