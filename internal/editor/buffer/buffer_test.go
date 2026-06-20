package buffer

import "testing"

func TestFromStringTrimsTrailingNewline(t *testing.T) {
	b := FromString("a\nb\nc\n")
	if b.LineCount() != 3 {
		t.Fatalf("LineCount=%d want 3 (%q)", b.LineCount(), b.Lines())
	}
	if b.Line(1) != "b" {
		t.Fatalf("Line(1)=%q want b", b.Line(1))
	}
}

func TestFromStringCRLFAndEmpty(t *testing.T) {
	if got := FromString("a\r\nb").Lines(); len(got) != 2 || got[1] != "b" {
		t.Fatalf("CRLF split=%q", got)
	}
	if got := FromString(""); got.LineCount() != 1 || got.Line(0) != "" {
		t.Fatalf("empty buffer=%q", got.Lines())
	}
}

func TestRuneLenUnicode(t *testing.T) {
	b := FromString("héllo\nwörld")
	if b.RuneLen(0) != 5 {
		t.Fatalf("RuneLen(0)=%d want 5", b.RuneLen(0))
	}
}

func TestClampAndClampCursor(t *testing.T) {
	b := FromString("abc\n\nxy")
	// past-end column allowed by Clamp, pulled back by ClampCursor.
	if p := b.Clamp(Position{0, 99}); p.Col != 3 {
		t.Fatalf("Clamp col=%d want 3", p.Col)
	}
	if p := b.ClampCursor(Position{0, 99}); p.Col != 2 {
		t.Fatalf("ClampCursor col=%d want 2", p.Col)
	}
	// empty line clamps cursor to 0, not -1.
	if p := b.ClampCursor(Position{1, 5}); p.Col != 0 {
		t.Fatalf("empty line cursor col=%d want 0", p.Col)
	}
	if p := b.Clamp(Position{99, 0}); p.Line != 2 {
		t.Fatalf("Clamp line=%d want 2", p.Line)
	}
}

func TestSliceSingleAndMultiLine(t *testing.T) {
	b := FromString("hello\nworld\nfoo")
	if got := b.Slice(NewRange(Position{0, 1}, Position{0, 4})); got != "ell" {
		t.Fatalf("single-line slice=%q want ell", got)
	}
	got := b.Slice(NewRange(Position{0, 2}, Position{2, 1}))
	if got != "llo\nworld\nf" {
		t.Fatalf("multi-line slice=%q", got)
	}
	if got := b.Slice(NewRange(Position{0, 2}, Position{0, 2})); got != "" {
		t.Fatalf("empty slice=%q", got)
	}
}

func TestApplyInsertWithinLine(t *testing.T) {
	b := FromString("ac")
	inv, end := b.Apply(Insert(Position{0, 1}, "b"))
	if b.Line(0) != "abc" {
		t.Fatalf("after insert=%q want abc", b.Line(0))
	}
	if end != (Position{0, 2}) {
		t.Fatalf("end=%v want {0 2}", end)
	}
	// inverse undoes it.
	b.Apply(inv)
	if b.Line(0) != "ac" {
		t.Fatalf("after undo=%q want ac", b.Line(0))
	}
}

func TestApplyInsertNewline(t *testing.T) {
	b := FromString("abcd")
	_, end := b.Apply(Insert(Position{0, 2}, "\n"))
	if b.LineCount() != 2 || b.Line(0) != "ab" || b.Line(1) != "cd" {
		t.Fatalf("split=%q", b.Lines())
	}
	if end != (Position{1, 0}) {
		t.Fatalf("end=%v want {1 0}", end)
	}
}

func TestApplyDeleteAcrossLinesAndUndo(t *testing.T) {
	b := FromString("one\ntwo\nthree")
	inv, end := b.Apply(Delete(NewRange(Position{0, 1}, Position{2, 2})))
	if b.LineCount() != 1 || b.Line(0) != "oree" {
		t.Fatalf("after delete=%q", b.Lines())
	}
	if end != (Position{0, 1}) {
		t.Fatalf("end=%v want {0 1}", end)
	}
	b.Apply(inv)
	if b.LineCount() != 3 || b.Line(1) != "two" {
		t.Fatalf("after undo=%q", b.Lines())
	}
}

func TestApplyReplaceMultilineText(t *testing.T) {
	b := FromString("abc")
	_, end := b.Apply(Edit{Range: NewRange(Position{0, 1}, Position{0, 2}), Text: "X\nY"})
	if b.LineCount() != 2 || b.Line(0) != "aX" || b.Line(1) != "Yc" {
		t.Fatalf("replace=%q", b.Lines())
	}
	if end != (Position{1, 1}) {
		t.Fatalf("end=%v want {1 1}", end)
	}
}
