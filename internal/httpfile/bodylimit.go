package httpfile

// bodylimit.go parses the two response-body directives of #3117:
//
//   - `# @stream` (no value): the body is spooled from the first byte and the
//     response pane shows the head while the rest is still arriving.
//   - `# @max-body 5mb`: reading stops after that many bytes; what was read is
//     kept, the response is marked truncated and the connection is closed.
//
// Like `@timeout` (#2630) both are comment lines the parser only
// *recognises*; spooling and capping happen at dispatch time
// (internal/httpclient), where the body is read.

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// The directives' markers.
const (
	StreamKeyword  = "@stream"
	MaxBodyKeyword = "@max-body"
)

// streamRE matches `@stream`, optionally followed by a value — captured only
// so it can be rejected on its own line. maxBodyRE matches `@max-body` with
// its value as one word. Three hashes are not accepted, for the reason
// captureRE gives: `###` opens a new request block.
var (
	streamRE  = regexp.MustCompile(`^[ \t]*(?:##?|//)[ \t]*@stream(?:[ \t]+(\S.*?))?[ \t]*$`)
	maxBodyRE = regexp.MustCompile(`^[ \t]*(?:##?|//)[ \t]*@max-body(?:[ \t]+(\S+))?[ \t]*$`)
)

// StreamDirective recognises a `# @stream` line. ok is false when the line is
// not the directive; a directive followed by a value answers ok with err set —
// it takes none, and silently ignoring `# @stream on` would hide a
// misunderstanding. Exposed like NoLogDirectiveAt: the highlighter reads
// exactly what the parser reads.
func StreamDirective(line string) (err error, ok bool) {
	m := streamRE.FindStringSubmatch(line)
	if m == nil {
		return nil, false
	}
	if m[1] != "" {
		return fmt.Errorf("%s takes no value, got %q", StreamKeyword, m[1]), true
	}
	return nil, true
}

// MaxBodyDirective recognises a `# @max-body <size>` line and returns the
// limit in bytes. ok is false when the line is not a directive at all; a
// directive whose value does not parse answers ok with err set, so a typo is
// reported on its own line instead of silently leaving the body uncapped.
// raw is the value as written, for the message and the highlighter.
func MaxBodyDirective(line string) (n int64, raw string, err error, ok bool) {
	m := maxBodyRE.FindStringSubmatch(line)
	if m == nil {
		return 0, "", nil, false
	}
	raw = m[1]
	if raw == "" {
		return 0, "", fmt.Errorf("%s needs a size, e.g. `# @max-body 5mb`", MaxBodyKeyword), true
	}
	n, err = ParseByteSize(raw)
	if err != nil {
		return 0, raw, fmt.Errorf("invalid %s %q — %v", MaxBodyKeyword, raw, err), true
	}
	return n, raw, nil, true
}

// sizeRE splits a size into its number and its (optional) unit.
var sizeRE = regexp.MustCompile(`^([+-]?[0-9]+(?:\.[0-9]+)?)([A-Za-z]*)$`)

// sizeUnits are the accepted units, 1024-based like the rest of ike's byte
// figures (and like curl's `--max-filesize 5M`).
var sizeUnits = map[string]float64{
	"":   1,
	"b":  1,
	"kb": 1 << 10,
	"mb": 1 << 20,
	"gb": 1 << 30,
}

// ParseByteSize reads a size: a number with a unit `b`, `kb`, `mb` or `gb`
// (case-insensitive, 1024-based) or a bare byte count. Fractions are allowed
// with a unit (`1.5mb`) and rounded down to whole bytes. Zero and negative
// sizes are rejected rather than read as "no limit": `# @max-body 0` would be
// an invisible way to drop every body.
func ParseByteSize(raw string) (int64, error) {
	m := sizeRE.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return 0, fmt.Errorf("use a size like 512kb, 5mb or a byte count")
	}
	mult, known := sizeUnits[strings.ToLower(m[2])]
	if !known {
		return 0, fmt.Errorf("unknown unit %q — use b, kb, mb or gb", m[2])
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, fmt.Errorf("use a size like 512kb, 5mb or a byte count")
	}
	v := math.Floor(f * mult)
	if v <= 0 {
		return 0, fmt.Errorf("the size must be positive")
	}
	if v >= float64(int64(1)<<62) {
		return 0, fmt.Errorf("the size is too large")
	}
	return int64(v), nil
}

// FormatByteSize renders n the way a directive would spell it — the largest
// unit that divides it evenly, so `@max-body 5mb` reads back as "5mb" and an
// odd byte count stays exact.
func FormatByteSize(n int64) string {
	for _, u := range []struct {
		name string
		mult int64
	}{{"gb", 1 << 30}, {"mb", 1 << 20}, {"kb", 1 << 10}} {
		if n >= u.mult && n%u.mult == 0 {
			return strconv.FormatInt(n/u.mult, 10) + u.name
		}
	}
	return strconv.FormatInt(n, 10) + "b"
}
