package lsp

import (
	"context"

	"ike/internal/editor/buffer"
	"ike/internal/intention"
)

// renamegate.go position-gates the intention popup's "Rename Symbol" entry
// (#2025). #2020 offered the entry on the server *capability* alone, so in a
// Markdown buffer it showed up in every paragraph and picking it ended in the
// "cannot rename here" toast — the position check (`prepareRename`) only ran
// after the pick.
//
// The check cannot move into the provider itself: `intention.Provider.Items`
// is synchronous and runs while the popup is being built, and prepareRename is
// a server round trip. So the *bridge* validates instead: `codeAction` fires
// prepareRename concurrently with the code-action request (one round trip's
// latency, not two) and records the verdict here; the provider — which the app
// only queries once that reply lands — reads it. Picking the entry then reuses
// the recorded verdict rather than asking again, which is what makes "cannot
// rename here" unreachable from the popup.
//
// Servers without prepareRename support keep the #426 contract: the manager
// answers ok for them without asking, so the entry stays offered and the
// rename attempt decides.

// renameGate is the last recorded prepareRename verdict, valid for exactly one
// (path, line, col). known is false when nothing has been validated (yet) —
// the entry is then withheld rather than offered on a guess.
type renameGate struct {
	path      string
	line, col int
	known     bool
	ok        bool
	// placeholder is the symbol text the prompt prefills, empty when the
	// server offers no range (defaultBehavior) or no prepareRename at all.
	placeholder string
}

// refreshRenameGate validates pos and records the verdict. It blocks on the
// server round trip, so callers run it off the Update goroutine.
func (b *bridge) refreshRenameGate(path string, pos buffer.Position) {
	g := renameGate{path: path, line: pos.Line, col: pos.Col, known: true}
	if mgr := b.manager(); mgr != nil && path != "" {
		// Both an error (including ErrRenameUnsupported) and a rejected
		// position close the gate: neither would reach a rename prompt.
		ph, ok, err := mgr.PrepareRename(context.Background(), path, pos)
		g.ok, g.placeholder = ok && err == nil, ph
	}
	b.mu.Lock()
	b.rgate = g
	b.mu.Unlock()
}

// renameGateAt reports the recorded verdict for a position. known is false for
// any other position (or none recorded), which the callers treat as "do not
// offer" — a fresh popup always records one first.
func (b *bridge) renameGateAt(path string, line, col int) (placeholder string, ok, known bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	g := b.rgate
	if !g.known || g.path != path || g.line != line || g.col != col {
		return "", false, false
	}
	return g.placeholder, g.ok, true
}

// takeRenameGate is renameGateAt plus consumption: the verdict answers exactly
// one rename, so a later request re-validates instead of trusting an aging
// answer.
func (b *bridge) takeRenameGate(path string, line, col int) (placeholder string, ok, known bool) {
	placeholder, ok, known = b.renameGateAt(path, line, col)
	if known {
		b.mu.Lock()
		b.rgate = renameGate{}
		b.mu.Unlock()
	}
	return placeholder, ok, known
}

// invalidateRenameGate drops a verdict the buffer has outgrown: an edit can
// turn a renameable position into an ordinary one (and shift every position
// after it), so a recorded answer for path must not survive it.
func (b *bridge) invalidateRenameGate(path string) {
	b.mu.Lock()
	if b.rgate.path == path {
		b.rgate = renameGate{}
	}
	b.mu.Unlock()
}

// clearRenameGate drops the verdict outright — the popup opening with nothing
// to validate against (no server, no position) must not inherit an older one.
func (b *bridge) clearRenameGate() {
	b.mu.Lock()
	b.rgate = renameGate{}
	b.mu.Unlock()
}

// renameIntentionItems is the body of the "lsp.rename" intention provider,
// taken as a function of the bridge so tests can drive it without the
// process-wide singleton. Both gates must pass: the server declares rename at
// all, and the recorded prepareRename verdict accepts this exact caret.
func renameIntentionItems(b *bridge, cx intention.Context) []intention.Item {
	mgr := b.manager()
	if cx.Path == "" || mgr == nil || !mgr.RenameSupported(cx.Path) {
		return nil
	}
	if _, ok, known := b.renameGateAt(cx.Path, cx.Line, cx.Col); !known || !ok {
		return nil
	}
	return []intention.Item{{Title: "Rename Symbol", Kind: "refactor", CommandID: "lsp.rename"}}
}
