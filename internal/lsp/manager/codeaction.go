package manager

import (
	"context"

	"ike/internal/lsp/protocol"
)

// codeaction.go serves codeAction/resolve (#2252). An action offered without
// an edit is not necessarily a command: many servers ship lean actions and
// compute the WorkspaceEdit on demand, so both the intention popup's preview
// and the apply path resolve one before concluding there is nothing to edit.

// ResolveCodeAction fills in the edit of a lazily computed action. An action
// that already carries one — or a server without the resolve capability —
// returns unchanged, so the caller's only distinction stays "has an edit" vs
// "has none".
func (m *Manager) ResolveCodeAction(ctx context.Context, path string, action protocol.CodeAction) (protocol.CodeAction, error) {
	if action.Edit != nil {
		return action, nil
	}
	srv, _, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().CodeActionResolve {
		return action, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return srv.cl.ResolveCodeAction(cctx, action)
}

// CodeActionResolveSupported reports whether a ready server tracks path and
// promises codeAction/resolve — the honest wording gate for an action that
// arrived with neither edit nor command.
func (m *Manager) CodeActionResolveSupported(path string) bool {
	srv, _, ok := m.docServer(path)
	return ok && srv.cl.Caps().CodeActionResolve
}
