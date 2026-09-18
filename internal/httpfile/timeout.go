package httpfile

// timeout.go parses the `# @timeout 5s` directive (#2630): the per-request
// deadline that overrides the `http.timeout_ms` setting and a `.curlrc`
// `max-time` for one request only. Like the capture (#1993) and assertion
// (#2546) directives it is a comment line, so the parser only *recognises* it
// and attaches the duration to its request — applying it to the client
// happens at dispatch time (internal/httpclient), because that is where the
// exchange exists.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// timeoutRE matches a timeout directive: a comment line (`#`, `##` or `//`)
// whose text is `@timeout <value>`. Three hashes are deliberately not
// accepted, for the reason captureRE gives: `###` opens a new request block.
// The value is captured as one word; parseTimeoutValue decides whether it is
// a duration ike understands.
var timeoutRE = regexp.MustCompile(`^[ \t]*(?:##?|//)[ \t]*@timeout(?:[ \t]+(\S+))?[ \t]*$`)

// TimeoutDirective recognises a timeout directive line and returns its
// duration (#2630). ok is false when the line is not a directive at all; a
// directive whose value does not parse answers ok with err set, so a typo is
// reported on its own line instead of silently leaving the default deadline
// in place. raw is the value as written, for the message. Exposed like
// CaptureDirective: the highlighter reads exactly what the parser reads.
func TimeoutDirective(line string) (d time.Duration, raw string, err error, ok bool) {
	m := timeoutRE.FindStringSubmatch(line)
	if m == nil {
		return 0, "", nil, false
	}
	raw = m[1]
	if raw == "" {
		return 0, "", fmt.Errorf("@timeout needs a duration, e.g. `# @timeout 5s`"), true
	}
	d, err = parseTimeoutValue(raw)
	return d, raw, err, true
}

// parseTimeoutValue reads the directive's value: a Go duration ("5s",
// "1500ms", "2m") or — curl's `max-time` spelling, which a .http file author
// coming from `.curlrc` will try — a bare, possibly fractional number of
// seconds. Zero and negative values are rejected rather than read as "no
// deadline": a request that may hang forever is what this setting exists to
// prevent, and `# @timeout 0` would be an invisible way to ask for it.
func parseTimeoutValue(raw string) (time.Duration, error) {
	d, err := time.ParseDuration(raw)
	if err != nil {
		f, ferr := strconv.ParseFloat(raw, 64)
		if ferr != nil {
			return 0, fmt.Errorf("invalid @timeout %q — use a duration like 5s, 1500ms or 2m", raw)
		}
		d = time.Duration(f * float64(time.Second))
	}
	if d <= 0 {
		return 0, fmt.Errorf("invalid @timeout %q — the deadline must be positive", raw)
	}
	return d, nil
}

// timeoutAt builds the Timeout for a directive line, given its 0-based index.
func timeoutAt(line string, idx int) (Timeout, bool) {
	d, raw, err, ok := TimeoutDirective(line)
	if !ok {
		return Timeout{}, false
	}
	return Timeout{
		Duration: d,
		Raw:      raw,
		Err:      err,
		Line:     idx + 1,
		EndCol:   utf8.RuneCountInString(strings.TrimRight(line, " \t")),
	}, true
}

// Timeout is one `# @timeout 5s` directive of a request block (#2630).
// Duration is the parsed deadline (0 when the value did not parse), Raw the
// value as written, Err the reason it was rejected, and Line/EndCol locate
// the directive in the file — the anchor a rejected value is marked at.
type Timeout struct {
	Duration time.Duration
	Raw      string
	Err      error
	Line     int // 1-based
	EndCol   int // 0-based rune column past the last non-space character
}
