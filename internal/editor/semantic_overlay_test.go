package editor

import (
	"testing"

	"ike/internal/highlight"
	ilsp "ike/internal/lsp"
)

// TestSemanticOverlayPrecedence guards the merge order: the semantic overlay
// wins over the Tree-sitter base where both cover a cell, the base still
// applies elsewhere.
func TestSemanticOverlayPrecedence(t *testing.T) {
	m, path := loaded(t, "alpha beta\n")
	// Fake a Tree-sitter base: the whole first word is a "string".
	m.hlIndex = highlight.NewIndex([]highlight.Span{{Line: 0, StartCol: 0, EndCol: 10, Capture: "string"}})
	m, _ = m.Update(ilsp.SemanticSpansMsg{Path: path, Spans: []highlight.Span{
		{Line: 0, StartCol: 0, EndCol: 5, Capture: "function"},
	}})

	if got := m.semIndex.CaptureAt(0, 2); got != "function" {
		t.Fatalf("overlay missing: %q", got)
	}
	// styleAt resolves semantic first, base as fallback.
	stSem, okSem := m.styleAt(0, 2)
	stBase, okBase := m.styleAt(0, 7)
	if !okSem || !okBase {
		t.Fatal("both cells should style")
	}
	if stSem.GetForeground() == stBase.GetForeground() {
		t.Fatal("semantic capture must win over the base where covered")
	}

	// A msg for another document is ignored; an empty result clears.
	m, _ = m.Update(ilsp.SemanticSpansMsg{Path: "/other.go", Spans: []highlight.Span{{Line: 0, EndCol: 1, Capture: "type"}}})
	if m.semIndex.CaptureAt(0, 2) != "function" {
		t.Fatal("other-path msg must not replace the overlay")
	}
	m, _ = m.Update(ilsp.SemanticSpansMsg{Path: path})
	if !m.semIndex.Empty() {
		t.Fatal("empty spans should clear the overlay")
	}
}
