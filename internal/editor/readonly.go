package editor

import (
	"errors"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/highlight"
	"ike/internal/textenc"
)

// readonly.go holds the permanent read-only buffer (#1762): content shown in a
// full editor — motions, search, highlighting, folds — that can never be
// written back. The archive viewer uses it for an extracted entry, whose
// "path" names a member inside an archive file and therefore has no writable
// on-disk home.
//
// It is deliberately separate from the dependency-file guard (#565), which
// blocks the *first* edit and unlocks on confirmation: here there is nothing
// to unlock into, so mutations are refused outright and reported on the ex
// line, vim's `readonly` option rather than its `nomodifiable` prompt.

// errReadOnly is the write refusal surfaced by every save entry point.
var errReadOnly = errors.New("buffer is read-only")

// roMessage is the ex-line text for a refused mutation.
const roMessage = "E45: buffer is read-only"

// ShowReadOnly installs text as a permanently read-only buffer under path.
// path is a *display* path — for an archive entry, "<archive>!<entry>" — and
// is never written to: it exists so the tab title, the language sniff and the
// syntax highlighting resolve from the entry's own file name. Everything else
// resets exactly like NewFile.
func (m *Model) ShowReadOnly(path, text string) {
	m.path = path
	m.resolveEditorconfig()
	m.buf = buffer.FromString(text)
	m.sniffLanguage()
	m.seedBreakpointLines()
	m.seedMarkLines()
	m.clearLocalMarks()
	m.eol, m.enc, m.mixedEOL = textenc.LF, textenc.UTF8, false
	m.largeFile = m.limits().Exceeded(int64(len(text)), m.buf.LineCount())
	m.docBytes = int64(len(text))
	m.cursor = buffer.Position{}
	m.desiredCol = 0
	m.mode = Normal
	m.pending.Reset()
	m.wait = awaitNone
	m.cmdline = ""
	m.cmdMsg = ""
	m.searching = false
	m.dirty = false
	m.stale = false
	// Whatever the view held before is gone, a merged rotation set (#1996)
	// included; ShowMergedLog re-declares itself right after this call.
	m.mergedLog, m.followSrc, m.mergeWait = false, "", false
	// The dependency guard is about confirming an edit; a read-only buffer has
	// no edit to confirm, so it never applies here.
	m.depFile, m.depOK, m.depPending = false, false, nil
	m.hist = history.New()
	m.changes = changeList{}
	m.diskHash = "" // nothing on disk backs this content: no persistent undo
	m.docVersion++
	m.hlIndex = highlight.Index{}
	m.conceal = nil
	m.decodes = nil
	m.notes = nil
	m.scopes = nil
	m.resetFolds()
	m.semIndex = highlight.Index{}
	m.occurrences = nil
	m.inlayHints, m.hintsByLine = nil, nil
	m.readOnly = true
	// The whole buffer was just replaced outside the Update choke point, so
	// the line cache must not serve bodies of the previous content (#614) —
	// callers (archive viewer, jq playground #1970) install text directly.
	m.bumpRender()
	m.applyConfig()
	m.scroll()
}

// ReadOnly reports whether the buffer refuses edits and writes (#1762).
func (m Model) ReadOnly() bool { return m.readOnly }

// SetReadOnly locks or unlocks the buffer explicitly. Restoring a real file
// into the same view (Load, NewFile) clears the flag on its own.
func (m *Model) SetReadOnly(ro bool) { m.readOnly = ro }

// refuseRO reports whether a mutation must be refused, leaving the reason on
// the ex line. Every guarded edit path calls it before touching the buffer.
func (m *Model) refuseRO() bool {
	if !m.readOnly {
		return false
	}
	m.cmdMsg = roMessage
	return true
}
