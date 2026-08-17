package lsp

import (
	"sync"

	tea "charm.land/bubbletea/v2"
)

// selectionrange.go is the editor↔bridge seam for textDocument/selectionRange
// (#1912). The editor's extend-selection command asks RequestSelectionRanges
// for the syntactic ladder at the cursor; the reply arrives as a
// SelectionRangesMsg. The provider is registered by the LSP plugin's bridge —
// package-level like the save chain, so internal/editor needs no plugin
// import. A nil return (no provider, LSP disabled) tells the editor to use
// its Tree-sitter fallback directly; a provider that finds no server ranges
// answers with an empty ladder, which triggers the same fallback.

var (
	selRangeMu sync.Mutex
	selRangeFn func(path string, line, col int) tea.Cmd
)

// SetSelectionRanges registers (or clears, with nil) the selection-range
// provider.
func SetSelectionRanges(f func(path string, line, col int) tea.Cmd) {
	selRangeMu.Lock()
	selRangeFn = f
	selRangeMu.Unlock()
}

// RequestSelectionRanges returns the command fetching the selection ladder
// for the cursor position, or nil when no provider is registered.
func RequestSelectionRanges(path string, line, col int) tea.Cmd {
	selRangeMu.Lock()
	f := selRangeFn
	selRangeMu.Unlock()
	if f == nil {
		return nil
	}
	return f(path, line, col)
}
