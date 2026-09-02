package httppane

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestBeginSearchIsThePaneKey (#2400): http.search dispatches into
// BeginSearch, which must leave the pane in exactly the state the pane-local
// "/" key leaves it in — prompt open, query prefilled from the selection,
// matches live — so the bound chord and the pane key cannot drift apart.
func TestBeginSearchIsThePaneKey(t *testing.T) {
	viaKey := searchViewer(t)
	viaKey.handleKey(keyPress("/"))

	viaCommand := searchViewer(t)
	viaCommand.BeginSearch()

	q1, open1 := viaKey.SearchQuery()
	q2, open2 := viaCommand.SearchQuery()
	if q1 != q2 || open1 != open2 {
		t.Fatalf("BeginSearch state = (%q, %v), pane key = (%q, %v)", q2, open2, q1, open1)
	}
	if !open2 {
		t.Fatal("BeginSearch must open the prompt")
	}

	// The prompt really takes text afterwards, and alt+backspace kills the
	// word before the cursor there like in every other input (#2400).
	for _, r := range "token value" {
		viaCommand.handleKey(keyPress(string(r)))
	}
	viaCommand.handleKey(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if q, _ := viaCommand.SearchQuery(); q != "token " {
		t.Fatalf("query after alt+backspace = %q, want %q", q, "token ")
	}
}
