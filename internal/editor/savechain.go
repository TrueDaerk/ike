package editor

import (
	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
)

// savechain.go defers a manual save behind the LSP pre-save steps (#1148):
// with editor.organize_imports_on_save and/or editor.format_on_save enabled
// and a capable server attached, the write waits until the bridge ran
// organize imports, then format (each edit applied in-buffer, each step
// time-boxed) and reported back with an ilsp.SaveChainDoneMsg. Only manual
// saves (:w, :wq, editor.write, editor.write_quit, editor.saveAll) chain;
// autosave (focus/idle), shutdown/switch writes ("write_raw") and backup
// writes stay raw by design — they must land synchronously and must never
// hinge on a language server.

// pendingSave is the parked manual save: the chain is running, the write
// happens in CompleteChainedSave. closeAfter carries the ":wq" intent.
type pendingSave struct{ closeAfter bool }

// beginSaveChain starts (or coalesces into) the save chain for this buffer.
// It returns nil when no chain applies — flags off, pathless or large-file
// buffer, no provider, no capable server — in which case the caller writes
// immediately; a non-nil command means the write is parked in m.pendingSave.
func (m *Model) beginSaveChain(closeAfter bool) tea.Cmd {
	if m.pendingSave != nil {
		// Re-entrancy (#1148): a second save while the chain runs coalesces —
		// the pending chain's completion writes the then-current buffer, so
		// nothing is lost and no second chain stacks up. ":wq" intent latches.
		m.pendingSave.closeAfter = m.pendingSave.closeAfter || closeAfter
		return func() tea.Msg { return nil }
	}
	if m.path == "" || m.largeFile {
		return nil // large-file mode has no synced document to act on (#149)
	}
	organize := m.saveChainFlag("editor.organize_imports_on_save")
	format := m.saveChainFlag("editor.format_on_save")
	if !organize && !format {
		return nil
	}
	cmd := ilsp.StartSaveChain(m.path, organize, format)
	if cmd == nil {
		return nil // no provider or no capable server: plain write, right now
	}
	m.pendingSave = &pendingSave{closeAfter: closeAfter}
	return cmd
}

// saveChainFlag reads one of the on-save toggles; unset means off, matching
// the config defaults.
func (m *Model) saveChainFlag(key string) bool {
	if m.cfg == nil {
		return false
	}
	v, ok := m.cfg.Get(key)
	return ok && v == "true"
}

// CompleteChainedSave performs the deferred write once the save chain
// finished (the app routes ilsp.SaveChainDoneMsg here). Views without a
// pending save no-op. The write goes through the raw guarded path: a conflict
// that appeared mid-chain (external change) still yields the prompt, and a
// latched ":wq" intent still closes the pane after a successful write.
func (m *Model) CompleteChainedSave() tea.Cmd {
	if m.pendingSave == nil {
		return nil
	}
	closeAfter := m.pendingSave.closeAfter
	m.pendingSave = nil
	cmd, ok := m.saveGuardedRaw(m.path, closeAfter)
	if cmd != nil {
		return cmd // conflict prompt; the save stays with the user
	}
	if ok && closeAfter {
		return func() tea.Msg { return CloseMsg{} }
	}
	return nil
}

// SavePending reports whether a chained save is waiting for its LSP steps —
// for tests and any UI that wants to reflect the in-flight save.
func (m *Model) SavePending() bool { return m.pendingSave != nil }
