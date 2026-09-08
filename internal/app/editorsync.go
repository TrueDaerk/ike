package app

import (
	"sync"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/pane"
	"ike/internal/textenc"
)

// editorsync.go is the settled-pass half of document-change syncs (#2541).
//
// An editor's Emit runs synchronously inside Update and cannot return a
// Cmd, so the shared-document sync (#142) used to leave the editor as a
// goroutine host.Send of an editor.SyncMsg — a message of its own, and so a
// second Update+View pass per keystroke whose frame the key's own pass had
// already drawn (the typing trace behind #2541 counted one per key). The
// emitter now pushes the sync onto this queue and app.Update drains it once
// the pass settled, applying it in the same pass through applyEditorSync.

// editorSyncQueue collects the syncs emitted during one Update pass. The
// emitter runs on the Update goroutine, but the queue is locked anyway: an
// editor driven off-loop (a test, a plugin) must not race the drain.
type editorSyncQueue struct {
	mu      sync.Mutex
	pending []editor.SyncMsg
}

// push queues one sync; a second sync for the same path and origin within
// the pass folds into the first — the handler reads the document state
// fresh when it applies, so the duplicate carried nothing.
func (q *editorSyncQueue) push(msg editor.SyncMsg) {
	q.mu.Lock()
	for _, p := range q.pending {
		if p.Path == msg.Path && p.FromKey == msg.FromKey {
			q.mu.Unlock()
			return
		}
	}
	q.pending = append(q.pending, msg)
	q.mu.Unlock()
}

// drain pops every queued sync in emit order.
func (q *editorSyncQueue) drain() []editor.SyncMsg {
	q.mu.Lock()
	out := q.pending
	q.pending = nil
	q.mu.Unlock()
	return out
}

// drainEditorSyncs applies the syncs the editors queued during this pass
// (#2541). A pass without an edit costs one locked length check.
func (m *Model) drainEditorSyncs() tea.Cmd {
	if m.editorSyncs == nil {
		return nil
	}
	syncs := m.editorSyncs.drain()
	if len(syncs) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	for _, msg := range syncs {
		if cmd := m.applyEditorSync(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// applyEditorSync applies one document-change sync (#142): the crash-recovery
// and idle-autosave debounces, the .http variable lint, the other views of
// the document and its markdown previews. Called from the settled pass for
// the syncs the editors queued during the Update (#2541) and from the
// editor.SyncMsg case for a sync fed through the loop.
func (m *Model) applyEditorSync(msg editor.SyncMsg) tea.Cmd {
	// Any buffer change also outdates the file's stored coverage (#2081):
	// the flag makes later opens of the file show the marks as stale
	// (each live view detects its own edits by document version).
	m.coverage.MarkStale(msg.Path)
	// A shared document changed in one pane (#142): every other view of the
	// same file re-clamps and mirrors the flags. Dirty/stale are read from
	// the originating pane *now* (not at emit time), so late or reordered
	// broadcasts always converge on the current document state.
	var skip *editor.Model
	if origin := m.activeWS().Panes.Get(msg.FromKey); origin != nil && origin.Kind() == pane.KindEditor {
		if ed := origin.EditorForPath(msg.Path); ed != nil {
			skip = ed
			msg.Dirty = ed.Dirty()
			msg.Stale = ed.Stale()
			msg.Large = ed.LargeFile()
			msg.Hash = ed.DiskHash()
			msg.EOL = textenc.LineEnding(ed.LineEnding())
			msg.Enc = textenc.Encoding(ed.EncodingName())
			msg.MixedEOL = ed.MixedEOL()
			msg.Vault, msg.VaultPass, msg.VaultLabel = ed.VaultState()
		}
	}
	var cmds []tea.Cmd
	// Crash-recovery write side (#167): the same seam drives the snapshot
	// debounce — dirty (re)arms it, clean cancels and drops the snapshot.
	if c := m.backupOnSync(msg.FromKey, msg.Path); c != nil {
		cmds = append(cmds, c)
	}
	// Idle autosave (#731) rides the same seam: dirty (re)arms the idle
	// deadline, clean cancels it.
	if c := m.autosaveIdleOnSync(msg.FromKey, msg.Path); c != nil {
		cmds = append(cmds, c)
	}
	// An edited .http buffer is re-linted for unknown {{variables}} once
	// it goes quiet (#2158); every other path is a cheap no-op.
	if c := m.httpVarsOnSync(msg.Path); c != nil {
		cmds = append(cmds, c)
	}
	// Deliver to every other view of the document — other panes and this
	// pane's background tabs alike; only the originating tab is skipped.
	for _, key := range m.editorKeysForPath(msg.Path) {
		if cmd := m.activeWS().Panes.Get(key).UpdateForPath(msg.Path, skip, msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// Markdown previews of the document re-render debounced off the same
	// seam (#62), pulling the text fresh from the originating editor.
	if previews := m.previewsForPath(msg.Path); len(previews) > 0 {
		src := skip
		if src == nil {
			if key := m.editorWithFile(msg.Path); key != "" {
				src = m.activeWS().Panes.Get(key).EditorForPath(msg.Path)
			}
		}
		if src != nil {
			text := src.Text()
			line, _ := src.CursorPos()
			for _, inst := range previews {
				if cmd := inst.Preview().SetSource(text); cmd != nil {
					cmds = append(cmds, cmd)
				}
				inst.Preview().SetCursorLine(line)
			}
		}
	}
	return tea.Batch(cmds...)
}
