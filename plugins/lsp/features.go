package lsp

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// features.go wires the #1912 protocol features through the bridge: code
// lenses and folding ranges as per-path coalesced decoration requests (the
// requestSemanticTokens pattern), selection ranges and willRenameFiles as
// seam providers for the editor and explorer, and the workspace/*/refresh
// server requests as re-requests over every open document.

// willRenameTimeout bounds the willRenameFiles round trip: a slow or dead
// server delays an explorer rename by at most this before the FS operation
// proceeds without edits (mirroring the save chain's per-step timeout).
const willRenameTimeout = 2 * time.Second

// selectionRangeTimeout bounds the selectionRange request behind an
// interactive extend-selection keypress.
const selectionRangeTimeout = 2 * time.Second

// --- config gates (all default on, so "unset" means enabled) ---

// codeLensEnabled reads the lsp.code_lens toggle (#1912).
func (b *bridge) codeLensEnabled() bool {
	v, ok := b.configGet("lsp.code_lens")
	return !ok || v != "false"
}

// foldingEnabled reads the lsp.folding toggle (#1912); the editor gates its
// fold merge on the same key, so off means no traffic and no stale merge.
func (b *bridge) foldingEnabled() bool {
	v, ok := b.configGet("lsp.folding")
	return !ok || v != "false"
}

// semanticTokensEnabled reads the lsp.semantic_tokens toggle (#1912); the
// editor gates rendering on the same key and keeps cached spans.
func (b *bridge) semanticTokensEnabled() bool {
	v, ok := b.configGet("lsp.semantic_tokens")
	return !ok || v != "false"
}

// selectionRangeEnabled reads the lsp.selection_range toggle (#1912); off
// makes the provider decline so extend/shrink selection uses the editor's
// Tree-sitter fallback.
func (b *bridge) selectionRangeEnabled() bool {
	v, ok := b.configGet("lsp.selection_range")
	return !ok || v != "false"
}

// willRenameEnabled reads the lsp.will_rename toggle (#1912); off makes the
// provider decline so explorer renames run synchronously as before.
func (b *bridge) willRenameEnabled() bool {
	v, ok := b.configGet("lsp.will_rename")
	return !ok || v != "false"
}

// --- code lenses ---

// requestCodeLenses refreshes the code lenses for path, coalesced per path
// like requestSemanticTokens: at most one request runs; changes during a run
// mark a pending re-request that fires when it lands. The reply feeds both
// the editor's lens annotations and the cached set the lsp.codeLens picker
// executes from. Errors stay silent — a passive decoration.
func (b *bridge) requestCodeLenses(path string) {
	mgr := b.manager()
	if mgr == nil || !b.codeLensEnabled() {
		return
	}
	b.mu.Lock()
	if b.lensInFlight == nil {
		b.lensInFlight = map[string]bool{}
		b.lensPending = map[string]bool{}
	}
	if b.lensInFlight[path] {
		b.lensPending[path] = true
		b.mu.Unlock()
		return
	}
	b.lensInFlight[path] = true
	b.mu.Unlock()

	go func() {
		for {
			lenses, err := mgr.CodeLenses(context.Background(), path)
			if err == nil {
				b.mu.Lock()
				if b.lensCache == nil {
					b.lensCache = map[string][]ilsp.CodeLens{}
				}
				b.lensCache[path] = lenses
				h := b.h
				b.mu.Unlock()
				if h != nil {
					h.Send(ilsp.CodeLensesMsg{Path: path, Lenses: lenses})
				}
			}
			b.mu.Lock()
			if b.lensPending[path] {
				b.lensPending[path] = false
				b.mu.Unlock()
				continue
			}
			b.lensInFlight[path] = false
			b.mu.Unlock()
			return
		}
	}()
}

