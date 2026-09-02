package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/allfind"
	"ike/internal/finder"
	"ike/internal/search"
)

// findparity_test.go pins the promise of #2413: the all-projects results are
// the find-in-path results UI, so the keys that walk and open a match have to
// mean the same thing in both. One table drives both surfaces over the same
// match set and compares where enter lands.

// parityMatches is the same result set for both surfaces: three matches over
// two files, all in one project so the all-projects list has a single section
// and the two lists hold identical items.
var parityMatches = []search.Match{
	{Path: "/p/a.go", Line: 3, Text: "needle here", StartCol: 0, EndCol: 6},
	{Path: "/p/a.go", Line: 9, Text: "a needle", StartCol: 2, EndCol: 8},
	{Path: "/p/b.go", Line: 1, Text: "needle too", StartCol: 0, EndCol: 6},
}

// parityKey spells the shared keys out for both surfaces.
func parityKey(name string) tea.KeyPressMsg {
	switch name {
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	}
	panic("unmapped parity key " + name)
}

// parityFinder is a find-in-path overlay holding parityMatches. The empty
// query means Open starts no scan, so generation 0 is still current and the
// batch below lands in the list without touching the disk.
func parityFinder(t *testing.T) *finder.Model {
	t.Helper()
	f := finder.New(search.New(func(tea.Msg) {}))
	f.SetSize(120, 40)
	f.Open("/p")
	f.Apply(search.BatchMsg{Matches: parityMatches})
	f.View() // fix the list height, so the page keys measure the same window
	return f
}

// parityResults is the all-projects results overlay holding the same matches,
// sized so its list window is the same height as the finder's.
func parityResults(t *testing.T) *allfind.Results {
	t.Helper()
	r := allfind.NewResults()
	r.SetSize(120, 32) // listH = height/2-5, the finder's height/2-9 at 40
	r.Begin("needle", []allfind.Project{{Root: "/p", Name: "p"}})
	r.Append("/p", parityMatches)
	r.Finish(false, nil)
	r.View()
	return r
}

// TestFindInPathAndAllProjectsKeyParity: the same keys select the same match
// and open it, on both surfaces.
func TestFindInPathAndAllProjectsKeyParity(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		want int // index into parityMatches
	}{
		{"no motion opens the first hit", nil, 0},
		{"down steps forward", []string{"down"}, 1},
		{"down twice", []string{"down", "down"}, 2},
		{"down wraps past the last", []string{"down", "down", "down"}, 0},
		{"up wraps back to the last", []string{"up"}, 2},
		{"down then up returns", []string{"down", "up"}, 0},
		{"pgdown lands on the last", []string{"pgdown"}, 2},
		{"pgdown then pgup returns", []string{"pgdown", "pgup"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := parityMatches[tc.want]

			f := parityFinder(t)
			for _, k := range tc.keys {
				f.Update(parityKey(k))
			}
			cmd := f.Update(parityKey("enter"))
			if cmd == nil {
				t.Fatal("find in path: enter must open the selected match")
			}
			loc, ok := cmd().(finder.OpenLocationMsg)
			if !ok {
				t.Fatalf("find in path: got %T, want OpenLocationMsg", cmd())
			}
			if loc.Path != want.Path || loc.Line != want.Line || loc.Col != want.StartCol {
				t.Fatalf("find in path opened %s:%d:%d, want %s:%d:%d",
					loc.Path, loc.Line, loc.Col, want.Path, want.Line, want.StartCol)
			}

			r := parityResults(t)
			for _, k := range tc.keys {
				r.Update(parityKey(k))
			}
			cmd = r.Update(parityKey("enter"))
			if cmd == nil {
				t.Fatal("all projects: enter must open the selected match")
			}
			hit, ok := cmd().(allfind.OpenMatchMsg)
			if !ok {
				t.Fatalf("all projects: got %T, want OpenMatchMsg", cmd())
			}
			if hit.Path != loc.Path || hit.Line != loc.Line || hit.Col != loc.Col {
				t.Fatalf("all projects opened %s:%d:%d, find in path %s:%d:%d — the keys must agree",
					hit.Path, hit.Line, hit.Col, loc.Path, loc.Line, loc.Col)
			}
			if hit.Root != "/p" {
				t.Fatalf("all projects must carry the match's project root, got %q", hit.Root)
			}
		})
	}
}

// TestFindInPathAndAllProjectsCloseParity: esc closes both overlays and both
// keep their results for the retained-cursor commands.
func TestFindInPathAndAllProjectsCloseParity(t *testing.T) {
	f := parityFinder(t)
	f.Update(parityKey("esc"))
	if f.IsOpen() {
		t.Fatal("esc must close the find-in-path overlay")
	}
	if !f.HasResults() {
		t.Fatal("find-in-path results must survive esc")
	}

	r := parityResults(t)
	r.Update(parityKey("esc"))
	if r.IsOpen() {
		t.Fatal("esc must close the all-projects overlay")
	}
	if !r.HasResults() {
		t.Fatal("all-projects results must survive esc")
	}
}

// TestAllProjectsOpenClosesLikeFindInPath: both surfaces hand the keyboard
// back when a match is opened.
func TestAllProjectsOpenClosesLikeFindInPath(t *testing.T) {
	f := parityFinder(t)
	f.Update(parityKey("enter"))
	if f.IsOpen() {
		t.Fatal("opening a match must close the find-in-path overlay")
	}
	r := parityResults(t)
	r.Update(parityKey("enter"))
	if r.IsOpen() {
		t.Fatal("opening a match must close the all-projects overlay")
	}
}
