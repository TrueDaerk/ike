package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/lang"
)

func init() {
	// Private test languages so indent tests never depend on the compiled-in
	// language plugins (which live under plugins/ and are not imported here).
	// "itest" mimics Python (colon opener), "itestb" a brace language.
	lang.Register(lang.Language{ID: "itest", Extensions: []string{"itest"}, IndentAfter: []string{":"}})
	lang.Register(lang.Language{ID: "itestb", Extensions: []string{"itestb"}, IndentAfter: []string{"{", "(", "["}})
	lang.Register(lang.Language{ID: "itest-none", Extensions: []string{"itestnone"}})
}

// insertEnterAtEOL enters insert mode at the end of the given line and presses
// Enter, returning the model for inspection.
func insertEnterAtEOL(m Model, lineIdx int) Model {
	m.cursor.Line = lineIdx
	m = send(m, key('A'), special(tea.KeyEnter))
	return m
}

func TestEnterIndentsAfterOpener(t *testing.T) {
	m := loadedExt(t, "itest", "def m():\n")
	m.autoIndent = true
	m.useSpaces = true
	m = insertEnterAtEOL(m, 0)
	if got := line(m, 1); got != "    " {
		t.Fatalf("opener must deepen one level, line 1 = %q", got)
	}
	if m.cursor.Line != 1 || m.cursor.Col != 4 {
		t.Fatalf("cursor = %+v", m.cursor)
	}
}

func TestEnterKeepsIndentWithoutOpener(t *testing.T) {
	m := loadedExt(t, "itest", "    z = f(x) + 1\n")
	m.autoIndent = true
	m.useSpaces = true
	m = insertEnterAtEOL(m, 0)
	if got := line(m, 1); got != "    " {
		t.Fatalf("no opener must copy indent, line 1 = %q", got)
	}
}

func TestEnterNestedOpenerAddsToCurrentIndent(t *testing.T) {
	m := loadedExt(t, "itest", "    def my_fn():\n")
	m.autoIndent = true
	m.useSpaces = true
	m = insertEnterAtEOL(m, 0)
	if got := line(m, 1); got != "        " {
		t.Fatalf("nested opener must indent to 8, line 1 = %q", got)
	}
}

func TestEnterBraceOpener(t *testing.T) {
	m := loadedExt(t, "itestb", "func f() {\n")
	m.autoIndent = true
	m.useSpaces = true
	m = insertEnterAtEOL(m, 0)
	if got := line(m, 1); got != "    " {
		t.Fatalf("brace opener must deepen one level, line 1 = %q", got)
	}
}

func TestEnterMidLineSplitUsesLeftOfCursor(t *testing.T) {
	// Cursor after the colon of "if x:" with trailing "pass" on the same line:
	// the split indents by what stays on the line (which ends with the opener).
	m := loadedExt(t, "itest", "if x:pass\n")
	m.autoIndent = true
	m.useSpaces = true
	m = send(m, keys("05l")...) // cursor on 'p' (col 5)
	m = send(m, key('i'), special(tea.KeyEnter))
	if got := line(m, 0); got != "if x:" {
		t.Fatalf("line 0 = %q", got)
	}
	if got := line(m, 1); got != "    pass" {
		t.Fatalf("split must indent the carried text, line 1 = %q", got)
	}
}

func TestEnterUnknownLanguageCopiesIndent(t *testing.T) {
	m := loadedExt(t, "itestnone", "  block:\n")
	m.autoIndent = true
	m.useSpaces = true
	m = insertEnterAtEOL(m, 0)
	if got := line(m, 1); got != "  " {
		t.Fatalf("no rules must mean plain copy-indent, line 1 = %q", got)
	}
}

func TestOpenBelowIndentsAfterOpener(t *testing.T) {
	m := loadedExt(t, "itest", "def m():\n")
	m.autoIndent = true
	m.useSpaces = true
	m = send(m, key('o'))
	if got := line(m, 1); got != "    " {
		t.Fatalf("o after opener must deepen, line 1 = %q", got)
	}
	// O (open above) keeps plain copy-indent of the current line.
	m = send(m, special(tea.KeyEscape))
	m.cursor = m.buf.Clamp(m.cursor)
	m = send(m, key('O'))
	if got := line(m, 1); got != "    " {
		t.Fatalf("O must copy indent, not deepen, line 1 = %q", got)
	}
}

func TestEnterSmartIndentIsOneUndoUnit(t *testing.T) {
	m := loadedExt(t, "itest", "def m():\n")
	m.autoIndent = true
	m.useSpaces = true
	m = insertEnterAtEOL(m, 0)
	m = send(m, keys("pass")...)
	m = send(m, special(tea.KeyEscape), key('u'))
	if got := line(m, 0); got != "def m():" || m.buf.LineCount() > 2 {
		t.Fatalf("undo must revert the whole insert: %q lines=%d", got, m.buf.LineCount())
	}
}
