package vcs

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Branch picker + file-vs-HEAD diff support (Roadmap 0320, #467).

// Branch is one local branch.
type Branch struct {
	Name    string
	Current bool
}

// BranchesMsg carries the local branch list.
type BranchesMsg struct {
	Branches []Branch
	Err      error
}

// BranchesCmd lists the local branches, current first marked.
func BranchesCmd(root string) tea.Cmd {
	return func() tea.Msg {
		out, err := runGit(root, "branch", "--format=%(HEAD) %(refname:short)")
		if err != nil {
			return BranchesMsg{Err: err}
		}
		var branches []Branch
		for _, line := range strings.Split(string(out), "\n") {
			if len(line) < 2 {
				continue
			}
			name := strings.TrimSpace(line[1:])
			if name == "" {
				continue
			}
			branches = append(branches, Branch{Name: name, Current: line[0] == '*'})
		}
		return BranchesMsg{Branches: branches}
	}
}

// CheckoutDoneMsg reports a finished branch switch.
type CheckoutDoneMsg struct {
	Branch string
	Err    error
}

// CheckoutCmd switches to the named branch. Dirty-tree conflicts surface as
// git's own error ("would be overwritten by checkout").
func CheckoutCmd(root, name string) tea.Cmd {
	return func() tea.Msg {
		_, err := runGit(root, "checkout", name)
		return CheckoutDoneMsg{Branch: name, Err: err}
	}
}

// HeadDiffMsg carries the HEAD blob backing a file-vs-HEAD diff view.
type HeadDiffMsg struct {
	Path string
	Head string
	Err  error
}

// HeadDiffCmd loads the HEAD content of path for the diff viewer.
func HeadDiffCmd(root, path string) tea.Cmd {
	return func() tea.Msg {
		head, err := HeadContent(root, path)
		return HeadDiffMsg{Path: path, Head: head, Err: err}
	}
}
