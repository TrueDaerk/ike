package matcher

import (
	"strings"
	"testing"
)

// feedAll runs one matcher's fresh state over the lines and collects every
// problem, flush included.
func feedAll(t *testing.T, m Matcher, lines ...string) []Problem {
	t.Helper()
	s := m.NewState()
	var out []Problem
	for _, l := range lines {
		out = append(out, s.Feed(l)...)
	}
	return append(out, s.Flush()...)
}

func TestGoMatcher(t *testing.T) {
	m, ok := Builtin("go")
	if !ok {
		t.Fatal("go matcher not registered")
	}
	got := feedAll(t, m,
		"# ike/internal/app",
		"./main.go:5:2: undefined: foo",
		"internal/app/run.go:132:10: cannot use x (variable of type int)",
		"vet.go:8: unreachable code",
		"some prose without a location",
	)
	want := []Problem{
		{File: "./main.go", Line: 5, Col: 2, Severity: SevError, Message: "undefined: foo"},
		{File: "internal/app/run.go", Line: 132, Col: 10, Severity: SevError, Message: "cannot use x (variable of type int)"},
		{File: "vet.go", Line: 8, Severity: SevError, Message: "unreachable code"},
	}
	assertProblems(t, got, want)
}

func TestGenericMatcher(t *testing.T) {
	m, _ := Builtin("generic")
	got := feedAll(t, m,
		"src/lib.c:12:8: warning: unused variable 'x'",
		"a/b.rs:3:14: error: mismatched types",
		"pkg/mod.py:44: something failed",
		"12:30:45 INFO starting up", // timestamp, no file — must not match
	)
	want := []Problem{
		{File: "src/lib.c", Line: 12, Col: 8, Severity: SevWarning, Message: "unused variable 'x'"},
		{File: "a/b.rs", Line: 3, Col: 14, Severity: SevError, Message: "mismatched types"},
		{File: "pkg/mod.py", Line: 44, Severity: SevError, Message: "something failed"},
	}
	assertProblems(t, got, want)
}

func TestTscMatcher(t *testing.T) {
	m, _ := Builtin("tsc")
	got := feedAll(t, m,
		"src/app.ts(12,5): error TS2304: Cannot find name 'foo'.",
		"src/util.ts(3,1): warning TS6133: 'x' is declared but never used.",
		"src/app.ts:12:5 - error TS2304: nope", // pretty format not covered
	)
	want := []Problem{
		{File: "src/app.ts", Line: 12, Col: 5, Severity: SevError, Message: "Cannot find name 'foo'."},
		{File: "src/util.ts", Line: 3, Col: 1, Severity: SevWarning, Message: "'x' is declared but never used."},
	}
	assertProblems(t, got, want)
}

func TestPythonMatcher(t *testing.T) {
	m, _ := Builtin("python")
	got := feedAll(t, m,
		"Traceback (most recent call last):",
		`  File "app/main.py", line 10, in <module>`,
		"    run()",
		`  File "app/run.py", line 4, in run`,
		"    return 1 / 0",
		"ZeroDivisionError: division by zero",
	)
	// The deepest frame carries the location.
	want := []Problem{
		{File: "app/run.py", Line: 4, Severity: SevError, Message: "ZeroDivisionError: division by zero"},
	}
	assertProblems(t, got, want)
}

func TestPythonMatcherBareExceptionAndSyntaxError(t *testing.T) {
	m, _ := Builtin("python")
	got := feedAll(t, m,
		"Traceback (most recent call last):",
		`  File "loop.py", line 2, in <module>`,
		"KeyboardInterrupt",
		// A second traceback through the same state.
		`  File "bad.py", line 1`,
		"    def broken(:",
		"               ^",
		"SyntaxError: invalid syntax",
	)
	want := []Problem{
		{File: "loop.py", Line: 2, Severity: SevError, Message: "KeyboardInterrupt"},
		{File: "bad.py", Line: 1, Severity: SevError, Message: "SyntaxError: invalid syntax"},
	}
	assertProblems(t, got, want)
}

func TestEngineChunkingANSIAndDedup(t *testing.T) {
	e := NewEngine([]Matcher{goRule, genericRule})
	// One error split across chunks, wrapped in ANSI colour, CRLF endings —
	// and matched by both the go and the generic matcher (dedup keeps one).
	var got []Problem
	got = append(got, e.Feed([]byte("\x1b[31m./x.go:3"))...)
	got = append(got, e.Feed([]byte(":7: boom\x1b[0m\r\n./x.go:3:7: boom\r\n"))...)
	got = append(got, e.Close()...)
	want := []Problem{{File: "./x.go", Line: 3, Col: 7, Severity: SevError, Message: "boom"}}
	assertProblems(t, got, want)
}

func TestEngineFlushesTrailingPartialLine(t *testing.T) {
	e := NewEngine([]Matcher{goRule})
	if got := e.Feed([]byte("./y.go:1:1: no newline at end")); len(got) != 0 {
		t.Fatalf("partial line matched early: %v", got)
	}
	got := e.Close()
	want := []Problem{{File: "./y.go", Line: 1, Col: 1, Severity: SevError, Message: "no newline at end"}}
	assertProblems(t, got, want)
}

func TestCompileValidCustomMatcher(t *testing.T) {
	r, err := Compile("mylint", `^(\S+) at line (\d+): (.+)$`, 1, 2, 0, 0, 3, "warning")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	got := feedAll(t, r, "foo.txt at line 9: dodgy content")
	want := []Problem{{File: "foo.txt", Line: 9, Severity: SevWarning, Message: "dodgy content"}}
	assertProblems(t, got, want)
}

func TestCompileRejectsBadInput(t *testing.T) {
	cases := []struct {
		name                      string
		pattern                   string
		file, line, col, sev, msg int
		defSev                    string
		wantSub                   string
	}{
		{"bad", `([unclosed`, 1, 2, 0, 0, 3, "", "invalid regex"},
		{"", `(a):(\d+): (.+)`, 1, 2, 0, 0, 3, "", "needs a name"},
		{"nofile", `(a):(\d+): (.+)`, 0, 2, 0, 0, 3, "", "file group index is required"},
		{"range", `(a):(\d+): (.+)`, 1, 2, 0, 0, 9, "", "exceeds the pattern's 3 capture groups"},
		{"badsev", `(a):(\d+): (.+)`, 1, 2, 0, 0, 3, "loud", "unknown default severity"},
	}
	for _, c := range cases {
		_, err := Compile(c.name, c.pattern, c.file, c.line, c.col, c.sev, c.msg, c.defSev)
		if err == nil || !strings.Contains(err.Error(), c.wantSub) {
			t.Errorf("Compile(%q): err = %v, want containing %q", c.name, err, c.wantSub)
		}
	}
}

func assertProblems(t *testing.T, got, want []Problem) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d problems %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("problem %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
