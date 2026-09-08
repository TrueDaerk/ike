package keymap

import "testing"

// TestIssuesPlainArrows (#2537): the four plain arrows are bound in the
// issues context — up/down onto the selection walk the ctrl forms already
// carried, left/right onto the tab walk — and they stay scoped to that pane,
// so no other context loses an arrow to them.
func TestIssuesPlainArrows(t *testing.T) {
	table := BuildTable(DefaultsFor(PresetJetBrains, "darwin"), nil, "darwin")
	want := map[string]string{
		"up":        "issues.selectPrev",
		"down":      "issues.selectNext",
		"left":      "issues.prevTab",
		"right":     "issues.nextTab",
		"ctrl+up":   "issues.selectPrev",
		"ctrl+down": "issues.selectNext",
	}
	for chord, cmd := range want {
		k := key(t, chord)
		b, ok := table.Lookup(Chord{Steps: []Key{k}}, Issues)
		if !ok || b.Command != cmd {
			t.Errorf("%s in the issues context = %q (found %v), want %q", chord, b.Command, ok, cmd)
		}
		if _, ok := table.Lookup(Chord{Steps: []Key{k}}, Editor); ok && (chord == "up" || chord == "down" || chord == "left" || chord == "right") {
			t.Errorf("%s must stay scoped to the issues window, but the editor resolves it too", chord)
		}
	}
}
