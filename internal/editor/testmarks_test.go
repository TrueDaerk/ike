package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/host"
	"ike/internal/lang"
)

// The fake test language (#1150): Go-shaped detection on _test.tmk files.
func init() {
	lang.Register(lang.Language{
		ID:         "tmklang",
		Extensions: []string{"tmk"},
		Test: &lang.TestSpec{
			FilePattern: `_test\.tmk$`,
			Pattern:     `^func (?P<name>(?P<kind>Test)\w*)\s*\(`,
			Kinds:       map[string][]string{"Test": {"tmk", "test", "-run", "^{name}$"}},
			FileArgv:    []string{"tmk", "test"},
			Tool:        "tmk",
		},
	})
}

// tmEditor loads a test-file buffer with two test declarations.
func tmEditor(t *testing.T) Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x_test.tmk")
	content := "package x\nfunc TestOne(t T) {\n}\nfunc helper() {\n}\nfunc TestTwo(t T) {\n}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	m.Configure(host.MapConfig{"editor.line_numbers": "true"})
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	m.SetFocused(true)
	return m
}

func TestTestMarksDetected(t *testing.T) {
	m := tmEditor(t)
	if got := m.TestLines(); len(got) != 2 || got[0] != 1 || got[1] != 5 {
		t.Fatalf("TestLines = %v, want [1 5]", got)
	}
	if tm, ok := m.TestMarkAt(1); !ok || tm.Name != "TestOne" || tm.Kind != "Test" {
		t.Fatalf("TestMarkAt(1) = %+v, %v", tm, ok)
	}
	if _, ok := m.TestMarkAt(2); ok {
		t.Fatal("line 2 carries no test")
	}
}

func TestNearestTestAt(t *testing.T) {
	m := tmEditor(t)
	if tm, ok := m.NearestTestAt(0); ok {
		t.Fatalf("no test at or above line 0, got %+v", tm)
	}
	if tm, ok := m.NearestTestAt(3); !ok || tm.Name != "TestOne" {
		t.Fatalf("NearestTestAt(3) = %+v, %v — want the preceding TestOne", tm, ok)
	}
	if tm, ok := m.NearestTestAt(6); !ok || tm.Name != "TestTwo" {
		t.Fatalf("NearestTestAt(6) = %+v, %v — want the enclosing TestTwo", tm, ok)
	}
}

func TestTestMarksRecomputeOnEdit(t *testing.T) {
	m := tmEditor(t)
	// Append a new test declaration at the buffer end: o<decl><esc>.
	m = send(m, key('G'))
	m = send(m, key('o'))
	m = typeKeys(m, "func TestThree(t T) {")
	m = send(m, special(rune(27))) // esc
	if got := m.TestLines(); len(got) != 3 {
		t.Fatalf("after edit TestLines = %v, want 3 marks", got)
	}
}

func TestTestMarksNotOnNonTestFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.tmk")
	if err := os.WriteFile(path, []byte("func TestOne(t T) {\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	if got := m.TestLines(); len(got) != 0 {
		t.Fatalf("non-test file must carry no marks, got %v", got)
	}
}

// TestTestMarkGutterRender pins the sign: ▶ on a test line, and the
// breakpoint ● winning the cell when both apply (breakpoints on test
// functions stay visible and toggleable).
func TestTestMarkGutterRender(t *testing.T) {
	m := tmEditor(t)
	out := m.View()
	if !strings.Contains(out, "▶") {
		t.Fatal("the gutter must render the test run marker")
	}
	var bps []int
	m.SetBreakpointSource(func(string) []int { return bps })
	bps = []int{1, 5} // both test lines carry breakpoints now
	out = m.View()
	if strings.Contains(out, "▶") {
		t.Fatal("a breakpoint must win the sign cell over the test marker")
	}
	if !strings.Contains(out, "●") {
		t.Fatal("the breakpoint glyph must render")
	}
}
