package httpfile

// configskip.go parses the `# @no-curlrc` and `# @no-netrc` directives
// (#3114): per-request opt-outs of the local client configuration. Like
// `# @timeout` (#2630) they are comment lines the parser only *recognises*;
// skipping the `.curlrc` / `.netrc` load happens at dispatch time
// (internal/httpclient), for that one exchange.

import (
	"fmt"
	"regexp"
)

// Names of the two configuration opt-out directives, without the `@`.
const (
	NoCurlrcDirective = "no-curlrc"
	NoNetrcDirective  = "no-netrc"
)

// configSkipRE matches either directive: a comment line (`#`, `##` or `//`)
// whose text is `@no-curlrc` or `@no-netrc`, optionally followed by a value
// — captured only so it can be rejected on its own line. Three hashes are
// not accepted, for the reason captureRE gives: `###` opens a new request
// block.
var configSkipRE = regexp.MustCompile(`^[ \t]*(?:##?|//)[ \t]*@(no-curlrc|no-netrc)(?:[ \t]+(\S.*?))?[ \t]*$`)

// ConfigSkipDirective recognises a `# @no-curlrc` / `# @no-netrc` line and
// returns which one it is (NoCurlrcDirective or NoNetrcDirective). ok is false
// when the line is not such a directive; a directive followed by a value
// answers ok with err set — neither takes one, and silently ignoring
// `# @no-netrc yes` would hide a misunderstanding. Exposed like
// TimeoutDirective: the highlighter reads exactly what the parser reads.
func ConfigSkipDirective(line string) (name string, err error, ok bool) {
	m := configSkipRE.FindStringSubmatch(line)
	if m == nil {
		return "", nil, false
	}
	if m[2] != "" {
		return m[1], fmt.Errorf("@%s takes no value, got %q", m[1], m[2]), true
	}
	return m[1], nil, true
}
