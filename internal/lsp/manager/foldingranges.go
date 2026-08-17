package manager

import (
	"context"
	"sort"

	"ike/internal/highlight"
	"ike/internal/lsp/protocol"
)

// foldingranges.go serves textDocument/foldingRange (#1912): server folding
// ranges normalised into the editor's highlight.Fold model — line-based,
// pre-ordered (header ascending, outer before inner), one fold per header
// line — so the editor can merge them with the Tree-sitter folds.

// FoldingRanges requests the foldable regions of one document. Degenerate
// ranges (end at or before the header) are dropped, out-of-document ends are
// clamped to the last line, and of several ranges sharing a header line only
// the largest survives — the fold model keys toggles by header.
func (m *Manager) FoldingRanges(ctx context.Context, path string) ([]highlight.Fold, error) {
	srv, doc, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().FoldingRange {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	ranges, err := srv.cl.FoldingRanges(cctx, protocol.FoldingRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
	})
	if err != nil {
		return nil, err
	}
	last := len(doc.lines) - 1
	folds := make([]highlight.Fold, 0, len(ranges))
	for _, r := range ranges {
		end := r.EndLine
		if end > last {
			end = last
		}
		if r.StartLine < 0 || r.StartLine > last || end <= r.StartLine {
			continue
		}
		folds = append(folds, highlight.Fold{HeaderLine: r.StartLine, EndLine: end, Kind: r.Kind})
	}
	// Pre-order for the fold model: header ascending, and for a shared header
	// the larger range first — which the dedup below then keeps.
	sort.SliceStable(folds, func(i, j int) bool {
		if folds[i].HeaderLine != folds[j].HeaderLine {
			return folds[i].HeaderLine < folds[j].HeaderLine
		}
		return folds[i].EndLine > folds[j].EndLine
	})
	out := folds[:0]
	for _, f := range folds {
		if n := len(out); n > 0 && out[n-1].HeaderLine == f.HeaderLine {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}
