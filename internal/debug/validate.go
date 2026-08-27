package debug

import (
	"errors"
	"strings"
	"unicode"
)

// validate.go holds the classification and the input rules of the breakpoint
// refinements (#2245). The store keeps whatever it is handed — the rules live
// here so every surface that edits a refinement (the Breakpoints window's
// inline editors and the breakpoint-properties form) rejects the same input
// with the same wording, before it reaches an adapter that would answer with
// an opaque error at the next `setBreakpoints`.

// Kind classifies a breakpoint for rendering: a plain stop, a conditional
// stop (condition and/or hit count) or a logpoint. It is what the gutter and
// the Breakpoints window pick their glyph from, so both always agree.
type Kind int

const (
	// KindPlain is a breakpoint without refinements — it always stops.
	KindPlain Kind = iota
	// KindConditional carries a condition and/or a hit count.
	KindConditional
	// KindLogpoint carries a log message: it logs instead of stopping. A log
	// message wins over a condition, which merely gates the logging.
	KindLogpoint
)

// IsLogpoint reports whether m logs instead of stopping.
func (m Meta) IsLogpoint() bool { return strings.TrimSpace(m.LogMessage) != "" }

// IsConditional reports whether m carries a condition or a hit count.
func (m Meta) IsConditional() bool {
	return strings.TrimSpace(m.Condition) != "" || strings.TrimSpace(m.HitCondition) != ""
}

// Kind classifies m for the gutter and the list glyphs.
func (m Meta) Kind() Kind {
	switch {
	case m.IsLogpoint():
		return KindLogpoint
	case m.IsConditional():
		return KindConditional
	default:
		return KindPlain
	}
}

// LogpointLines returns path's enabled logpoint lines, sorted ascending —
// the gutter draws them with their own glyph (#2245).
func (b *Breakpoints) LogpointLines(path string) []int {
	return b.kindLines(path, KindLogpoint)
}

// ConditionalLines returns path's enabled conditional lines (condition and/or
// hit count), sorted ascending (#2245).
func (b *Breakpoints) ConditionalLines(path string) []int {
	return b.kindLines(path, KindConditional)
}

// kindLines collects path's breakpoints of one kind. Disabled breakpoints are
// included: a muted logpoint keeps its shape in the gutter and only loses its
// fill, exactly like a muted plain breakpoint (#1377).
func (b *Breakpoints) kindLines(path string, want Kind) []int {
	var out []int
	for _, l := range b.files[path] {
		if b.meta[path][l].Kind() == want {
			out = append(out, l)
		}
	}
	return out
}

// A field holding only whitespace is "empty but enabled": non-empty, so it
// would be sent, yet empty in substance. Clearing the field is the way to
// drop a refinement — a blank condition makes adapters reject the whole
// breakpoint — so each rule below rejects that case with its own wording.

// ValidateCondition checks a condition expression. Only blankness is checked:
// the expression language belongs to the debuggee, and the adapter is the
// only thing that can judge it.
func ValidateCondition(s string) error {
	if s != "" && strings.TrimSpace(s) == "" {
		return errors.New("condition is blank — clear it to drop the condition")
	}
	return nil
}

// ValidateHitCondition checks a hit-count condition. DAP leaves the syntax to
// the adapter, but every adapter IKE speaks to (delve, debugpy) reads the
// VS Code form: a count, optionally prefixed by a comparison or `%` for
// "every n-th hit". Anything else is a typo far more often than an exotic
// adapter dialect, so it is rejected here with the accepted forms spelled out.
func ValidateHitCondition(s string) error {
	if s == "" {
		return nil
	}
	if strings.TrimSpace(s) == "" {
		return errors.New("hit count is blank — clear it to drop the hit count")
	}
	rest := strings.TrimSpace(s)
	for _, op := range []string{">=", "<=", "==", "=", ">", "<", "%"} {
		if strings.HasPrefix(rest, op) {
			rest = strings.TrimSpace(rest[len(op):])
			break
		}
	}
	if rest == "" {
		return errors.New(`hit count needs a number, e.g. "5", ">3" or "%2"`)
	}
	for _, r := range rest {
		if !unicode.IsDigit(r) {
			return errors.New(`hit count must be a number, optionally prefixed by >, >=, <, <=, == or %`)
		}
	}
	return nil
}

// ValidateLogMessage checks a logpoint message. `{expr}` placeholders are
// interpolated by the adapter; an unbalanced or empty brace pair is a typo
// that would either print literally or fail the whole breakpoint, depending
// on the adapter, so it is caught here.
func ValidateLogMessage(s string) error {
	if s == "" {
		return nil
	}
	if strings.TrimSpace(s) == "" {
		return errors.New("log message is blank — clear it to drop the logpoint")
	}
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '{':
			if depth > 0 {
				return errors.New("nested { in log message")
			}
			depth, start = 1, i
		case '}':
			if depth == 0 {
				return errors.New("unmatched } in log message")
			}
			if strings.TrimSpace(s[start+1:i]) == "" {
				return errors.New("empty {} in log message")
			}
			depth = 0
		}
	}
	if depth != 0 {
		return errors.New("unclosed { in log message")
	}
	return nil
}

// Validate checks all three fields of m, returning the first complaint.
func (m Meta) Validate() error {
	if err := ValidateCondition(m.Condition); err != nil {
		return err
	}
	if err := ValidateHitCondition(m.HitCondition); err != nil {
		return err
	}
	return ValidateLogMessage(m.LogMessage)
}
