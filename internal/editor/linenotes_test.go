package editor

// linenotes_test.go covers the app-fed trailing line notes (#3120): the note
// renders after the line's text (and after a code lens), a set computed at
// another document version is dropped, and an empty set clears.

import (
	"testing"

	ilsp "ike/internal/lsp"
)

func TestLineNotesRenderTrailing(t *testing.T) {
	m, _ := loaded(t, "# @capture id = .id\nGET http://x\n")
	if !m.SetLineNotes(map[int]string{0: "= 42"}, m.DocVersion()) {
		t.Fatal("a set at the current version must be adopted")
	}
	if got, want := firstLine(m), "# @capture id = .id  = 42"; got != want {
		t.Errorf("rendered = %q, want %q", got, want)
	}
	if m.buf.Line(0) != "# @capture id = .id" {
		t.Error("notes are virtual text and must not change the buffer")
	}
	if text, ok := m.LineNoteAt(0); !ok || text != "= 42" {
		t.Errorf("LineNoteAt(0) = %q, %v", text, ok)
	}
	if _, ok := m.LineNoteAt(1); ok {
		t.Error("line 1 carries no note")
	}
	m.SetLineNotes(nil, m.DocVersion()+5)
	if got := firstLine(m); got != "# @capture id = .id" || m.HasLineNotes() {
		t.Errorf("after clear rendered = %q (has=%v)", got, m.HasLineNotes())
	}
}

func TestLineNotesAfterCodeLens(t *testing.T) {
	m, path := loaded(t, "# @capture id = .id\n")
	m, _ = m.Update(lensesMsg(path, ilsp.CodeLens{Line: 0, Title: "lens"}))
	m.SetLineNotes(map[int]string{0: "= 7"}, m.DocVersion())
	if got, want := firstLine(m), "# @capture id = .id  lens  = 7"; got != want {
		t.Errorf("rendered = %q, want %q", got, want)
	}
}

func TestLineNotesStaleVersionDropped(t *testing.T) {
	m, _ := loaded(t, "a\nb\n")
	if m.SetLineNotes(map[int]string{0: "x"}, m.DocVersion()+1) || m.HasLineNotes() {
		t.Fatal("a set computed at another version must be dropped")
	}
}
