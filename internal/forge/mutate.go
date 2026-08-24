package forge

// mutate.go is the write side of the forge layer (#2088): the repository
// metadata a picker needs (labels, assignable users, the caller's
// capabilities), the neutral Mutation description of one write, and the
// tea.Cmds wrapping both. Like every other call in this package nothing here
// runs from Update — the pane hands a Mutation to the injected factory and
// gets a MutationMsg back.
//
// One Mutation can carry several changes at once; they are applied in the
// order the user means them: the comment first (so "close with a comment"
// posts before the state change and the timeline reads in the right order),
// then the label diff, then the assignee set, then the state.

import (
	"errors"

	tea "charm.land/bubbletea/v2"
)

// Mutation kinds, echoed by MutationMsg so the pane can word its error.
const (
	// MutateLabels is a label add/remove diff.
	MutateLabels = "labels"
	// MutateAssignees replaces an issue's assignee set.
	MutateAssignees = "assignees"
	// MutateState closes or reopens an issue, optionally with a comment.
	MutateState = "state"
)

// Mutation describes one write against an issue. The zero fields are skipped,
// so a caller only fills what it changes.
type Mutation struct {
	// Issue is the issue number every part of the mutation applies to.
	Issue int
	// Kind names what the user asked for; it only shapes the error wording.
	Kind string
	// AddLabels / RemoveLabels are the label diff a picker computed.
	AddLabels    []string
	RemoveLabels []string
	// Assignees is the new assignee set; it is only applied when SetAssignees
	// is set, so "no assignees" and "do not touch assignees" stay distinct.
	Assignees    []string
	SetAssignees bool
	// State is "closed" or "open"; "" leaves the issue's state alone.
	State string
	// Comment, when non-empty, is posted before everything else.
	Comment string
}

// MutationMsg reports one finished mutation back into Update. Issue and Kind
// echo the request so the pane can roll its optimistic state back for exactly
// the issue that failed.
type MutationMsg struct {
	Issue int
	Kind  string
	Err   error
}

// RepoMetaMsg carries the repository metadata the mutation UI needs: what the
// authenticated user may do, the repository's label set (the label picker's
// rows, with the forge's colors) and its assignable users (the assignee
// picker's rows). Labels and users are only fetched when the capabilities
// allow mutating at all — without triage the pickers never open.
type RepoMetaMsg struct {
	Caps   Capabilities
	Labels []Label
	Users  []string
	Err    error
}

// MutateCmd applies one Mutation, resolving to a MutationMsg. An unavailable
// backend resolves to Err — the pane only offers mutations once a listing
// arrived, so the setup state was already shown there.
func MutateCmd(dir string, mut Mutation) tea.Cmd {
	return func() tea.Msg {
		f, setup := Detect(dir)
		if setup != "" {
			return MutationMsg{Issue: mut.Issue, Kind: mut.Kind, Err: errors.New(setup)}
		}
		return MutationMsg{Issue: mut.Issue, Kind: mut.Kind, Err: applyMutation(f, mut)}
	}
}

// applyMutation runs one Mutation's parts against a backend, stopping at the
// first failure so the caller's rollback covers a half-applied write too.
func applyMutation(f Forge, mut Mutation) error {
	if mut.Comment != "" {
		if err := f.CreateComment(mut.Issue, mut.Comment); err != nil {
			return err
		}
	}
	if len(mut.RemoveLabels) > 0 {
		if err := f.RemoveLabels(mut.Issue, mut.RemoveLabels); err != nil {
			return err
		}
	}
	if len(mut.AddLabels) > 0 {
		if err := f.AddLabels(mut.Issue, mut.AddLabels); err != nil {
			return err
		}
	}
	if mut.SetAssignees {
		if err := f.SetAssignees(mut.Issue, mut.Assignees); err != nil {
			return err
		}
	}
	switch mut.State {
	case "closed":
		return f.CloseIssue(mut.Issue)
	case "open":
		return f.ReopenIssue(mut.Issue)
	}
	return nil
}

// MutateFactory is the mutation factory the issues window is injected with,
// mirroring RefreshFactory: the pane describes the write, the app runs it.
func MutateFactory(dir string) func(Mutation) tea.Cmd {
	return func(mut Mutation) tea.Cmd { return MutateCmd(dir, mut) }
}

// RepoMetaCmd probes the capabilities and — only when they allow mutating —
// the repository's labels and assignable users, resolving to one RepoMetaMsg.
// A failing label or user listing is not fatal: the capabilities still travel,
// and the pane falls back to the labels and assignees its listing carries.
func RepoMetaCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		f, setup := Detect(dir)
		if setup != "" {
			return RepoMetaMsg{Err: errors.New(setup)}
		}
		caps, err := f.Capabilities()
		if err != nil {
			return RepoMetaMsg{Err: err}
		}
		msg := RepoMetaMsg{Caps: caps}
		if !caps.Triage {
			return msg
		}
		if labels, err := f.RepoLabels(); err == nil {
			msg.Labels = labels
		} else {
			msg.Err = err
		}
		if users, err := f.Collaborators(); err == nil {
			msg.Users = users
		} else if msg.Err == nil {
			msg.Err = err
		}
		return msg
	}
}

// MetaFactory is the metadata factory the issues window is injected with; the
// pane runs it once per session (and again after a failure).
func MetaFactory(dir string) func() tea.Cmd {
	return func() tea.Cmd { return RepoMetaCmd(dir) }
}
