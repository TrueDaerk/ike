package editor

// Tests for stepping the *open* search line's preview with cmd+g /
// cmd+shift+g (#2603): the line stays open while the preview walks the
// matches of the half-typed pattern, Enter commits where the step landed and
// Esc still returns to the search origin.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// stepEditor opens "/" over a three-match buffer with the cursor at the top.
func stepEditor(t *testing.T) Model {
	t.Helper()
	m, _ := loaded(t, "alpha\nfoo one\nbar\nfoo two\nbaz\nfoo three\n")
	m = send(m, key('/'))
	return typeKeys(m, "foo")
}

func TestSearchStepNextMovesPreviewAndKeepsLine(t *testing.T) {
	m := stepEditor(t)
	if m.cursor.Line != 1 {
		t.Fatalf("preview line=%d want 1", m.cursor.Line)
	}
	st := m.StepSearchPreview(false)
	if !st.Handled || st.Total != 3 || st.Index != 2 || st.Wrapped {
		t.Fatalf("step = %+v want handled 2/3 unwrapped", st)
	}
	if m.cursor.Line != 3 {
		t.Fatalf("after cmd+g line=%d want 3", m.cursor.Line)
	}
	// The line stays open, with its text and cursor intact.
	if m.mode != Command || !m.searching {
		t.Fatal("stepping must leave the search line open")
	}
	if m.cmdline != "foo" || m.cmdCur != 3 {
		t.Fatalf("cmdline=%q cmdCur=%d want %q 3", m.cmdline, m.cmdCur, "foo")
	}
	m.StepSearchPreview(false)
	if m.cursor.Line != 5 {
		t.Fatalf("second cmd+g line=%d want 5", m.cursor.Line)
	}
}

func TestSearchStepPrevWalksBackwards(t *testing.T) {
	m := stepEditor(t)
	m.StepSearchPreview(false) // line 3
	st := m.StepSearchPreview(true)
	if !st.Handled || st.Index != 1 || st.Total != 3 || st.Wrapped {
		t.Fatalf("prev step = %+v want handled 1/3 unwrapped", st)
	}
	if m.cursor.Line != 1 {
		t.Fatalf("cmd+shift+g line=%d want 1", m.cursor.Line)
	}
}

func TestSearchStepWrapsAround(t *testing.T) {
	m := stepEditor(t)
	m.StepSearchPreview(false)
	m.StepSearchPreview(false) // last match, line 5
	st := m.StepSearchPreview(false)
	if !st.Wrapped || st.Index != 1 {
		t.Fatalf("wrap step = %+v want wrapped 1/3", st)
	}
	if m.cursor.Line != 1 {
		t.Fatalf("wrapped line=%d want 1", m.cursor.Line)
	}
	if m.cmdMsg != "search wrapped" {
		t.Fatalf("cmdMsg=%q want the wrap hint", m.cmdMsg)
	}
	// Backwards off the first match wraps to the last.
	st = m.StepSearchPreview(true)
	if !st.Wrapped || m.cursor.Line != 5 {
		t.Fatalf("backward wrap: %+v line=%d want wrapped line 5", st, m.cursor.Line)
	}
}

func TestSearchStepCommitKeepsSteppedMatch(t *testing.T) {
	m := stepEditor(t)
	m.StepSearchPreview(false) // line 3
	m = send(m, special(tea.KeyEnter))
	if m.mode != Normal || m.searching {
		t.Fatal("enter must close the search line")
	}
	if m.cursor.Line != 3 {
		t.Fatalf("committed cursor line=%d want the stepped match on 3", m.cursor.Line)
	}
	if m.query.Pattern != "foo" {
		t.Fatalf("query=%q want %q", m.query.Pattern, "foo")
	}
	// n continues from there, not from the origin.
	m = send(m, key('n'))
	if m.cursor.Line != 5 {
		t.Fatalf("n after a stepped commit: line=%d want 5", m.cursor.Line)
	}
}

func TestSearchStepCancelRestoresOrigin(t *testing.T) {
	m, _ := loaded(t, "alpha\nfoo one\nbar\nfoo two\nbaz\nfoo three\n")
	m = send(m, key('j'), key('j')) // origin: line 2
	origin := m.cursor
	m = send(m, key('/'))
	m = typeKeys(m, "foo")
	m.StepSearchPreview(false)
	m.StepSearchPreview(false)
	m = send(m, special(tea.KeyEsc))
	if m.cursor != origin {
		t.Fatalf("esc cursor=%v want the origin %v", m.cursor, origin)
	}
	if m.view.Top != 0 {
		t.Fatalf("esc viewport top=%d want 0", m.view.Top)
	}
}

func TestSearchStepThenEditPreviewsFromOrigin(t *testing.T) {
	m := stepEditor(t)
	m.StepSearchPreview(false)
	m.StepSearchPreview(false) // line 5
	m = typeKeys(m, " ")       // pattern "foo " — previews from the origin again
	if m.searchStepped {
		t.Fatal("editing the pattern must drop the stepped preview")
	}
	if m.cursor.Line != 1 {
		t.Fatalf("re-preview line=%d want the first match from the origin (1)", m.cursor.Line)
	}
	// Enter now commits the first match from the origin as before.
	m = send(m, special(tea.KeyEnter))
	if m.cursor.Line != 1 {
		t.Fatalf("commit after re-preview: line=%d want 1", m.cursor.Line)
	}
}

func TestSearchStepWithoutOpenLineIsNoStep(t *testing.T) {
	m, _ := loaded(t, "foo\nfoo\n")
	if st := m.StepSearchPreview(false); st.Handled {
		t.Fatalf("closed line = %+v want NoStep so cmd+g keeps its old meaning", st)
	}
}

func TestSearchStepWithoutMatchesIsHandled(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\n")
	m = send(m, key('/'))
	m = typeKeys(m, "zzz")
	st := m.StepSearchPreview(false)
	if !st.Handled || st.Total != 0 {
		t.Fatalf("no-match step = %+v want handled with no matches", st)
	}
	if m.cursor.Line != 0 || m.cursor.Col != 0 {
		t.Fatalf("no-match step moved the cursor to %v", m.cursor)
	}
}
