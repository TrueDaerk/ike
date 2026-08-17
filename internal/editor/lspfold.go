package editor

// lspfold.go merges server-provided folding ranges (#1912) into the code-fold
// engine (fold.go). textDocument/foldingRange results arrive as an
// ilsp.FoldingRangesMsg and are stored next to the Tree-sitter folds; every
// fold consumer reads the merged set through foldRanges, so za/zc/zM and the
// copy commands see one fold list regardless of where a range came from. On a
// shared header line the LSP range wins — servers know imports/comment/region
// semantics the grammar walk cannot — and its Kind survives the merge. Server
// folds go stale between edits until the next reply, so the merge clamps them
// to the current buffer instead of trusting their lines.

import (
	"sort"

	"ike/internal/highlight"
)

// foldRanges is the fold set this view acts on: the Tree-sitter folds alone
// while LSP folding is disabled (lsp.folding=false) or no server ranges are
// cached, else the merged set. The merge allocates only when both sources
// contribute; fold lists are small (one entry per foldable node), so merging
// on demand keeps the collapsed set free of a second cache to invalidate.
func (m Model) foldRanges() []highlight.Fold {
	if !m.lspFolding || len(m.lspFolds) == 0 {
		return m.folds
	}
	return mergeFolds(m.folds, m.lspFolds, m.buf.LineCount())
}

// mergeFolds merges the Tree-sitter and LSP fold sets into pre-order (header
// ascending, end descending on ties — outer before inner, the order
// InnermostFold and reconcileFolds rely on). Duplicate headers keep the LSP
// range (Kind included); LSP ranges are clamped to lineCount and dropped when
// nothing foldable remains, so a stale server reply can never hide lines the
// buffer no longer has.
func mergeFolds(ts, lsp []highlight.Fold, lineCount int) []highlight.Fold {
	out := make([]highlight.Fold, 0, len(ts)+len(lsp))
	lspHeader := make(map[int]bool, len(lsp))
	for _, f := range lsp {
		if f.HeaderLine < 0 || f.HeaderLine >= lineCount {
			continue
		}
		if last := lineCount - 1; f.EndLine > last {
			f.EndLine = last
		}
		if f.EndLine <= f.HeaderLine || lspHeader[f.HeaderLine] {
			continue
		}
		lspHeader[f.HeaderLine] = true
		out = append(out, f)
	}
	for _, f := range ts {
		if !lspHeader[f.HeaderLine] {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].HeaderLine != out[j].HeaderLine {
			return out[i].HeaderLine < out[j].HeaderLine
		}
		return out[i].EndLine > out[j].EndLine
	})
	return out
}
