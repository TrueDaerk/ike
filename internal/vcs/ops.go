package vcs

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Mutating repository operations (Roadmap 0320): stage/unstage and commit,
// each an async tea.Cmd resolving to a result message. The app answers every
// completed operation with a status refresh, so consumers never mutate the
// snapshot themselves.

// OpDoneMsg reports a finished stage/unstage operation.
type OpDoneMsg struct {
	Op   string // "stage" / "unstage"
	Path string
	Err  error
}

// StageCmd stages one path (git add).
func StageCmd(root, path string) tea.Cmd {
	return func() tea.Msg {
		_, err := runGit(root, "add", "--", path)
		return OpDoneMsg{Op: "stage", Path: path, Err: err}
	}
}

// UnstageCmd removes one path from the index (git restore --staged).
func UnstageCmd(root, path string) tea.Cmd {
	return func() tea.Msg {
		_, err := runGit(root, "restore", "--staged", "--", path)
		return OpDoneMsg{Op: "unstage", Path: path, Err: err}
	}
}

// CommitDoneMsg reports a finished commit attempt: the new short hash and
// subject on success, the decisive git error (hook failure, empty index)
// otherwise.
type CommitDoneMsg struct {
	Hash    string
	Summary string
	Err     error
}

// CommitCmd commits the staged changes with message.
func CommitCmd(root, message string) tea.Cmd {
	return func() tea.Msg {
		if _, err := runGit(root, "commit", "-m", message); err != nil {
			return CommitDoneMsg{Err: err}
		}
		hash, _ := runGit(root, "rev-parse", "--short", "HEAD")
		subject, _ := runGit(root, "log", "-1", "--pretty=%s")
		return CommitDoneMsg{
			Hash:    strings.TrimSpace(string(hash)),
			Summary: strings.TrimSpace(string(subject)),
		}
	}
}
