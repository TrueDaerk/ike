package editor

import (
	"strings"
	"testing"
)

func TestExDeleteRange(t *testing.T) {
	m, _ := loaded(t, "l1\nl2\nl3\nl4\nl5\n")
	m.moveTo(pos(4, 0))
	m = runEx(m, "2,4d")
	if got := []string{line(m, 0), line(m, 1)}; got[0] != "l1" || got[1] != "l5" {
		t.Fatalf("after :2,4d lines = %q", got)
	}
	if m.buf.LineCount() != 2 {
		t.Fatalf("line count = %d, want 2", m.buf.LineCount())
	}
	// Cursor on the line that takes the range's place (old l5), first non-blank.
	if m.cursor.Line != 1 {
		t.Fatalf("cursor line = %d, want 1", m.cursor.Line)
	}
	// Deleted text is in the unnamed register (linewise).
	if e := m.regs.Get(0); !e.Linewise || !strings.Contains(e.Text, "l2") {
		t.Fatalf("unnamed register = %+v", e)
	}
}

func TestExDeleteIntoNamedRegister(t *testing.T) {
	m, _ := loaded(t, "a\nb\nc\n")
	m = runEx(m, "1,2d x")
	if e := m.regs.Get('x'); !strings.Contains(e.Text, "a") || !strings.Contains(e.Text, "b") {
		t.Fatalf("register x = %+v", e)
	}
	if line(m, 0) != "c" {
		t.Fatalf("remaining = %q", line(m, 0))
	}
}

func TestExYankLeavesCursorAndBuffer(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree\n")
	m.moveTo(pos(0, 0))
	m = runEx(m, "2,3y")
	// Buffer unchanged, cursor unmoved (vim :y does not move the cursor).
	if line(m, 0) != "one" || m.buf.LineCount() != 3 {
		t.Fatalf(":y must not modify the buffer")
	}
	if m.cursor.Line != 0 {
		t.Fatalf("cursor line = %d, want 0 (unmoved)", m.cursor.Line)
	}
	if e := m.regs.Get(0); !e.Linewise || !strings.Contains(e.Text, "two") || !strings.Contains(e.Text, "three") {
		t.Fatalf("yank register = %+v", e)
	}
}

func TestExIndentRange(t *testing.T) {
	m, _ := loaded(t, "a\nb\nc\n")
	m = runEx(m, "%>")
	for i, want := range []string{"\ta", "\tb", "\tc"} {
		if got := line(m, i); got != want {
			t.Fatalf("line %d = %q want %q", i, got, want)
		}
	}
	// Cursor on the range's last line, first non-blank (after the inserted tab).
	if m.cursor.Line != 2 || m.cursor.Col != 1 {
		t.Fatalf("cursor = (%d,%d), want (2,1)", m.cursor.Line, m.cursor.Col)
	}
}

func TestExDedentRange(t *testing.T) {
	m, _ := loaded(t, "\ta\n\tb\n")
	m = runEx(m, "1,2<")
	if line(m, 0) != "a" || line(m, 1) != "b" {
		t.Fatalf("dedent = %q,%q", line(m, 0), line(m, 1))
	}
}

func TestExIndentRepeated(t *testing.T) {
	m, _ := loaded(t, "x\n")
	m = runEx(m, ">>")
	if got := line(m, 0); got != "\t\tx" {
		t.Fatalf(":>> = %q, want two tabs", got)
	}
}

func TestExRangeOpsSingleUndoUnit(t *testing.T) {
	m, _ := loaded(t, "a\nb\nc\n")
	m = runEx(m, "%>")
	if line(m, 0) != "\ta" {
		t.Fatalf("setup: %q", line(m, 0))
	}
	m = send(m, key('u'))
	for i, want := range []string{"a", "b", "c"} {
		if got := line(m, i); got != want {
			t.Fatalf("after one undo line %d = %q want %q", i, got, want)
		}
	}
}
