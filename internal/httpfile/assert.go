package httpfile

// assert.go parses the `# @assert <subject> [arg] <op> [expected]` directive
// (#2546): the declarative way to say what a response must look like, so a
// request that is re-run every few minutes is *checked* instead of eyeballed.
// The parser only recognises the directive and attaches it to its request —
// evaluating it needs a response, which exists at dispatch time
// (internal/httpclient/assert.go).
//
// A malformed directive is not a parse error of the block: the request still
// runs, and the directive lands in the pass/fail block as a failure carrying
// the reason. That puts the typo where the author reads the outcome instead
// of silently dropping the line.

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Assertion subjects — what the directive inspects.
const (
	AssertStatus   = "status"   // the response status code
	AssertHeader   = "header"   // a response header, named by Arg
	AssertJSONPath = "jsonpath" // a JSONPath into a JSON body, in Arg
	AssertBody     = "body"     // the whole response body
	AssertTime     = "time"     // the wall clock of the exchange
)

// AssertSubjects lists the subjects a directive may name, in the order
// completion offers them.
var AssertSubjects = []string{AssertStatus, AssertHeader, AssertJSONPath, AssertBody, AssertTime}

// Assertion operators.
const (
	OpEq       = "=="
	OpNe       = "!="
	OpLt       = "<"
	OpLe       = "<="
	OpGt       = ">"
	OpGe       = ">="
	OpContains = "contains"
	OpMatches  = "matches"
	OpExists   = "exists"
)

// AssertOps lists the operators a directive may use, in the order completion
// offers them. OpExists is unary: it takes no expected value.
var AssertOps = []string{OpEq, OpNe, OpLt, OpLe, OpGt, OpGe, OpContains, OpMatches, OpExists}

// AssertKeyword is the directive marker as it is typed.
const AssertKeyword = "@assert"

// Assertion is one `# @assert` directive of a request block (#2546). Subject
// is one of the Assert* constants, Arg the header name or JSONPath the subject
// needs ("" for the others), Op one of the Op* constants and Expected the
// value to compare with — "" for OpExists. Line/EndCol locate the directive
// in the file, exactly like Capture: the 1-based line and the 0-based rune
// column just past its last non-space character. Err carries the reason a
// directive could not be read; such a directive is kept so the failure is
// reported with the response rather than lost.
type Assertion struct {
	Subject  string `json:"subject"`
	Arg      string `json:"arg,omitempty"`
	Op       string `json:"op"`
	Expected string `json:"expected,omitempty"`
	Line     int    `json:"line,omitempty"`
	EndCol   int    `json:"-"`
	Err      string `json:"err,omitempty"`
	// Raw is the directive text after the marker, kept for a directive that
	// did not parse — the pass/fail block shows what was written.
	Raw string `json:"raw,omitempty"`
}

// String renders the directive the way the response pane and the Test
// Results window name it: `status == 200`, `header Content-Type contains
// json`, `body matches /ok/`. An unparsable directive renders as written.
func (a Assertion) String() string {
	if a.Err != "" && a.Subject == "" {
		return a.Raw
	}
	parts := []string{a.Subject}
	if a.Arg != "" {
		parts = append(parts, a.Arg)
	}
	parts = append(parts, a.Op)
	if a.Op != OpExists && a.Expected != "" {
		parts = append(parts, a.Expected)
	}
	return strings.Join(parts, " ")
}

// assertRE matches an assertion directive: a comment line (`#`, `##` or `//`)
// whose text is `@assert <spec>`. Three hashes are deliberately not accepted,
// for the reason captureRE gives: `###` opens a new request block.
var assertRE = regexp.MustCompile(`^[ \t]*(?:##?|//)[ \t]*@assert(?:[ \t]+(\S.*?))?[ \t]*$`)

// AssertDirective recognises an assertion directive line and returns the
// parsed assertion (#2546). ok is false when the line is not a directive at
// all; a directive that does not parse still answers ok with a.Err set.
// Exposed like CaptureDirective: the highlighter and the completion source
// must read exactly what the parser reads.
func AssertDirective(line string) (a Assertion, ok bool) {
	m := assertRE.FindStringSubmatch(line)
	if m == nil {
		return Assertion{}, false
	}
	return parseAssertSpec(m[1]), true
}

