package editor

import (
	"strings"
	"testing"

	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// renderFresh forces a full recompute by moving the epoch past whatever the cache
// holds, so its output is guaranteed cache-free. Comparing an ordinary View()
// against it detects a stale cache — i.e. a mutation that failed to bump the
// render epoch.
func renderFresh(m Model) string {
	m.renderEpoch += 1 << 20
	return m.View()
}

func longContent() string {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", 3))
		b.WriteString(" with some_identifier = func(a, b) and a fairly long tail of text ")
		b.WriteString(strings.Repeat("z", 20))
		b.WriteByte('\n')
	}
	return b.String()
}

// TestLineCacheNeverStale is the core safety net (#614): after every kind of
// render-affecting mutation, the (possibly cached) View must equal a forced-fresh
// render. A failure means the mutation did not invalidate the cache.
func TestLineCacheNeverStale(t *testing.T) {
	mutations := []struct {
		name string
		do   func(m *Model)
	}{
		{"vertical-scroll", func(m *Model) { m.ScrollBy(5) }},
		{"vertical-scroll-back", func(m *Model) { m.ScrollBy(-3) }},
		{"horizontal-scroll", func(m *Model) { m.ScrollXBy(8) }},
		{"cursor-move", func(m *Model) { m.SetCursor(6, 2) }},
		{"resize-width", func(m *Model) { m.SetSize(50, 15) }},
		{"blur", func(m *Model) { m.SetFocused(false) }},
		{"refocus", func(m *Model) { m.SetFocused(true) }},
		{"edit", func(m *Model) { *m = typeKeys(*m, "x") }},
		{"cursor-key", func(m *Model) { *m = typeKeys(*m, "jjl") }},
		{"visual-select", func(m *Model) { *m = typeKeys(*m, "vjl") }},
		{"diagnostics", func(m *Model) {
			p := protocol.PublishDiagnosticsParams{Diagnostics: []protocol.Diagnostic{{
				Range:    protocol.Range{Start: protocol.Position{Line: 4, Character: 0}, End: protocol.Position{Line: 4, Character: 5}},
				Severity: 1, Message: "boom",
			}}}
			mm, _ := m.Update(ilsp.DiagnosticsMsg{Path: m.path, Diagnostics: ilsp.ConvertDiagnostics(p, strings.Split(m.buf.String(), "\n"), "")})
			*m = mm
		}},
	}

	for _, mut := range mutations {
		t.Run(mut.name, func(t *testing.T) {
			m, _ := loaded(t, longContent())
			m.SetSize(60, 20)
			m.SetFocused(true)
			_ = m.View() // warm the cache in the pre-mutation state

			mut.do(&m)

			got := m.View()          // may be served from cache
			want := renderFresh(m)   // guaranteed cache-free
			if got != want {
				t.Fatalf("%s: cached render differs from fresh render (stale cache — missing epoch bump)\n--- cached ---\n%s\n--- fresh ---\n%s", mut.name, got, want)
			}
		})
	}
}

// TestLineCacheReusesOnVerticalScroll verifies the cache actually hits on a
// vertical scroll (the optimization's whole point) — scrolling down then back up
// re-serves the original lines without recomputing.
func TestLineCacheReusesOnVerticalScroll(t *testing.T) {
	m, _ := loaded(t, longContent())
	m.SetSize(60, 20)
	m.SetFocused(true)
	_ = m.View()
	epochAfterWarm := m.renderEpoch
	entriesAfterWarm := len(m.lineCache.entries)
	if entriesAfterWarm == 0 {
		t.Fatal("expected the cache to hold rendered lines after a render")
	}

	m.ScrollBy(5) // vertical scroll must NOT bump the epoch
	if m.renderEpoch != epochAfterWarm {
		t.Fatalf("vertical scroll bumped the render epoch (%d → %d); the cache would clear every scroll", epochAfterWarm, m.renderEpoch)
	}
	_ = m.View()
	// Newly exposed lines add entries; previously cached ones are still there.
	if len(m.lineCache.entries) < entriesAfterWarm {
		t.Fatal("cache shrank on scroll — entries were not reused")
	}
}
