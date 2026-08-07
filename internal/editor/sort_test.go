package editor

import (
	"strings"
	"testing"
)

// bufLines joins the whole buffer so a sort result reads as one string.
func bufLines(m Model) string { return strings.Join(m.buf.Lines(), "\n") }

func TestExSortWholeBufferByDefault(t *testing.T) {
	m, _ := loaded(t, "pear\napple\ncherry\n")
	m = runEx(m, "sort")
	if got, want := bufLines(m), "apple\ncherry\npear"; got != want {
		t.Fatalf(":sort = %q, want %q", got, want)
	}
	if m.cursor.Line != 0 {
		t.Fatalf("cursor line = %d, want 0", m.cursor.Line)
	}
}

func TestExSortLongNameAndAbbreviation(t *testing.T) {
	for _, name := range []string{"sor", "sort"} {
		m, _ := loaded(t, "b\na\n")
		m = runEx(m, name)
		if got := bufLines(m); got != "a\nb" {
			t.Fatalf(":%s = %q", name, got)
		}
	}
}

func TestExSortRangeLeavesRestUntouched(t *testing.T) {
	m, _ := loaded(t, "head\nc\na\nb\ntail\n")
	m = runEx(m, "2,4sort")
	if got, want := bufLines(m), "head\na\nb\nc\ntail"; got != want {
		t.Fatalf(":2,4sort = %q, want %q", got, want)
	}
}

func TestExSortVisualRange(t *testing.T) {
	m, _ := loaded(t, "z\nc\na\nb\n")
	m.visualStart, m.visualEnd = 1, 3
	m = runEx(m, "'<,'>sort")
	if got, want := bufLines(m), "z\na\nb\nc"; got != want {
		t.Fatalf(":'<,'>sort = %q, want %q", got, want)
	}
}

func TestExSortBangReverses(t *testing.T) {
	m, _ := loaded(t, "b\nc\na\n")
	m = runEx(m, "sort!")
	if got, want := bufLines(m), "c\nb\na"; got != want {
		t.Fatalf(":sort! = %q, want %q", got, want)
	}
}

func TestExSortUniqueDropsDuplicates(t *testing.T) {
	m, _ := loaded(t, "b\na\nb\na\nc\n")
	m = runEx(m, "sort u")
	if got, want := bufLines(m), "a\nb\nc"; got != want {
		t.Fatalf(":sort u = %q, want %q", got, want)
	}
	if !strings.Contains(m.cmdMsg, "2 duplicates removed") {
		t.Fatalf("message = %q", m.cmdMsg)
	}
}

func TestExSortNumeric(t *testing.T) {
	m, _ := loaded(t, "item 10\nitem 9\nitem -3\nitem 100\n")
	m = runEx(m, "sort n")
	if got, want := bufLines(m), "item -3\nitem 9\nitem 10\nitem 100"; got != want {
		t.Fatalf(":sort n = %q, want %q", got, want)
	}
}

func TestExSortNumericPutsUnnumberedLinesFirstInOrder(t *testing.T) {
	m, _ := loaded(t, "3\nzeta\n1\nalpha\n")
	m = runEx(m, "sort n")
	// Lines without a number sort first and keep their original order.
	if got, want := bufLines(m), "zeta\nalpha\n1\n3"; got != want {
		t.Fatalf(":sort n = %q, want %q", got, want)
	}
}

func TestExSortIgnoreCase(t *testing.T) {
	m, _ := loaded(t, "banana\nApple\ncherry\n")
	m = runEx(m, "sort i")
	if got, want := bufLines(m), "Apple\nbanana\ncherry"; got != want {
		t.Fatalf(":sort i = %q, want %q", got, want)
	}
	// Without "i", uppercase sorts before lowercase (byte order).
	m2, _ := loaded(t, "banana\nApple\ncherry\n")
	m2 = runEx(m2, "sort")
	if got, want := bufLines(m2), "Apple\nbanana\ncherry"; got != want {
		t.Fatalf(":sort = %q, want %q", got, want)
	}
	m3, _ := loaded(t, "banana\napple\nCherry\n")
	m3 = runEx(m3, "sort")
	if got, want := bufLines(m3), "Cherry\napple\nbanana"; got != want {
		t.Fatalf(":sort = %q, want %q", got, want)
	}
}

