package editor

import (
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/theme"
	"ike/internal/yamlanchor"
)

// TestUnresolvedAliasRendersAsError (#1629): the anchor.unresolved capture
// resolves like an unmatched bracket — the theme's error colour with an
// underline — so an alias without an anchor reads as the mistake it is.
func TestUnresolvedAliasRendersAsError(t *testing.T) {
	m := New()
	m.SetPalette(theme.DefaultPalette())
	m.buf = buffer.FromString("a: *ghost")
	m.path = "cfg.yaml"
	m = feedSpans(t, m, highlight.SpansMsg{
		Path:  "cfg.yaml",
		Spans: []highlight.Span{{Line: 0, StartCol: 3, EndCol: 9, Capture: yamlanchor.Unresolved}},
	})
	st, ok := m.styleAt(0, 3)
	if !ok {
		t.Fatal("an unresolved alias must resolve to a style")
	}
	if st.GetForeground() != m.theme().Error {
		t.Errorf("unresolved alias colour = %v want the theme's Error", st.GetForeground())
	}
	if !st.GetUnderline() {
		t.Error("an unresolved alias must be underlined")
	}
}

// TestResolvedAliasKeepsItsRainbowSlot (#1629): a paired alias renders with
// its name-hashed rainbow capture, not the error styling.
func TestResolvedAliasKeepsItsRainbowSlot(t *testing.T) {
	m := New()
	m.SetPalette(theme.DefaultPalette())
	m.buf = buffer.FromString("a: &x 1\nb: *x")
	m.path = "cfg.yaml"
	m = feedSpans(t, m, highlight.SpansMsg{
		Path:  "cfg.yaml",
		Spans: []highlight.Span{{Line: 1, StartCol: 3, EndCol: 5, Capture: yamlanchor.Capture("x")}},
	})
	st, ok := m.styleAt(1, 3)
	if !ok {
		t.Fatal("a paired alias must resolve to a style")
	}
	if st.GetUnderline() {
		t.Error("a paired alias must not carry the error underline")
	}
	if st.GetForeground() == m.theme().Error {
		t.Error("a paired alias must not take the error colour")
	}
}
