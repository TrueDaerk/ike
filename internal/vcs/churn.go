package vcs

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/vcs/vcscore"
)

// churn.go is the tea.Cmd layer over vcscore.Churn (#2957): the Complexity
// window's hotspot columns fetch the per-file commit counts off the Update
// loop, like LogCmd.

// ChurnMsg carries one finished churn run: commits per root-relative slash
// path since Since, or the git error (outside a repository among others).
type ChurnMsg struct {
	Root   string
	Since  time.Time
	Counts map[string]int
	Err    error
}

// Churn counts the commits per root-relative path since since (see
// vcscore.Churn: renames are not followed).
func Churn(root string, since time.Time) (map[string]int, error) {
	return vcscore.Churn(root, since)
}

// ChurnCmd runs Churn inside a tea.Cmd.
func ChurnCmd(root string, since time.Time) tea.Cmd {
	return func() tea.Msg {
		counts, err := vcscore.Churn(root, since)
		return ChurnMsg{Root: root, Since: since, Counts: counts, Err: err}
	}
}
