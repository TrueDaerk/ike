package forge

// text.go is the editable-text half of the forge layer (#2087): the neutral
// target vocabulary a markdown edit buffer is bound to, the stale-base check
// that runs before an overwrite, and the tea.Cmds the app dispatches when
// such a buffer is saved. The three mutations themselves live on the Forge
// interface (EditIssueBody / EditComment / CreateComment) so each binding
// spells them in its own dialect; everything neutral is here.
//
// Package rule unchanged: nothing runs from Update — SaveTextCmd wraps the
// calls and resolves to one message. The permissions the edit gating reads
// come from #2088's one-shot repository-metadata probe (mutate.go), which
// already carries Capabilities.

import (
	"errors"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// TextKind names one editable forge text.
type TextKind int

const (
	// TextIssueBody is an issue's own body.
	TextIssueBody TextKind = iota
	// TextComment is an existing comment, addressed by its forge ID.
	TextComment
	// TextNewComment is a comment that does not exist yet: pushing it creates
	// one on the issue. It has no base text, so no stale check applies.
	TextNewComment
)

// TextTarget identifies one editable forge text. Issue is always set (a
// comment is edited in the context of the issue whose timeline shows it, and
// the refresh after a successful push needs the number); ID carries the forge
// comment ID for TextComment.
type TextTarget struct {
	Kind  TextKind
	Issue int
	ID    string
}

// Label is the human phrasing of the target, used in buffer names, toasts and
// the conflict dialog: "issue #12 body", "comment on #12", "new comment on
// #12".
func (t TextTarget) Label() string {
	n := "#" + strconv.Itoa(t.Issue)
	switch t.Kind {
	case TextComment:
		return "comment on " + n
	case TextNewComment:
		return "new comment on " + n
	default:
		return "issue " + n + " body"
	}
}

// Slug is the target's file-name fragment, so an edit buffer's tab names what
// it edits: "issue-12-body", "issue-12-comment-98", "issue-12-comment-new".
func (t TextTarget) Slug() string {
	base := "issue-" + strconv.Itoa(t.Issue)
	switch t.Kind {
	case TextComment:
		return base + "-comment-" + t.ID
	case TextNewComment:
		return base + "-comment-new"
	default:
		return base + "-body"
	}
}

// NormalizeText is the comparison and push form of a forge text: CRLF folded
// to LF and trailing blank space dropped. An editor writes its buffer with a
// trailing newline the forge never had, so comparing (and pushing) the raw
// text would report a conflict — or churn the forge — on every save that
// changed nothing.
func NormalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.TrimRight(s, " \t\n")
}

// FetchText reads the target's current text from the forge — the read half of
// the stale-base check. A new comment has no server text yet, so it reads as
// empty without a call.
func FetchText(f Forge, t TextTarget) (string, error) {
	switch t.Kind {
	case TextComment:
		return f.CommentBody(t.ID)
	case TextNewComment:
		return "", nil
	default:
		return f.IssueBody(t.Issue)
	}
}

// PushText writes body to the target through the binding's matching mutation.
func PushText(f Forge, t TextTarget, body string) error {
	switch t.Kind {
	case TextComment:
		return f.EditComment(t.ID, body)
	case TextNewComment:
		return f.CreateComment(t.Issue, body)
	default:
		return f.EditIssueBody(t.Issue, body)
	}
}

// SaveTextMsg reports one finished push attempt back into Update (#2087).
// Path echoes the edit buffer the attempt belongs to, so the app can find it
// again whatever happened in between.
//
// Exactly one of three outcomes holds: Err (the push failed or the backend is
// unavailable — the buffer keeps its text and the error is shown), Stale (the
// server text moved away from the base the buffer was opened with; Current
// carries what the forge has now, and nothing was written), or success.
type SaveTextMsg struct {
	Target  TextTarget
	Path    string
	Body    string
	Current string
	Stale   bool
	Err     error
}

// SaveTextCmd pushes body to the target's text in the repository containing
// dir, resolving to one SaveTextMsg.
//
// Unless force is set it first re-reads the server text and compares it
// against base — the text the buffer was prefilled with. A mismatch resolves
// to Stale without writing anything, so a concurrently changed text is never
// silently clobbered; the user then decides between overwriting (force) and
// reloading. A base check that cannot run (the read itself fails) is reported
// as an ordinary error rather than assumed safe.
func SaveTextCmd(dir, path string, target TextTarget, base, body string, force bool) tea.Cmd {
	return func() tea.Msg {
		f, setup := Detect(dir)
		if setup != "" {
			return SaveTextMsg{Target: target, Path: path, Body: body, Err: errors.New(setup)}
		}
		return saveText(f, path, target, base, body, force)
	}
}

// saveText is SaveTextCmd's backend-facing half, split off so the check-then-
// push order is testable against a stub Forge without a real repository.
func saveText(f Forge, path string, target TextTarget, base, body string, force bool) SaveTextMsg {
	msg := SaveTextMsg{Target: target, Path: path, Body: body}
	if !force && target.Kind != TextNewComment {
		current, err := FetchText(f, target)
		if err != nil {
			msg.Err = err
			return msg
		}
		if NormalizeText(current) != NormalizeText(base) {
			msg.Stale, msg.Current = true, current
			return msg
		}
	}
	if err := PushText(f, target, NormalizeText(body)); err != nil {
		msg.Err = err
	}
	return msg
}