// assertAt builds the Assertion for a directive line, given its 0-based
// index.
func assertAt(line string, idx int) (Assertion, bool) {
	a, ok := AssertDirective(line)
	if !ok {
		return Assertion{}, false
	}
	a.Line = idx + 1
	a.EndCol = utf8.RuneCountInString(strings.TrimRight(line, " \t"))
	return a, true
}

// parseAssertSpec reads the text after the marker: subject, the argument the
// subject takes, the operator and the expected value. The expected value is
// everything after the operator, trimmed — a header value may hold spaces —
// with one pair of surrounding quotes stripped, or `/…/` for a regex.
func parseAssertSpec(spec string) Assertion {
	a := Assertion{Raw: spec}
	rest := strings.TrimSpace(spec)
	if rest == "" {
		a.Err = "missing subject: want one of " + strings.Join(AssertSubjects, ", ")
		return a
	}
	subject, rest := cutField(rest)
	if !isAssertSubject(subject) {
		a.Err = fmt.Sprintf("unknown subject %q: want one of %s", subject, strings.Join(AssertSubjects, ", "))
		return a
	}
	a.Subject = subject
	if subject == AssertHeader || subject == AssertJSONPath {
		arg, tail := cutField(rest)
		if arg == "" {
			what := "header name"
			if subject == AssertJSONPath {
				what = "JSONPath"
			}
			a.Err = "missing " + what + " after " + subject
			return a
		}
		a.Arg, rest = arg, tail
	}
	op, rest := cutField(rest)
	if op == "" {
		a.Err = "missing operator: want one of " + strings.Join(AssertOps, ", ")
		return a
	}
	if !isAssertOp(op) {
		a.Err = fmt.Sprintf("unknown operator %q: want one of %s", op, strings.Join(AssertOps, ", "))
		return a
	}
	a.Op = op
	if op == OpExists {
		if rest != "" {
			a.Err = fmt.Sprintf("%s takes no value, got %q", OpExists, rest)
		}
		return a
	}
	if rest == "" {
		a.Err = "missing expected value after " + op
		return a
	}
	a.Expected = unquoteExpected(rest, op == OpMatches)
	if a.Subject == AssertStatus && op != OpContains && op != OpMatches {
		if !isNumber(a.Expected) {
			a.Err = fmt.Sprintf("status compares with a number, got %q", a.Expected)
		}
	}
	return a
}

// cutField splits the first whitespace-delimited field off s and returns it
// with the trimmed remainder.
func cutField(s string) (field, rest string) {
	s = strings.TrimSpace(s)
	i := strings.IndexAny(s, " \t")
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimSpace(s[i:])
}

// unquoteExpected strips one pair of matching quotes around an expected
// value — `"a b"`, `'a b'` — and, for a regex, the `/…/` delimiters. A value
// without them is taken verbatim; a lone quote is content.
func unquoteExpected(s string, regex bool) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		switch {
		case regex && first == '/' && last == '/':
			return s[1 : len(s)-1]
		case (first == '"' && last == '"') || (first == '\'' && last == '\''):
			return s[1 : len(s)-1]
		}
	}
	return s
}

func isAssertSubject(s string) bool {
	for _, k := range AssertSubjects {
		if k == s {
			return true
		}
	}
	return false
}

func isAssertOp(s string) bool {
	for _, k := range AssertOps {
		if k == s {
			return true
		}
	}
	return false
}

// isNumber reports whether s reads as a decimal number.
func isNumber(s string) bool {
	if s == "" {
		return false
	}
	dot := false
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r == '.' && !dot:
			dot = true
		case (r == '-' || r == '+') && i == 0:
		default:
			return false
		}
	}
	return s != "-" && s != "+" && s != "."
}

// Asserting reports whether any request of the file carries an assertion.
func (f *File) Asserting() bool {
	for _, r := range f.Requests {
		if len(r.Assertions) > 0 {
			return true
		}
	}
	return false
}
