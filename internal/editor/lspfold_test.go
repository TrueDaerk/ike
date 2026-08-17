package editor

// lspfold_test.go covers the server-provided folding ranges (#1912,
// lspfold.go): the merge with the Tree-sitter folds, the lsp.folding gate, and
// the fold commands acting on server-only ranges.

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// lspFoldsMsg builds a FoldingRangesMsg for the foldModel fixture's path.
func lspFoldsMsg(path string, folds ...highlight.Fold) ilsp.FoldingRangesMsg {
	return ilsp.FoldingRangesMsg{Path: path, Folds: folds}
}

func TestLSPFoldToggles(t *testing.T) {
	// A fold only the server knows (no matching Tree-sitter range) closes and
	// opens with za like any parsed fold.
	m := foldModel(t)
	m, _ = m.Update(lspFoldsMsg("main.go", highlight.Fold{HeaderLine: 12, EndLine: 13, Kind: "region"}))
	m = send(m, keys("12jza")...)
	if e, ok := m.folded[12]; !ok || e != 13 {
		t.Fatalf("za on an LSP-only fold: folded=%v, want {12: 13}", m.folded)
	}
	m = send(m, keys("za")...)
	if _, ok := m.folded[12]; ok {
		t.Fatalf("za again should open it, folded=%v", m.folded)
	}
}

func TestLSPFoldWinsOverTreeSitterHeader(t *testing.T) {
	// The fixture has a Tree-sitter fold 2-7; the server reports 2-6 with a
	// kind. The LSP range wins: zc closes 2-6, Kind survives the merge.
	m := foldModel(t)
	m, _ = m.Update(lspFoldsMsg("main.go", highlight.Fold{HeaderLine: 2, EndLine: 6, Kind: "imports"}))
	folds := m.foldRanges()
	found := false
	for _, f := range folds {
		if f.HeaderLine == 2 {
			if found {
				t.Fatalf("header 2 must appear once in the merge, got %v", folds)
			}
			found = true
			if f.EndLine != 6 || f.Kind != "imports" {
				t.Fatalf("LSP fold must win with Kind intact, got %+v", f)
			}
		}
	}
	if !found {
		t.Fatalf("merged set lost header 2: %v", folds)
	}
	m = send(m, keys("2jzc")...)
	if e, ok := m.folded[2]; !ok || e != 6 {
		t.Fatalf("zc must close the LSP range, folded=%v want {2: 6}", m.folded)
	}
}

func TestLSPFoldConfigGateOff(t *testing.T) {
	// lsp.folding=false ignores the cached server ranges without dropping
	// them: flipping the gate back restores the merge.
	m := foldModel(t)
	m, _ = m.Update(lspFoldsMsg("main.go", highlight.Fold{HeaderLine: 12, EndLine: 13}))
	m.Configure(host.MapConfig{"lsp.folding": "false"})
	m = send(m, keys("12jzc")...)
	if len(m.folded) != 0 {
		t.Fatalf("gate off: zc on an LSP-only fold must do nothing, folded=%v", m.folded)
	}
	m.Configure(host.MapConfig{"lsp.folding": "true"})
	m = send(m, keys("zc")...)
	if e, ok := m.folded[12]; !ok || e != 13 {
		t.Fatalf("gate back on: cached LSP folds must resume, folded=%v", m.folded)
	}
}

func TestLSPFoldOutOfRangeClamped(t *testing.T) {
	// The fixture buffer has 14 lines (0-13). Ranges beyond it clamp or drop
	// instead of panicking, and zM over the merged set stays in range.
	m := foldModel(t)
	m, _ = m.Update(lspFoldsMsg("main.go",
		highlight.Fold{HeaderLine: 12, EndLine: 99}, // end past the buffer: clamp to 13
		highlight.Fold{HeaderLine: 50, EndLine: 60}, // fully outside: drop
		highlight.Fold{HeaderLine: -3, EndLine: 4},  // negative header: drop
		highlight.Fold{HeaderLine: 13, EndLine: 99}, // clamps to a single line: drop
	))
	folds := m.foldRanges()
	for _, f := range folds {
		if f.HeaderLine < 0 || f.EndLine > 13 || f.EndLine <= f.HeaderLine {
			t.Fatalf("merged set holds an out-of-range fold: %+v", f)
		}
	}
	m = send(m, keys("zM")...)
	if e, ok := m.folded[12]; !ok || e != 13 {
		t.Fatalf("clamped fold must close as 12-13, folded=%v", m.folded)
	}
}

func TestLSPFoldOtherPathIgnored(t *testing.T) {
	m := foldModel(t)
	m, _ = m.Update(lspFoldsMsg("other.go", highlight.Fold{HeaderLine: 12, EndLine: 13}))
	if len(m.lspFolds) != 0 {
		t.Fatalf("another document's folding ranges must be ignored, got %v", m.lspFolds)
	}
}

func TestLSPFoldReconcileKeepsCollapsedServerFold(t *testing.T) {
	// A collapsed server fold survives a reparse whose Tree-sitter set does
	// not know its header — the merged set is what reconcile validates
	// against — and resetFolds drops the server ranges with everything else.
	m := foldModel(t)
	m, _ = m.Update(lspFoldsMsg("main.go", highlight.Fold{HeaderLine: 12, EndLine: 13}))
	m = send(m, keys("12jzc")...)
	m = feedSpans(t, m, highlight.SpansMsg{
		Path:  "main.go",
		Folds: []highlight.Fold{{HeaderLine: 2, EndLine: 7}},
	})
	if e, ok := m.folded[12]; !ok || e != 13 {
		t.Fatalf("collapsed LSP fold must survive reconcile, folded=%v", m.folded)
	}
	m.resetFolds()
	if m.lspFolds != nil || m.folded != nil {
		t.Fatalf("resetFolds must clear the server ranges, lspFolds=%v folded=%v", m.lspFolds, m.folded)
	}
}

func TestMergeFoldsOrderingAndDedup(t *testing.T) {
	ts := []highlight.Fold{
		{HeaderLine: 0, EndLine: 20},
		{HeaderLine: 2, EndLine: 10},
		{HeaderLine: 12, EndLine: 18},
	}
	lsp := []highlight.Fold{
		{HeaderLine: 2, EndLine: 9, Kind: "region"}, // wins over the TS 2-10
		{HeaderLine: 2, EndLine: 5},                 // duplicate LSP header: first wins
		{HeaderLine: 0, EndLine: 25, Kind: "comment"},
	}
	got := mergeFolds(ts, lsp, 26)
	want := []highlight.Fold{
		{HeaderLine: 0, EndLine: 25, Kind: "comment"},
		{HeaderLine: 2, EndLine: 9, Kind: "region"},
		{HeaderLine: 12, EndLine: 18},
	}
	if len(got) != len(want) {
		t.Fatalf("merge = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("merge[%d] = %+v, want %+v (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestMergeFoldsPreOrderOnSharedHeader(t *testing.T) {
	// Same header from both sides plus a nested fold: the result must stay
	// pre-order (header asc, end desc), or InnermostFold picks wrong.
	got := mergeFolds(
		[]highlight.Fold{{HeaderLine: 1, EndLine: 8}, {HeaderLine: 3, EndLine: 5}},
		[]highlight.Fold{{HeaderLine: 1, EndLine: 6}},
		20,
	)
	if len(got) != 2 || got[0] != (highlight.Fold{HeaderLine: 1, EndLine: 6}) || got[1] != (highlight.Fold{HeaderLine: 3, EndLine: 5}) {
		t.Fatalf("merge = %v, want [{1 6} {3 5}]", got)
	}
}
