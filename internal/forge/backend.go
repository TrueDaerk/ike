package forge

// backend.go defines the Forge interface (#2083): the operation surface the
// 0470 stream builds on (listing, timeline, mutations, capabilities), with
// one binding per forge. Backend selection by remote host lives in detect.go;
// the bindings are gh.go (GitHub via the gh CLI) and tea.go (Gitea/Forgejo
// via the tea CLI's login + the Gitea REST API). Operations a binding has not
// implemented yet return an *ErrUnsupported, so later sub-issues only fill in
// bindings without touching the interface.

import (
	"errors"
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
	// IssuesAll lists open and closed issues together; both bindings accept
	// it verbatim (gh's --state all, Gitea's state=all).
	IssuesAll IssueState = "all"
)

// Capabilities reports what the authenticated user may do to the repository,
// split along the two permission tiers the stream's actions need, plus who
// that user is.
type Capabilities struct {
	// Triage: mutate labels, assignees and issue state (close/reopen).
	Triage bool
	// Push: write access — merge pull requests, edit foreign issue texts.
	Push bool
	// Login is the authenticated user's login, "" when the backend could not
	// resolve it. The edit gating (#2087) matches it against an issue's
	// author to decide whether the body is the user's own text; comments
	// carry their own TimelineEntry.Own flag.
	Login string
}

// Timeline event kinds (#2084): the neutral vocabulary both bindings map
// their forge's timeline onto. Events of any other kind are dropped by the
// parsers — the set is extensible by adding a constant and teaching the
// parsers and the renderer about it.
const (
	// TimelineComment is a comment with a markdown body.
	TimelineComment = "comment"
	// TimelineLabeled / TimelineUnlabeled are label additions and removals;
	// Body carries the label name, LabelColor its color.
	TimelineLabeled   = "labeled"
	TimelineUnlabeled = "unlabeled"
	// TimelineClosed / TimelineReopened are issue state changes.
	TimelineClosed   = "closed"
	TimelineReopened = "reopened"
	// TimelineAssigned / TimelineUnassigned are assignee changes; Body carries
	// the assignee's login.
	TimelineAssigned   = "assigned"
	TimelineUnassigned = "unassigned"
)

// TimelineEntry is one event of an issue's timeline (#2084) in the neutral
// vocabulary above. Body carries the comment's markdown (or the
// label/assignee name); ID is the forge-assigned comment identifier a later
// comment edit needs, and Own marks a comment authored by the authenticated
// user — both only set for comments.
type TimelineEntry struct {
	Kind       string
	Actor      string
	Time       time.Time
	Body       string
	LabelColor string // bare rrggbb of a labeled/unlabeled event's label
	ID         string
	Own        bool
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
	// Timeline fetches one page (1-based) of an issue's timeline, oldest
	// first, and reports whether more pages follow. Long histories are never
	// fetched whole — the pane asks for the next page on demand (#2084).
	Timeline(issue, page int) ([]TimelineEntry, bool, error)
	// CreateComment adds a comment to an issue.
	CreateComment(issue int, body string) error
	// EditComment replaces the body of an existing comment by its forge ID.
	EditComment(commentID string, body string) error
	// EditIssueBody replaces an issue's body text.
	EditIssueBody(issue int, body string) error
	// IssueBody reads an issue's current body text — the read half of the
	// stale-base check an edit runs before it overwrites (#2087).
	IssueBody(issue int) (string, error)
	// CommentBody reads a comment's current body text by its forge ID, the
	// same stale-base check for comments.
	CommentBody(commentID string) (string, error)
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
	// RepoLabels lists the repository's whole label set with its colors —
	// the label picker's rows, which are not limited to what the current
	// listing happens to carry (#2088).
	RepoLabels() ([]Label, error)
	// Collaborators lists the logins an issue can be assigned to — the
	// assignee picker's rows (#2088).
	Collaborators() ([]string, error)
	// PRDetail fetches one pull request in full (#2089): body, base branch,
	// per-check CI results, mergeability and the merge method a merge would
	// use.
	PRDetail(pr int) (PRDetail, error)
	// CommentPR adds a comment to a pull request.
	CommentPR(pr int, body string) error
	// MergePR merges an open pull request with the given merge method
	// ("merge", "squash", "rebase", …); "" lets the binding pick its default.
	MergePR(pr int, method string) error
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
// containing dir, resolving to one IssuesMsg.
func RefreshCmd(dir string) tea.Cmd { return RefreshStateCmd(dir, IssuesOpen) }

// RefreshStateCmd is RefreshCmd for an explicit issue state — the issues
// window's open/closed/all filter refetches through it (#2090). An
// unavailable backend (no forge CLI, no matching remote or login) resolves to
// the Setup state; a failing fetch to Err. A failing PR listing keeps the
// issues and drops only the PR states — the list is still useful without
// them. Pull requests are always fetched in every state and split client-side,
// so switching the issue state never re-costs the PR tab.
func RefreshStateCmd(dir string, state IssueState) tea.Cmd {
	if state == "" {
		state = IssuesOpen
	}
	return func() tea.Msg {
		f, setup := Detect(dir)
		if setup != "" {
			return IssuesMsg{State: state, Setup: setup}
		}
		issues, err := f.Issues(state)
		if err != nil {
			return IssuesMsg{State: state, Err: err}
		}
		msg := IssuesMsg{State: state, Issues: issues}
		if prs, err := f.PRs(); err == nil {
			msg.PRs = prs
		}
		return msg
	}
}

// RefreshFactory is the per-state fetch factory the issues window is injected
// with: the pane calls it with whatever its state filter selects, so the
// filter stays a pane concern and the pane stays subprocess-free.
func RefreshFactory(dir string) func(IssueState) tea.Cmd {
	return func(state IssueState) tea.Cmd { return RefreshStateCmd(dir, state) }
}

// TimelineMsg carries one fetched timeline page back into Update (#2084).
// Issue and Page echo the request so the pane can drop an answer for an issue
// it no longer shows; More reports whether another page follows.
type TimelineMsg struct {
	Issue   int
	Page    int
	Entries []TimelineEntry
	More    bool
	Err     error
}

// TimelineCmd fetches one page of an issue's timeline, resolving to one
// TimelineMsg. An unavailable backend resolves to Err — the pane only asks
// once a listing arrived, so the setup state was already shown there.
func TimelineCmd(dir string, issue, page int) tea.Cmd {
	if page < 1 {
		page = 1
	}
	return func() tea.Msg {
		f, setup := Detect(dir)
		if setup != "" {
			return TimelineMsg{Issue: issue, Page: page, Err: errors.New(setup)}
		}
		entries, more, err := f.Timeline(issue, page)
		if err != nil {
			return TimelineMsg{Issue: issue, Page: page, Err: err}
		}
		return TimelineMsg{Issue: issue, Page: page, Entries: entries, More: more}
	}
}

// TimelineFactory is the per-issue/page fetch factory the issues window is
// injected with, mirroring RefreshFactory.
func TimelineFactory(dir string) func(issue, page int) tea.Cmd {
	return func(issue, page int) tea.Cmd { return TimelineCmd(dir, issue, page) }
}
