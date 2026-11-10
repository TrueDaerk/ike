package httpfile

// retry.go parses the `# @retry 3 2s on 5xx,timeout` directive (#3116): how
// often a request is tried again when an attempt fails, how long to wait in
// between and which outcomes count as a failure worth another try. Like
// `@timeout` (#2630) it is a comment line, so the parser only *recognises* it
// and attaches the spec to its request — the attempts themselves happen at
// dispatch time (internal/httpclient), because that is where the exchange
// exists.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RetryKeyword is the directive's marker.
const RetryKeyword = "@retry"

// retryRE matches a retry directive: a comment line (`#`, `##` or `//`) whose
// text is `@retry` plus whatever follows. Three hashes are deliberately not
// accepted, for the reason captureRE gives: `###` opens a new request block.
// The spec is captured whole; parseRetrySpec reads its grammar.
var retryRE = regexp.MustCompile(`^[ \t]*(?:##?|//)[ \t]*@retry(?:[ \t]+(.*?))?[ \t]*$`)

// RetryCondition names the outcomes of an attempt worth another try.
const (
	// RetryOnTimeout is the attempt hitting its deadline (`@timeout` or the
	// configured default), which applies per attempt.
	RetryOnTimeout = "timeout"
	// RetryOnError is a failure without any response at all: a refused
	// connection, a DNS or TLS failure, a connection reset.
	RetryOnError = "error"
)

// DefaultRetryConditions is what a directive without an `on` clause retries
// on: the failures a flapping service or a cold start produce, never a 4xx —
// a request the server rejected is answered the same way the next time.
var DefaultRetryConditions = []string{RetryOnError, RetryOnTimeout, "5xx"}

// statusClassRE and statusCodeRE recognise the two status condition forms.
var (
	statusClassRE = regexp.MustCompile(`^[1-5]xx$`)
	statusCodeRE  = regexp.MustCompile(`^[1-5][0-9][0-9]$`)
)

// Retry is one `# @retry` directive of a request block (#3116). Count is the
// number of *retries* — `@retry 3` allows four attempts — Delay the fixed
// pause between two attempts and Conditions the normalised (lower-case)
// condition words, DefaultRetryConditions when the directive names none.
// The Raw fields are the parts as written, in line order, for the
// highlighter — each only once it was read successfully, so a rejected part
// is never painted as what it failed to be; Err is the reason a directive
// was rejected.
type Retry struct {
	Count      int
	Delay      time.Duration
	Conditions []string

	CountRaw string
	DelayRaw string
	// OnRaw is the `on` keyword as written ("" when the clause is absent),
	// CondRaw the condition words as written.
	OnRaw   string
	CondRaw []string

	Err  error
	Line int // 1-based
}

// Attempts is how many times the request may go out in total.
func (r *Retry) Attempts() int {
	if r == nil || r.Count <= 0 {
		return 1
	}
	return r.Count + 1
}

// OnStatus reports whether a response with status code is retried.
func (r *Retry) OnStatus(code int) bool {
	if r == nil {
		return false
	}
	for _, c := range r.Conditions {
		if statusClassRE.MatchString(c) && code/100 == int(c[0]-'0') {
			return true
		}
		if statusCodeRE.MatchString(c) && strconv.Itoa(code) == c {
			return true
		}
	}
	return false
}

// OnTimeout reports whether an attempt that hit its deadline is retried.
func (r *Retry) OnTimeout() bool { return r.has(RetryOnTimeout) }

// OnError reports whether an attempt without any response is retried.
func (r *Retry) OnError() bool { return r.has(RetryOnError) }

func (r *Retry) has(cond string) bool {
	if r == nil {
		return false
	}
	for _, c := range r.Conditions {
		if c == cond {
			return true
		}
	}
	return false
}

// RetryDirective recognises a retry directive line and parses it (#3116). ok
// is false when the line is not a directive at all; a directive whose spec
// does not parse answers ok with Err set, so a typo is reported on its own
// line instead of silently sending the request once. Exposed like
// TimeoutDirective: the highlighter reads exactly what the parser reads.
func RetryDirective(line string) (Retry, bool) {
	m := retryRE.FindStringSubmatch(line)
	if m == nil {
		return Retry{}, false
	}
	r := parseRetrySpec(m[1])
	return r, true
}

// parseRetrySpec reads `<count> [<delay>] [on <cond>[,<cond>…]]`. The parts it
// did read stay filled in on an error, so the highlighter can still paint
// them.
func parseRetrySpec(spec string) Retry {
	var r Retry
	fields := strings.Fields(spec)
	if len(fields) == 0 {
		r.Err = fmt.Errorf("@retry needs a count, e.g. `# @retry 3` or `# @retry 3 2s on 5xx,timeout`")
		return r
	}
	n, err := strconv.Atoi(fields[0])
	if err != nil || n <= 0 {
		r.Err = fmt.Errorf("invalid @retry count %q — use a positive number of retries, e.g. `# @retry 3`", fields[0])
		return r
	}
	r.Count, r.CountRaw = n, fields[0]
	rest := fields[1:]
	if len(rest) > 0 && !strings.EqualFold(rest[0], "on") {
		d, err := parseRetryDelay(rest[0])
		if err != nil {
			r.Err = err
			return r
		}
		r.Delay, r.DelayRaw = d, rest[0]
		rest = rest[1:]
	}
	if len(rest) == 0 {
		r.Conditions = append([]string(nil), DefaultRetryConditions...)
		return r
	}
	if !strings.EqualFold(rest[0], "on") {
		r.Err = fmt.Errorf("invalid @retry: unexpected %q — conditions follow `on`, e.g. `# @retry 3 2s on 503,timeout`", rest[0])
		return r
	}
	r.OnRaw = rest[0]
	// The list may carry blanks after its commas (`on 503, timeout`); the
	// words are what matter.
	for _, w := range strings.Split(strings.Join(rest[1:], ","), ",") {
		if w == "" {
			continue
		}
		c := strings.ToLower(w)
		if c != RetryOnTimeout && c != RetryOnError && !statusClassRE.MatchString(c) && !statusCodeRE.MatchString(c) {
			r.Err = fmt.Errorf("invalid @retry condition %q — use a status (503), a class (5xx), timeout or error", w)
			return r
		}
		r.Conditions = append(r.Conditions, c)
		r.CondRaw = append(r.CondRaw, w)
	}
	if len(r.Conditions) == 0 {
		r.Err = fmt.Errorf("@retry `on` needs at least one condition, e.g. `on 5xx,timeout`")
	}
	return r
}

// parseRetryDelay reads the pause between attempts: a Go duration or bare
// seconds, the spellings `@timeout` accepts. Zero is allowed (and the same as
// leaving it out); a negative pause is not.
func parseRetryDelay(raw string) (time.Duration, error) {
	d, err := time.ParseDuration(raw)
	if err != nil {
		f, ferr := strconv.ParseFloat(raw, 64)
		if ferr != nil {
			return 0, fmt.Errorf("invalid @retry delay %q — use a duration like 2s or 500ms", raw)
		}
		d = time.Duration(f * float64(time.Second))
	}
	if d < 0 {
		return 0, fmt.Errorf("invalid @retry delay %q — the delay must not be negative", raw)
	}
	return d, nil
}

// retryAt builds the Retry for a directive line, given its 0-based index.
func retryAt(line string, idx int) (Retry, bool) {
	r, ok := RetryDirective(line)
	if !ok {
		return Retry{}, false
	}
	r.Line = idx + 1
	return r, true
}
