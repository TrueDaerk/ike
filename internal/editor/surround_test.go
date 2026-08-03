package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
)

// --- add (ys / visual S) ---------------------------------------------------

func TestSurroundAddObjectClosing(t *testing.T) {
	m, _ := loaded(t, "foo bar")
	m = typeKeys(m, "ysiw)")
	if got := line(m, 0); got != "(foo) bar" {
		t.Fatalf("ysiw) got %q", got)
	}
	if m.cursor != (buffer.Position{Line: 0, Col: 0}) {
		t.Fatalf("cursor must land on the opening delimiter, at %v", m.cursor)
	}
}

func TestSurroundAddObjectOpeningPads(t *testing.T) {
	m, _ := loaded(t, "foo")
	m = typeKeys(m, "ysiw(")
	if got := line(m, 0); got != "( foo )" {
		t.Fatalf("ysiw( got %q", got)
	}
}

func TestSurroundAddQuote(t *testing.T) {
	m, _ := loaded(t, "foo")
	m = typeKeys(m, `ysiw"`)
	if got := line(m, 0); got != `"foo"` {
		t.Fatalf("ysiw\" got %q", got)
	}
}

func TestSurroundAddMotion(t *testing.T) {
	m, _ := loaded(t, "foo bar")
	m = typeKeys(m, "yse]")
	if got := line(m, 0); got != "[foo] bar" {
		t.Fatalf("yse] got %q", got)
	}
}

func TestSurroundAddWholeLine(t *testing.T) {
	m, _ := loaded(t, "  foo bar")
	m = typeKeys(m, "yss}")
	if got := line(m, 0); got != "  {foo bar}" {
		t.Fatalf("yss} must wrap from the first non-blank, got %q", got)
	}
}

func TestSurroundVisual(t *testing.T) {
	m, _ := loaded(t, "foo bar")
	m = typeKeys(m, `viwS"`)
	if got := line(m, 0); got != `"foo" bar` {
		t.Fatalf("viwS\" got %q", got)
	}
	if m.mode != Normal {
		t.Fatalf("S must leave visual mode, in %v", m.mode)
	}
}

func TestSurroundAddCancelledByEscape(t *testing.T) {
	m, _ := loaded(t, "foo")
	m = typeKeys(m, "ys")
	m = send(m, special(tea.KeyEscape))
	m = typeKeys(m, "w")
	if got := line(m, 0); got != "foo" {
		t.Fatalf("cancelled surround must not edit, got %q", got)
	}
}

// --- change (cs) -----------------------------------------------------------

func TestSurroundChangeQuotes(t *testing.T) {
	m, _ := loaded(t, `say "hi" now`)
	m = typeKeys(m, "fh") // cursor inside the quotes
	m = typeKeys(m, `cs"'`)
	if got := line(m, 0); got != "say 'hi' now" {
		t.Fatalf("cs\"' got %q", got)
	}
}

func TestSurroundChangeBracketToQuote(t *testing.T) {
	m, _ := loaded(t, "a (hi) b")
	m = typeKeys(m, "fh")
	m = typeKeys(m, `cs)"`)
	if got := line(m, 0); got != `a "hi" b` {
		t.Fatalf("cs)\" got %q", got)
	}
}

func TestSurroundChangeClosingKeepsPadding(t *testing.T) {
	m, _ := loaded(t, "( hi )")
	m = typeKeys(m, "fh")
	m = typeKeys(m, "cs)]")
	if got := line(m, 0); got != "[ hi ]" {
		t.Fatalf("cs)] got %q", got)
	}
}

func TestSurroundChangeOpeningStripsPadding(t *testing.T) {
	m, _ := loaded(t, "( hi )")
	m = typeKeys(m, "fh")
	m = typeKeys(m, `cs("`)
	if got := line(m, 0); got != `"hi"` {
		t.Fatalf("cs(\" got %q", got)
	}
}

func TestSurroundChangeToOpeningAddsPadding(t *testing.T) {
	m, _ := loaded(t, `"hi"`)
	m = typeKeys(m, "fh")
	m = typeKeys(m, `cs"{`)
	if got := line(m, 0); got != "{ hi }" {
		t.Fatalf("cs\"{ got %q", got)
	}
}

// --- delete (ds) -----------------------------------------------------------

func TestSurroundDeleteQuotes(t *testing.T) {
	m, _ := loaded(t, `say "hi" now`)
	m = typeKeys(m, "fh")
	m = typeKeys(m, `ds"`)
	if got := line(m, 0); got != "say hi now" {
		t.Fatalf("ds\" got %q", got)
	}
}

