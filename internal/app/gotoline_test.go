package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// gotoline_test.go covers editor.goToLine (#2486): the target parser on its
// own, and the prompt's round trip from cmd+l to a moved caret.

func TestParseGoToLineTargets(t *testing.T) {
	// Ten lines (0..9), caret on line 4 (1-based: 5).
	const count, cur = 10, 4
	cases := []struct {
		in        string
		line, col int
	}{
		{"42", 9, 0},      // clamped to the last line
		{"1", 0, 0},       // 1-based on the wire
		{"7", 6, 0},       //
		{"7:3", 6, 2},     // line:column, both 1-based
		{" 7 : 3 ", 6, 2}, // spaces around the parts are noise
		{"+5", 9, 0},      // relative, clamped
		{"+2", 6, 0},      //
		{"-3", 1, 0},      //
		{"-99", 0, 0},     // clamped to the first line
		{"+2:4", 6, 3},    // relative with a column
		{"0", 0, 0},       // below the first line clamps
		{"-0", 4, 0},      // a signed zero is a relative no-op
		{"3:0", 2, 0},     // column below 1 clamps to the line start
	}
	for _, c := range cases {
		line, col, err := parseGoToLine(c.in, cur, count)
		if err != nil {
			t.Fatalf("%q: unexpected error %v", c.in, err)
		}
		if line != c.line || col != c.col {
			t.Errorf("%q: got %d:%d, want %d:%d", c.in, line, col, c.line, c.col)
		}
	}
}

func TestParseGoToLineRejectsNonNumeric(t *testing.T) {
	for _, in := range []string{"abc", "4x", "7:zz", "7:", ":7", "+", "1.5", "--2"} {
		if _, _, err := parseGoToLine(in, 4, 10); err == nil {
			t.Errorf("%q should be rejected", in)
		}
	}
	// An empty target is not an error the user has to read — it closes.
	if _, _, err := parseGoToLine("   ", 4, 10); err != errGoToLineEmpty {
		t.Fatalf("blank target: got %v, want errGoToLineEmpty", err)
	}
}

// goToLineModel opens a ten-line file and the go-to-line prompt on top of it.
func goToLineModel(t *testing.T) (Model, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "a.go")
	if err := os.WriteFile(path, []byte("l0\nl1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	// The first-start LSP dialog owns the keyboard while it is up (#301) and
	// would swallow the typed target; esc is what a user presses too.
	for m.onboardingOpen() {
		tm, _ := m.updateOnboarding(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = tm.(Model)
	}
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	tm, _ = m.Update(GoToLineMsg{})
	m = tm.(Model)
	if !m.goToLinePromptOpen() {
		t.Fatal("GoToLineMsg should open the prompt")
	}
	return m, path
}

// typeGoToLine types the target and confirms it with enter.
func typeGoToLine(t *testing.T, m Model, target string) Model {
	t.Helper()
	for _, r := range target {
		tm, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = tm.(Model)
	}
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return tm.(Model)
}

// caretAt asserts the active editor's 1-based caret position.
func caretAt(t *testing.T, m Model, line, col int) {
	t.Helper()
	ed := m.activeEditor()
	gotLine, gotCol := ed.Cursor()
	if gotLine != line || gotCol != col {
		t.Fatalf("caret at %d:%d, want %d:%d", gotLine, gotCol, line, col)
	}
}

func TestGoToLineAbsoluteAndColumn(t *testing.T) {
	m, _ := goToLineModel(t)
	m = typeGoToLine(t, m, "7")
	if m.goToLinePromptOpen() {
		t.Fatal("enter should close the prompt")
	}
	caretAt(t, m, 7, 1)

	m, _ = goToLineModel(t)
	// "l2" is two characters, so column 2 exists on line 3.
	m = typeGoToLine(t, m, "3:2")
	caretAt(t, m, 3, 2)
}

func TestGoToLineRelativeAndClamped(t *testing.T) {
	m, _ := goToLineModel(t)
	m = typeGoToLine(t, m, "5")
	caretAt(t, m, 5, 1)

	tm, _ := m.Update(GoToLineMsg{})
	m = typeGoToLine(t, tm.(Model), "+3")
	caretAt(t, m, 8, 1)

	tm, _ = m.Update(GoToLineMsg{})
	m = typeGoToLine(t, tm.(Model), "-5")
	caretAt(t, m, 3, 1)

	// Out of range clamps to the last line instead of being rejected.
	tm, _ = m.Update(GoToLineMsg{})
	m = typeGoToLine(t, tm.(Model), "99999")
	caretAt(t, m, 10, 1)
}

func TestGoToLineRejectionKeepsPromptOpen(t *testing.T) {
	m, _ := goToLineModel(t)
	m = typeGoToLine(t, m, "abc")
	if !m.goToLinePromptOpen() {
		t.Fatal("a non-numeric target must keep the prompt open")
	}
	if !strings.Contains(m.shell.Content().Render(80), "not a line number") {
		t.Fatalf("the rejection reason should be shown, body:\n%s", m.shell.Content().Render(80))
	}
	// The caret has not moved, and the typed text is still editable.
	caretAt(t, m, 1, 1)
	if m.goToLineInput.Text != "abc" {
		t.Fatalf("input should survive the rejection, got %q", m.goToLineInput.Text)
	}
}

func TestGoToLineEscapeCancels(t *testing.T) {
	m, _ := goToLineModel(t)
	for _, r := range "9" {
		tm, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = tm.(Model)
	}
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.goToLinePromptOpen() {
		t.Fatal("esc should close the prompt")
	}
	caretAt(t, m, 1, 1)
}

// The jump is a navigation departure: nav.back returns to the pre-jump caret.
func TestGoToLineRecordsNavHistory(t *testing.T) {
	m, path := goToLineModel(t)
	m = typeGoToLine(t, m, "8")
	caretAt(t, m, 8, 1)
	tm, _ := m.Update(NavBackMsg{})
	tm.(Model).atPosition(t, path, 0)
}
