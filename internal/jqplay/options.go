package jqplay

// options.go is the playground's answer to jq's three most used command-line
// flags (#2784): `-r` (raw strings), `-c` (one line per output) and `-s`
// (slurp the input stream into one array). They are run-time options rather
// than part of the program, so the host carries them next to the query line
// and hands them to RunWith; the xmq dialect has no such flags and ignores
// them.

import (
	"context"
	"fmt"
	"strings"

	"github.com/itchyny/gojq"
)

// Options are the output/input toggles of one run. The zero value is the
// playground's default: every output pretty-printed in the dialect's own
// spelling, the program run once per input value.
type Options struct {
	// Raw prints a string output as its own text, without quotes or escapes
	// (`jq -r`); every other value keeps its encoding. A raw result is plain
	// text, so it is named `.txt` and does not fold.
	Raw bool
	// Compact prints each output on a single line (`jq -c`): compact JSON,
	// which a YAML reader also accepts as a flow value. A compact result has
	// nothing to fold.
	Compact bool
	// Slurp runs the program once over one array holding every input value
	// (`jq -s`) instead of once per value — the way to count, sort or group
	// the lines of a JSONL stream.
	Slurp bool
	// Vars is the variables line (#2786), `name=value` entries bound as
	// `$name` (see ParseVars) — jq's `--arg` / `--argjson`. It is kept as
	// written, so Options stays comparable and persists as one string; a
	// line that does not parse fails the run with its message. Flags does
	// not spell it.
	Vars string
	// RoundTrip renders a yq output by patching the input document's own
	// node tree (#2798, roundtrip.go), so comments, anchors, quoting and key
	// order survive an edit-style program; a reshaping program falls back to
	// the plain form with a note. Ignored by the other dialects and not
	// spelled by Flags: it is remembered for the session, not persisted.
	RoundTrip bool
}

// Flags spells the active toggles the way the command line does, in `-r -c
// -s` order and space-separated; "" when none is on.
func (o Options) Flags() string {
	var f []string
	if o.Raw {
		f = append(f, "-r")
	}
	if o.Compact {
		f = append(f, "-c")
	}
	if o.Slurp {
		f = append(f, "-s")
	}
	return strings.Join(f, " ")
}

// ParseFlags is Flags read back. Unknown words are ignored, so a flag string
// persisted by a later version never fails to load.
func ParseFlags(s string) Options {
	var o Options
	for _, f := range strings.Fields(s) {
		switch f {
		case "-r":
			o.Raw = true
		case "-c":
			o.Compact = true
		case "-s":
			o.Slurp = true
		}
	}
	return o
}

// RunWith is Run with the toggles applied. The xmq dialect ignores them: its
// engine is the external binary, whose command line has output options of
// its own. The variables line applies to every dialect (#2786).
func RunWith(ctx context.Context, program string, in *Input, opts Options) Result {
	if in.Dialect() == DialectXMQ {
		opts = Options{Vars: opts.Vars}
	}
	if opts.Slurp && in != nil && len(in.values) > 0 {
		in = in.slurped()
	}
	return run(ctx, program, in, opts)
}

// slurped is the input as `-s` sees it: one array holding every value. The
// original document trees do not come along — the array is no document the
// round-trip (#2798) could patch.
func (in *Input) slurped() *Input {
	slurped := *in
	slurped.values = []any{append([]any(nil), in.values...)}
	slurped.docs = nil
	return &slurped
}

// Options reports the toggles the result was produced with.
func (r Result) Options() Options { return r.opts }

// encodeWith renders one output value in the dialect's spelling under the
// output toggles: a raw string as itself, a compact value as one line of
// JSON, anything else as the dialect's pretty form.
func (d Dialect) encodeWith(v any, opts Options) string {
	if s, ok := v.(string); ok && opts.Raw {
		return s
	}
	if opts.Compact {
		out, err := gojq.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(out)
	}
	return d.encode(v)
}
