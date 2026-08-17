package matcher

import "regexp"

// builtins.go ships the built-in problem matchers (#1915): "go" for go
// build/vet, "generic" for the file:line:col: message convention, "tsc" for
// the TypeScript compiler and "python" for interpreter tracebacks. Task
// providers reference them by name as their defaults; run configurations and
// [[tasks.matcher]] entries may name them too.

// goRule reads go build / go vet output: `./main.go:5:2: undefined: foo`.
// The column is optional (some toolchain messages omit it); the file must end
// in .go so the matcher stays quiet on unrelated colon-separated lines.
var goRule = Rule{
	MatcherName: "go",
	Regexp:      regexp.MustCompile(`^\s*([^\s:]+\.go):(\d+)(?::(\d+))?:\s*(.+)$`),
	File:        1, Line: 2, Col: 3, Message: 4,
	DefaultSeverity: SevError,
}

// genericRule reads the near-universal `file:line:col: message` convention
// (gcc, clang, rustc's short form, many linters). The file part must carry a
// dot or slash so timestamps and prose with colons do not match; a leading
// severity word in the message ("warning: unused variable") sets the severity.
var genericRule = Rule{
	MatcherName: "generic",
	Regexp:      regexp.MustCompile(`^\s*([^:\s]*[./][^:\s]*):(\d+)(?::(\d+))?:\s*(?:(error|warning|warn|info|note|hint)[:,]\s*)?(.+)$`),
	File:        1, Line: 2, Col: 3, Severity: 4, Message: 5,
	DefaultSeverity: SevError,
}

// tscRule reads TypeScript compiler diagnostics:
// `src/app.ts(12,5): error TS2304: Cannot find name 'foo'.`
var tscRule = Rule{
	MatcherName: "tsc",
	Regexp:      regexp.MustCompile(`^\s*(\S.*?)\((\d+),(\d+)\):\s*(error|warning|message)\s+TS\d+:\s*(.+)$`),
	File:        1, Line: 2, Col: 3, Severity: 4, Message: 5,
	DefaultSeverity: SevError,
}

// pythonMatcher reads interpreter tracebacks — a multi-line sequence, so it
// is a small state machine rather than a Rule: `File "x.py", line N` frames
// set the location (the deepest frame wins), and the first following
// non-indented line is the exception summary that becomes the message.
type pythonMatcher struct{}

func (pythonMatcher) Name() string    { return "python" }
func (pythonMatcher) NewState() State { return &pythonState{} }

var (
	pyFrameRe = regexp.MustCompile(`^\s+File "(.+)", line (\d+)`)
	// pyErrRe is the exception summary line: `NameError: name 'x' is not
	// defined`, or a bare `KeyboardInterrupt`. Anchored to column 0 — frame
	// code lines and carets are indented.
	pyErrRe = regexp.MustCompile(`^([A-Za-z_][\w.]*(?:Error|Exception|Warning|Exit|Interrupt|Iteration))(?::\s*(.*))?$`)
)

// pythonState tracks the deepest traceback frame seen since the last emit.
type pythonState struct {
	file string
	line int
}

// Feed implements State.
func (s *pythonState) Feed(line string) []Problem {
	if m := pyFrameRe.FindStringSubmatch(line); m != nil {
		s.file, s.line = m[1], atoi(m[2])
		return nil
	}
	if s.file == "" {
		return nil
	}
	m := pyErrRe.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	msg := m[1]
	if m[2] != "" {
		msg = m[1] + ": " + m[2]
	}
	sev := SevError
	// A traceback ending in a *Warning class (warnings.warn with an error
	// filter off) reads as a warning, everything else as an error.
	if w := m[1]; len(w) > 7 && w[len(w)-7:] == "Warning" {
		sev = SevWarning
	}
	p := Problem{File: s.file, Line: s.line, Severity: sev, Message: msg}
	s.file, s.line = "", 0
	return []Problem{p}
}

// Flush implements State: a traceback whose summary line never arrived
// (truncated output) is dropped — a location without a message is noise.
func (s *pythonState) Flush() []Problem { return nil }

// builtins lists the shipped matchers in a stable order.
var builtins = []Matcher{goRule, genericRule, tscRule, pythonMatcher{}}

// Builtins returns the built-in matchers, in a stable order.
func Builtins() []Matcher { return append([]Matcher(nil), builtins...) }

// Builtin resolves a built-in matcher by name.
func Builtin(name string) (Matcher, bool) {
	for _, m := range builtins {
		if m.Name() == name {
			return m, true
		}
	}
	return nil, false
}

// BuiltinNames lists the built-in matcher names, in the Builtins order.
func BuiltinNames() []string {
	out := make([]string, 0, len(builtins))
	for _, m := range builtins {
		out = append(out, m.Name())
	}
	return out
}
