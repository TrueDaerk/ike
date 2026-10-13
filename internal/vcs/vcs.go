// Package vcs owns all Git interaction for IKE (Roadmap 0320). It shells out
// to the git CLI — never from Update: every call runs inside a tea.Cmd with a
// timeout. The package produces immutable Snapshots of the repository state
// (branch, ahead/behind, per-file status) that consumers (explorer coloring,
// status line, commit UI) read; consumers never run git themselves.
package vcs

import (
	tea "charm.land/bubbletea/v2"
)

// SnapshotMsg carries a refreshed snapshot back into Update. A nil Snap means
// the workspace is not a git repository (or git is unavailable).
type SnapshotMsg struct {
	Snap *Snapshot
}

// Refresh returns a command that loads a fresh snapshot for the workspace at
// dir. It never fails loudly: any error (no repo, no git binary, timeout)
// resolves to SnapshotMsg{Snap: nil} so non-git workspaces stay quiet.
func Refresh(dir string) tea.Cmd {
	return func() tea.Msg {
		snap, err := Load(dir)
		if err != nil {
			return SnapshotMsg{}
		}
		return SnapshotMsg{Snap: snap}
	}
}
