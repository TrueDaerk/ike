package keymap

import "testing"

// bindingin_test.go covers LiveBindings.BindingIn (#2549): the palette's
// post-pick hint names only chords the user could have pressed in the focused
// context — not a binding of another pane, not a chord shadowed there.

func contextTable(t *testing.T) *LiveBindings {
	t.Helper()
	l := &LiveBindings{}
	l.Set(BuildTable([]Binding{
		{Chord: MustParseChord("ctrl+y"), Command: "t.global", Context: Global},
		{Chord: MustParseChord("ctrl+u"), Command: "t.editor", Context: Editor},
		// ctrl+j fires t.shadowed everywhere except in the editor, where the
		// pane-scoped binding takes the chord.
		{Chord: MustParseChord("ctrl+j"), Command: "t.shadowed", Context: Global},
		{Chord: MustParseChord("ctrl+j"), Command: "t.editorWins", Context: Editor},
	}, nil, "darwin"))
	return l
}

func TestBindingInHonoursContext(t *testing.T) {
	l := contextTable(t)
	cases := []struct {
		id     string
		active Context
		want   string
		ok     bool
	}{
		{"t.global", Explorer, "ctrl+y", true},
		{"t.global", Editor, "ctrl+y", true},
		{"t.editor", Editor, "ctrl+u", true},
		{"t.editor", Explorer, "", false},
		{"t.shadowed", Explorer, "ctrl+j", true},
		{"t.shadowed", Editor, "", false},
		{"t.editorWins", Editor, "ctrl+j", true},
		{"t.editorWins", Explorer, "", false},
		{"t.unbound", Editor, "", false},
		{"", Editor, "", false},
	}
	for _, c := range cases {
		got, ok := l.BindingIn(c.id, c.active)
		if got != c.want || ok != c.ok {
			t.Errorf("BindingIn(%q, %q) = (%q, %v), want (%q, %v)", c.id, c.active, got, ok, c.want, c.ok)
		}
	}
	if _, ok := (&LiveBindings{}).BindingIn("t.global", Editor); ok {
		t.Fatal("an unset table must report no chord")
	}
}
