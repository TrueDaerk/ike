package editor

import "testing"

// intentionpreview_test.go covers the read-only probes the intention popup's
// diff preview reads (#2252): each returns the text its command *would*
// produce and leaves the buffer exactly as it found it.

func TestToggleValuePreviewShowsTheFlippedLine(t *testing.T) {
	m, _ := loaded(t, "debug = true\n")
	m.SetCursor(0, 8)

	before, after, line, ok := m.ToggleValuePreview()
	if !ok {
		t.Fatal("the caret sits on a toggle pair")
	}
	if before != "debug = true" || after != "debug = false" {
		t.Fatalf("preview = %q → %q", before, after)
	}
	if line != 0 {
		t.Fatalf("line = %d, want the caret line", line)
	}
	if got := m.Text(); got != "debug = true" {
		t.Fatalf("buffer = %q — previewing must not toggle anything", got)
	}
	if m.Dirty() {
		t.Fatal("previewing must not dirty the buffer")
	}
}

func TestToggleValuePreviewMatchesWhatApplyingWrites(t *testing.T) {
	m, _ := loaded(t, "debug = true\n")
	m.SetCursor(0, 8)
	_, after, _, _ := m.ToggleValuePreview()

	m, _ = m.Update(ActionMsg{Action: "toggle_value"})
	if got := m.Text(); got != after {
		t.Fatalf("applied %q, previewed %q", got, after)
	}
}

func TestToggleValuePreviewDeclinesWithoutAPair(t *testing.T) {
	m, _ := loaded(t, "name = ikeee\n")
	m.SetCursor(0, 8)
	if _, _, _, ok := m.ToggleValuePreview(); ok {
		t.Fatal("nothing on this line toggles")
	}
}

func TestConflictPreviewAtCaretKeepsTheChosenSide(t *testing.T) {
	m, _ := loaded(t, conflictText)
	m.SetCursor(2, 0)

	wantBefore := "<<<<<<< HEAD\nours1\nours2\n=======\ntheirs1\n>>>>>>> feature"
	cases := []struct {
		name         string
		ours, theirs bool
		want         string
	}{
		{"ours", true, false, "ours1\nours2"},
		{"theirs", false, true, "theirs1"},
		{"both", true, true, "ours1\nours2\ntheirs1"},
	}
	for _, tc := range cases {
		before, after, line, ok := m.ConflictPreviewAtCaret(tc.ours, tc.theirs)
		if !ok {
			t.Fatalf("%s: the caret sits inside the block", tc.name)
		}
		if before != wantBefore {
			t.Fatalf("%s: before = %q", tc.name, before)
		}
		if after != tc.want {
			t.Fatalf("%s: after = %q, want %q", tc.name, after, tc.want)
		}
		if line != 1 {
			t.Fatalf("%s: line = %d, want the block start", tc.name, line)
		}
	}
	if got := m.Text(); got != "top\n<<<<<<< HEAD\nours1\nours2\n=======\ntheirs1\n>>>>>>> feature\nbottom" {
		t.Fatalf("buffer = %q — previewing must resolve nothing", got)
	}
	if m.Dirty() {
		t.Fatal("previewing must not dirty the buffer")
	}
}

func TestConflictPreviewMatchesWhatAcceptingWrites(t *testing.T) {
	m, _ := loaded(t, conflictText)
	m.SetCursor(2, 0)
	_, after, _, _ := m.ConflictPreviewAtCaret(true, false)

	got := acceptOn(t, conflictText, 2, "merge_accept_ours").Text()
	if want := "top\n" + after + "\nbottom"; got != want {
		t.Fatalf("accepting wrote %q, the preview promised %q", got, want)
	}
}

func TestConflictPreviewDeclinesOutsideABlock(t *testing.T) {
	m, _ := loaded(t, conflictText)
	m.SetCursor(0, 0)
	if _, _, _, ok := m.ConflictPreviewAtCaret(true, false); ok {
		t.Fatal("line 0 is outside the conflict block")
	}
}
