package editor

import (
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"ike/internal/highlight"
	"ike/internal/textenc"
	"ike/internal/undostore"
	"ike/internal/watch"
)

// reload.go handles external file changes (Roadmap 0140). A clean buffer whose
// file changed on disk reloads in place, preserving cursor and scroll (clamped
// like session restore). A dirty buffer is never silently reloaded: it is
// marked stale (tab + status indicator), and the next save is intercepted by
// the conflict guard — a ConflictMsg asks the root model to prompt keep mine /
// reload / cancel.

// handleExternalChange consumes one watcher event routed to this editor by the
// root model. Only content changes of the open file are acted on; a rename-in-
// place save (write temp + rename) coalesces to FileCreated, so both kinds
// count as "changed".
func (m Model) handleExternalChange(msg watch.EventMsg) (Model, tea.Cmd) {
	if !m.HasFile() || !samePath(m.path, msg.Path) {
		return m, nil
	}
	if msg.Kind == watch.FileRemoved {
		// Externally deleted while dirty (the root model closes clean editors
		// before routing here): the buffer is the only copy left — keep it,
		// marked stale so the next save goes through the conflict prompt.
		if m.dirty {
			m.stale = true
		}
		return m, nil
	}
	if msg.Kind != watch.FileChanged && msg.Kind != watch.FileCreated {
		return m, nil
	}
	if m.dirty {
		m.stale = true // never silently reload unsaved edits; guard the next save
		return m, nil
	}
	if !m.autoReload() {
		return m, nil
	}
	return m.reloadFromDisk()
}

// autoReload reads files.auto_reload: "clean" (the default) reloads clean
// buffers in place, "never" leaves stale content until the file is reopened.
func (m Model) autoReload() bool {
	if m.cfg != nil {
		if v, ok := m.cfg.Get("files.auto_reload"); ok {
			return v != "never"
		}
	}
	return true
}

// reloadFromDisk re-reads the open file into a fresh buffer, restoring cursor
// and viewport clamped to the new content (the session-restore pattern). Undo
// history restarts: the old edits describe positions in text that no longer
// exists, so replaying them could corrupt the reloaded content — losing the
// stack is the documented trade-off. The change event bumps docVersion and
// carries the new text, so highlighting and LSP re-sync exactly as after an
// edit.
func (m Model) reloadFromDisk() (Model, tea.Cmd) {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return m, nil // e.g. changed then removed within one debounce window
	}
	text, info, err := textenc.Decode(data, m.fallbackEncoding())
	if err != nil {
		// The rewritten file is no longer decodable (#66): keep the buffer as
		// is rather than replacing it with mojibake; the next open reports the
		// error properly.
		return m, nil
	}
	line, col := m.cursor.Line, m.cursor.Col
	top, left := m.view.Top, m.view.Left
	// Mutate the document in place (not fresh pointers): other panes sharing
	// it (#142) must see the reloaded text and the cleared undo stack too.
	m.buf.ReplaceAll(text)
	m.eol, m.enc, m.mixedEOL = info.EOL, info.Encoding, info.MixedEOL
	if eol, ok := m.editorconfigEOL(); ok {
		m.eol = eol // end_of_line keeps applying across external reloads (#63)
	}
	m.largeFile = m.limits().Exceeded(int64(len(data)), m.buf.LineCount())
	m.hist.Reset()
	m.diskHash = "" // re-keyed below unless large-file mode opts out (#148)
	if !m.largeFile {
		m.diskHash = undostore.Hash(data)
	}
	m.dirty = false
	m.stale = false
	m.hlIndex = highlight.Index{}
	m.semIndex = highlight.Index{}
	m.occurrences = nil
	m.inlayHints, m.hintsByLine = nil, nil
	m.SetCursor(line, col)
	m.clampCarets() // carets clamp into the reloaded text like the cursor (#145)
	m.SetScroll(top, left)
	m.emit(EventChange)
	return m, m.parseCmd()
}

// ConflictMsg asks the root model to open the save-conflict prompt: the user
// tried to save a stale buffer (its file changed externally since the edits).
type ConflictMsg struct{ Path string }

// conflictCmd produces the ConflictMsg for the current buffer.
func (m Model) conflictCmd() tea.Cmd {
	path := m.path
	return func() tea.Msg { return ConflictMsg{Path: path} }
}

// saveGuarded is the conflict-guarded save every save entry point (":w",
// editor.write, save-all) goes through: writing target over a stale buffer's
// own file would clobber the external change, so it yields the prompt instead.
// Saving to a different path (":w other") is not a conflict.
// It reports ok=false when nothing was written — a pending conflict, a write
// error, or an untitled buffer (whose save turns into the save-as prompt) —
// so ":wq" knows not to close; closeAfter carries the ":wq" intent into that
// prompt (#730). The outcome lands on the ex line (#261): `"file" written`
// on success, vim-style "E: …" on failure.
func (m *Model) saveGuarded(target string, closeAfter bool) (tea.Cmd, bool) {
	if m.stale && samePath(target, m.path) {
		return m.conflictCmd(), false
	}
	if target == "" && m.path == "" {
		// Untitled buffer (#730): ask the app to prompt for a path instead
		// of failing with "no file name".
		return func() tea.Msg { return SaveAsPromptMsg{CloseAfter: closeAfter} }, false
	}
	if err := m.saveAs(target); err != nil {
		m.cmdMsg = "E: " + err.Error()
		return nil, false
	}
	m.cmdMsg = `"` + filepath.Base(m.path) + `" written`
	return nil, true
}

// SaveTo writes the buffer to path and binds the editor to it — the accept
// side of the untitled save-as prompt (#730). It goes through the normal
// saveAs path (EventSave, undo checkpoint, editorconfig re-resolve) and
// reports the outcome on the ex line like any save.
func (m *Model) SaveTo(path string) error {
	if err := m.saveAs(path); err != nil {
		m.cmdMsg = "E: " + err.Error()
		return err
	}
	m.cmdMsg = `"` + filepath.Base(m.path) + `" written`
	return nil
}

// Autosave writes the buffer when focus leaves the pane or its document is
// about to be replaced (editor.auto_save = "focus", #174). It goes through the
// normal save path, so EventSave fires (watcher suppression, LSP didSave,
// shared-view sync) and undo history is untouched. A stale buffer is skipped —
// auto-save must never clobber an external change; the conflict guard handles
// the next explicit save. It reports whether a write happened.
func (m *Model) Autosave() bool {
	if !m.dirty || m.stale || m.path == "" {
		return false
	}
	return m.saveAs(m.path) == nil
}

// ResolveConflictKeepMine resolves the save conflict by overwriting the
// external change with the buffer. The save emits EventSave, which stamps the
// watcher's save epoch, so the overwrite does not echo back as a new external
// change.
func (m *Model) ResolveConflictKeepMine() {
	m.stale = false
	if err := m.save(); err != nil {
		m.cmdMsg = "E: " + err.Error()
	}
}

// ResolveConflictReload resolves the save conflict by discarding the buffer's
// edits in favour of the on-disk content (the clean-reload path). Local
// history (#35), once it lands, snapshots the buffer here before the discard.
func (m *Model) ResolveConflictReload() tea.Cmd {
	nm, cmd := m.reloadFromDisk()
	*m = nm
	return cmd
}

// samePath reports whether two paths name the same file, tolerating the
// watcher's absolute paths against the editor's as-opened (possibly relative)
// path.
func samePath(a, b string) bool {
	if a == b {
		return true
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	return errA == nil && errB == nil && absA == absB
}
