package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestApplyTextEditsAmendOneUndoUnit: the save chain's format step amends the
// organize-imports change (#2253), so a single undo reverts both rewrites.
func TestApplyTextEditsAmendOneUndoUnit(t *testing.T) {
	orig := "import a\ncode"
	m, _ := loaded(t, orig)

	m.ApplyTextEdits([]TextEdit{{StartLine: 0, StartCol: 0, EndLine: 1, EndCol: 0, Text: ""}}) // organize: drop the import
	if m.Text() != "code" {
		t.Fatalf("after organize: text = %q", m.Text())
	}
	if n := m.ApplyTextEditsAmend([]TextEdit{{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 4, Text: "CODE"}}); n != 1 {
		t.Fatalf("amended edits applied = %d, want 1", n)
	}
	if m.Text() != "CODE" {
		t.Fatalf("after format: text = %q", m.Text())
	}

	m = send(m, key('u'))
	if m.Text() != orig {
		t.Fatalf("one undo must revert organize+format, got %q", m.Text())
	}
	m = send(m, key('u'))
	if m.Text() != orig {
		t.Fatalf("there must be nothing left to undo, got %q", m.Text())
	}
}

// TestApplyTextEditsAmendWithoutAnchorPushes: an amend with no preceding
// ApplyTextEdits (or after the user typed in between) must still apply — as
// its own undo unit rather than merging into an unrelated change.
func TestApplyTextEditsAmendWithoutAnchorPushes(t *testing.T) {
	m, _ := loaded(t, "abc")
	m.ApplyTextEditsAmend([]TextEdit{{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 3, Text: "xyz"}})
	if m.Text() != "xyz" {
		t.Fatalf("text = %q", m.Text())
	}
	m = send(m, key('u'))
	if m.Text() != "abc" {
		t.Fatalf("orphan amend must undo on its own, got %q", m.Text())
	}
}

// TestApplyTextEditsAmendAfterTypingStaysSeparate: a keystroke between the two
// chain steps moves the history away from the anchor, so the format edit must
// not swallow the user's change.
func TestApplyTextEditsAmendAfterTypingStaysSeparate(t *testing.T) {
	m, _ := loaded(t, "abc")
	m.ApplyTextEdits([]TextEdit{{StartLine: 0, StartCol: 3, EndLine: 0, EndCol: 3, Text: "d"}})
	m = send(m, key('i'))
	m = send(m, key('Z'))
	m = send(m, special(tea.KeyEscape))
	typed := m.Text()

	m.ApplyTextEditsAmend([]TextEdit{{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 1, Text: "A"}})
	m = send(m, key('u'))
	if m.Text() != typed {
		t.Fatalf("undo must revert only the amended edit, got %q, want %q", m.Text(), typed)
	}
}
