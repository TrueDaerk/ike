package editor

import (
	"strings"
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// unihint_test.go — invisible/deceptive Unicode (#1654): the render-loop
// placeholders and the language-agnostic note pass.

// TestInvisibleRunesRenderPlaceholders: zero-width characters, NBSP and bidi
// controls draw as their one-cell glyphs — the editor never renders a
// character as nothing.
func TestInvisibleRunesRenderPlaceholders(t *testing.T) {
	m, _ := loaded(t, "a\u200bb\nc\u00a0d\ne\u202ef\ng\u00adh\n")
	view := plainView(m)
	for _, want := range []string{"a∅b", "c⍽d", "e◊f", "g-h"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
}

// TestBOMRendersPlaceholder: an interior BOM (textenc only strips a leading
// one) draws as the zero-width glyph.
func TestBOMRendersPlaceholder(t *testing.T) {
	m, _ := loaded(t, "x\ufeffy\n")
	if view := plainView(m); !strings.Contains(view, "x∅y") {
		t.Errorf("view lacks the BOM placeholder:\n%s", view)
	}
}

// TestPlainBufferGetsUnicodeNotes: a .txt file has no language, yet the parse
// pass still runs and its Unicode findings mark the buffer — gutter severity,
// underline and the note message all present.
func TestPlainBufferGetsUnicodeNotes(t *testing.T) {
	m, _ := loaded(t, "safe\nbad\u200bline\n")
	cmd := m.Reparse()
	if cmd == nil {
		t.Fatal("plain buffers must still schedule the language-agnostic pass")
	}
	msg, ok := cmd().(highlight.SpansMsg)
	if !ok {
		t.Fatalf("parse yielded %T; want SpansMsg", msg)
	}
	mm, _ := m.Update(msg)
	m = mm
	if sev, ok := m.worstSeverityOnLine(1); !ok || sev != lang.NoteWarn {
		t.Errorf("worstSeverityOnLine(1) = %d, %v; want a warning", sev, ok)
	}
	if _, ok := m.worstSeverityOnLine(0); ok {
		t.Error("the clean line must not be marked")
	}
	if n, ok := m.NoteAt(1, 3); !ok || !strings.Contains(n.Message, "zero-width space") {
		t.Errorf("NoteAt(1,3) = %+v, %v; want the zero-width note", n, ok)
	}
}

// TestNotesJoinDiagnosticJump: with no server diagnostics at all, the jump
// walk still lands on note findings (#1654 problems flow).
func TestNotesJoinDiagnosticJump(t *testing.T) {
	m, path := loaded(t, "clean\nx\u202ey\n")
	notes := []lang.Note{{Line: 1, StartCol: 1, EndCol: 2, Severity: lang.NoteWarn, Message: "bidi control character"}}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Notes: notes})
	m = mm
	m.diagnosticJump(true)
	if m.cursor.Line != 1 || m.cursor.Col != 1 {
		t.Errorf("cursor after jump = %d:%d; want 1:1", m.cursor.Line, m.cursor.Col)
	}
}

// TestNotesJoinDiagnosticCounts: the status-line counters include notes.
func TestNotesJoinDiagnosticCounts(t *testing.T) {
	m, path := loaded(t, "x\n")
	notes := []lang.Note{
		{Line: 0, StartCol: 0, EndCol: 1, Severity: lang.NoteWarn, Message: "w"},
		{Line: 0, StartCol: 0, EndCol: 1, Severity: lang.NoteError, Message: "e"},
	}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Notes: notes})
	m = mm
	if e, w := m.DiagnosticCounts(); e != 1 || w != 1 {
		t.Errorf("DiagnosticCounts() = %d, %d; want 1 error, 1 warning", e, w)
	}
}

// TestNotesJoinShowDiagnostics: the caret-line details popup lists notes.
func TestNotesJoinShowDiagnostics(t *testing.T) {
	m, path := loaded(t, "x\u200by\n")
	notes := []lang.Note{{Line: 0, StartCol: 1, EndCol: 2, Severity: lang.NoteWarn, Message: "invisible character: zero-width space (U+200B)"}}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Notes: notes})
	m = mm
	if !m.ShowDiagnostics() {
		t.Fatal("ShowDiagnostics must open for a note-marked line")
	}
	var text strings.Builder
	for _, l := range m.hover.lines {
		text.WriteString(l.text)
	}
	if !strings.Contains(text.String(), "zero-width space") {
		t.Errorf("popup %q lacks the note message", text.String())
	}
}

// TestPureASCIIBufferStaysClean: an all-ASCII buffer produces no Unicode
// findings end-to-end.
func TestPureASCIIBufferStaysClean(t *testing.T) {
	m, _ := loaded(t, "just plain ascii\nnothing to see\n")
	msg := m.Reparse()()
	mm, _ := m.Update(msg)
	m = mm
	for i := 0; i < 2; i++ {
		if _, ok := m.worstSeverityOnLine(i); ok {
			t.Errorf("line %d marked in a pure-ASCII buffer", i)
		}
	}
	if e, w := m.DiagnosticCounts(); e != 0 || w != 0 {
		t.Errorf("counts = %d, %d; want 0, 0", e, w)
	}
}