// codeLensPick lists the lenses on the cursor line — or, when the line has
// none, every lens in the file — through the code-action picker, and executes
// the chosen one (lsp.codeLens).
func (b *bridge) codeLensPick(h host.API) tea.Cmd {
	b.ensure(h)
	path, line, _ := b.cur()
	if path == "" {
		return nil
	}
	b.mu.Lock()
	all := b.lensCache[path]
	b.mu.Unlock()
	if len(all) == 0 {
		h.Send(ilsp.ServerStatusMsg{Text: "no code lenses here", Kind: ilsp.ServerEventInfo})
		return nil
	}
	lenses := make([]ilsp.CodeLens, 0, len(all))
	for _, l := range all {
		if l.Line == line {
			lenses = append(lenses, l)
		}
	}
	if len(lenses) == 0 {
		lenses = all
	}
	choices := make([]ilsp.CodeActionChoice, len(lenses))
	for i, l := range lenses {
		choices[i] = ilsp.CodeActionChoice{Title: l.Title, Kind: "codelens"}
	}
	h.Send(ilsp.CodeActionsMsg{
		Path:    path,
		Actions: choices,
		Apply: func(i int) tea.Cmd {
			if i < 0 || i >= len(lenses) {
				return nil
			}
			return b.executeCodeLens(h, path, lenses[i])
		},
	})
	return nil
}

// executeCodeLens runs one lens: unresolved lenses (empty Command) go through
// codeLens/resolve first, then the command executes via workspace/executeCommand
// — its effects come back as workspace/applyEdit, exactly like a code action.
func (b *bridge) executeCodeLens(h host.API, path string, lens ilsp.CodeLens) tea.Cmd {
	mgr := b.manager()
	if mgr == nil {
		return nil
	}
	go func() {
		if lens.Command == "" {
			resolved, err := mgr.ResolveCodeLens(context.Background(), path, lens)
			if err != nil || resolved.Command == "" {
				h.Send(ilsp.ServerStatusMsg{Text: "'" + lens.Title + "' has no command", Kind: ilsp.ServerEventWarn})
				return
			}
			lens = resolved
		}
		cmd := protocol.Command{Title: lens.Title, Command: lens.Command, Arguments: lens.Arguments}
		if err := mgr.ExecuteCommand(context.Background(), path, cmd); err != nil {
			h.Send(ilsp.ServerStatusMsg{Text: "code lens failed: " + err.Error(), Kind: ilsp.ServerEventError})
			return
		}
		h.Send(ilsp.ServerStatusMsg{Text: "'" + lens.Title + "' executed", Kind: ilsp.ServerEventInfo})
	}()
	return nil
}

// --- folding ranges ---

// requestFoldingRanges refreshes the server folding ranges for path,
// coalesced per path like the other decorations. The editor merges the reply
// with its Tree-sitter folds; no capability means no ranges and the
// Tree-sitter fallback stands alone.
func (b *bridge) requestFoldingRanges(path string) {
	mgr := b.manager()
	if mgr == nil || !b.foldingEnabled() {
		return
	}
	b.mu.Lock()
	if b.foldInFlight == nil {
		b.foldInFlight = map[string]bool{}
		b.foldPending = map[string]bool{}
	}
	if b.foldInFlight[path] {
		b.foldPending[path] = true
		b.mu.Unlock()
		return
	}
	b.foldInFlight[path] = true
	b.mu.Unlock()

	go func() {
		for {
			folds, err := mgr.FoldingRanges(context.Background(), path)
			if err == nil && folds != nil && b.h != nil {
				b.h.Send(ilsp.FoldingRangesMsg{Path: path, Folds: folds})
			}
			b.mu.Lock()
			if b.foldPending[path] {
				b.foldPending[path] = false
				b.mu.Unlock()
				continue
			}
			b.foldInFlight[path] = false
			b.mu.Unlock()
			return
		}
	}()
}

// --- selection ranges (seam provider, editor-driven) ---

// selectionRangesCmd is the ilsp.SetSelectionRanges provider: it returns the
// command fetching the syntactic ladder at the cursor, or nil when the
// feature is off so the editor falls straight back to Tree-sitter. An error
// or unsupported server answers with an empty ladder — same fallback.
func (b *bridge) selectionRangesCmd(path string, line, col int) tea.Cmd {
	mgr := b.manager()
	if mgr == nil || !b.selectionRangeEnabled() {
		return nil
	}
	return func() tea.Msg {
		// Sync pending edits first so the ladder matches the visible text.
		b.flushChange(path)
		ctx, cancel := context.WithTimeout(context.Background(), selectionRangeTimeout)
		defer cancel()
		ranges, _ := mgr.SelectionRanges(ctx, path, line, col)
		return ilsp.SelectionRangesMsg{Path: path, Line: line, Col: col, Ranges: ranges}
	}
}

