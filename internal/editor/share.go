package editor

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
)

// share.go implements shared documents (#142): two editor panes showing the
// same file are two *views* of one document — they alias the same
// *buffer.Buffer and *history.History (JetBrains/vim-split semantics), while
// cursor, scroll, mode, and registers stay per pane. Text is therefore never
// copied between panes; what needs propagating after an edit is the
// housekeeping around it, carried by SyncMsg.

// SyncMsg tells the other views of a shared document that it just changed (an
// edit, undo, save, or reload happened in one pane). The emitter adapter sends
// it with Path and FromKey only; the root model fills Dirty/Stale from the
// originating pane at delivery time (broadcasts travel through a goroutine, so
// emit-time flags could arrive out of order) and routes it to every editor
// pane showing Path except FromKey. Receivers clamp their cursor and scroll
// into the mutated buffer, mirror the flags, and re-run highlighting; the
// shared buffer already holds the new text.
type SyncMsg struct {
	Path    string
	FromKey string // pane key of the originating editor; opaque to the editor
	Dirty   bool
	Stale   bool
}

// ShareDocumentWith turns m into a second view of src's document: buffer and
// history are aliased (one text, one undo stack), flags and version copied,
// and this view's cursor starts at the top. Load is not called — the document
// is already in memory.
func (m *Model) ShareDocumentWith(src *Model) {
	m.path = src.path
	m.buf = src.buf
	m.hist = src.hist
	m.dirty = src.dirty
	m.stale = src.stale
	m.docVersion = src.docVersion
	m.cursor = buffer.Position{}
	m.desiredCol = 0
	m.mode = Normal
	m.pending.Reset()
	m.wait = awaitNone
	m.cmdline = ""
	m.searching = false
	m.hlIndex = highlight.Index{}
	m.semIndex = highlight.Index{}
	m.occurrences = nil
	m.inlayHints, m.hintsByLine = nil, nil
	m.scroll()
}

// SharesBufferWith reports whether both models view the same document.
func (m Model) SharesBufferWith(other *Model) bool { return m.buf == other.buf }

// applySync consumes one SyncMsg routed to this pane: the shared buffer was
// mutated elsewhere, so re-clamp the view into it, mirror the document flags,
// and reparse. It must not emit a change event — the originating pane already
// did, and echoing would ping-pong syncs between the views forever.
func (m Model) applySync(msg SyncMsg) (Model, tea.Cmd) {
	if !m.HasFile() || !samePath(m.path, msg.Path) {
		return m, nil
	}
	m.cursor = m.buf.ClampCursor(m.cursor)
	m.desiredCol = m.cursor.Col
	m.SetScroll(m.view.Top, m.view.Left) // re-clamp into the new line count
	m.scroll()
	m.dirty = msg.Dirty
	m.stale = msg.Stale
	m.docVersion++
	m.hlIndex = highlight.Index{}
	m.semIndex = highlight.Index{}
	m.occurrences = nil
	m.inlayHints, m.hintsByLine = nil, nil
	return m, m.parseCmd()
}
