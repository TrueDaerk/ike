package theme

import "testing"

// TestBuiltinsSliceIsTheCallers guards the memoised set (#3135): the themes
// are built once, but each call hands out its own slice, so a caller that
// edits or reorders what it got never changes what the next caller sees.
func TestBuiltinsSliceIsTheCallers(t *testing.T) {
	first := Builtins()
	if len(first) == 0 {
		t.Fatal("no built-in themes")
	}
	name := first[0].Name
	first[0].Name = "edited"
	first[0], first[len(first)-1] = first[len(first)-1], first[0]

	second := Builtins()
	if len(second) != len(first) {
		t.Fatalf("built-ins = %d themes, then %d", len(first), len(second))
	}
	if second[0].Name != name {
		t.Fatalf("a caller's edit leaked into the next call: first theme is %q, want %q", second[0].Name, name)
	}
	for _, th := range second {
		if len(th.Files) == 0 || len(th.Captures) == 0 {
			t.Fatalf("theme %q lost a color group", th.Name)
		}
	}
}
