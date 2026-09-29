package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/jqplay"
)

// TestPlayValueGlyphMap: every value type gets its own gutter glyph (#2789),
// in both the JSON and the YAML rendering.
func TestPlayValueGlyphMap(t *testing.T) {
	cases := map[string]string{
		"{\n  \"a\": 1\n}": "{",
		"[\n  1\n]":        "[",
		"{}":               "{",
		`"x"`:              `"`,
		"42":               "#",
		"-1.5e3":           "#",
		"null":             "∅",
		"true":             "⊤",
		"false":            "⊥",
		"a: 1\nb: 2":       "{",
		"- 1\n- 2":         "[",
		"hello":            `"`,
		"~":                "∅",
	}
	for in, want := range cases {
		if got := jqplay.ValueGlyph(in); got != want {
			t.Errorf("ValueGlyph(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPlayValueSignsJQ: each value's first line carries its glyph in the
// gutter, the line numbers keep their column, and the info row counts the
// value under the result cursor as it moves.
func TestPlayValueSignsJQ(t *testing.T) {
	m := openJQ(t, dismissOnboarding(playApp(t, `{"items":[{"a":1},2,null]}`)))
	// Room for the -r/-c/-s chips (#2784) and the whole summary beside them.
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = tm.(Model)
	m = setProgram(m, ".items[]")
	if got := m.play.valueStarts; len(got) != 3 || got[0] != 0 || got[1] != 3 || got[2] != 4 {
		t.Fatalf("value starts = %v, want [0 3 4]", got)
	}
	view := ansi.Strip(m.render())
	for _, g := range []string{"{", "#", "∅"} {
		if !strings.Contains(view, g+" ") {
			t.Errorf("gutter glyph %q missing, got:\n%s", g, view)
		}
	}
	if !strings.Contains(view, "value 1/3") {
		t.Fatalf("info row must count the value under the cursor, got:\n%s", view)
	}
	gw := m.play.resultEd.GutterWidth()
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = playKeys(m, "gg")
	if c := m.play.playValueCounter(); c != "value 1/3" {
		t.Errorf("counter at the top = %q, want value 1/3", c)
	}
	m = playKeys(m, "G")
	if view := ansi.Strip(m.render()); !strings.Contains(view, "value 3/3") {
		t.Errorf("the counter must follow the cursor, got:\n%s", view)
	}
	if m.play.resultEd.GutterWidth() != gw {
		t.Errorf("the glyph must not widen the gutter")
	}
}

// TestPlayValueSignsYQ: the `---` separator belongs to the value above it.
func TestPlayValueSignsYQ(t *testing.T) {
	m := openYQ(t, dismissOnboarding(yqApp(t, "items:\n  - a: 1\n  - 2\n")))
	m = setProgram(m, ".items[]")
	if got := m.play.valueStarts; len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("value starts = %v, want [0 2]", got)
	}
	if i := jqplay.ValueIndex(m.play.valueStarts, 1); i != 0 {
		t.Errorf("the separator line must count to the first value, got %d", i)
	}
}

// TestPlayValueCounterSingleValue: one value needs no counter.
func TestPlayValueCounterSingleValue(t *testing.T) {
	m := openJQ(t, dismissOnboarding(playApp(t, `{"a":1}`)))
	m = setProgram(m, ".")
	if c := m.play.playValueCounter(); c != "" {
		t.Errorf("a single value must not show a counter, got %q", c)
	}
}
