package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
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

// shiftTab is the Shift+Tab key press as the Kitty/legacy decoders deliver it.
func shiftTab() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift} }

func TestShiftTabDedentsWholeLine(t *testing.T) {
	// Cursor mid-word on a written line: the whole line shifts left one unit.
	m := loadedExt(t, "itest", "        pass\n")
	m.autoIndent = true
	m.useSpaces = true
	m = send(m, key('A'), shiftTab())
	if got := line(m, 0); got != "    pass" {
		t.Fatalf("dedent: %q", got)
	}
	if m.cursor.Col != 8 { // was 12 (one past end), minus 4 removed columns
		t.Fatalf("cursor must follow the removed columns, col=%d", m.cursor.Col)
	}
	// A second Shift+Tab reaches column 0; a third is a no-op.
	m = send(m, shiftTab(), shiftTab())
	if got := line(m, 0); got != "pass" {
		t.Fatalf("dedent to column 0: %q", got)
	}
}

func TestShiftTabLeadingTabAndNoop(t *testing.T) {
	m := loadedExt(t, "itest", "\tx\n")
	m = send(m, key('i'), shiftTab())
	if got := line(m, 0); got != "x" {
		t.Fatalf("a leading tab is one unit: %q", got)
	}
	if m.cursor.Col != 0 {
		t.Fatalf("cursor clamps to 0, col=%d", m.cursor.Col)
	}
	m = send(m, shiftTab())
	if got := line(m, 0); got != "x" {
		t.Fatalf("no leading whitespace must be a no-op: %q", got)
	}
}

func TestShiftTabWhitespaceOnlyLine(t *testing.T) {
	m := loadedExt(t, "itest", "        \n")
	m.useSpaces = true
	m = send(m, key('A'), shiftTab())
	if got := line(m, 0); got != "    " {
		t.Fatalf("whitespace-only line dedents too: %q", got)
	}
}

func TestShiftTabInsideInsertUndoUnit(t *testing.T) {
	m := loadedExt(t, "itest", "    x\n")
	m.useSpaces = true
	m = send(m, key('A'), key('y'), shiftTab(), key('z'))
	if got := line(m, 0); got != "xyz" {
		t.Fatalf("edit result: %q", got)
	}
	m = send(m, special(tea.KeyEscape), key('u'))
	if got := line(m, 0); got != "    x" {
		t.Fatalf("one undo must revert the whole insert incl. dedent: %q", got)
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

// blockSplitModel loads a brace-language buffer with the caret placed between
// the pair at line/col, in insert mode, ready for Enter (#518).
func blockSplitModel(t *testing.T, ext, content string, col int) Model {
	t.Helper()
	m := loadedExt(t, ext, content)
	m.autoIndent = true
	m.useSpaces = true
	m.cursor = buffer.Position{Line: 0, Col: col}
	return m
}

func TestEnterBetweenBracesOpensBlock(t *testing.T) {
	m := blockSplitModel(t, "itestb", "function abc() {}\n", 16)
	m = send(m, key('i'), special(tea.KeyEnter))
	if got := line(m, 0); got != "function abc() {" {
		t.Fatalf("line 0 = %q", got)
	}
	if got := line(m, 1); got != "    " {
		t.Fatalf("middle line = %q, want one deepened indent", got)
	}
	if got := line(m, 2); got != "}" {
		t.Fatalf("closer line = %q, want %q", got, "}")
	}
	if m.cursor.Line != 1 || m.cursor.Col != 4 {
		t.Fatalf("cursor = %+v, want end of middle line", m.cursor)
	}
}

func TestEnterBetweenBracesKeepsOuterIndent(t *testing.T) {
	m := blockSplitModel(t, "itestb", "    if (x) {}\n", 12)
	m = send(m, key('i'), special(tea.KeyEnter))
	if got := line(m, 1); got != "        " {
		t.Fatalf("middle line = %q, want indent deepened from 4 to 8", got)
	}
	if got := line(m, 2); got != "    }" {
		t.Fatalf("closer line = %q, want closer at the reference indent", got)
	}
}

func TestEnterBetweenBracesMidLine(t *testing.T) {
	m := blockSplitModel(t, "itestb", "foo({})\n", 5)
	m = send(m, key('i'), special(tea.KeyEnter))
	if got := line(m, 0); got != "foo({" {
		t.Fatalf("line 0 = %q", got)
	}
	if got := line(m, 2); got != "})" {
		t.Fatalf("closer line = %q, want %q — text right of the pair follows", got, "})")
	}
}

func TestEnterBetweenBracesPlainTextCopiesIndent(t *testing.T) {
	m := blockSplitModel(t, "itestnone", "  {}\n", 3)
	m = send(m, key('i'), special(tea.KeyEnter))
	if got := line(m, 1); got != "  " {
		t.Fatalf("middle line = %q, want plain copy-indent without lang rules", got)
	}
	if got := line(m, 2); got != "  }" {
		t.Fatalf("closer line = %q", got)
	}
}

func TestEnterMismatchedPairSplitsNormally(t *testing.T) {
	m := blockSplitModel(t, "itestb", "(]\n", 1)
	m = send(m, key('i'), special(tea.KeyEnter))
	if m.buf.LineCount() != 2 { // "(", "    ]"
		t.Fatalf("lines = %d, want a plain split", m.buf.LineCount())
	}
	if got := line(m, 1); got != "    ]" {
		t.Fatalf("line 1 = %q, want the closer to stay on the cursor line", got)
	}
}

func TestEnterBlockSplitNeedsAutoIndent(t *testing.T) {
	m := blockSplitModel(t, "itestb", "{}\n", 1)
	m.autoIndent = false
	m = send(m, key('i'), special(tea.KeyEnter))
	if got := line(m, 1); got != "}" {
		t.Fatalf("line 1 = %q — auto_indent off must keep the plain split", got)
	}
}

func TestEnterBlockSplitIsOneUndoUnit(t *testing.T) {
	m := blockSplitModel(t, "itestb", "{}\n", 1)
	m = send(m, key('i'), special(tea.KeyEnter))
	m = send(m, keys("x")...)
	m = send(m, special(tea.KeyEscape), key('u'))
	if got := line(m, 0); got != "{}" || m.buf.LineCount() > 2 {
		t.Fatalf("undo must revert the whole insert: %q lines=%d", got, m.buf.LineCount())
	}
}

func TestEnterBlockSplitMultiCaret(t *testing.T) {
	m := blockSplitModel(t, "itestb", "a {}\nb {}\n", 3)
	m.addCaret(buffer.Position{Line: 1, Col: 3})
	m = send(m, key('i'), special(tea.KeyEnter))
	if got := line(m, 2); got != "}" {
		t.Fatalf("first closer line = %q", got)
	}
	if got := line(m, 3); got != "b {" || line(m, 5) != "}" {
		t.Fatalf("second block wrong: %q / %q", line(m, 3), line(m, 5))
	}
}
