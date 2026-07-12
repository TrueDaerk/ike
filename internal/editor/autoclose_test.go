package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
)

// autoCloseModel loads content as a plain-text buffer with auto-close pairs
// enabled (the config default) — the feature is language-independent.
func autoCloseModel(t *testing.T, content string) Model {
	t.Helper()
	m := loadedExt(t, "txt", content)
	m.autoClosePairs = true
	return m
}

func TestAutoCloseInsertsPairAtEOL(t *testing.T) {
	for _, tc := range []struct{ open, want string }{
		{"(", "x()"},
		{"[", "x[]"},
		{"{", "x{}"},
	} {
		m := autoCloseModel(t, "x\n")
		m = send(m, key('A'), key([]rune(tc.open)[0]))
		if got := line(m, 0); got != tc.want {
			t.Errorf("typing %q: line = %q, want %q", tc.open, got, tc.want)
		}
		if m.cursor.Col != 2 {
			t.Errorf("typing %q: cursor col = %d, want 2 (between the pair)", tc.open, m.cursor.Col)
		}
	}
}

func TestAutoCloseBeforeWhitespaceAndCloser(t *testing.T) {
	// Before whitespace and before an existing closer the pair still closes.
	m := autoCloseModel(t, "( x)\n")
	m.cursor = buffer.Position{Line: 0, Col: 1}
	m = send(m, key('i'), key('['))
	if got := line(m, 0); got != "([] x)" {
		t.Fatalf("line = %q, want %q", got, "([] x)")
	}
}

func TestAutoCloseBeforeTextInsertsOpenerAlone(t *testing.T) {
	m := autoCloseModel(t, "foo\n")
	m = send(m, key('i'), key('('))
	if got := line(m, 0); got != "(foo" {
		t.Fatalf("line = %q, want %q — no closer before text", got, "(foo")
	}
}

func TestAutoCloseSkipsOverCloser(t *testing.T) {
	m := autoCloseModel(t, "x\n")
	m = send(m, key('A'), key('('), key('y'), key(')'))
	if got := line(m, 0); got != "x(y)" {
		t.Fatalf("line = %q, want %q — closer must be skipped, not duplicated", got, "x(y)")
	}
	if m.cursor.Col != 4 {
		t.Fatalf("cursor col = %d, want 4 (past the closer)", m.cursor.Col)
	}
}

func TestCloserInsertsWhenNotAtCloser(t *testing.T) {
	m := autoCloseModel(t, "x\n")
	m = send(m, key('A'), key(')'))
	if got := line(m, 0); got != "x)" {
		t.Fatalf("line = %q, want %q", got, "x)")
	}
}

func TestBackspaceDeletesEmptyPair(t *testing.T) {
	m := autoCloseModel(t, "x\n")
	m = send(m, key('A'), key('{'), special(tea.KeyBackspace))
	if got := line(m, 0); got != "x" {
		t.Fatalf("line = %q, want %q — backspace must remove both pair runes", got, "x")
	}
}

func TestBackspaceInsideNonEmptyPairDeletesOneRune(t *testing.T) {
	m := autoCloseModel(t, "(a)\n")
	m.cursor = buffer.Position{Line: 0, Col: 2}
	m = send(m, key('i'), special(tea.KeyBackspace))
	if got := line(m, 0); got != "()" {
		t.Fatalf("line = %q, want %q", got, "()")
	}
}

func TestAutoCloseDisabledInsertsOpenerAlone(t *testing.T) {
	m := loadedExt(t, "txt", "x\n")
	m.autoClosePairs = false
	m = send(m, key('A'), key('('))
	if got := line(m, 0); got != "x(" {
		t.Fatalf("line = %q, want %q with the setting off", got, "x(")
	}
}

func TestAutoCloseMultiCaret(t *testing.T) {
	m := autoCloseModel(t, "a\nfoo\n")
	// Primary caret at EOL of line 0 (pairs), secondary before "foo" (opener
	// alone) — one fan-out mixes both behaviors.
	m.cursor = buffer.Position{Line: 0, Col: 1}
	m.addCaret(buffer.Position{Line: 1, Col: 0})
	m = send(m, key('i'), key('('))
	if got := line(m, 0); got != "a()" {
		t.Fatalf("line 0 = %q, want %q", got, "a()")
	}
	if got := line(m, 1); got != "(foo" {
		t.Fatalf("line 1 = %q, want %q", got, "(foo")
	}
}

