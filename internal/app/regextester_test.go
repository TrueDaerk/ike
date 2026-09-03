package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/regextest"
)

// regextester_test.go covers the tester dialog (#1937): live evaluation,
// group display, error display, selection prefill, copy and history.

// openTester opens the tester through the real command message.
func openTester(t *testing.T, m Model) Model {
	t.Helper()
	tm, cmd := m.Update(OpenRegexTesterMsg{})
	m = drainCmd(tm.(Model), cmd)
	if !m.regexTesterOpen() {
		t.Fatal("tools.regexTester must open the tester")
	}
	return m
}

// TestRegexTesterEvaluatesLive: typing a pattern re-evaluates against the
// test text, and the view shows the match count and the group values.
func TestRegexTesterEvaluatesLive(t *testing.T) {
	m := openTester(t, newSized())
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeInto(m, "a1 b2")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeInto(m, `([a-z])(\d)`)

	s := m.regexTester
	if s.result.Err != "" {
		t.Fatalf("valid pattern reported error %q", s.result.Err)
	}
	if len(s.result.Matches) != 2 {
		t.Fatalf("got %d matches, want 2", len(s.result.Matches))
	}
	if got := s.result.Matches[0].Groups[1].Value; got != "a" {
		t.Errorf("group 1 of the first match = %q, want %q", got, "a")
	}
	v := ansi.Strip(m.render())
	if !strings.Contains(v, "2 match(es)") {
		t.Error("the dialog should show the match count")
	}
	if !strings.Contains(v, "RE2") {
		t.Error("the dialog should state its RE2 semantics")
	}
}

// TestRegexTesterNamedGroups: a named group is listed with its name.
func TestRegexTesterNamedGroups(t *testing.T) {
	m := openTester(t, newSized())
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeInto(m, "main.go:42")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeInto(m, `(?P<file>[^:]+):(?P<line>\d+)`)

	v := ansi.Strip(m.render())
	if !strings.Contains(v, "1 file") || !strings.Contains(v, "2 line") {
		t.Errorf("the group list should name the groups, got:\n%s", v)
	}
	if !strings.Contains(v, `"main.go"`) || !strings.Contains(v, `"42"`) {
		t.Errorf("the group list should show the captured values, got:\n%s", v)
	}
}

// TestRegexTesterInvalidPattern: a broken pattern shows the compile error
// inline and leaves the dialog usable.
func TestRegexTesterInvalidPattern(t *testing.T) {
	m := openTester(t, newSized())
	m = typeInto(m, "(unclosed")

	s := m.regexTester
	if s.result.Err == "" {
		t.Fatal("an invalid pattern must report a compile error")
	}
	v := ansi.Strip(m.render())
	if !strings.Contains(v, "missing closing )") {
		t.Errorf("the compile error should be shown inline, got:\n%s", v)
	}
	// Still editable: backspacing back to a valid pattern clears the error.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	for i := 0; i < 8; i++ {
		m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	if m.regexTester.result.Err != "" {
		t.Errorf("clearing the pattern should clear the error, got %q", m.regexTester.result.Err)
	}
}

// TestRegexTesterPrefillsSelection: an open visual selection becomes the test
// text, so the pattern can be tried against the lines at hand.
func TestRegexTesterPrefillsSelection(t *testing.T) {
	m := newSized()
	path := writeTempFile(t, "log.txt", "keep\nsecret\n")
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	m = drainKey(m, tea.KeyPressMsg{Code: 'V', Text: "V"}) // visual-line select line 1

	m = openTester(t, m)
	if got := m.regexTester.regexText(); got != "keep" {
		t.Fatalf("test text = %q, want the selected line", got)
	}
	// And without a selection the area starts empty.
	m2 := openTester(t, newSized())
	if got := m2.regexTester.regexText(); got != "" {
		t.Errorf("test text without a selection = %q, want empty", got)
	}
}

// TestRegexTesterMultiLineTextAndHighlight: enter splits the test area into
// lines, and the spans cover the matches on both of them.
func TestRegexTesterMultiLineTextAndHighlight(t *testing.T) {
	m := openTester(t, newSized())
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeInto(m, "foo")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = typeInto(m, "xfoo")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeInto(m, "foo")

	s := m.regexTester
	if got := s.regexText(); got != "foo\nxfoo" {
		t.Fatalf("test text = %q, want two lines", got)
	}
	spans := regextest.LineSpans(s.regexText(), s.result.Matches)
	if len(spans) != 2 {
		t.Fatalf("got %d highlight spans, want one per line", len(spans))
	}
	if spans[1].Line != 1 || spans[1].Start != 1 {
		t.Errorf("second span = %+v, want line 1 starting at column 1", spans[1])
	}
}

// TestRegexTesterCopyAndHistory: ctrl+o cycles the quote format, ctrl+y
// copies the pattern in it, and closing records the pattern for the session.
func TestRegexTesterCopyAndHistory(t *testing.T) {
	var copied string
	orig := clipboardWrite
	clipboardWrite = func(text string) { copied = text }
	t.Cleanup(func() { clipboardWrite = orig })

	m := openTester(t, newSized())
	m = typeInto(m, `\d+`)
	m = drainKey(m, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if copied != "`\\d+`" {
		t.Errorf("copied %s, want the Go raw literal", copied)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	m = drainKey(m, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if copied != `"\\d+"` {
		t.Errorf("copied %s after cycling, want the Go quoted literal", copied)
	}

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.regexTesterOpen() {
		t.Fatal("esc must close the tester")
	}
	if m.regexHistory.Len() != 1 {
		t.Fatalf("history holds %d patterns, want 1", m.regexHistory.Len())
	}
	// Reopening offers it again: ↑ recalls the last pattern.
	m = openTester(t, m)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.regexTester.pattern.Text; got != `\d+` {
		t.Errorf("history recall = %q, want %q", got, `\d+`)
	}
}

// TestRegexTesterLargeTextEvaluatesOffLoop: past the async threshold the key
// handler returns a command instead of matching inline, so a big paste cannot
// stall the UI — and the result still lands.
func TestRegexTesterLargeTextEvaluatesOffLoop(t *testing.T) {
	m := openTester(t, newSized())
	big := strings.Repeat("needle haystack\n", regextest.AsyncThreshold/8)
	m.regexTester.focus = regexFocusText
	cmd, handled := m.pasteRegexTester(big)
	if !handled {
		t.Fatal("the tester must consume a paste into the test area")
	}
	if cmd == nil {
		t.Fatal("a text past the async threshold must be evaluated off the event loop")
	}
	m.regexTester.focus = regexFocusPattern
	tm, kcmd := m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m = tm.(Model)
	if kcmd == nil {
		t.Fatal("a keystroke over a large text must schedule an off-loop evaluation")
	}
	m = drainCmd(m, kcmd)
	if s := m.regexTester; s.pending || len(s.result.Matches) == 0 {
		t.Fatalf("off-loop result did not land: pending=%v matches=%d", s.pending, len(s.result.Matches))
	}
	// The stale first command must not overwrite the newer result.
	m = drainCmd(m, cmd)
	if got := m.regexTester.result.Matches; len(got) == 0 {
		t.Error("a stale evaluation overwrote the current result")
	}
}
