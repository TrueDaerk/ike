package hltest

// async_test.go covers the off-loop syntax pass (#2353): a fresh response
// composes plain and colours in only when the scheduled pass is applied, a
// stale pass loses to a newer response, and a body past the configured limit
// is never scheduled at all — with the notice row saying why.

import (
	"strings"
	"testing"

	"ike/internal/highlight"
	"ike/internal/httppane"
	"ike/internal/theme"
)

// TestHighlightArrivesOffLoop: Set composes the body immediately but plain;
// the colours land only when the scheduled pass runs and is applied.
func TestHighlightArrivesOffLoop(t *testing.T) {
	if !highlight.FencedSupported("json") {
		t.Skip("no JSON grammar in this build")
	}
	m := httppane.New(theme.DefaultPalette())
	m.SetSize(80, 20)
	m.Set("one", resp("application/json", `{"ok":true}`))

	if !strings.Contains(m.BodyText(), `"ok": true`) {
		t.Fatalf("the body must compose immediately:\n%s", m.BodyText())
	}
	if m.Highlighted() {
		t.Fatal("Set must not highlight synchronously (#2353)")
	}
	if !m.FinishHighlight() {
		t.Fatal("a pass must have been scheduled")
	}
	if !m.Highlighted() {
		t.Error("the applied pass must colour the body")
	}
}

// TestStaleHighlightDropped: a pass still in flight when a newer response
// recomposes the rows must not paint them — the new response wins.
func TestStaleHighlightDropped(t *testing.T) {
	if !highlight.FencedSupported("json") {
		t.Skip("no JSON grammar in this build")
	}
	m := httppane.New(theme.DefaultPalette())
	m.SetSize(80, 20)
	m.Set("one", resp("application/json", `{"first":1}`))
	stale := m.HighlightCmd()
	if stale == nil {
		t.Fatal("the first response must schedule a pass")
	}

	// The second response arrives while the first pass is "still running".
	m.Set("two", resp("application/json", `{"second":2}`))
	msg, ok := stale().(httppane.HighlightedMsg)
	if !ok {
		t.Fatal("the pass must yield a HighlightedMsg")
	}
	if m.ApplyHighlight(msg) {
		t.Fatal("a stale pass must be dropped")
	}
	if m.Highlighted() {
		t.Fatal("the stale spans must not paint the new rows")
	}
	if !m.FinishHighlight() || !m.Highlighted() {
		t.Error("the new response's own pass must still land")
	}
}

// TestHighlightLimitCapsThePass: past http.highlight_limit_kb no pass is
// scheduled and the pane says so in a warning row.
func TestHighlightLimitCapsThePass(t *testing.T) {
	if !highlight.FencedSupported("json") {
		t.Skip("no JSON grammar in this build")
	}
	httppane.SetHighlightLimit(1) // 1 KiB
	defer httppane.SetHighlightLimit(httppane.DefaultHighlightLimitKB)

	body := `{"rows":["` + strings.Repeat("ä", 2<<10) + `"]}`
	m := httppane.New(theme.DefaultPalette())
	m.SetSize(80, 20)
	m.Set("big", resp("application/json", body))

	if m.FinishHighlight() {
		t.Fatal("no pass may be scheduled past the limit")
	}
	if m.Highlighted() {
		t.Fatal("the capped body must stay plain")
	}
	found := false
	for _, w := range m.Warnings() {
		if strings.Contains(w, "highlight limit") {
			found = true
		}
	}
	if !found {
		t.Errorf("the cap must be surfaced in a notice row, warnings: %v", m.Warnings())
	}
}
