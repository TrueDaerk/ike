package editor

import (
	"testing"

	"ike/internal/editor/search"
)

func TestSubstituteBasicAndGlobal(t *testing.T) {
	m, _ := loaded(t, "foo foo foo\n")
	// Without g, only the first match on the line changes.
	m = runEx(m, "s/foo/bar/")
	if got := line(m, 0); got != "bar foo foo" {
		t.Fatalf("first-only: %q", got)
	}
	// With g, all remaining matches change.
	m = runEx(m, "s/foo/bar/g")
	if got := line(m, 0); got != "bar bar bar" {
		t.Fatalf("global: %q", got)
	}
}

func TestSubstituteRangeWholeFile(t *testing.T) {
	m, _ := loaded(t, "a x\nb x\nc x\n")
	m = runEx(m, "%s/x/Y/g")
	for i, want := range []string{"a Y", "b Y", "c Y"} {
		if got := line(m, i); got != want {
			t.Fatalf("line %d: %q want %q", i, got, want)
		}
	}
	// Cursor lands on the last changed line.
	if m.cursor.Line != 2 {
		t.Fatalf("cursor line = %d, want 2", m.cursor.Line)
	}
}

func TestSubstituteCaseInsensitive(t *testing.T) {
	m, _ := loaded(t, "Foo FOO foo\n")
	m = runEx(m, "s/foo/x/gi")
	if got := line(m, 0); got != "x x x" {
		t.Fatalf("case-insensitive: %q", got)
	}
}

func TestSubstituteCaptureGroups(t *testing.T) {
	m, _ := loaded(t, "key=value\n")
	m = runEx(m, `s/\v(\w+)\=(\w+)/\2=\1/`)
	if got := line(m, 0); got != "value=key" {
		t.Fatalf("capture groups: %q", got)
	}
}

func TestSubstituteWholeMatchAmp(t *testing.T) {
	m, _ := loaded(t, "cat\n")
	m = runEx(m, "s/cat/[&]/")
	if got := line(m, 0); got != "[cat]" {
		t.Fatalf("& whole match: %q", got)
	}
}

func TestSubstituteAlternateDelimiter(t *testing.T) {
	m, _ := loaded(t, "a/b/c\n")
	m = runEx(m, "s#/#-#g")
	if got := line(m, 0); got != "a-b-c" {
		t.Fatalf("alt delimiter: %q", got)
	}
}

func TestSubstituteCountOnly(t *testing.T) {
	m, _ := loaded(t, "a a\nb\na\n")
	m = runEx(m, "%s/a/z/gn")
	// n reports without modifying.
	if got := line(m, 0); got != "a a" {
		t.Fatalf("count-only must not modify: %q", got)
	}
	if m.cmdMsg == "" {
		t.Fatal("count-only should report a match count")
	}
}

func TestSubstituteEmptyPatternReusesSearch(t *testing.T) {
	m, _ := loaded(t, "alpha beta alpha\n")
	m.query = search.Compile("alpha", false, search.CaseSmart)
	m = runEx(m, "s//X/g")
	if got := line(m, 0); got != "X beta X" {
		t.Fatalf("empty pattern reuse: %q", got)
	}
}

func TestSubstituteBareRepeat(t *testing.T) {
	m, _ := loaded(t, "aaa\nbbb\naaa\n")
	m = runEx(m, "s/a/X/g") // line 0 only
	if got := line(m, 0); got != "XXX" {
		t.Fatalf("setup: %q", got)
	}
	// Move to line 2 and repeat with a bare ":s".
	m.moveTo(pos(2, 0))
	m = runEx(m, "s")
	if got := line(m, 2); got != "XXX" {
		t.Fatalf("bare :s repeat: %q", got)
	}
}

func TestSubstituteSingleUndoUnit(t *testing.T) {
	m, _ := loaded(t, "a\na\na\n")
	m = runEx(m, "%s/a/b/g")
	for i := 0; i < 3; i++ {
		if line(m, i) != "b" {
			t.Fatalf("setup line %d: %q", i, line(m, i))
		}
	}
	// One undo reverts the entire substitute.
	m = send(m, key('u'))
	for i := 0; i < 3; i++ {
		if got := line(m, i); got != "a" {
			t.Fatalf("after undo line %d: %q want a", i, got)
		}
	}
}

func TestSubstituteErrors(t *testing.T) {
	m, _ := loaded(t, "hello\n")
	// Pattern not found: no change, error message.
	m = runEx(m, "s/zzz/x/")
	if line(m, 0) != "hello" {
		t.Fatalf("not-found must not modify: %q", line(m, 0))
	}
	if m.cmdMsg == "" {
		t.Fatal("not-found should report an error")
	}
	// Unknown flag.
	m = runEx(m, "s/h/H/q")
	if m.cmdMsg == "" || line(m, 0) != "hello" {
		t.Fatalf("unknown flag should error without modifying: msg=%q line=%q", m.cmdMsg, line(m, 0))
	}
}
