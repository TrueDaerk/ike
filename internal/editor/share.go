package editor

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/search"
	"ike/internal/highlight"
	"ike/internal/textenc"
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
	Large   bool   // large-file flag (#149), a document property like Dirty/Stale
	Hash    string // disk-content hash for persistent undo (#148), same lifecycle
	// On-disk line-ending flavor and encoding (#66), document properties like
	// Dirty/Stale — a file.setLineEndings/file.setEncoding conversion in one
	// view must save identically from every view.
	EOL      textenc.LineEnding
	Enc      textenc.Encoding
	MixedEOL bool
	// Vault document properties (#2293), same lifecycle as EOL/Enc — a
	// "Treat as Vault File" in one view must save encrypted from every view.
	Vault      bool
	VaultPass  string
	VaultLabel string
}

// ShareDocumentWith turns m into a second view of src's document: buffer and
// history are aliased (one text, one undo stack), flags and version copied,
// and this view's cursor starts at the top. Load is not called — the document
// is already in memory.
func (m *Model) ShareDocumentWith(src *Model) {
	// A separate view of the same document needs its own line cache — its cursor,
	// scroll and size differ, so it must never share cached bodies (#614/#142).
	m.lineCache = newLineCache()
	m.foldIdx = &foldIndex{}             // the collapsed set is per-view too (#144/#2187)
	m.stickyCache = &stickyStore{}       // pinned headers follow this view's top (#2187)
	m.testCache = newTestMarkStore()     // per-view cache like lineCache (#1150)
	m.tally = newSearchTally()           // per-view: keyed by this view's query (#2145)
	m.docPathCache = &docPathState{}     // keyed by the caret, so per-view too (#1660)
	m.conflictCache = newConflictStore() // per-view cache like testCache (#1149)
	m.renderEpoch++
	m.path = src.path
	m.langOverride = src.langOverride // a chosen type is a document property (#2033)
	m.buf = src.buf
	m.seedBreakpointLines()
	m.seedMarkLines()
	m.clearLocalMarks() // local marks are per-view state, like carets (#1151)
	m.hist = src.hist
	m.dirty = src.dirty
	m.stale = src.stale
	m.largeFile = src.largeFile
	m.diskHash = src.diskHash
	m.eol, m.enc, m.mixedEOL = src.eol, src.enc, src.mixedEOL
	m.vault, m.vaultPass, m.vaultLabel = src.vault, src.vaultPass, src.vaultLabel
	m.docVersion = src.docVersion
	m.cursor = buffer.Position{}
	m.desiredCol = 0
	m.carets = nil // carets are per-view state (#145)
	m.caretQuery = search.Query{}
	m.mode = Normal
	m.pending.Reset()
	m.wait = awaitNone
	m.cmdline = ""
	m.cmdCur = 0
	m.searching = false
	m.filtering = false
	m.logFilt, m.filtPrev = logFilter{}, logFilter{} // #2255: per-view filter
	// Highlighting is document-derived like the fold ranges, and the index is
	// immutable — the new view adopts the source's spans instead of starting
	// blank (#857): clearing here left drag/drop-opened views unhighlighted
	// until the next edit, because no share caller schedules a reparse and a
	// finished parse has no SpansMsg in flight to route over.
	m.hlIndex = src.hlIndex
	m.semIndex = src.semIndex
	m.hlVersion = src.hlVersion
	// Fold ranges are document-derived and travel with the share — the
	// server-provided set (#1912) included; the collapsed set is per-view
	// state (#144) and starts empty.
	m.folds = src.folds
	m.lspFolds = src.lspFolds
	m.folded = nil
	m.bumpFolds()
	m.foldLines = m.buf.LineCount()
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
	m.clampCarets()                      // this view's carets survive, snapped into the new text (#145)
	m.seedMarkLines()                    // the remote edit already adjusted the marks store (#1151)
	m.SetScroll(m.view.Top, m.view.Left) // re-clamp into the new line count
	m.scroll()
	m.dirty = msg.Dirty
	m.stale = msg.Stale
	m.largeFile = msg.Large
	m.diskHash = msg.Hash
	m.eol, m.enc, m.mixedEOL = msg.EOL, msg.Enc, msg.MixedEOL
	m.vault, m.vaultPass, m.vaultLabel = msg.Vault, msg.VaultPass, msg.VaultLabel
	m.docVersion++
	// This view's collapsed folds (#144) survive the remote edit where they
	// can: drop the ones out of range now, and let the reparse scheduled
	// below reconcile the rest against fresh fold ranges.
	m.foldLines = m.buf.LineCount()
	for h, e := range m.folded {
		if e >= m.foldLines {
			delete(m.folded, h)
			m.bumpFolds()
		}
	}
	m.hlIndex = highlight.Index{}
	m.conceal = nil
	m.semIndex = highlight.Index{}
	m.occurrences = nil
	m.inlayHints, m.hintsByLine = nil, nil
	return m, m.parseCmd()
}