// --- willRenameFiles (seam provider, explorer-driven) ---

// willRenameCmd is the ilsp.SetWillRename provider: it returns the command
// running the workspace/willRenameFiles round trip and applying the returned
// edits, or nil when the feature is off or no manager runs (the explorer then
// renames synchronously). The command always yields the WillRenameDoneMsg —
// the FS operation may be delayed by at most the timeout, never lost.
func (b *bridge) willRenameCmd(req ilsp.WillRenameRequest) tea.Cmd {
	mgr := b.manager()
	if mgr == nil || !b.willRenameEnabled() {
		return nil
	}
	return func() tea.Msg {
		done := ilsp.WillRenameDoneMsg{Old: req.Old, New: req.New, IsDir: req.IsDir}
		// Sync every pending edit so the servers compute edits against
		// current text (a directory move can touch any open document).
		b.flushAllChanges()
		ctx, cancel := context.WithTimeout(context.Background(), willRenameTimeout)
		defer cancel()
		files, err := mgr.WillRenameFiles(ctx, req.Old, req.New)
		b.mu.Lock()
		h := b.h
		b.mu.Unlock()
		if err != nil || h == nil || len(files) == 0 {
			return done
		}
		n, derr := dispatchWorkspaceEdits(h, files)
		switch {
		case derr != nil:
			h.Send(ilsp.ServerStatusMsg{Text: "rename refactoring applied partially: " + derr.Error(), Kind: ilsp.ServerEventWarn})
		case n > 0:
			h.Send(ilsp.ServerStatusMsg{Text: "rename refactoring: " + applySummary(n), Kind: ilsp.ServerEventInfo})
		}
		return done
	}
}

// flushAllChanges drains every pending didChange immediately.
func (b *bridge) flushAllChanges() {
	b.mu.Lock()
	paths := make([]string, 0, len(b.pendingChange))
	for p := range b.pendingChange {
		paths = append(paths, p)
	}
	b.mu.Unlock()
	for _, p := range paths {
		b.flushChange(p)
	}
}

// --- workspace/*/refresh ---

// refreshMinInterval is the per-capability floor between two refresh rounds
// (#2193): a misbehaving server spamming workspace/*/refresh notifications
// used to drive the pending/in-flight re-request loop at wire speed (a
// docServer miss returns instantly, so per-path coalescing was no throttle at
// all). The first notification runs immediately; further ones inside the
// window coalesce into one trailing round.
const refreshMinInterval = time.Second

// onRefresh handles a server-initiated workspace/<kind>/refresh request
// (#1912) by re-requesting that decoration for every open document. It runs
// off the manager's dispatch goroutine. Rounds are rate-limited per kind
// (#2193): leading edge fires at once, refreshes arriving during the cooldown
// collapse into a single trailing round when it expires.
func (b *bridge) onRefresh(kind string) {
	b.mu.Lock()
	if b.refreshCooling == nil {
		b.refreshCooling = map[string]bool{}
		b.refreshPending = map[string]bool{}
	}
	if b.refreshCooling[kind] {
		b.refreshPending[kind] = true
		b.mu.Unlock()
		return
	}
	b.refreshCooling[kind] = true
	b.mu.Unlock()
	b.refreshRound(kind)
}

// refreshRound runs one re-request round for kind and arms the cooldown timer
// that either releases the rate limit or runs the coalesced trailing round.
func (b *bridge) refreshRound(kind string) {
	b.mu.Lock()
	paths := make([]string, 0, len(b.openDocs))
	for p := range b.openDocs {
		paths = append(paths, p)
	}
	b.mu.Unlock()
	for _, p := range paths {
		switch kind {
		case "codeLens":
			b.requestCodeLenses(p)
		case "semanticTokens":
			b.requestSemanticTokens(p)
		case "inlayHint":
			b.requestInlayHints(p)
		}
	}
	time.AfterFunc(refreshMinInterval, func() {
		b.mu.Lock()
		if !b.refreshPending[kind] {
			b.refreshCooling[kind] = false
			b.mu.Unlock()
			return
		}
		b.refreshPending[kind] = false
		b.mu.Unlock()
		b.refreshRound(kind)
	})
}
