package editor

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/watch"
)

// mergedlog.go is the editor seam for a merged rotated log set (#1996): the
// root model assembles the set's members into one chronological timeline and
// installs it here as a read-only buffer whose path names nothing on disk (the
// archive-entry shape of #1762). Everything the log stack does — severity
// colours, deltas, repeat folding, search, motions — applies to it as to any
// log buffer; the two things that need a seam are follow mode and re-merging.
//
// Follow mode (#1928) tails a *file*, and a merged buffer has none: it tails
// the set's newest member instead (followSrc) while the buffer keeps its
// virtual path. Appends stream in through the ordinary follow path — the
// newest member's lines are the buffer's last ones, so an append lands exactly
// where it belongs. A rotation replaces that member with a new file whose
// content belongs *after* the old one in the timeline, which no append can
// express: the view asks the root model for a fresh merge instead
// (MergeLogSetMsg) and parks its event handling until it arrives.

// MergeLogSetMsg asks the root model to re-merge the rotated log set behind a
// merged view and re-install it (#1996). Path is the view's virtual path, the
// one the root model resolves the set's stem from. Emitted when the followed
// newest member is truncated, removed or replaced — the wholesale-reload cases
// of follow mode, which for a merged timeline means re-reading the set.
type MergeLogSetMsg struct{ Path string }

// ShowMergedLog installs text as the merged timeline of a rotated log set:
// a permanently read-only buffer under the virtual path vpath (ShowReadOnly's
// contract), with follow mode anchored on src — the set's newest member — at
// byte offset off, term reporting whether that offset sits just past a line
// terminator. An empty src means the set offers nothing to tail (its newest
// member is compressed); follow mode then refuses rather than tailing a file
// whose bytes are not the buffer's.
//
// A view already following keeps following: this is how a re-merge after a
// rotation lands, so the auto-scroll resumes at the new end unless the user
// had paused it.
func (m *Model) ShowMergedLog(vpath, text, src string, off int64, term bool) {
	following, paused := m.follow, m.followPaused
	if src == "" {
		// Nothing to tail: keeping the badge on would promise a stream that
		// can never arrive.
		following = false
	}
	m.ShowReadOnly(vpath, text)
	m.mergedLog = true
	m.followSrc = src
	m.mergeWait = false
	m.followOffset, m.followTerm = off, term
	m.followRotated = false
	m.follow, m.followPaused = following, paused
	if following {
		m.followPrevRO = true // a merged buffer is read-only either way
		if !paused {
			m.followToEnd()
		}
	}
}

// MergedLog reports whether this view holds a merged rotation set (#1996).
func (m Model) MergedLog() bool { return m.mergedLog }

// FollowSource is the file follow mode tails for this view: the merged set's
// newest member, or "" for an ordinary buffer, which follows its own path.
// The root model routes watcher events for it through FollowExternalChange.
func (m Model) FollowSource() string { return m.followSrc }

// FollowExternalChange feeds one watcher event for the follow source into this
// view — the route the ordinary path match cannot take, the merged buffer's
// path being virtual (#1996). It returns the command the follow handling
// produced (a re-merge request, a re-parse) and nothing at all while the view
// is not following that file.
func (m *Model) FollowExternalChange(msg watch.EventMsg) tea.Cmd {
	if !m.follow || m.followSrc == "" || !samePath(m.followSrc, msg.Path) {
		return nil
	}
	nm, cmd := m.followHandleEvent(msg)
	*m = nm
	return cmd
}

// ReanchorFollow re-anchors follow mode on the current follow source at byte
// offset off without touching the buffer. It is the root model's fallback when
// a re-merge fails: skipping whatever was written meanwhile keeps the view
// following instead of stranding it on a request that never lands.
func (m *Model) ReanchorFollow(off int64, term bool) {
	m.mergeWait = false
	m.followOffset, m.followTerm = off, term
	m.followRotated = false
}
