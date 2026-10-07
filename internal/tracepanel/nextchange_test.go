package tracepanel

import "testing"

// nextchange_test.go covers NextChange (#2910): the diff view's step to the
// neighbouring change, in path order in the graph view (prompts, answers
// and separators skipped) and in tree order otherwise, false at the ends.

func TestNextChangeWalksTheChanges(t *testing.T) {
	m, _ := graphPanel(t, true, 80, 30)
	// The graph folds a MultiEdit's edits of one file into one stop; the
	// tree has a row (and a diff) per edit.
	order := map[ViewMode][]string{
		ViewGraph: {"e4/f0", "e5/f0", "e7/f0", "e8/f0"},
		ViewTree:  {"e4/f0", "e5/f0", "e7/f0", "e7/f1", "e8/f0"},
	}
	for _, view := range []ViewMode{ViewGraph, ViewTree} {
		m.SetViewMode(view)
		want := order[view]
		walk := func(from string, dir int) []string {
			var got []string
			for key := from; ; {
				msg, ok := m.NextChange(key, dir)
				if !ok {
					return got
				}
				if msg.Path == "" {
					t.Errorf("%v: %s has no path", view, msg.Key)
				}
				got = append(got, msg.Key)
				key = msg.Key
			}
		}
		if got := walk(want[0], 1); !equal(got, want[1:]) {
			t.Errorf("%v: next from %s = %v, want %v", view, want[0], got, want[1:])
		}
		last := len(want) - 1
		var back []string
		for i := last - 1; i >= 0; i-- {
			back = append(back, want[i])
		}
		if got := walk(want[last], -1); !equal(got, back) {
			t.Errorf("%v: previous from %s = %v, want %v", view, want[last], got, back)
		}
		if _, ok := m.NextChange("nope", 1); ok {
			t.Errorf("%v: an unknown key has no neighbour", view)
		}
	}
	// The graph starts from a prompt too, skipping it and the separator.
	m.SetViewMode(ViewGraph)
	if msg, ok := m.NextChange("t1", 1); !ok || msg.Key != "e4/f0" {
		t.Errorf("graph: next from t1 = %v %v", msg.Key, ok)
	}
	if _, ok := m.NextChange("e8/f0", 1); ok {
		t.Error("graph: the answers, prompt and separator after the last change are no change")
	}
	// In the tree a tool row showing one file steps past that file's row.
	m.SetViewMode(ViewTree)
	if msg, ok := m.NextChange("e4", 1); !ok || msg.Key != "e5/f0" {
		t.Errorf("tree: next from tool e4 = %v %v", msg.Key, ok)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
