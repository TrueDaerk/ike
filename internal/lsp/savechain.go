package lsp

import (
	"sync"

	tea "charm.land/bubbletea/v2"

	"ike/internal/format"
)

// savechain.go is the editor↔bridge seam for format/organize-imports on save
// (#1148). A manual save (:w, editor.write, Save All) asks StartSaveChain for
// a command running the enabled pre-save LSP steps; the editor defers its
// write until the chain reports back with a SaveChainDoneMsg. The provider is
// registered by the LSP plugin's bridge — package-level like the host's editor
// emitters, so internal/editor needs no plugin import.

// SaveChainRequest carries one pre-save chain invocation: which steps the
// config enables, plus the buffer snapshot and effective per-buffer options
// the format step hands to text-based formatter providers (Roadmap 0470 —
// format-on-save routes through the formatter registry, so it needs what the
// registry's Request needs).
type SaveChainRequest struct {
	Path     string
	Organize bool
	Format   bool
	Lines    []string
	Options  format.Options
}

var (
	saveChainMu sync.Mutex
	saveChainFn func(req SaveChainRequest) tea.Cmd
)

// SetSaveChain registers (or clears, with nil) the save-chain provider.
func SetSaveChain(f func(req SaveChainRequest) tea.Cmd) {
	saveChainMu.Lock()
	saveChainFn = f
	saveChainMu.Unlock()
}

// StartSaveChain returns the command running the pre-save steps for the
// request — organize imports, then format, each capability-gated and
// time-boxed — or nil when no provider is registered or no step applies.
// A nil return means "write immediately"; a non-nil command obliges the
// caller to defer its write until the SaveChainDoneMsg for path arrives.
func StartSaveChain(req SaveChainRequest) tea.Cmd {
	saveChainMu.Lock()
	f := saveChainFn
	saveChainMu.Unlock()
	if f == nil {
		return nil
	}
	return f(req)
}

// SaveChainDoneMsg reports a finished save chain for Path — every step ran,
// was skipped (no capability, empty answer) or timed out. The app routes it
// to the editors owning Path, which perform their deferred write; a slow or
// dead server therefore delays a save by at most the per-step timeouts and
// can never lose it.
type SaveChainDoneMsg struct{ Path string }
