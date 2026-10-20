package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/search"
)

// pasteprefill_test.go covers #3020: a paste into the search line while its
// prefill (#2826 last query, #2063 visual selection) is still selected
// replaces the prefill like a typed rune does; once the selection is dropped
// the paste inserts at the cursor again.

func TestPasteReplacesSelectedSlashPrefill(t *testing.T) {
	m, _ := loaded(t, "alpha\nfoo one\nbar two\nfoo three\n")
	m = commitSearchLine(m, "foo")
	m.cursor = buffer.Position{Line: 0}
	m = typeKeys(m, "/")
	m.PasteText("bar")
	if m.cmdline != "bar" || m.cmdCur != 3 {
		t.Fatalf("paste over prefill: cmdline=%q cmdCur=%d, want %q 3", m.cmdline, m.cmdCur, "bar")
	}
	if m.cmdSelStart != m.cmdSelEnd {
		t.Fatalf("selection must be dropped after the paste, got [%d,%d)", m.cmdSelStart, m.cmdSelEnd)
	}
	if m.preview.Pattern != "bar" || m.cursor.Line != 2 {
		t.Fatalf("preview=%q on line %d, want bar previewed on line 2", m.preview.Pattern, m.cursor.Line)
	}
}

func TestPasteReplacesSelectedQuestionPrefill(t *testing.T) {
	m, _ := loaded(t, "bar one\nfoo\nalpha\n")
	m = commitSearchLine(m, "foo")
	m.cursor = buffer.Position{Line: 2}
	m = typeKeys(m, "?")
	if m.searchDir != search.Backward {
		t.Fatalf("? should search backward")
	}
	m.PasteText("bar")
	if m.cmdline != "bar" {
		t.Fatalf("paste over ? prefill: cmdline=%q, want %q", m.cmdline, "bar")
	}
	if m.preview.Pattern != "bar" || m.cursor.Line != 0 {
		t.Fatalf("preview=%q on line %d, want bar previewed on line 0", m.preview.Pattern, m.cursor.Line)
	}
}

func TestPasteReplacesVisualSelectionPrefill(t *testing.T) {
	m, _ := loaded(t, "foo bar baz\n")
	m.cursor = buffer.Position{Line: 0, Col: 4}
	m.enterVisual(Visual)
	m.cursor = buffer.Position{Line: 0, Col: 6} // selects "bar"
	m, _ = m.Update(ActionMsg{Action: "find"})
	m.PasteText("baz")
	if m.cmdline != "baz" || m.cmdCur != 3 {
		t.Fatalf("paste over visual prefill: cmdline=%q cmdCur=%d, want %q 3", m.cmdline, m.cmdCur, "baz")
	}
	if m.preview.Pattern != "baz" || m.cursor.Col != 8 {
		t.Fatalf("preview=%q at col %d, want baz previewed at col 8", m.preview.Pattern, m.cursor.Col)
	}
}

func TestPasteAfterDeselectInsertsAtCursor(t *testing.T) {
	m, _ := loaded(t, "foobar\nfoo\n")
	m = commitSearchLine(m, "foo")
	m = typeKeys(m, "/")
	m = send(m, special(tea.KeyLeft)) // drops the selection, cursor between "fo" and "o"
	m.PasteText("X")
	if m.cmdline != "foXo" {
		t.Fatalf("paste after arrow: cmdline=%q, want %q", m.cmdline, "foXo")
	}
	m = send(m, special(tea.KeyEnd))
	m.PasteText("bar")
	if m.cmdline != "foXobar" {
		t.Fatalf("paste at end: cmdline=%q, want %q", m.cmdline, "foXobar")
	}
}

func TestEmptyPasteKeepsSelectedPrefill(t *testing.T) {
	m, _ := loaded(t, "foo\n")
	m = commitSearchLine(m, "foo")
	m = typeKeys(m, "/")
	m.PasteText("\n")
	if m.cmdline != "foo" || m.cmdSelStart != 0 || m.cmdSelEnd != 3 {
		t.Fatalf("empty paste: cmdline=%q sel=[%d,%d), want the prefill kept selected", m.cmdline, m.cmdSelStart, m.cmdSelEnd)
	}
}
