//go:build cgo

package langhttp

import (
	"testing"

	"ike/internal/highlight"
)

func TestTripleQuoteHighlighting(t *testing.T) {
	lines := tripleQuoteLines("application/json")
	ix := highlight.NewIndex(highlight.Highlight("x.http", lines))
	for _, cell := range [][2]int{{4, 14}, {4, 20}, {4, 28}, {5, 20}, {5, 33}, {6, 20}, {6, 37}} {
		if got := ix.CaptureAt(cell[0], cell[1]); got != "string" {
			t.Errorf("literal cell %v capture %q, want string", cell, got)
		}
	}
	// the brackets inside the literal do not shift the body's bracket depth
	if got := ix.CaptureAt(7, 10); got != highlight.RainbowCapture(1) {
		t.Errorf("list bracket capture %q, want %q", got, highlight.RainbowCapture(1))
	}
	if got := ix.CaptureAt(7, 14); got != highlight.RainbowCapture(2) {
		t.Errorf("object bracket capture %q, want %q", got, highlight.RainbowCapture(2))
	}
	if got := ix.CaptureAt(4, 3); got != "property" {
		t.Errorf("the member key keeps its JSON capture, got %q", got)
	}
}
