package lsp

import (
	"context"
	"os"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/format"
	"ike/internal/host"
	"ike/internal/lang"
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
// whether any enabled step has a capable source for path — nil means "no
// chain, write immediately" — and coalesces re-entrant requests: a second
// save while path's chain is pending returns a no-op command, and the pending
// chain's SaveChainDoneMsg completes that save too. Organize-imports needs a
// capable server; the format step routes through the formatter registry
// (#1401), so any resolvable provider — server or not — qualifies.
func (b *bridge) saveChainCmd(req ilsp.SaveChainRequest) tea.Cmd {
	mgr := b.manager()
	b.mu.Lock()
	h := b.h
	b.mu.Unlock()
	path := req.Path
	if h == nil || path == "" {
		return nil
	}
	organize := req.Organize && mgr != nil && mgr.OrganizeImportsSupported(path)
	doFormat := req.Format && format.Has(langIDFor(path), path)
	if !organize && !doFormat {
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
	return func() tea.Msg {
		go b.runSaveChain(h, mgr, req, organize, doFormat)
		return nil
	}
}

// runSaveChain executes the chain off the Update goroutine and always ends
// with the SaveChainDoneMsg that releases the editor's deferred write.
func (b *bridge) runSaveChain(h host.API, mgr *manager.Manager, req ilsp.SaveChainRequest, organize, doFormat bool) {
	defer func() {
		b.mu.Lock()
		delete(b.saveChains, req.Path)
		b.mu.Unlock()
		h.Send(ilsp.SaveChainDoneMsg{Path: req.Path})
	}()
	if organize {
		b.organizeImportsStep(h, mgr, req.Path)
	}
	if doFormat {
		b.formatStep(h, mgr, req)
	}
}

// langIDFor resolves path's registered language id ("" when unknown).
func langIDFor(path string) string {
	if l, ok := lang.ByPath(path); ok {
		return l.ID
	}
	return ""
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

// formatStep resolves the buffer's formatter through the registry (#1401) and
// applies the winning provider's edits acked. Text-based providers read the
// freshest lines available: the manager's synced document when a server
// tracks the file (the organize step's edits are in there), else the buffer
// snapshot the editor sent with the request (no server means no organize step
// ran, so it cannot be stale).
func (b *bridge) formatStep(h host.API, mgr *manager.Manager, req ilsp.SaveChainRequest) {
	path := req.Path
	prov, ok := format.Resolve(langIDFor(path), path)
	if !ok {
		return
	}
	b.flushChange(path) // sync the organize-imports result before formatting
	lines := req.Lines
	if mgr != nil {
		if dl, tracked := mgr.DocLines(path); tracked {
			lines = dl
		}
	}
	root, _ := os.Getwd() // project root: external tools run with it as cwd (#1402)
	freq := format.Request{Path: path, Language: langIDFor(path), Lines: lines, Options: req.Options, Root: root}
	ctx, cancel := context.WithTimeout(context.Background(), saveChainStepTimeout)
	res, err := prov.Format(ctx, freq)
	cancel()
	if err != nil {
		return
	}
	edits := format.EditsForResult(lines, res)
	if len(edits) == 0 {
		return
	}
	b.applyEditsAcked(h, path, toFormatEdits(edits))
}

// toFormatEdits converts registry edits into the FormatEditsMsg shape.
func toFormatEdits(edits []format.Edit) []ilsp.FormatEdit {
	out := make([]ilsp.FormatEdit, len(edits))
	for i, e := range edits {
		out[i] = ilsp.FormatEdit{
			StartLine: e.StartLine, StartCol: e.StartCol,
			EndLine: e.EndLine, EndCol: e.EndCol,
			Text: e.Text,
		}
	}
	return out
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
