package editor

import (
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/editor/register"
	"ike/internal/host"
)

// yankLines puts a linewise block into the unnamed register.
func yankLines(m *Model, text string) {
	m.regs.Yank('"', register.Entry{Text: text, Linewise: true})
}

// TestSmartPasteDeeperToShallower re-indents a block yanked from a deeper
// nesting level to the target line's indentation, keeping relative structure.
func TestSmartPasteDeeperToShallower(t *testing.T) {
	m, _ := loadedWith(t, host.MapConfig{"editor.use_spaces": "true"}, "f.txt", "top\nend\n")
	yankLines(&m, "        if x {\n            y()\n        }\n")
	m = typeKeys(m, "p") // paste below "top" (indent "")
	if line(m, 1) != "if x {" || line(m, 2) != "    y()" || line(m, 3) != "}" {
		t.Fatalf("smart paste = %q", m.buf.Lines())
	}
}

// TestSmartPasteShallowerToDeeper applies the deeper target indentation.
func TestSmartPasteShallowerToDeeper(t *testing.T) {
	m, _ := loadedWith(t, host.MapConfig{"editor.use_spaces": "true"}, "f.txt", "    inner\nend\n")
	yankLines(&m, "a()\n    b()\n")
	m = typeKeys(m, "p") // target line "    inner" → indent "    "
	if line(m, 1) != "    a()" || line(m, 2) != "        b()" {
		t.Fatalf("smart paste = %q", m.buf.Lines())
	}
}

// TestSmartPasteBeforeP re-indents P (paste above) the same way.
func TestSmartPasteBeforeP(t *testing.T) {
	m, _ := loadedWith(t, host.MapConfig{"editor.use_spaces": "true"}, "f.txt", "    inner\nend\n")
	yankLines(&m, "a()\n    b()\n")
	m = typeKeys(m, "P")
	if line(m, 0) != "    a()" || line(m, 1) != "        b()" {
		t.Fatalf("smart paste P = %q", m.buf.Lines())
	}
}

// TestSmartPasteMixedTabsToSpaces converts the block's relative tab indent
// into the buffer's space style (editor.use_spaces + tab width).
func TestSmartPasteMixedTabsToSpaces(t *testing.T) {
	m, _ := loadedWith(t, host.MapConfig{"editor.use_spaces": "true", "editor.tab_width": "4"}, "f.txt", "  ctx\nend\n")
	yankLines(&m, "\tfirst\n\t\tsecond\n")
	m = typeKeys(m, "p")
	if line(m, 1) != "  first" || line(m, 2) != "  "+"    second" {
		t.Fatalf("tab block into space buffer = %q", m.buf.Lines())
	}
}

// TestSmartPasteSpacesToTabs re-emits the relative indent with tabs when the
// buffer indents with tabs.
func TestSmartPasteSpacesToTabs(t *testing.T) {
	m, _ := loadedWith(t, host.MapConfig{"editor.use_spaces": "false", "editor.tab_width": "4"}, "f.txt", "\tctx\nend\n")
	yankLines(&m, "one\n    two\n")
	m = typeKeys(m, "p")
	if line(m, 1) != "\tone" || line(m, 2) != "\t\ttwo" {
		t.Fatalf("space block into tab buffer = %q", m.buf.Lines())
	}
}

// TestSmartPasteSingleLineUnchanged leaves single-line pastes alone.
func TestSmartPasteSingleLineUnchanged(t *testing.T) {
	m, _ := loadedWith(t, host.MapConfig{"editor.use_spaces": "true"}, "f.txt", "    inner\nend\n")
	yankLines(&m, "        deep()\n")
	m = typeKeys(m, "p")
	if line(m, 1) != "        deep()" {
		t.Fatalf("single-line paste must stay verbatim, got %q", line(m, 1))
	}
}

// TestSmartPasteOptOut disables the transform via editor.smart_paste.
func TestSmartPasteOptOut(t *testing.T) {
	m, _ := loadedWith(t, host.MapConfig{"editor.smart_paste": "false"}, "f.txt", "top\nend\n")
	yankLines(&m, "        a\n        b\n")
	m = typeKeys(m, "p")
	if line(m, 1) != "        a" || line(m, 2) != "        b" {
		t.Fatalf("opt-out paste must stay verbatim, got %q", m.buf.Lines())
	}
}

