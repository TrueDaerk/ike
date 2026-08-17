package testresults

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/lang"
)

func sampleResults() []lang.TestResult {
	return []lang.TestResult{
		{Group: "example/pkg", Name: "TestOK", Status: lang.TestPass, Elapsed: 0.12, Output: "ok output\n", RerunID: "TestOK"},
		{Group: "example/pkg", Name: "TestTable", Status: lang.TestFail, RerunID: "TestTable"},
		{Group: "example/pkg", Name: "TestTable/case_a", Status: lang.TestFail, Output: "    x_test.go:7: nope\n", File: "x_test.go", Line: 7, RerunID: "TestTable"},
		{Group: "example/pkg", Name: "TestSkipped", Status: lang.TestSkip, RerunID: "TestSkipped"},
	}
}

func filled(t *testing.T) *Model {
	t.Helper()
	m := New(nil)
	m.SetSize(100, 12)
	m.StartRun("tests: pkg", "/proj/pkg")
	if !m.Running() {
		t.Fatal("StartRun must mark the panel running")
	}
	m.FinishRun(sampleResults(), "raw run output\n")
	return &m
}

func key(k string) tea.KeyPressMsg {
	if len(k) == 1 {
		return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
	}
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}
	t := tea.KeyPressMsg{}
	return t
}

func TestTreeShape(t *testing.T) {
	m := filled(t)
	// group + TestOK + TestTable + case_a + TestSkipped
	if m.Rows() != 5 {
		t.Fatalf("rows = %d, want 5", m.Rows())
	}
	v := m.View()
	if !strings.Contains(v, "example/pkg") || !strings.Contains(v, "case_a") {
		t.Fatalf("view misses tree nodes:\n%s", v)
	}
	if !strings.Contains(v, "1 passed") || !strings.Contains(v, "2 failed") || !strings.Contains(v, "1 skipped") {
		t.Fatalf("summary wrong:\n%s", v)
	}
	if !strings.Contains(v, "✗") || !strings.Contains(v, "✓") || !strings.Contains(v, "○") {
		t.Fatalf("status glyphs missing:\n%s", v)
	}
}

func TestFailedIDsDeduped(t *testing.T) {
	m := filled(t)
	if got := m.FailedIDs(); !reflect.DeepEqual(got, []string{"TestTable"}) {
		t.Fatalf("FailedIDs = %v, want [TestTable]", got)
	}
}

func TestActivateFailureLocation(t *testing.T) {
	m := filled(t)
	// Move the cursor to the subtest row (group, TestOK, TestTable, case_a).
	m.cursor = 3
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter on a failed test must emit a command")
	}
	msg, ok := cmd().(OpenLocationMsg)
	if !ok {
		t.Fatalf("message = %T, want OpenLocationMsg", cmd())
	}
	want := filepath.Join("/proj/pkg", "x_test.go")
	if msg.Path != want || msg.Line != 6 {
		t.Fatalf("open = %+v, want %s:6 (0-based)", msg, want)
	}
}

func TestActivatePassedTestLocates(t *testing.T) {
	m := filled(t)
	m.cursor = 1 // TestOK: no failure location
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter on a passed test must emit a locate command")
	}
	if msg, ok := cmd().(LocateTestMsg); !ok || msg.RerunID != "TestOK" {
		t.Fatalf("message = %#v, want LocateTestMsg{TestOK}", cmd())
	}
}

func TestActivateGroupPrefersFailure(t *testing.T) {
	m := filled(t)
	m.cursor = 0 // the group header
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter on the group must open its first failing descendant")
	}
	if msg, ok := cmd().(OpenLocationMsg); !ok || !strings.HasSuffix(msg.Path, "x_test.go") {
		t.Fatalf("message = %#v", cmd())
	}
}

func TestRerunKeys(t *testing.T) {
	m := filled(t)
	if msg := m.Update(key("r"))(); !reflect.DeepEqual(msg, RerunMsg{}) {
		t.Fatalf("r = %#v, want RerunMsg{}", msg)
	}
	if msg := m.Update(key("f"))(); !reflect.DeepEqual(msg, RerunMsg{Failed: true}) {
		t.Fatalf("f = %#v, want RerunMsg{Failed}", msg)
	}
	m.cursor = 2 // TestTable
	if msg := m.Update(key("t"))(); !reflect.DeepEqual(msg, RerunMsg{ID: "TestTable"}) {
		t.Fatalf("t = %#v, want RerunMsg{ID}", msg)
	}
	// With nothing failed, f is inert.
	m.FinishRun([]lang.TestResult{{Group: "p", Name: "TestOK", Status: lang.TestPass, RerunID: "TestOK"}}, "")
	if cmd := m.Update(key("f")); cmd != nil {
		t.Fatal("f with no failures must be inert")
	}
}

func TestRawToggleAndDetail(t *testing.T) {
	m := filled(t)
	m.cursor = 3 // subtest with output
	if v := m.View(); !strings.Contains(v, "nope") {
		t.Fatalf("detail must show the selected test's output:\n%s", v)
	}
	m.Update(key("o"))
	if v := m.View(); !strings.Contains(v, "raw run output") {
		t.Fatalf("raw mode must show the run output:\n%s", v)
	}
	m.Update(key("o"))
	if v := m.View(); strings.Contains(v, "raw run output") {
		t.Fatalf("toggling back must restore per-test output:\n%s", v)
	}
}

func TestNoParserFallsBackToRaw(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.StartRun("tests: x", "/p")
	m.FinishRun(nil, "plain text output\n")
	v := m.View()
	if !strings.Contains(v, "no structured results") {
		t.Fatalf("summary must flag the raw fallback:\n%s", v)
	}
	if !strings.Contains(v, "plain text output") {
		t.Fatalf("raw output must show by default with no results:\n%s", v)
	}
}

func TestCursorSurvivesRefresh(t *testing.T) {
	m := filled(t)
	m.cursor = 2 // TestTable
	m.StartRun("tests: pkg", "/proj/pkg")
	m.FinishRun(sampleResults(), "")
	if m.Cursor() != 2 {
		t.Fatalf("cursor = %d, want 2 (same node path)", m.Cursor())
	}
}

func TestClickAndDoubleClick(t *testing.T) {
	m := filled(t)
	at := time.Unix(0, 0)
	m.now = func() time.Time { return at }
	m.Click(2, 4) // row index 3: the subtest
	if m.Cursor() != 3 {
		t.Fatalf("cursor = %d, want 3", m.Cursor())
	}
	at = at.Add(100 * time.Millisecond)
	if cmd := m.Click(2, 4); cmd == nil {
		t.Fatal("double click must activate the row")
	}
	// A click right of the separator focuses the detail column.
	tw, _ := m.colWidths()
	m.Click(tw+2, 2)
	if !m.detailFocus {
		t.Fatal("detail column click must focus it")
	}
}
