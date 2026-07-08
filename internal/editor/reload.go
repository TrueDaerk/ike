package editor

import (
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/highlight"
	"ike/internal/watch"
)

// reload.go is the clean-buffer half of Roadmap 0140 (external file changes):
// when the watcher reports that the open file changed on disk and the buffer
// has no unsaved edits, the editor reloads it in place, preserving cursor and
// scroll (clamped like session restore). Dirty buffers are never touched here —
// stale marking and the save conflict guard are the dirty-buffer half (#82).

// handleExternalChange consumes one watcher event routed to this editor by the
// root model. Only content changes of the open file are acted on; a rename-in-
// place save (write temp + rename) coalesces to FileCreated, so both kinds
// count as "changed".
func (m Model) handleExternalChange(msg watch.EventMsg) (Model, tea.Cmd) {
	if msg.Kind != watch.FileChanged && msg.Kind != watch.FileCreated {
		return m, nil
	}
	if !m.HasFile() || !samePath(m.path, msg.Path) {
		return m, nil
	}
	if m.dirty {
		return m, nil // never silently reload unsaved edits (#82 marks stale)
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
	line, col := m.cursor.Line, m.cursor.Col
	top, left := m.view.Top, m.view.Left
	m.buf = buffer.FromString(string(data))
	m.hist = history.New()
	m.dirty = false
	m.hlIndex = highlight.Index{}
	m.SetCursor(line, col)
	m.SetScroll(top, left)
	m.emit(EventChange)
	return m, m.parseCmd()
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
