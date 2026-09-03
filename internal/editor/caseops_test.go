package editor

import (
	"strings"
	"testing"
)

// caseops_test.go covers the #2418 command half of the case family. The vim
// operators gu/gU/g~ keep their own tests in vimops_test.go.

func TestCaseCommandsOnWordUnderCaret(t *testing.T) {
	m, _ := loaded(t, "Hello World\n")
	m, _ = m.Update(ActionMsg{Action: "case_upper"})
	if line(m, 0) != "HELLO World" {
		t.Fatalf("case_upper got %q", line(m, 0))
	}
	m, _ = m.Update(ActionMsg{Action: "case_lower"})
	if line(m, 0) != "hello World" {
		t.Fatalf("case_lower got %q", line(m, 0))
	}
	m, _ = m.Update(ActionMsg{Action: "case_toggle"})
	if line(m, 0) != "HELLO World" {
		t.Fatalf("case_toggle got %q", line(m, 0))
	}
	m, _ = m.Update(ActionMsg{Action: "undo"})
	if line(m, 0) != "hello World" {
		t.Fatalf("undo got %q", line(m, 0))
	}
}

func TestCaseCommandOnSelection(t *testing.T) {
	m, _ := loaded(t, "foo bar baz\n")
	m = typeKeys(m, "vlllll") // charwise over "foo ba"
	m, _ = m.Update(ActionMsg{Action: "case_upper"})
	if line(m, 0) != "FOO BAr baz" {
		t.Fatalf("selection upper got %q", line(m, 0))
	}
	if m.mode != Normal {
		t.Fatal("case command should leave visual mode")
	}
	// A linewise selection covers its lines whole.
	m = typeKeys(m, "V")
	m, _ = m.Update(ActionMsg{Action: "case_lower"})
	if line(m, 0) != "foo bar baz" {
		t.Fatalf("linewise lower got %q", line(m, 0))
	}
}

func TestCaseCommandFansOverCarets(t *testing.T) {
	m, _ := loaded(t, "aaa bbb ccc")
	caretAt(&m, 0, 4)
	caretAt(&m, 0, 8)
	m, _ = m.Update(ActionMsg{Action: "case_upper"})
	if got := line(m, 0); got != "AAA BBB CCC" {
		t.Fatalf("fan-out got %q", got)
	}
	// One undo takes the whole fan-out back.
	m, _ = m.Update(ActionMsg{Action: "undo"})
	if got := line(m, 0); got != "aaa bbb ccc" {
		t.Fatalf("undo got %q", got)
	}
}

func TestCaseCycleRotatesIdentifierUnderCaret(t *testing.T) {
	m, _ := loaded(t, "var fooBar = 1\n")
	m = typeKeys(m, "w") // onto "fooBar"
	for _, want := range []string{"var foo_bar = 1", "var foo-bar = 1", "var FooBar = 1", "var FOO_BAR = 1", "var fooBar = 1"} {
		m, _ = m.Update(ActionMsg{Action: "case_cycle"})
		if got := line(m, 0); got != want {
			t.Fatalf("cycle got %q, want %q", got, want)
		}
	}
}

func TestCaseCycleSpansKebabToken(t *testing.T) {
	// vim's word object stops at "-"; the cycle's own token scan does not.
	m, _ := loaded(t, "foo-bar-baz\n")
	m, _ = m.Update(ActionMsg{Action: "case_cycle"})
	if got := line(m, 0); got != "FooBarBaz" {
		t.Fatalf("kebab cycle got %q", got)
	}
}

func TestCaseCycleLeavesNonIdentifiersAlone(t *testing.T) {
	m, _ := loaded(t, "-> 42\n")
	m, cmd := m.Update(ActionMsg{Action: "case_cycle"})
	if got := line(m, 0); got != "-> 42" {
		t.Fatalf("cycle touched %q", got)
	}
	if txt := noticeIn(t, cmd); !strings.Contains(txt, "no identifier") {
		t.Fatalf("notice = %q", txt)
	}
	if m.Dirty() {
		t.Fatal("a refused cycle must not dirty the buffer")
	}
}

func TestCaseCycleFansOverCarets(t *testing.T) {
	m, _ := loaded(t, "fooBar bazQux")
	caretAt(&m, 0, 7)
	m, _ = m.Update(ActionMsg{Action: "case_cycle"})
	if got := line(m, 0); got != "foo_bar baz_qux" {
		t.Fatalf("cycle fan-out got %q", got)
	}
}

func TestCaseCommandKeepsRuneCountStable(t *testing.T) {
	// Rune-wise mapping, not language-aware special casing: "ß" stays one
	// rune, so nothing to its right shifts.
	m, _ := loaded(t, "straße x\n")
	m, _ = m.Update(ActionMsg{Action: "case_upper"})
	if got := line(m, 0); got != "STRAßE x" {
		t.Fatalf("upper got %q", got)
	}
}

func TestCaseCommandDotRepeat(t *testing.T) {
	m, _ := loaded(t, "one two\n")
	m, _ = m.Update(ActionMsg{Action: "case_upper"})
	m = typeKeys(m, "w.")
	if got := line(m, 0); got != "ONE TWO" {
		t.Fatalf("dot repeat got %q", got)
	}
}

func TestIdentifierAt(t *testing.T) {
	cases := []struct {
		line     string
		col      int
		want     string
		wantOK   bool
		wantCol0 int
	}{
		{"foo-bar baz", 1, "foo-bar", true, 0},
		{"foo-bar baz", 3, "foo-bar", true, 0}, // parked on the joining dash
		{"a - b", 2, "", false, 0},             // a lone dash is not a join
		{"  ", 0, "", false, 0},
		{"x_1", 2, "x_1", true, 0},
		{"", 0, "", false, 0},
	}
	for _, c := range cases {
		r := []rune(c.line)
		a, z, ok := identifierAt(r, c.col)
		if ok != c.wantOK {
			t.Errorf("identifierAt(%q,%d) ok=%v", c.line, c.col, ok)
			continue
		}
		if !ok {
			continue
		}
		if got := string(r[a:z]); got != c.want || a != c.wantCol0 {
			t.Errorf("identifierAt(%q,%d) = %q@%d, want %q@%d", c.line, c.col, got, a, c.want, c.wantCol0)
		}
	}
}
