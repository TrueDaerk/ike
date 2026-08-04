package editor

import (
	"testing"

	"ike/internal/lang"
)

// TestPasteTextInsertModeBlock verifies a bracketed paste in insert mode splices
// the whole block at the cursor in one edit (#603).
func TestPasteTextInsertModeBlock(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	m = typeKeys(m, "i") // insert at start
	m.PasteText("XY\nZ")
	if line(m, 0) != "XY" || line(m, 1) != "Zabc" {
		t.Fatalf("insert paste = %q", m.buf.Lines())
	}
}

// TestPasteTextNormalModeBlock verifies a normal-mode bracketed paste inserts the
// block after the cursor (like p) rather than character by character.
func TestPasteTextNormalModeBlock(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	m.PasteText("XYZ")
	if line(m, 0) != "aXYZbc" {
		t.Fatalf("normal paste = %q", line(m, 0))
	}
}

// TestPasteTextIsOneUndoUnit verifies a whole pasted block is removed by a single
// undo — not one undo step per character.
func TestPasteTextIsOneUndoUnit(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	m.PasteText("hello world block")
	if line(m, 0) == "abc" {
		t.Fatal("paste did not insert")
	}
	m = typeKeys(m, "u") // single undo
	if line(m, 0) != "abc" {
		t.Fatalf("one undo did not remove the whole paste: %q", line(m, 0))
	}
}

// TestPasteTextVisualReplace verifies a bracketed paste over a visual selection
// replaces it in one edit.
func TestPasteTextVisualReplace(t *testing.T) {
	m, _ := loaded(t, "abcdef\n")
	m = typeKeys(m, "vll") // select a, b, c
	m.PasteText("XY")
	if line(m, 0) != "XYdef" {
		t.Fatalf("visual paste replace = %q", line(m, 0))
	}
}

// TestPasteTextEmptyNoop verifies an empty paste changes nothing.
func TestPasteTextEmptyNoop(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	m.PasteText("")
	if line(m, 0) != "abc" || m.Dirty() {
		t.Fatalf("empty paste mutated: %q dirty=%v", line(m, 0), m.Dirty())
	}
}

// TestPasteTextSchedulesReparse guards #1491: a bracketed paste bypasses the
// Update loop's maybeReparse, so PasteText itself must return the parse
// command that refreshes highlighting — otherwise the pasted region renders
// stale until the next keystroke.
func TestPasteTextSchedulesReparse(t *testing.T) {
	lang.Register(lang.Language{ID: "pbtest", Extensions: []string{"pbtest"}, Grammar: struct{}{}})
	m := loadedExt(t, "pbtest", "abc\n")
	if cmd := m.PasteText("XYZ"); cmd == nil {
		t.Fatal("a paste that changed a highlighted buffer must schedule a reparse")
	}
	// An empty paste leaves the buffer untouched — no parse to schedule.
	if cmd := m.PasteText(""); cmd != nil {
		t.Fatal("an unchanged buffer must not schedule a reparse")
	}
}

// TestPasteTextNoReparseWithoutGrammar: a paste into a plain-text buffer
// returns no command — there is nothing to highlight (#1491).
func TestPasteTextNoReparseWithoutGrammar(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	if cmd := m.PasteText("XYZ"); cmd != nil {
		t.Fatal("no grammar means no parse command")
	}
}