// TestSmartPasteOneUndoUnit removes the whole re-indented paste with one undo.
func TestSmartPasteOneUndoUnit(t *testing.T) {
	m, _ := loaded(t, "top\nend\n")
	yankLines(&m, "    a\n        b\n")
	m = typeKeys(m, "p")
	if m.buf.LineCount() != 4 {
		t.Fatalf("paste did not insert: %q", m.buf.Lines())
	}
	m = typeKeys(m, "u")
	if m.buf.LineCount() != 2 || line(m, 0) != "top" {
		t.Fatalf("one undo did not remove the paste: %q", m.buf.Lines())
	}
}

// TestSmartPasteBlankTargetUsesContextAbove pastes on a blank line and takes
// the indentation of the nearest non-blank line above.
func TestSmartPasteBlankTargetUsesContextAbove(t *testing.T) {
	m, _ := loadedWith(t, host.MapConfig{"editor.use_spaces": "true"}, "f.txt", "    ctx\n\nend\n")
	yankLines(&m, "a\n    b\n")
	m = typeKeys(m, "jp") // cursor on the blank line, paste below it
	if line(m, 2) != "    a" || line(m, 3) != "        b" {
		t.Fatalf("blank-target paste = %q", m.buf.Lines())
	}
}

// TestSmartPasteBlankLinesStayEmpty keeps blank block lines free of trailing
// indentation.
func TestSmartPasteBlankLinesStayEmpty(t *testing.T) {
	m, _ := loaded(t, "    ctx\nend\n")
	yankLines(&m, "a\n\nb\n")
	m = typeKeys(m, "p")
	if line(m, 1) != "    a" || line(m, 2) != "" || line(m, 3) != "    b" {
		t.Fatalf("blank line handling = %q", m.buf.Lines())
	}
}

// TestSmartPasteRegisters works from a named register.
func TestSmartPasteRegisters(t *testing.T) {
	m, _ := loadedWith(t, host.MapConfig{"editor.use_spaces": "true"}, "f.txt", "    inner\nend\n")
	m.regs.Yank('a', register.Entry{Text: "x\n    y\n", Linewise: true})
	m = typeKeys(m, "\"ap")
	if line(m, 1) != "    x" || line(m, 2) != "        y" {
		t.Fatalf("register paste = %q", m.buf.Lines())
	}
}

// TestSmartPasteInsertModeContinuation re-indents the continuation lines of a
// clipboard paste mid-insert; the first line splices verbatim at the cursor.
func TestSmartPasteInsertModeContinuation(t *testing.T) {
	m, _ := loaded(t, "    ctx\nend\n")
	m.regs.Yank('+', register.Entry{Text: "first\n        second"})
	m = typeKeys(m, "A") // insert at end of "    ctx"
	m.clipboardPaste()
	if line(m, 0) != "    ctxfirst" || line(m, 1) != "    second" {
		t.Fatalf("insert-mode smart paste = %q", m.buf.Lines())
	}
}

// TestPasteTextStaysVerbatim keeps terminal bracketed paste untouched: no
// re-indentation ever.
func TestPasteTextStaysVerbatim(t *testing.T) {
	m, _ := loaded(t, "    ctx\nend\n")
	m = typeKeys(m, "A")
	m.PasteText("a\n        b")
	if line(m, 0) != "    ctxa" || line(m, 1) != "        b" {
		t.Fatalf("bracketed paste must stay verbatim: %q", m.buf.Lines())
	}
}

// TestSmartPasteMultiCaret re-indents per caret, each to its own target line.
func TestSmartPasteMultiCaret(t *testing.T) {
	m, _ := loaded(t, "top\n        deep\n")
	yankLines(&m, "    a\n        b\n")
	m.addCaret(buffer.Position{Line: 1, Col: 0}) // second caret on the deeper line
	m.paste('"', true, 1, false)
	found := 0
	for i := 0; i < m.buf.LineCount(); i++ {
		switch line(m, i) {
		case "a", "        a":
			found++
		}
	}
	if found != 2 {
		t.Fatalf("per-caret indents wrong: %q", m.buf.Lines())
	}
}
