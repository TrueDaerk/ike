package vcs

import (
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Update project + revert file (Roadmap 0320, #466), JetBrains' "Update
// Project" and "Rollback" scaled to the CLI: both async, both reporting
// through result messages the app turns into toasts and refreshes.

// UpdateDoneMsg reports a finished vcs.updateProject run.
type UpdateDoneMsg struct {
	UpToDate bool
	Commits  int // incoming commits applied
	Files    int // files touched by the update
	Err      error
}

// UpdateCmd fetches and integrates the upstream: strategy "rebase" runs
// `git pull --rebase`, anything else merges. Errors (no remote, auth,
// conflicts) surface via the decisive git line.
func UpdateCmd(root, strategy string) tea.Cmd {
	return func() tea.Msg {
		before, err := runGit(root, "rev-parse", "HEAD")
		if err != nil {
			return UpdateDoneMsg{Err: err}
		}
		pull := []string{"pull", "--no-rebase"}
		if strategy == "rebase" {
			pull = []string{"pull", "--rebase"}
		}
		if _, err := runGit(root, pull...); err != nil {
			return UpdateDoneMsg{Err: err}
		}
		old := strings.TrimSpace(string(before))
		now, err := runGit(root, "rev-parse", "HEAD")
		if err != nil {
			return UpdateDoneMsg{Err: err}
		}
		if strings.TrimSpace(string(now)) == old {
			return UpdateDoneMsg{UpToDate: true}
		}
		msg := UpdateDoneMsg{}
		if out, err := runGit(root, "rev-list", "--count", old+"..HEAD"); err == nil {
			msg.Commits, _ = strconv.Atoi(strings.TrimSpace(string(out)))
		}
		if out, err := runGit(root, "diff", "--name-only", old, "HEAD"); err == nil {
			if names := strings.TrimSpace(string(out)); names != "" {
				msg.Files = len(strings.Split(names, "\n"))
			}
		}
		return msg
	}
}

// RevertInfoMsg carries what a revert would discard, for the confirmation
// prompt: the number of buffer lines differing from HEAD on disk.
type RevertInfoMsg struct {
	Path    string
	Changed int
	Err     error
}

// RevertInfoCmd counts the on-disk changes of path against HEAD.
func RevertInfoCmd(root, path string) tea.Cmd {
	return func() tea.Msg {
		head, err := HeadContent(root, path)
		if err != nil {
			return RevertInfoMsg{Path: path, Err: err}
		}
		disk, err := os.ReadFile(path)
		if err != nil {
			return RevertInfoMsg{Path: path, Err: err}
		}
		return RevertInfoMsg{Path: path, Changed: len(LineMarks(head, string(disk)))}
	}
}

// RevertDoneMsg reports a finished vcs.revertFile run.
type RevertDoneMsg struct {
	Path string
	Err  error
}

// RevertCmd restores path (worktree and index) to its HEAD state.
func RevertCmd(root, path string) tea.Cmd {
	return func() tea.Msg {
		_, err := runGit(root, "checkout", "HEAD", "--", path)
		return RevertDoneMsg{Path: path, Err: err}
	}
}