func TestAutoCloseDotReplay(t *testing.T) {
	m := autoCloseModel(t, "a\nb\n")
	m = send(m, key('A'), key('('), key('x'), key(')'), special(tea.KeyEscape))
	m = send(m, key('j'), key('.'))
	if got := line(m, 1); got != "b(x)" {
		t.Fatalf("line 1 = %q, want %q — '.' must replay the full (x) run", got, "b(x)")
	}
}

func TestAutoCloseUndoIsOneUnit(t *testing.T) {
	m := autoCloseModel(t, "x\n")
	m = send(m, key('A'), key('('), key('y'), key(')'), special(tea.KeyEscape), key('u'))
	if got := line(m, 0); got != "x" {
		t.Fatalf("line = %q, want %q — the whole insert must undo as one unit", got, "x")
	}
}

func TestQuoteAutoClosesAtEOL(t *testing.T) {
	for _, q := range []rune{'"', '\'', '`'} {
		m := autoCloseModel(t, "x \n")
		m = send(m, key('A'), key(q))
		want := "x " + string(q) + string(q)
		if got := line(m, 0); got != want {
			t.Errorf("typing %q: line = %q, want %q", q, got, want)
		}
		if m.cursor.Col != 3 {
			t.Errorf("typing %q: cursor col = %d, want 3 (between the pair)", q, m.cursor.Col)
		}
	}
}

func TestQuoteSkipsOverClosingQuote(t *testing.T) {
	m := autoCloseModel(t, "x \n")
	m = send(m, key('A'), key('"'), key('y'), key('"'))
	if got := line(m, 0); got != `x "y"` {
		t.Fatalf("line = %q, want %q — closing quote must be skipped", got, `x "y"`)
	}
	if m.cursor.Col != 5 {
		t.Fatalf("cursor col = %d, want 5 (past the closing quote)", m.cursor.Col)
	}
}

func TestApostropheAfterWordInsertsAlone(t *testing.T) {
	m := autoCloseModel(t, "don\n")
	m = send(m, key('A'), key('\''), key('t'))
	if got := line(m, 0); got != "don't" {
		t.Fatalf("line = %q, want %q — no pairing mid-word", got, "don't")
	}
}

func TestQuoteBeforeTextInsertsAlone(t *testing.T) {
	m := autoCloseModel(t, "foo\n")
	m = send(m, key('i'), key('"'))
	if got := line(m, 0); got != `"foo` {
		t.Fatalf("line = %q, want %q", got, `"foo`)
	}
}

func TestQuoteAfterSameQuoteInsertsAlone(t *testing.T) {
	// A quote right after the same quote (e.g. building a doubled quote)
	// must not open another pair.
	m := autoCloseModel(t, "\"a\" \n")
	m.cursor = buffer.Position{Line: 0, Col: 2}
	m = send(m, key('a'), key('"')) // append after the closing quote
	if got := line(m, 0); got != `"a"" ` {
		t.Fatalf("line = %q, want %q", got, `"a"" `)
	}
}

func TestBackspaceDeletesEmptyQuotePair(t *testing.T) {
	m := autoCloseModel(t, "x \n")
	m = send(m, key('A'), key('"'), special(tea.KeyBackspace))
	if got := line(m, 0); got != "x " {
		t.Fatalf("line = %q, want %q — backspace must remove both quotes", got, "x ")
	}
}

func TestQuoteDisabledInsertsAlone(t *testing.T) {
	m := loadedExt(t, "txt", "x \n")
	m.autoClosePairs = false
	m = send(m, key('A'), key('"'))
	if got := line(m, 0); got != `x "` {
		t.Fatalf("line = %q, want %q with the setting off", got, `x "`)
	}
}

func TestQuoteMultiCaretMixesPairAndSkip(t *testing.T) {
	m := autoCloseModel(t, "a \n\"b\n")
	// Primary pairs at EOL of line 0; secondary sits on the quote in line 1
	// and skips over it.
	m.cursor = buffer.Position{Line: 0, Col: 2}
	m.addCaret(buffer.Position{Line: 1, Col: 0})
	m = send(m, key('i'), key('"'))
	if got := line(m, 0); got != `a ""` {
		t.Fatalf("line 0 = %q, want %q", got, `a ""`)
	}
	if got := line(m, 1); got != `"b` {
		t.Fatalf("line 1 = %q, want unchanged %q (skip-over)", got, `"b`)
	}
}

func TestQuoteDotReplay(t *testing.T) {
	m := autoCloseModel(t, "a \nb \n")
	m = send(m, key('A'), key('"'), key('x'), key('"'), special(tea.KeyEscape))
	m = send(m, key('j'), key('.'))
	if got := line(m, 1); got != `b "x"` {
		t.Fatalf("line 1 = %q, want %q — '.' must replay the full quoted run", got, `b "x"`)
	}
}
