// Package matcher turns build-tool output lines into structured problems
// (#1915): named, regex-based problem matchers in the VS Code mold. A run
// configuration references matchers by name; the run terminal's output is
// teed through an Engine which feeds every referenced matcher line by line
// and emits file/line/col/severity/message records for the Problems tool
// window. Unmatched output is simply passed over — the terminal stream itself
// is never altered.
//
// The package is UI-free and dependency-light (stdlib plus the ANSI stripper)
// so both the app wiring and the config validation can use it.
package matcher

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// LSP-scale severities (protocol.Severity*): the Problems window consumes
// these directly.
const (
	SevError   = 1
	SevWarning = 2
	SevInfo    = 3
	SevHint    = 4
)

// Problem is one matched finding. File is the path exactly as captured —
// resolving it against the run's working directory is the caller's job. Line
// and Col are 1-based; Col 0 means unknown.
type Problem struct {
	File     string
	Line     int
	Col      int
	Severity int
	Message  string
}

// Matcher names one way of reading tool output. NewState mints the per-run
// parsing state, so one registered matcher serves concurrent runs.
type Matcher interface {
	Name() string
	NewState() State
}

// State consumes one run's output line by line. Feed returns the problems the
// line completed (usually zero or one); Flush returns whatever an unfinished
// multi-line sequence can still salvage at end of output.
type State interface {
	Feed(line string) []Problem
	Flush() []Problem
}

// Rule is a single-line regex matcher: the capture groups indexed by
// File/Line/Col/Severity/Message pull the problem's fields out of a matching
// line. File, Line and Message are required (index >= 1); Col and Severity
// are optional (0 = absent). A Severity group capturing an unknown word — or
// no Severity group at all — falls back to DefaultSeverity.
type Rule struct {
	MatcherName     string
	Regexp          *regexp.Regexp
	File            int
	Line            int
	Col             int
	Severity        int
	Message         int
	DefaultSeverity int
}

// Name implements Matcher.
func (r Rule) Name() string { return r.MatcherName }

// NewState implements Matcher; a Rule is stateless, so the state is itself.
func (r Rule) NewState() State { return ruleState{r} }

type ruleState struct{ r Rule }

// Feed implements State: one line either matches the rule completely or
// contributes nothing.
func (s ruleState) Feed(line string) []Problem {
	m := s.r.Regexp.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	p := Problem{
		File:     m[s.r.File],
		Line:     atoi(m[s.r.Line]),
		Message:  m[s.r.Message],
		Severity: s.r.DefaultSeverity,
	}
	if p.File == "" || p.Line <= 0 || p.Message == "" {
		return nil
	}
	if s.r.Col > 0 {
		p.Col = atoi(m[s.r.Col])
	}
	if s.r.Severity > 0 {
		if sev, ok := SeverityWord(m[s.r.Severity]); ok {
			p.Severity = sev
		}
	}
	if p.Severity == 0 {
		p.Severity = SevError
	}
	return []Problem{p}
}

// Flush implements State; a single-line rule holds nothing back.
func (ruleState) Flush() []Problem { return nil }

// SeverityWord maps a captured severity word onto the LSP scale,
// case-insensitively: error, warning/warn, info/note, hint.
func SeverityWord(w string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(w)) {
	case "error", "fatal":
		return SevError, true
	case "warning", "warn":
		return SevWarning, true
	case "info", "note", "message":
		return SevInfo, true
	case "hint":
		return SevHint, true
	}
	return 0, false
}

// atoi is strconv.Atoi without the error: capture groups are \d+, so the only
// failure mode is overflow, which 0 handles as "invalid".
func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
		if n > 1<<30 {
			return 0
		}
	}
	return n
}

// Compile builds a custom single-line Rule from its config spelling (#1915,
// [[tasks.matcher]]) and validates it: the pattern must compile, File, Line
// and Message groups are required, and every group index must exist in the
// pattern. The error text is user-facing — config validation surfaces it
// verbatim.
func Compile(name, pattern string, file, line, col, severity, message int, defaultSeverity string) (Rule, error) {
	if name == "" {
		return Rule{}, fmt.Errorf("matcher needs a name")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return Rule{}, fmt.Errorf("matcher %q: invalid regex: %v", name, err)
	}
	groups := re.NumSubexp()
	check := func(what string, idx int, required bool) error {
		if idx == 0 && !required {
			return nil
		}
		if idx < 1 {
			return fmt.Errorf("matcher %q: %s group index is required (1-based capture group)", name, what)
		}
		if idx > groups {
			return fmt.Errorf("matcher %q: %s group %d exceeds the pattern's %d capture groups", name, what, idx, groups)
		}
		return nil
	}
	for _, c := range []struct {
		what     string
		idx      int
		required bool
	}{{"file", file, true}, {"line", line, true}, {"message", message, true}, {"col", col, false}, {"severity", severity, false}} {
		if err := check(c.what, c.idx, c.required); err != nil {
			return Rule{}, err
		}
	}
	sev := SevError
	if defaultSeverity != "" {
		s, ok := SeverityWord(defaultSeverity)
		if !ok {
			return Rule{}, fmt.Errorf("matcher %q: unknown default severity %q (error, warning, info, hint)", name, defaultSeverity)
		}
		sev = s
	}
	return Rule{
		MatcherName: name, Regexp: re,
		File: file, Line: line, Col: col, Severity: severity, Message: message,
		DefaultSeverity: sev,
	}, nil
}

// Engine tees one run's raw output through a set of matchers: chunks are
// assembled into lines, ANSI escape sequences stripped, and every complete
// line offered to every matcher's state. Problems are deduplicated over the
// whole run (two overlapping matchers reading the same line — or a tool
// printing the same error twice — yield one entry). Not safe for concurrent
// use; the caller serializes Feed/Close.
type Engine struct {
	states []State
	buf    []byte
	seen   map[Problem]bool
}

// NewEngine builds the engine over ms; a nil or empty set yields an engine
// that never matches.
func NewEngine(ms []Matcher) *Engine {
	e := &Engine{seen: map[Problem]bool{}}
	for _, m := range ms {
		e.states = append(e.states, m.NewState())
	}
	return e
}

// Feed consumes one raw output chunk (PTY bytes, \r\n line endings, ANSI
// colours) and returns the problems its complete lines produced. A trailing
// partial line is buffered for the next chunk.
func (e *Engine) Feed(chunk []byte) []Problem {
	if len(e.states) == 0 || len(chunk) == 0 {
		return nil
	}
	e.buf = append(e.buf, chunk...)
	var out []Problem
	for {
		i := bytes.IndexByte(e.buf, '\n')
		if i < 0 {
			break
		}
		line := string(e.buf[:i])
		e.buf = e.buf[i+1:]
		out = append(out, e.feedLine(line)...)
	}
	return out
}

// Close flushes the buffered partial line and every matcher's pending state;
// call it once when the run's output ends.
func (e *Engine) Close() []Problem {
	if len(e.states) == 0 {
		return nil
	}
	var out []Problem
	if len(e.buf) > 0 {
		out = append(out, e.feedLine(string(e.buf))...)
		e.buf = nil
	}
	for _, s := range e.states {
		for _, p := range s.Flush() {
			if !e.seen[p] {
				e.seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// feedLine strips the line and offers it to every matcher, deduplicating the
// results against the whole run.
func (e *Engine) feedLine(line string) []Problem {
	line = strings.TrimRight(ansi.Strip(line), "\r")
	var out []Problem
	for _, s := range e.states {
		for _, p := range s.Feed(line) {
			if !e.seen[p] {
				e.seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}
