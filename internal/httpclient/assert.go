package httpclient

// assert.go evaluates a request's `# @assert` directives (#2546) against the
// response that just arrived. Parsing them is the file layer's job
// (httpfile.Assertion); checking them needs a response, so it happens here,
// right after one exists — and after the captures, since both read the same
// full body.
//
// An assertion never *errors* the exchange: the response arrived and is worth
// reading. A failed one marks the run as failed — the pane says so above the
// body, the completion notice counts it, and the Test Results window lists
// it — but the body, headers and history entry are exactly what they would
// have been without the directive.

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ike/internal/httpfile"
	"ike/internal/jqplay"
)

// AssertResult is the outcome of one assertion directive (#2546): the
// directive itself, whether it passed, the Actual value the subject yielded
// (rendered for a person), and — for a failure — the reason. A directive that
// did not parse fails with the parser's reason.
type AssertResult struct {
	httpfile.Assertion
	Pass   bool   `json:"pass"`
	Actual string `json:"actual,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Describe renders the result the way the pane's pass/fail block and the
// Test Results window show it: the directive, then the reason for a failure.
func (r AssertResult) Describe() string {
	s := r.Assertion.String()
	if r.Pass || r.Reason == "" {
		return s
	}
	return s + " — " + r.Reason
}

// AssertionsFailed counts the failed assertions of the response; 0 for a
// response without directives.
func (r *Response) AssertionsFailed() int {
	if r == nil {
		return 0
	}
	n := 0
	for _, a := range r.Assertions {
		if !a.Pass {
			n++
		}
	}
	return n
}

// AssertionSummary is the short form the status row and the notice carry:
// "3 assertions passed", "1 of 3 assertions failed"; "" without directives.
func (r *Response) AssertionSummary() string {
	if r == nil || len(r.Assertions) == 0 {
		return ""
	}
	total := len(r.Assertions)
	if failed := r.AssertionsFailed(); failed > 0 {
		return fmt.Sprintf("%d of %s failed", failed, pluralAssertions(total))
	}
	return pluralAssertions(total) + " passed"
}

func pluralAssertions(n int) string {
	if n == 1 {
		return "1 assertion"
	}
	return fmt.Sprintf("%d assertions", n)
}

// applyAssertions runs req's assertion directives against resp, filling
// resp.Assertions. Called for the dispatch paths that know the .http
// request; a re-send (#1832) repeats a stored snapshot and has none.
func applyAssertions(resp *Response, req *httpfile.Request) {
	if resp == nil || req == nil || len(req.Assertions) == 0 {
		return
	}
	// A spooled body (#2157) is only partly in memory; a body or JSONPath
	// assertion must see all of it. A missing spool degrades to the head,
	// which the warning already says (applyCaptures runs first).
	body, _ := resp.FullBody()
	resp.Assertions = runAssertions(req.Assertions, resp, body)
}

// runAssertions evaluates every directive. Each is independent: one that
// fails does not stop the others, so a run reports every broken expectation
// at once rather than the first.
func runAssertions(assertions []httpfile.Assertion, resp *Response, body []byte) []AssertResult {
	out := make([]AssertResult, 0, len(assertions))
	for _, a := range assertions {
		out = append(out, evalAssertion(a, resp, body))
	}
	return out
}

// evalAssertion checks one directive, phrasing every failure in terms of the
// *response* — the thing the author of the directive is looking at.
func evalAssertion(a httpfile.Assertion, resp *Response, body []byte) AssertResult {
	res := AssertResult{Assertion: a}
	if a.Err != "" {
		res.Reason = a.Err
		return res
	}
	actual, present, err := assertActual(a, resp, body)
	if err != nil {
		res.Reason = err.Error()
		return res
	}
	res.Actual = actual
	if a.Op == httpfile.OpExists {
		res.Pass = present
		if !present {
			res.Reason = assertMissing(a)
		}
		return res
	}
	if !present {
		res.Reason = assertMissing(a)
		return res
	}
	ok, err := compareAssert(a, actual)
	if err != nil {
		res.Reason = err.Error()
		return res
	}
	res.Pass = ok
	if !ok {
		res.Reason = "got " + quoteActual(a, actual)
	}
	return res
}

// assertMissing names what an absent subject was.
func assertMissing(a httpfile.Assertion) string {
	switch a.Subject {
	case httpfile.AssertHeader:
		return "header " + a.Arg + " is not present"
	case httpfile.AssertJSONPath:
		return a.Arg + " matched no value in the response body"
	}
	return "no value"
}

// quoteActual renders the actual value for a failure message: quoted when it
// is text, bare when it is a number or a duration — `got "text/html"` reads,
// `got "404"` does not.
func quoteActual(a httpfile.Assertion, actual string) string {
	switch a.Subject {
	case httpfile.AssertStatus, httpfile.AssertTime:
		return actual
	case httpfile.AssertBody:
		return strconv.Quote(clipActual(actual))
	}
	if _, err := strconv.ParseFloat(actual, 64); err == nil {
		return actual
	}
	return strconv.Quote(clipActual(actual))
}

// actualLimit bounds the actual value quoted in a failure message: a body
// assertion over a megabyte answer must not put the megabyte into one row.
const actualLimit = 120

func clipActual(s string) string {
	s = strings.ReplaceAll(s, "\n", "⏎")
	if r := []rune(s); len(r) > actualLimit {
		return string(r[:actualLimit]) + "…"
	}
	return s
}

// assertActual yields the subject's value: the text to compare, whether the
// subject is present at all (a header that is not there, a path that matched
// nothing), and an error for a subject that could not be read.
func assertActual(a httpfile.Assertion, resp *Response, body []byte) (actual string, present bool, err error) {
	switch a.Subject {
	case httpfile.AssertStatus:
		return strconv.Itoa(resp.StatusCode), true, nil
	case httpfile.AssertHeader:
		values := resp.Headers.Values(a.Arg)
		if len(values) == 0 {
			return "", false, nil
		}
		return strings.Join(values, ", "), true, nil
	case httpfile.AssertBody:
		return string(body), true, nil
	case httpfile.AssertTime:
		return resp.Duration.String(), true, nil
	case httpfile.AssertJSONPath:
		if len(bytes.TrimSpace(body)) == 0 {
			return "", false, errors.New("the response body is empty")
		}
		value, err := jqplay.EvaluateRaw(jsonPathToJQ(a.Arg), string(body))
		switch {
		case err == nil:
			return value, true, nil
		case errors.Is(err, jqplay.ErrNoValue):
			return "", false, nil
		default:
			var input *jqplay.InputError
			if errors.As(err, &input) {
				return "", false, fmt.Errorf("the response body is not JSON: %s", input.Detail)
			}
			return "", false, fmt.Errorf("%s: %v", a.Arg, err)
		}
	}
	return "", false, fmt.Errorf("unknown subject %q", a.Subject)
}

// jsonPathToJQ turns the dotted-and-bracketed JSONPath spelling into the jq
// program that reads the same value: `$.items[0].id` → `.items[0].id`,
// `$[0]` → `.[0]`, `$` → `.`. Anything past that subset (filters, wildcards
// with `..`) is handed to jq as written, whose own error then names it.
func jsonPathToJQ(path string) string {
	p := strings.TrimSpace(path)
	p = strings.TrimPrefix(p, "$")
	switch {
	case p == "":
		return "."
	case strings.HasPrefix(p, "["):
		return "." + p
	case strings.HasPrefix(p, "."):
		return p
	}
	return "." + p
}

// compareAssert applies the operator to the actual and expected values. The
// equality operators compare numerically when both sides are numbers (a
// JSON `42` against a typed `42`, a status code) and as text otherwise; the
// ordering operators need numbers — or, for the time subject, durations.
func compareAssert(a httpfile.Assertion, actual string) (bool, error) {
	switch a.Op {
	case httpfile.OpContains:
		return strings.Contains(actual, a.Expected), nil
	case httpfile.OpMatches:
		re, err := regexp.Compile(a.Expected)
		if err != nil {
			return false, fmt.Errorf("invalid regex %q: %v", a.Expected, regexErr(err))
		}
		return re.MatchString(actual), nil
	}
	if a.Subject == httpfile.AssertTime {
		return compareDurations(a, actual)
	}
	lhs, lerr := strconv.ParseFloat(strings.TrimSpace(actual), 64)
	rhs, rerr := strconv.ParseFloat(strings.TrimSpace(a.Expected), 64)
	numeric := lerr == nil && rerr == nil
	switch a.Op {
	case httpfile.OpEq:
		if numeric {
			return lhs == rhs, nil
		}
		return actual == a.Expected, nil
	case httpfile.OpNe:
		if numeric {
			return lhs != rhs, nil
		}
		return actual != a.Expected, nil
	}
	if !numeric {
		if rerr != nil {
			return false, fmt.Errorf("%s needs a number, got %q", a.Op, a.Expected)
		}
		return false, fmt.Errorf("%s needs a number, the response gave %s", a.Op, quoteActual(a, actual))
	}
	return ordered(a.Op, lhs, rhs), nil
}

// compareDurations is the ordering for the time subject: the expected value
// is a Go duration (`500ms`, `1.5s`) or a bare number of milliseconds.
func compareDurations(a httpfile.Assertion, actual string) (bool, error) {
	want, err := parseAssertDuration(a.Expected)
	if err != nil {
		return false, err
	}
	got, err := time.ParseDuration(actual)
	if err != nil {
		return false, fmt.Errorf("unreadable duration %q", actual)
	}
	switch a.Op {
	case httpfile.OpEq:
		return got == want, nil
	case httpfile.OpNe:
		return got != want, nil
	}
	return ordered(a.Op, float64(got), float64(want)), nil
}

// parseAssertDuration reads `500ms`, `2s`, `1m` — or a bare number, taken as
// milliseconds since that is the unit a response time is thought in.
func parseAssertDuration(s string) (time.Duration, error) {
	if ms, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Duration(ms * float64(time.Millisecond)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("time compares with a duration such as 500ms or 2s, got %q", s)
	}
	return d, nil
}

func ordered(op string, lhs, rhs float64) bool {
	switch op {
	case httpfile.OpLt:
		return lhs < rhs
	case httpfile.OpLe:
		return lhs <= rhs
	case httpfile.OpGt:
		return lhs > rhs
	case httpfile.OpGe:
		return lhs >= rhs
	}
	return false
}

// regexErr strips Go's "error parsing regexp: " prefix, which says nothing
// the surrounding message does not.
func regexErr(err error) string {
	return strings.TrimPrefix(err.Error(), "error parsing regexp: ")
}