func TestExSortCombinedFlags(t *testing.T) {
	// "un": numeric with duplicates dropped.
	m, _ := loaded(t, "20\n3\n20\n3\n1\n")
	m = runEx(m, "sort un")
	if got, want := bufLines(m), "1\n3\n20"; got != want {
		t.Fatalf(":sort un = %q, want %q", got, want)
	}
	// "iu": case-insensitive uniqueness drops lines differing only in case.
	m2, _ := loaded(t, "Apple\napple\nBerry\n")
	m2 = runEx(m2, "sort iu")
	if got, want := bufLines(m2), "Apple\nBerry"; got != want {
		t.Fatalf(":sort iu = %q, want %q", got, want)
	}
}

func TestExSortIsStable(t *testing.T) {
	// Equal keys keep their original order, both ascending and reversed.
	m, _ := loaded(t, "b 1\na 2\na 1\nb 2\n")
	m = runEx(m, "sort n")
	if got, want := bufLines(m), "b 1\na 1\na 2\nb 2"; got != want {
		t.Fatalf(":sort n = %q, want %q", got, want)
	}
	m2, _ := loaded(t, "b 1\na 2\na 1\nb 2\n")
	m2 = runEx(m2, "sort! n")
	if got, want := bufLines(m2), "a 2\nb 2\nb 1\na 1"; got != want {
		t.Fatalf(":sort! n = %q, want %q", got, want)
	}
}

func TestExSortSingleUndoStep(t *testing.T) {
	m, _ := loaded(t, "c\na\nb\n")
	m = runEx(m, "sort")
	if got := bufLines(m); got != "a\nb\nc" {
		t.Fatalf("sorted = %q", got)
	}
	m.undo(1)
	if got, want := bufLines(m), "c\na\nb"; got != want {
		t.Fatalf("after one undo = %q, want %q", got, want)
	}
}

func TestExSortEmptyAndSingleLineRange(t *testing.T) {
	m, _ := loaded(t, "")
	m = runEx(m, "sort")
	if got := bufLines(m); got != "" {
		t.Fatalf("empty buffer sort = %q", got)
	}
	if m.dirty {
		t.Fatalf("a no-op sort must not dirty the buffer")
	}
	m2, _ := loaded(t, "only\n")
	m2 = runEx(m2, "1,1sort")
	if got := bufLines(m2); got != "only" {
		t.Fatalf("one-line sort = %q", got)
	}
}

func TestExSortAlreadySortedIsNoOp(t *testing.T) {
	m, _ := loaded(t, "a\nb\nc\n")
	m = runEx(m, "sort")
	if m.dirty {
		t.Fatalf("sorting an ordered range must not dirty the buffer")
	}
	if m.cmdMsg != "already sorted" {
		t.Fatalf("message = %q", m.cmdMsg)
	}
}

func TestExSortUnknownFlag(t *testing.T) {
	m, _ := loaded(t, "b\na\n")
	m = runEx(m, "sort x")
	if !strings.Contains(m.cmdMsg, "unknown sort flag") {
		t.Fatalf("message = %q", m.cmdMsg)
	}
	if got := bufLines(m); got != "b\na" {
		t.Fatalf("buffer changed on flag error: %q", got)
	}
}

func TestSortFirstNumber(t *testing.T) {
	cases := []struct {
		line string
		want int64
		ok   bool
	}{
		{"42", 42, true},
		{"x-7y", -7, true},
		{"a 3 b 9", 3, true},
		{"no digits", 0, false},
		{"v1.2.3", 1, true},
		{"- 5", 5, true}, // the sign must touch the digits
	}
	for _, c := range cases {
		got, ok := firstNumber(c.line)
		if got != c.want || ok != c.ok {
			t.Fatalf("firstNumber(%q) = %d,%v want %d,%v", c.line, got, ok, c.want, c.ok)
		}
	}
}
