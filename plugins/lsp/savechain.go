package lsp

import (
	"context"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/manager"
	"ike/internal/lsp/protocol"
)

// savechain.go runs the pre-save LSP steps for format/organize-imports on
// save (#1148). A manual editor save calls ilsp.StartSaveChain (the seam
// registered in ensure); the returned command spawns a goroutine that runs,
// in order and each time-boxed by saveChainStepTimeout:
//
//  1. organize imports — the source.organizeImports code action, requested
//     with Context.Only and applied without the picker,
//  2. format — whole-document formatting with the editor's indent settings,
//
// waiting after each edit delivery until the editor applied it (the
// FormatEditsMsg.Applied ack), so the next step's request reads the updated
// text. The chain always finishes with a SaveChainDoneMsg — every failure,
// empty answer or timeout falls through to the next step, so a slow or dead
// server delays the save but can never block or lose it. Only user-initiated
// saves run the chain; autosave and shutdown writes stay raw (editor side).

// saveChainStepTimeout bounds each chain step — the server request and the
// wait for the editor to apply the delivered edits. Deliberately shorter than
// the manager's requestTimeout: a save should feel prompt even when a server
// hangs, and the contexts passed down cap the manager's own timeout too.
var saveChainStepTimeout = 2 * time.Second

// saveChainCmd is the ilsp.SetSaveChain provider: it decides synchronously
// whether any enabled step has a capable ready server for path — nil means
// "no chain, write immediately" — and coalesces re-entrant requests: a second
// save while path's chain is pending returns a no-op command, and the pending
// chain's SaveChainDoneMsg completes that save too.
func (b *bridge) saveChainCmd(path string, organize, format bool) tea.Cmd {
	mgr := b.manager()
	b.mu.Lock()
	h := b.h
	b.mu.Unlock()
	if mgr == nil || h == nil || path == "" {
		return nil
	}
	organize = organize && mgr.OrganizeImportsSupported(path)
	format = format && mgr.FormatSupported(path)
	if !organize && !format {
		return nil
	}
	b.mu.Lock()
	if b.saveChains == nil {
		b.saveChains = map[string]bool{}
	}
	if b.saveChains[path] {
		b.mu.Unlock()
		return func() tea.Msg { return nil } // coalesced into the pending chain
	}
	b.saveChains[path] = true
	b.mu.Unlock()
	opts := formattingOptions(h)
	return func() tea.Msg {
		go b.runSaveChain(h, mgr, path, organize, format, opts)
		return nil
	}
}

// runSaveChain executes the chain off the Update goroutine and always ends
// with the SaveChainDoneMsg that releases the editor's deferred write.
func (b *bridge) runSaveChain(h host.API, mgr *manager.Manager, path string, organize, format bool, opts protocol.FormattingOptions) {
	defer func() {
		b.mu.Lock()
		delete(b.saveChains, path)
		b.mu.Unlock()
		h.Send(ilsp.SaveChainDoneMsg{Path: path})
	}()
	if organize {
		b.organizeImportsStep(h, mgr, path)
	}
	if format {
		b.formatStep(h, mgr, path, opts)
	}
}

// organizeImportsStep requests the source.organizeImports actions and applies
// the first matching one without the picker. Errors and empty answers fall
// through silently — on a save, "nothing to organize" is the common case.
func (b *bridge) organizeImportsStep(h host.API, mgr *manager.Manager, path string) {
	b.flushChange(path) // the server must hold the latest text (#595)
	ctx, cancel := context.WithTimeout(context.Background(), saveChainStepTimeout)
	actions, err := mgr.CodeActionsByKind(ctx, path, protocol.KindSourceOrganizeImports)
	cancel()
	if err != nil {
		return
	}
	action, ok := pickActionByKind(actions, protocol.KindSourceOrganizeImports)
	if !ok {
		return
	}
	if action.Edit != nil {
		// Inline edit first (per spec): the open buffer's portion applies
		// acked so the next step sees it; other files go the standard route.
		var rest []manager.FileEdits
		for _, f := range mgr.ConvertWorkspaceEdit(path, *action.Edit) {
			if f.Open && f.Path == path {
				b.applyEditsAcked(h, path, f.Edits)
				continue
			}
			rest = append(rest, f)
		}
		if len(rest) > 0 {
			_, _ = dispatchWorkspaceEdits(h, rest)
		}
	}
	if action.Command != nil {
		ctx, cancel := context.WithTimeout(context.Background(), saveChainStepTimeout)
		_ = mgr.ExecuteCommand(ctx, path, *action.Command)
		cancel()
		if action.Edit == nil {
			// A command-only action's edits arrive asynchronously as
			// workspace/applyEdit — there is no ack to wait on, so give them
			// a short beat to land before the next step reads the buffer.
			time.Sleep(150 * time.Millisecond)
		}
	}
}

// formatStep requests whole-document formatting and applies the edits acked.
func (b *bridge) formatStep(h host.API, mgr *manager.Manager, path string, opts protocol.FormattingOptions) {
	b.flushChange(path) // sync the organize-imports result before formatting
	ctx, cancel := context.WithTimeout(context.Background(), saveChainStepTimeout)
	edits, err := mgr.Format(ctx, path, opts)
	cancel()
	if err != nil || len(edits) == 0 {
		return
	}
	b.applyEditsAcked(h, path, edits)
}

// applyEditsAcked delivers edits for path and blocks until the app applied
// them (FormatEditsMsg.Applied) or the step timeout expires — the chain's
// edit-applied signal keeping the steps ordered.
func (b *bridge) applyEditsAcked(h host.API, path string, edits []ilsp.FormatEdit) {
	if len(edits) == 0 {
		return
	}
	done := make(chan struct{})
	var once sync.Once
	h.Send(ilsp.FormatEditsMsg{Path: path, Edits: edits, Applied: func() {
		once.Do(func() { close(done) })
	}})
	select {
	case <-done:
	case <-time.After(saveChainStepTimeout):
	}
}

// pickActionByKind selects the action the save chain applies: the first one
// whose kind matches (hierarchically) wins; when the server answered the
// Only-filtered request with kindless entries (bare commands), the first
// entry counts as the match.
func pickActionByKind(actions []protocol.CodeAction, kind string) (protocol.CodeAction, bool) {
	for _, a := range actions {
		if a.Kind == kind || (a.Kind != "" && len(a.Kind) > len(kind) && a.Kind[:len(kind)+1] == kind+".") {
			return a, true
		}
	}
	for _, a := range actions {
		if a.Kind == "" {
			return a, true
		}
	}
	return protocol.CodeAction{}, false
}
