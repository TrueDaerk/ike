package lsp

import (
	"sync"

	tea "charm.land/bubbletea/v2"
)

// savechain.go is the editor↔bridge seam for format/organize-imports on save
// (#1148). A manual save (:w, editor.write, Save All) asks StartSaveChain for
// a command running the enabled pre-save LSP steps; the editor defers its
// write until the chain reports back with a SaveChainDoneMsg. The provider is
// registered by the LSP plugin's bridge — package-level like the host's editor
// emitters, so internal/editor needs no plugin import.

var (
	saveChainMu sync.Mutex
	saveChainFn func(path string, organize, format bool) tea.Cmd
)

// SetSaveChain registers (or clears, with nil) the save-chain provider.
func SetSaveChain(f func(path string, organize, format bool) tea.Cmd) {
	saveChainMu.Lock()
	saveChainFn = f
	saveChainMu.Unlock()
}

// StartSaveChain returns the command running the pre-save LSP steps for path
// — organize imports, then format, each capability-gated and time-boxed — or
// nil when no provider is registered or no capable server tracks the file.
// A nil return means "write immediately"; a non-nil command obliges the
// caller to defer its write until the SaveChainDoneMsg for path arrives.
func StartSaveChain(path string, organize, format bool) tea.Cmd {
	saveChainMu.Lock()
	f := saveChainFn
	saveChainMu.Unlock()
	if f == nil {
		return nil
	}
	return f(path, organize, format)
}

// SaveChainDoneMsg reports a finished save chain for Path — every step ran,
// was skipped (no capability, empty answer) or timed out. The app routes it
// to the editors owning Path, which perform their deferred write; a slow or
// dead server therefore delays a save by at most the per-step timeouts and
// can never lose it.
type SaveChainDoneMsg struct{ Path string }
