package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
	if m.mode != Visual {
		t.Fatal("case command should keep the selection active (#2618)")
	}
	// A linewise selection covers its lines whole.
	m = typeKeys(m, "V")
	m, _ = m.Update(ActionMsg{Action: "case_lower"})
	if line(m, 0) != "foo bar baz" {
		t.Fatalf("linewise lower got %q", line(m, 0))
	}
	if m.mode != VisualLine {
		t.Fatal("linewise selection should keep its mode (#2618)")
	}
}

// TestCaseCommandTogglesSelectionRepeatedly is the JetBrains Toggle Case
// contract (#2618): repeating the command keeps addressing the same
// selection instead of dropping to a single caret after the first hit.
func TestCaseCommandTogglesSelectionRepeatedly(t *testing.T) {
	m, _ := loaded(t, "foo bar baz\n")
	m = typeKeys(m, "v$")
	m, _ = m.Update(ActionMsg{Action: "case_toggle"})
	if got := line(m, 0); got != "FOO BAR BAZ" {
		t.Fatalf("first toggle got %q", got)
	}
	if m.mode != Visual {
		t.Fatal("selection should still be active after the first toggle")
	}
	m, _ = m.Update(ActionMsg{Action: "case_toggle"})
	if got := line(m, 0); got != "foo bar baz" {
		t.Fatalf("second toggle got %q", got)
	}
	if m.mode != Visual {
		t.Fatal("selection should still be active after the second toggle")
	}
}

// TestCaseCommandLowerUpperNoOpKeepsSelection covers the second-invocation
// no-op case for .lower/.upper: nothing changes, but the selection stays put.
func TestCaseCommandLowerUpperNoOpKeepsSelection(t *testing.T) {
	m, _ := loaded(t, "foo bar\n")
	m = typeKeys(m, "v$")
	m, _ = m.Update(ActionMsg{Action: "case_lower"})
	if got := line(m, 0); got != "foo bar" {
		t.Fatalf("first lower got %q", got)
	}
	if m.mode != Visual {
		t.Fatal("no-op case_lower should keep the selection")
	}
	m, _ = m.Update(ActionMsg{Action: "case_lower"})
	if got := line(m, 0); got != "foo bar" {
		t.Fatalf("second lower got %q", got)
	}
	if m.mode != Visual {
		t.Fatal("no-op case_lower should keep the selection")
	}
}

// TestCaseCycleOnSelectionRefitsSelection covers a length-changing rewrite:
// the selection must be re-fitted to the new identifier, not the old range.
func TestCaseCycleOnSelectionRefitsSelection(t *testing.T) {
	m, _ := loaded(t, "fooBar\n")
	m = typeKeys(m, "v$")
	m, _ = m.Update(ActionMsg{Action: "case_cycle"})
	if got := line(m, 0); got != "foo_bar" {
		t.Fatalf("cycle got %q", got)
	}
	if m.mode != Visual {
		t.Fatal("case_cycle should keep the selection")
	}
	if m.anchor.Col != 0 || m.cursor.Col != len([]rune("foo_bar"))-1 {
		t.Fatalf("selection not refitted: anchor=%v cursor=%v", m.anchor, m.cursor)
	}
}

func TestCaseToggleWholeTextSemantics(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Pogo", "POGO"},
		{"POGO", "pogo"},
		{"pOGO", "POGO"},
	}
	for _, c := range cases {
		if got := toggleCase(c.in); got != c.want {
			t.Errorf("toggleCase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestCaseCommandOnBlockSelectionKeepsMode covers a block-mode selection
// (which composes to a charwise range, #2618): the mode must stay VisualBlock.
func TestCaseCommandOnBlockSelectionKeepsMode(t *testing.T) {
	m, _ := loaded(t, "foo bar\n")
	m = send(m, modKey('v', tea.ModCtrl))
	m = typeKeys(m, "$")
	m, _ = m.Update(ActionMsg{Action: "case_upper"})
	if got := line(m, 0); got != "FOO BAR" {
		t.Fatalf("block upper got %q", got)
	}
	if m.mode != VisualBlock {
		t.Fatal("case command should keep block selection mode (#2618)")
	}
}

func TestCaseToggleThreeStepCycleUnderCaret(t *testing.T) {
	m, _ := loaded(t, "Pogo\n")
	for _, want := range []string{"POGO", "pogo", "POGO"} {
		m, _ = m.Update(ActionMsg{Action: "case_toggle"})
		if got := line(m, 0); got != want {
			t.Fatalf("case_toggle got %q, want %q", got, want)
		}
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
