package forge

// pr.go is the pull-request action side of the forge layer (#2089): the
// tea.Cmds behind the PR detail view (one full fetch), the neutral PRAction
// description of "merge with a comment" / "close with a comment", the
// Closes-#N link a PR body derives its issue from, and the post-merge branch
// cleanup of the change workflow. Package rule unchanged: nothing here runs
// from Update — the pane hands work to the injected factories and gets a
// message back.

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// PRDetailMsg carries one fetched PR detail back into Update. Number echoes
// the request so the pane can drop an answer for a PR it no longer shows.
type PRDetailMsg struct {
	Number int
	Detail PRDetail
	Err    error
}

// PRDetailCmd fetches one pull request in full, resolving to one PRDetailMsg.
func PRDetailCmd(dir string, pr int) tea.Cmd {
	return func() tea.Msg {
		f, setup := Detect(dir)
		if setup != "" {
			return PRDetailMsg{Number: pr, Err: errors.New(setup)}
		}
		d, err := f.PRDetail(pr)
		return PRDetailMsg{Number: pr, Detail: d, Err: err}
	}
}

// PRDetailFactory is the per-PR fetch factory the issues window is injected
// with, mirroring TimelineFactory.
func PRDetailFactory(dir string) func(pr int) tea.Cmd {
	return func(pr int) tea.Cmd { return PRDetailCmd(dir, pr) }
}

// PR action kinds.
const (
	// PRMerge merges the pull request.
	PRMerge = "merge"
	// PRClose closes it without merging.
	PRClose = "close"
)

// PRAction describes one write against a pull request: merge or close, with
// an optional comment that is posted first — so the timeline reads in the
// order the user meant.
type PRAction struct {
	// PR is the pull-request number.
	PR int
	// Kind is PRMerge or PRClose.
	Kind string
	// Method is the merge method (PRMerge only); "" lets the binding pick.
	Method string
	// Comment, when non-empty, is posted before the action.
	Comment string
}

// PRActionMsg reports one finished PR action back into Update. PR and Kind
// echo the request. Err carries the forge's own rejection — a merge conflict,
// a branch-protection refusal — worded by the forge, not a generic failure.
type PRActionMsg struct {
	PR   int
	Kind string
	Err  error
}

// PRActionCmd applies one PRAction, resolving to a PRActionMsg.
func PRActionCmd(dir string, act PRAction) tea.Cmd {
	return func() tea.Msg {
		f, setup := Detect(dir)
		if setup != "" {
			return PRActionMsg{PR: act.PR, Kind: act.Kind, Err: errors.New(setup)}
		}
		return PRActionMsg{PR: act.PR, Kind: act.Kind, Err: applyPRAction(f, act)}
	}
}

// applyPRAction runs one action's parts against a backend: the comment first,
// then the merge or close, stopping at the first failure — a posted comment
// with a failed merge still reads correctly on the forge.
func applyPRAction(f Forge, act PRAction) error {
	if act.Comment != "" {
		if err := f.CommentPR(act.PR, act.Comment); err != nil {
			return err
		}
	}
	switch act.Kind {
	case PRMerge:
		return f.MergePR(act.PR, act.Method)
	case PRClose:
		return f.ClosePR(act.PR)
	}
	return fmt.Errorf("forge: unknown PR action %q", act.Kind)
}

// PRActionFactory is the PR-write factory the issues window is injected with,
// mirroring MutateFactory.
func PRActionFactory(dir string) func(PRAction) tea.Cmd {
	return func(act PRAction) tea.Cmd { return PRActionCmd(dir, act) }
}

// closesRE matches the closing keywords GitHub and Gitea both honor, in the
// "keyword #number" form the change workflow's PR bodies use.
var closesRE = regexp.MustCompile(`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s*:?\s+#(\d+)`)

// LinkedIssue derives the issue a PR body claims to close ("Closes #N" and
// its keyword variants), 0 when none — the detail view's link back into the
// issues tab (#2089).
func LinkedIssue(body string) int {
	m := closesRE.FindStringSubmatch(body)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

// CleanupDoneMsg reports a finished post-merge branch cleanup (#2089): the
// branch that was removed, an optional warning for the non-fatal steps (the
// pull failed, the remote branch was already gone), or the error that stopped
// the flow.
type CleanupDoneMsg struct {
	Branch  string
	Warning string
	Err     error
}

// CleanupBranchCmd runs the change-workflow cleanup after a merged PR:
// switch to an up-to-date default branch and delete the issue branch locally
// and on origin. It is only ever run on the user's explicit confirmation —
// the pane offers it, never applies it.
func CleanupBranchCmd(dir, branch string) tea.Cmd {
	return func() tea.Msg {
		warning, err := cleanupBranch(dir, branch)
		return CleanupDoneMsg{Branch: branch, Warning: warning, Err: err}
	}
}

// cleanupBranch runs the flow synchronously; split out so tests drive it
// directly. It refuses a dirty worktree and a branch that is the default
// branch itself; the pull and the remote deletion degrade to warnings — a
// forge that auto-deletes merged branches must not turn the cleanup into an
// error.
func cleanupBranch(dir, branch string) (warning string, err error) {
	if branch == "" {
		return "", errors.New("no branch to clean up")
	}
	dirty, err := worktreeDirty(dir)
	if err != nil {
		return "", err
	}
	if dirty {
		return "", errors.New("working tree has uncommitted changes — commit or stash them before cleaning up")
	}
	def, err := defaultBranch(dir)
	if err != nil {
		return "", err
	}
	if branch == def {
		return "", errors.New("refusing to delete the default branch " + def)
	}
	if _, err := runGitQuick(dir, "checkout", def); err != nil {
		return "", err
	}
	var warnings []string
	if _, perr := runGitTimeout(dir, gitFetchTimeout, "pull", "--ff-only", "origin", def); perr != nil {
		warnings = append(warnings, "pull failed ("+perr.Error()+")")
	}
	// A branch that only ever existed remotely (or was already cleaned) is
	// not an error; -D because the merge may have happened with a different
	// method than the local history reflects (squash, rebase).
	if _, err := runGitQuick(dir, "rev-parse", "--verify", "refs/heads/"+branch); err == nil {
		if _, derr := runGitQuick(dir, "branch", "-D", branch); derr != nil {
			return strings.Join(warnings, "; "), derr
		}
	}
	if _, rerr := runGitTimeout(dir, gitFetchTimeout, "push", "origin", "--delete", branch); rerr != nil {
		warnings = append(warnings, "remote branch not deleted ("+rerr.Error()+")")
	}
	return strings.Join(warnings, "; "), nil
}
