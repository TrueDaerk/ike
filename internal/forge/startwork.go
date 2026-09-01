package forge

// startwork.go implements the "start work on an issue" action of the change
// workflow: branch issue/<number> off an up-to-date default branch and
// switch to it. The flow refuses a dirty worktree and an already-existing
// issue branch with a clear message, and degrades to the local default
// branch (with a warning) when the fetch fails — offline must never block
// starting work.

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// StartWorkCmd creates and switches to the issue branch for number in the
// repository containing dir, resolving to one StartWorkDoneMsg.
func StartWorkCmd(dir string, number int) tea.Cmd {
	return func() tea.Msg {
		branch, warning, err := startWork(dir, number)
		return StartWorkDoneMsg{Branch: branch, Warning: warning, Err: err}
	}
}

// startWork runs the flow synchronously; split out so tests drive it directly.
func startWork(dir string, number int) (branch, warning string, err error) {
	branch = branchPrefix(number)
	dirty, err := worktreeDirty(dir)
	if err != nil {
		return branch, "", err
	}
	if dirty {
		return branch, "", errors.New("working tree has uncommitted changes — commit or stash them before starting issue work")
	}
	if _, err := runGitQuick(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return branch, "", errors.New("branch " + branch + " already exists")
	}
	def, err := defaultBranch(dir)
	if err != nil {
		return branch, "", err
	}
	start := "origin/" + def
	if _, ferr := runGitTimeout(dir, gitFetchTimeout, "fetch", "origin", def); ferr != nil {
		// Offline (or no such remote branch): branch from the local default
		// branch instead of blocking, but say so.
		start = def
		warning = "fetch failed (" + ferr.Error() + ") — branching from local " + def
	}
	if _, err := runGitQuick(dir, "checkout", "-b", branch, start, "--no-track"); err != nil {
		return branch, "", err
	}
	return branch, warning, nil
}

// worktreeDirty reports whether the worktree carries uncommitted changes
// (staged, unstaged or untracked).
func worktreeDirty(dir string) (bool, error) {
	out, err := runGitQuick(dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// defaultBranch resolves the repository's default branch: origin/HEAD when
// the clone recorded it, else the first of main/master that exists locally.
func defaultBranch(dir string) (string, error) {
	if out, err := runGitQuick(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		ref := strings.TrimSpace(string(out))
		if name, ok := strings.CutPrefix(ref, "origin/"); ok && name != "" {
			return name, nil
		}
	}
	for _, name := range []string{"main", "master"} {
		if _, err := runGitQuick(dir, "rev-parse", "--verify", "refs/heads/"+name); err == nil {
			return name, nil
		}
	}
	return "", errors.New("cannot determine the default branch (no origin/HEAD, no main or master)")
}
