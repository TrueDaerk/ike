package manager

import (
	"context"
	"sort"

	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// codelens.go serves textDocument/codeLens and codeLens/resolve (#1912): the
// manager flattens the wire lens (range + optional command + opaque data) into
// the editor-facing line-anchored shape, and rebuilds it for the resolve round
// trip. Gated on the capability like every feature: no support means nil, nil.

// CodeLenses requests every code lens of one document, returned as
// line-anchored editor lenses sorted by line. An unresolved lens (server ships
// the command lazily) has an empty Command and keeps the server's opaque Data
// for ResolveCodeLens.
func (m *Manager) CodeLenses(ctx context.Context, path string) ([]lsp.CodeLens, error) {
	srv, _, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().CodeLens {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	lenses, err := srv.cl.CodeLens(cctx, protocol.CodeLensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
	})
	if err != nil {
		return nil, err
	}
	out := make([]lsp.CodeLens, 0, len(lenses))
	for _, l := range lenses {
		out = append(out, convertCodeLens(l, l.Range.Start.Line))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out, nil
}

// ResolveCodeLens fills in the command of an unresolved lens via
// codeLens/resolve. A lens that already carries a command — or a server
// without the resolve capability — returns unchanged: the anchor is never
// lost, only the label stays what it was.
func (m *Manager) ResolveCodeLens(ctx context.Context, path string, lens lsp.CodeLens) (lsp.CodeLens, error) {
	if lens.Command != "" {
		return lens, nil
	}
	srv, _, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().CodeLensResolve {
		return lens, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	// Rebuild the wire lens the server handed out: the anchor line as a
	// zero-width range plus the opaque data token, round-tripped verbatim.
	resolved, err := srv.cl.ResolveCodeLens(cctx, protocol.CodeLens{
		Range: protocol.Range{
			Start: protocol.Position{Line: lens.Line},
			End:   protocol.Position{Line: lens.Line},
		},
		Data: lens.Data,
	})
	if err != nil {
		return lens, err
	}
	return convertCodeLens(resolved, lens.Line), nil
}

// convertCodeLens flattens one wire lens onto its anchor line; a nil command
// (unresolved) leaves Title/Command empty and keeps Data for the resolve.
func convertCodeLens(l protocol.CodeLens, line int) lsp.CodeLens {
	out := lsp.CodeLens{Line: line, Data: l.Data}
	if l.Command != nil {
		out.Title = l.Command.Title
		out.Command = l.Command.Command
		out.Arguments = l.Command.Arguments
	}
	return out
}
