package lsp

import (
	"sync"

	tea "charm.land/bubbletea/v2"
)

// willrename.go is the explorer↔bridge seam for workspace/willRenameFiles
// (#1912). Before the explorer performs an FS rename or move it asks
// StartWillRename for a command running the server round trip; the explorer
// defers the os.Rename until the chain reports back with a WillRenameDoneMsg.
// The provider is registered by the LSP plugin's bridge — package-level like
// the save chain, so internal/explorer needs no plugin import.

// WillRenameRequest carries one pending rename/move: absolute old and new
// paths plus whether the entry is a directory.
type WillRenameRequest struct {
	Old   string
	New   string
	IsDir bool
}

var (
	willRenameMu sync.Mutex
	willRenameFn func(req WillRenameRequest) tea.Cmd
)

// SetWillRename registers (or clears, with nil) the willRenameFiles provider.
func SetWillRename(f func(req WillRenameRequest) tea.Cmd) {
	willRenameMu.Lock()
	willRenameFn = f
	willRenameMu.Unlock()
}

// StartWillRename returns the command asking the language servers for the
// refactoring edits a rename implies (workspace/willRenameFiles) and applying
// the returned workspace edit — or nil when no provider is registered or the
// feature is disabled. A nil return means "rename immediately"; a non-nil
// command obliges the caller to defer the FS operation until the
// WillRenameDoneMsg for the same request arrives. The provider is time-boxed,
// so a slow or dead server delays the rename by at most the timeout and can
// never lose it.
func StartWillRename(req WillRenameRequest) tea.Cmd {
	willRenameMu.Lock()
	f := willRenameFn
	willRenameMu.Unlock()
	if f == nil {
		return nil
	}
	return f(req)
}

// WillRenameDoneMsg reports a finished willRenameFiles round trip: any server
// edits were applied (or the request timed out / no server cared). The app
// routes it to the explorer, which performs the deferred FS rename.
type WillRenameDoneMsg struct {
	Old   string
	New   string
	IsDir bool
}
