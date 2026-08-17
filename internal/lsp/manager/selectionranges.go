package manager

import (
	"context"

	"ike/internal/editor/buffer"
	"ike/internal/lsp/protocol"
)

// selectionranges.go serves textDocument/selectionRange (#1912): the syntactic
// expansion ladder at one cursor position, converted to editor rune
// coordinates innermost-first — the order expand-selection steps through.

// SelectionRanges requests the selection ladder at an editor position. The
// server's linked list (innermost range → Parent chain outwards) flattens into
// a slice; empty ranges and consecutive duplicates are dropped so every step
// visibly grows the selection.
func (m *Manager) SelectionRanges(ctx context.Context, path string, line, col int) ([]buffer.Range, error) {
	srv, doc, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().SelectionRange {
		return nil, nil
	}
	enc := srv.cl.Encoding()
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	res, err := srv.cl.SelectionRanges(cctx, protocol.SelectionRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
		Positions:    []protocol.Position{protocol.ToLSPPosition(doc.lines, buffer.Position{Line: line, Col: col}, enc)},
	})
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, nil
	}
	var out []buffer.Range
	for sr := &res[0]; sr != nil; sr = sr.Parent {
		r := protocol.FromLSPRange(doc.lines, sr.Range, enc)
		if r.Empty() {
			continue
		}
		if n := len(out); n > 0 && out[n-1] == r {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}
