package lsp

import (
	"context"
	"sync"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/manager"
	"ike/internal/lsp/protocol"
)

// codeaction.go is the offer one code-action reply hands the app (#2252): the
// actions themselves plus the two things the popup does with them — preview
// the highlighted one and apply the picked one. Both go through the same
// actionSet, so a lazy action is resolved once (codeAction/resolve) and the
// edit that was previewed is the very edit that applies.

// actionSet is one popup's offer. Resolving replaces the stored action, so a
// second look — previewing again after scrolling back, or applying what was
// previewed — costs no further round trip and cannot answer differently. The
// lock is held across the resolve call: the only contenders are the preview
// and the apply of the *same* row, and serializing them is exactly what makes
// the second one reuse the first one's answer.
type actionSet struct {
	mu      sync.Mutex
	path    string
	actions []protocol.CodeAction
}

// choices renders the offer for the picker.
func (s *actionSet) choices() []ilsp.CodeActionChoice {
	out := make([]ilsp.CodeActionChoice, len(s.actions))
	for i, a := range s.actions {
		out[i] = ilsp.CodeActionChoice{Title: a.Title, Kind: a.Kind, Preferred: a.IsPreferred}
	}
	return out
}

// at returns the action at i, resolved when it arrived without an edit and
// the server promises codeAction/resolve. A failed resolve yields the
// unresolved action — the row still applies whatever it does carry.
func (s *actionSet) at(mgr *manager.Manager, i int) (protocol.CodeAction, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i < 0 || i >= len(s.actions) {
		return protocol.CodeAction{}, false
	}
	act := s.actions[i]
	if act.Edit != nil || mgr == nil {
		return act, true
	}
	resolved, err := mgr.ResolveCodeAction(context.Background(), s.path, act)
	if err != nil {
		return act, true
	}
	s.actions[i] = resolved
	return resolved, true
}

// previewCmd answers a preview request for row i (#2252): resolve the action
// far enough to know its edit, then compute the affected files' before/after
// text on copies. Nothing is applied and no buffer is touched — the reply is
// pure text the popup diffs. An action with no resolvable edit answers with a
// Note instead, which is what the popup renders as "no preview".
func (s *actionSet) previewCmd(h host.API, mgr *manager.Manager, i int) tea.Cmd {
	go func() {
		msg := ilsp.ActionPreviewMsg{Path: s.path, Index: i}
		act, ok := s.at(mgr, i)
		if !ok {
			return
		}
		switch {
		case act.Edit != nil:
			msg.Files = previewFiles(mgr, mgr.ConvertWorkspaceEdit(s.path, *act.Edit))
			if len(msg.Files) == 0 {
				msg.Note = "changes nothing here"
			}
		case act.Command != nil:
			msg.Note = "no preview — runs a server command"
		default:
			msg.Note = "no preview available"
		}
		h.Send(msg)
	}()
	return nil
}

// applyCmd applies row i, resolving it first: an action previewed a moment
// ago is already resolved and applies exactly what was shown, and one applied
// without ever being previewed resolves here rather than doing nothing. The
// resolve is a round trip, so it runs off the Update goroutine like the apply
// it precedes.
func (s *actionSet) applyCmd(h host.API, b *bridge, i int) tea.Cmd {
	go func() {
		act, ok := s.at(b.manager(), i)
		if !ok {
			return
		}
		b.runAction(h, s.path, act)
	}()
	return nil
}
