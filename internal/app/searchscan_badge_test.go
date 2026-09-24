package app

import (
	"strings"
	"testing"

	"ike/internal/editor"
	"ike/internal/editor/search"
)

// TestSearchPendingBadgeInStatusLine guards the escape hatch of #2734: while
// an in-file search landing is still scanning in the background, the status
// line's large-file slot says so, and the landing's arrival clears it.
func TestSearchPendingBadgeInStatusLine(t *testing.T) {
	m, _ := largeModel(t)
	ed := m.activeEditor()
	if ed == nil {
		t.Fatal("setup: the large file must be the active editor")
	}
	ed.SeedSearch(search.Compile("needle", false, search.CaseExact), search.Forward)
	_, cmd := ed.RepeatSearch(false)
	if cmd == nil || !ed.SearchPending() {
		t.Fatal("a landing over 1 MB of text without a match must be pending")
	}
	if frame := m.render(); !strings.Contains(frame, "[large file · searching…]") {
		t.Fatal("the status line must show the pending search in the large-file slot")
	}
	msg, ok := cmd().(editor.SearchScanMsg)
	if !ok {
		t.Fatalf("the scan command must yield a SearchScanMsg, got %T", msg)
	}
	m = step(m, msg)
	if m.activeEditor().SearchPending() {
		t.Fatal("the routed landing must settle the pending search")
	}
	if frame := m.render(); strings.Contains(frame, "searching…") || !strings.Contains(frame, "[large file]") {
		t.Fatal("the badge must return to the plain large-file marker once the landing arrived")
	}
}