func TestSurroundDeleteClosingKeepsPadding(t *testing.T) {
	m, _ := loaded(t, "a( x )b")
	m = typeKeys(m, "fx")
	m = typeKeys(m, "ds)")
	if got := line(m, 0); got != "a x b" {
		t.Fatalf("ds) got %q", got)
	}
}

func TestSurroundDeleteOpeningStripsPadding(t *testing.T) {
	m, _ := loaded(t, "a( x )b")
	m = typeKeys(m, "fx")
	m = typeKeys(m, "ds(")
	if got := line(m, 0); got != "axb" {
		t.Fatalf("ds( got %q", got)
	}
}

func TestSurroundDeleteNestedPicksNearest(t *testing.T) {
	m, _ := loaded(t, "(a (b) c)")
	m = typeKeys(m, "fb")
	m = typeKeys(m, "ds)")
	if got := line(m, 0); got != "(a b c)" {
		t.Fatalf("nested ds) got %q", got)
	}
}

func TestSurroundDeleteMultiline(t *testing.T) {
	m, _ := loaded(t, "{\n hi\n}")
	m = typeKeys(m, "j")
	m = typeKeys(m, "ds}")
	if got := m.Text(); got != "\n hi\n" {
		t.Fatalf("multiline ds} got %q", got)
	}
}

func TestSurroundDeleteNoPairIsNoop(t *testing.T) {
	m, _ := loaded(t, "plain text")
	m = typeKeys(m, "ds)")
	if got := line(m, 0); got != "plain text" {
		t.Fatalf("ds) without a pair must be a no-op, got %q", got)
	}
}

// --- undo & dot-repeat -----------------------------------------------------

func TestSurroundAddSingleUndoUnit(t *testing.T) {
	m, _ := loaded(t, "foo")
	m = typeKeys(m, "ysiw(")
	m = typeKeys(m, "u")
	if got := line(m, 0); got != "foo" {
		t.Fatalf("one undo must revert both inserts, got %q", got)
	}
}

func TestSurroundChangeSingleUndoUnit(t *testing.T) {
	m, _ := loaded(t, `"hi"`)
	m = typeKeys(m, "fh")
	m = typeKeys(m, `cs"'`)
	m = typeKeys(m, "u")
	if got := line(m, 0); got != `"hi"` {
		t.Fatalf("one undo must revert the whole change, got %q", got)
	}
}

func TestSurroundAddDotRepeat(t *testing.T) {
	m, _ := loaded(t, "foo bar")
	m = typeKeys(m, "ysiw)")
	m = typeKeys(m, "W.") // onto "bar", repeat
	if got := line(m, 0); got != "(foo) (bar)" {
		t.Fatalf("dot repeat got %q", got)
	}
}

func TestSurroundDeleteDotRepeat(t *testing.T) {
	m, _ := loaded(t, `"a" "b"`)
	m = typeKeys(m, `ds"`)
	m = typeKeys(m, `fbds"`)
	if got := line(m, 0); got != "a b" {
		t.Fatalf("second ds\" got %q", got)
	}
}

func TestSurroundChangeDotRepeat(t *testing.T) {
	m, _ := loaded(t, `"a" "b"`)
	m = typeKeys(m, `cs"'`)
	m = typeKeys(m, "fb.")
	if got := line(m, 0); got != "'a' 'b'" {
		t.Fatalf("dot repeat of cs got %q", got)
	}
}

// --- multi-caret fan-out (#145) --------------------------------------------

func TestSurroundAddFansOverCarets(t *testing.T) {
	m, _ := loaded(t, "foo\nbar\nbaz")
	caretAt(&m, 1, 0)
	caretAt(&m, 2, 0)
	m = typeKeys(m, "ysiw)")
	if got := m.Text(); got != "(foo)\n(bar)\n(baz)" {
		t.Fatalf("fan-out got %q", got)
	}
	m = typeKeys(m, "u")
	if got := m.Text(); got != "foo\nbar\nbaz" {
		t.Fatalf("one undo must revert every caret's wrap, got %q", got)
	}
}

func TestSurroundDeleteFansOverCarets(t *testing.T) {
	m, _ := loaded(t, `"a"`+"\n"+`"b"`)
	caretAt(&m, 1, 1)
	m.cursor = buffer.Position{Line: 0, Col: 1}
	m = typeKeys(m, `ds"`)
	if got := m.Text(); got != "a\nb" {
		t.Fatalf("caret ds\" got %q", got)
	}
}
