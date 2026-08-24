package forge

// backend.go defines the Forge interface (#2083): the operation surface the
// 0470 stream builds on (listing, timeline, mutations, capabilities), with
// one binding per forge. Backend selection by remote host lives in detect.go;
// the bindings are gh.go (GitHub via the gh CLI) and tea.go (Gitea/Forgejo
// via the tea CLI's login + the Gitea REST API). Operations a binding has not
// implemented yet return an *ErrUnsupported, so later sub-issues only fill in
// bindings without touching the interface.

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// IssueState selects which issues a listing fetches.
type IssueState string

const (
	// IssuesOpen lists the open issues.
	IssuesOpen IssueState = "open"
	// IssuesClosed lists the closed issues.
	IssuesClosed IssueState = "closed"
)

// Capabilities reports what the authenticated user may do to the repository,
// split along the two permission tiers the stream's actions need.
type Capabilities struct {
	// Triage: mutate labels, assignees and issue state (close/reopen).
	Triage bool
	// Push: write access — merge pull requests.
	Push bool
}

// TimelineEntry is one event of an issue's timeline: a comment, a label or
// assignee change, a state change, a cross-reference. Kind names the event
// ("comment", "labeled", "closed", …) in the forge's own vocabulary; Body
// carries the comment text (or the label/assignee name); ID is the
// forge-assigned identifier a comment edit needs.
type TimelineEntry struct {
	Kind  string
	Actor string
	Time  time.Time
	Body  string
	ID    string
}

// Forge is one backend bound to a repository: every method operates on the
// repository the backend was detected for. Implementations never run from
// Update — callers wrap them in tea.Cmds (RefreshCmd is the listing one).
// Methods a binding has not implemented yet return an *ErrUnsupported.
type Forge interface {
	// Issues lists the repository's issues in the given state, newest first,
	// with labels and assignees.
	Issues(state IssueState) ([]Issue, error)
	// PRs lists the repository's pull requests in every state.
	PRs() ([]PR, error)
	// Timeline fetches one issue's timeline, oldest first.
	Timeline(issue int) ([]TimelineEntry, error)
	// CreateComment adds a comment to an issue.
	CreateComment(issue int, body string) error
	// EditComment replaces the body of an existing comment by its forge ID.
	EditComment(commentID string, body string) error
	// EditIssueBody replaces an issue's body text.
	EditIssueBody(issue int, body string) error
	// AddLabels attaches the named labels to an issue.
	AddLabels(issue int, labels []string) error
	// RemoveLabels detaches the named labels from an issue.
	RemoveLabels(issue int, labels []string) error
	// SetAssignees replaces an issue's assignee set.
	SetAssignees(issue int, assignees []string) error
	// CloseIssue closes an open issue.
	CloseIssue(issue int) error
	// ReopenIssue reopens a closed issue.
	ReopenIssue(issue int) error
	// MergePR merges an open pull request.
	MergePR(pr int) error
	// ClosePR closes an open pull request without merging.
	ClosePR(pr int) error
	// Capabilities reports the authenticated user's permissions on the
	// repository.
	Capabilities() (Capabilities, error)
}

// ErrUnsupported reports an operation a backend has not implemented (yet).
// Callers detect it with errors.As and degrade (hide the action, say why).
type ErrUnsupported struct {
	Backend string // "gh" or "tea"
	Op      string // the operation, e.g. "merge PR"
}

func (e *ErrUnsupported) Error() string {
	return e.Op + " is not supported by the " + e.Backend + " backend yet"
}

// unsupported keeps the stub methods to one line.
func unsupported(backend, op string) error {
	return &ErrUnsupported{Backend: backend, Op: op}
}

// RefreshCmd fetches the open issues and the pull requests of the repository
// containing dir, resolving to one IssuesMsg. An unavailable backend (no
// forge CLI, no matching remote or login) resolves to the Setup state; a
// failing fetch to Err. A failing PR listing keeps the issues and drops only
// the PR states — the list is still useful without them.
func RefreshCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		f, setup := Detect(dir)
		if setup != "" {
			return IssuesMsg{Setup: setup}
		}
		issues, err := f.Issues(IssuesOpen)
		if err != nil {
			return IssuesMsg{Err: err}
		}
		msg := IssuesMsg{Issues: issues}
		if prs, err := f.PRs(); err == nil {
			msg.PRs = prs
		}
		return msg
	}
}
