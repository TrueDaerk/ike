// Package jqplay is the evaluation core of the jq and yq playgrounds (#1936,
// #2039): parse a JSON or YAML buffer once, then run jq programs against it
// as the user types and report what came out — the values, the compile or
// runtime error, and whether the output had to be capped.
//
// It holds no UI state on purpose: the floating playground in internal/app
// owns the query line, the result window and the rendering, and calls Run on
// every (debounced) keystroke. Everything here is pure, so the interesting
// behavior — programs, errors, caps, cancellation — is testable without a
// terminal.
//
// The engine is gojq (github.com/itchyny/gojq, MIT), the pure-Go jq
// reimplementation: no cgo, no `jq` binary on PATH, and the same language a
// user already knows. Its two dangerous shapes — an iterator that emits
// infinitely (`repeat(0)`) and one that never emits at all (`def f: f; f`) —
// are bounded by MaxOutputs/MaxResultBytes and by the context the host passes
// in, which carries EvalTimeout and is cancelled when a newer keystroke
// supersedes the run.
//
// The YAML half (#2039) is a second *input and output* path, not a second
// engine: a YAML stream decodes into the same value shapes gojq already runs
// over, and the outputs are written back as YAML. See dialect.go for the seam
// and yaml.go for the decoding rules.
package jqplay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/itchyny/gojq"
)

// MaxOutputs caps how many values one evaluation collects. A program like
// `range(infinite)` or `.[]` over a large array would otherwise produce more
// rows than the window could ever show; Result.Truncated marks a capped run.
const MaxOutputs = 500

// MaxResultBytes caps the total size of the collected output, so 500 values
// that each happen to be a megabyte cannot blow up the model either.
const MaxResultBytes = 256 << 10

// MaxInputValues caps how many top-level values Parse reads out of an input
// stream (a `.jsonl` export is one value per line, a YAML file one per `---`).
// Input.Truncated marks a capped read.
const MaxInputValues = 10000

// EvalTimeout bounds one evaluation. jq programs can loop without ever
// emitting a value, which no output cap can catch; the host builds the run
// context with this deadline so such a program ends as an error line instead
// of a leaked goroutine.
const EvalTimeout = 5 * time.Second

// AsyncThreshold is the input size (in bytes) above which the host parses the
// buffer off the event loop instead of inline in the command handler. Parsing
// a screenful of JSON is free; parsing a 10 MB API dump is not.
const AsyncThreshold = 64 << 10

// Input is a parsed input: the top-level values of a document stream, kept
// decoded so that typing in the query line re-runs the program without
// re-parsing the buffer. Numbers stay json.Number, which gojq understands
// natively, so a 64-bit id survives the round trip that float64 would round.
// dialect remembers which language the values were read from, so a run over
// them writes its outputs back in the same one (#2039).
type Input struct {
	values  []any
	size    int
	dialect Dialect
	// raw is the input text verbatim, kept only by the xmq dialect (#2414):
	// its engine is the external `xmq` binary, which reads the document
	// itself from stdin — there is nothing to decode on this side.
	raw string
	// xmqTree is the xmq input's element tree, built at parse for the
	// query line's XPath completion (#2790).
	xmqTree *xmqNode
	// origin says what the values were adapted from when the buffer was not
	// the dialect's own document language — "csv rows: 12" for the CSV
	// adapter (#2791) — and is empty otherwise.
	origin string
	// Truncated reports that the stream held more than MaxInputValues values
	// and the tail was dropped.
	Truncated bool
}

// Dialect reports which document language the input was read as.
func (in *Input) Dialect() Dialect {
	if in == nil {
		return DialectJQ
	}
	return in.dialect
}

// Parse decodes text as a JSON stream — one value for an ordinary document,
// many for a `.jsonl` export or a concatenated body. An empty or non-JSON
// text is an error the playground shows on its input line; it is never a
// crash. It is DialectJQ.Parse under the name the callers outside the
// playground (the .http client's capture directive, #1993) already use.
func Parse(text string) (*Input, error) { return parseJSON(text) }

// parseJSON is the JSON half of Dialect.Parse.
func parseJSON(text string) (*Input, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New(DialectJQ.emptyInput())
	}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	in := &Input{size: len(text)}
	for {
		var v any
		err := dec.Decode(&v)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, &InputError{Detail: decodeError(err, text)}
		}
		in.values = append(in.values, v)
		if len(in.values) >= MaxInputValues {
			in.Truncated = true
			break
		}
	}
	if len(in.values) == 0 {
		return nil, errors.New(DialectJQ.emptyInput())
	}
	return in, nil
}

// InputError reports that a text could not be read as the dialect's document
// stream. Detail is the decoder's own complaint (with the line it happened
// on) without any preamble, so a caller can phrase the failure in its own
// terms — the .http client's capture directive (#1993) says "the response
// body is not JSON" where the playground says "input".
type InputError struct {
	Detail string
	// Dialect names the language that failed to parse. The zero value is jq,
	// so every caller outside the yq path (#2039) keeps its JSON wording.
	Dialect Dialect
	// Format overrides the dialect's format name for an input read through an
	// adapter (#2791): a malformed CSV row is not "invalid JSON".
	Format string
}

func (e *InputError) Error() string {
	format := e.Format
	if format == "" {
		format = e.Dialect.Format()
	}
	return "input is not valid " + format + ": " + e.Detail
}

// decodeError renders a decode failure with the line it happened on, which a
// byte offset alone does not tell the reader of a pretty-printed document.
func decodeError(err error, text string) string {
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		return fmt.Sprintf("%s (line %d)", syn.Error(), lineOf(text, int(syn.Offset)))
	}
	return err.Error()
}

// lineOf returns the 1-based line holding byte offset off.
func lineOf(text string, off int) int {
	if off > len(text) {
		off = len(text)
	}
	if off < 0 {
		off = 0
	}
	return 1 + strings.Count(text[:off], "\n")
}

// Origin names the adapter the input was read through ("csv rows: 12"), or
// "" for a document read in the dialect's own language.
func (in *Input) Origin() string {
	if in == nil {
		return ""
	}
	return in.origin
}

// Len reports how many top-level values the input holds.
func (in *Input) Len() int {
	if in == nil {
		return 0
	}
	return len(in.values)
}

// Size reports the input's size in bytes, for the playground's header line.
func (in *Input) Size() int {
	if in == nil {
		return 0
	}
	return in.size
}

// Result is one evaluation of a program against an Input.
type Result struct {
	// Err is the compile error, or the runtime error that stopped the run. A
	// runtime error can arrive *after* some values were produced — jq's
	// `.[] | .x` over `[{"x":1},3]` prints one value and then fails — so Err
	// and Outputs are not mutually exclusive.
	Err string
	// Outputs are the produced values, each pretty-printed as JSON.
	Outputs []string
	// Truncated reports that collection stopped at MaxOutputs or
	// MaxResultBytes rather than at the end of the iterator.
	Truncated bool
	// dialect is the language the outputs are written in — it decides how
	// they are joined into one document (#2039). The zero value is jq, so a
	// Result built by hand for a failure the host phrases itself stays
	// JSON-shaped.
	dialect Dialect
	// ext overrides the dialect's default result extension (#2414): an xmq
	// run names its output language per command — `to-json` writes JSON,
	// `to-html` HTML — where the gojq dialects always write their own format.
	// Empty means the dialect's default.
	ext string
	// opts are the toggles the run applied (#2784): a raw result is text,
	// and neither a raw nor a compact one folds.
	opts Options
	// values are the produced values themselves (#2788), in output order —
	// what the CSV / TSV export tabulates, independent of the -r / -c form
	// Outputs are rendered in. nil for an xmq run: its outputs are text.
	values []any
}

// Dialect reports which document language the outputs are written in.
func (r Result) Dialect() Dialect { return r.dialect }

// Ext is the file extension this result's text is written under — the
// dialect's default unless the run named its own output language (#2414).
func (r Result) Ext() string {
	if r.opts.Raw {
		return "txt"
	}
	if r.ext != "" {
		return r.ext
	}
	return r.dialect.Ext()
}

// ResultPath is Dialect.ResultPath with this result's own extension: the
// display path the substitute result editor shows the text under, which is
// what resolves its highlighting.
func (r Result) ResultPath() string { return r.dialect.Name() + " result." + r.Ext() }

// Folds returns the result's foldable nodes. The gojq dialects scan the text
// their own encoder wrote; an xmq result folds only when its command produced
// a language the structural scans read (`to-json`, #2414).
func (r Result) Folds() []Fold {
	if r.opts.Raw || r.opts.Compact {
		return nil
	}
	if r.dialect == DialectXMQ {
		if r.ext == "json" {
			return jsonFolds(r.Text())
		}
		return nil
	}
	return r.dialect.Folds(r.Text())
}

// Text joins the outputs into the document the result buffer shows, which is
// also what the copy and open-as-scratch actions write: jq's stdout puts one
// value per line, a YAML stream separates its documents with `---`.
func (r Result) Text() string { return strings.Join(r.Outputs, r.separator()) }

// separator joins two outputs: the dialect's, except that raw output (#2784)
// is plain text, one value per line the way `jq -r` prints it.
func (r Result) separator() string {
	if r.opts.Raw {
		return "\n"
	}
	return r.dialect.separator()
}

// Lines splits the result into display rows.
func (r Result) Lines() []string {
	if len(r.Outputs) == 0 {
		return nil
	}
	return strings.Split(r.Text(), "\n")
}

// Empty is a result with no outputs in one dialect (#2700) — what the
// playground's "clear the output" installs. It is not Result{}: the zero
// value's dialect is jq, and the result buffer's display path (and with it
// its highlighting) is named after the dialect, so a cleared yq playground
// would re-open as a JSON buffer on the next run.
func Empty(d Dialect) Result { return Result{dialect: d} }

// Evaluate is Parse + Run against a fresh EvalTimeout context — the
// convenient form for tests and for callers that hold text rather than a
// parsed Input. A parse failure comes back as the Result's error, so one call
// covers both failure modes the playground renders identically.
func Evaluate(program, text string) Result { return EvaluateWith(DialectJQ, program, text) }

// EvaluateWith is Evaluate in one dialect (#2039): the yq path reads and
// renders YAML, the jq path is Evaluate itself.
func EvaluateWith(d Dialect, program, text string) Result {
	in, err := d.Parse(text)
	if err != nil {
		return Result{Err: err.Error(), dialect: d}
	}
	ctx, cancel := context.WithTimeout(context.Background(), EvalTimeout)
	defer cancel()
	return Run(ctx, program, in)
}

// Run compiles program and runs it over every value of in, collecting the
// output until the iterators are exhausted, a runtime error stops the run, a
// cap is hit, or ctx ends.
//
// A blank (empty or whitespace-only) program is the dialect's identity
// (#2807): clearing the query line to start a new program must not blank the
// result the user was looking at, so it is run as if `.` had been typed (jq,
// yq) — xmq already treats its own empty command line this way.
func Run(ctx context.Context, program string, in *Input) Result {
	return RunWith(ctx, program, in, Options{})
}

// run is Run under the toggles; RunWith has already slurped the input.
func run(ctx context.Context, program string, in *Input, opts Options) Result {
	program = strings.TrimSpace(program)
	vars, err := ParseVars(opts.Vars)
	if err != nil {
		return Result{Err: err.Error(), dialect: in.Dialect()}
	}
	if in.Dialect() == DialectXMQ {
		// The xmq dialect (#2414) runs the external binary instead of gojq —
		// including on an empty program, which is `xmq` with no command:
		// the input pretty-printed in xmq's own notation.
		res := runXMQ(ctx, program, in, vars)
		res.opts = Options{Vars: opts.Vars}
		return res
	}
	if program == "" {
		program = in.Dialect().identity()
	}
	if in == nil || len(in.values) == 0 {
		return Result{Err: in.Dialect().emptyInput(), dialect: in.Dialect()}
	}
	query, err := gojq.Parse(program)
	if err != nil {
		return Result{Err: err.Error(), dialect: in.dialect}
	}
	code, err := compileWith(query, vars)
	if err != nil {
		return Result{Err: err.Error(), dialect: in.dialect}
	}
	res := Result{dialect: in.dialect, opts: opts}
	size := 0
	values := vars.Values()
	for _, v := range in.values {
		iter := code.RunWithContext(ctx, v, values...)
		for {
			out, ok := iter.Next()
			if !ok {
				break
			}
			if err, ok := out.(error); ok {
				var halt *gojq.HaltError
				if errors.As(err, &halt) && halt.Value() == nil {
					return res // `halt`: a clean stop, not a diagnostic
				}
				res.Err = runtimeError(ctx, err)
				return res
			}
			if len(res.Outputs) >= MaxOutputs || size >= MaxResultBytes {
				res.Truncated = true
				return res
			}
			text := in.dialect.encodeWith(out, opts)
			size += len(text)
			res.Outputs = append(res.Outputs, text)
			res.values = append(res.values, out)
		}
		if ctx.Err() != nil {
			res.Err = contextError(ctx)
			return res
		}
	}
	return res
}

// Compile checks program without running it (#2780) and returns the error Run
// would report for it, "" when it compiles. It is what the playground calls on
// every query-line keystroke, ahead of the debounced run: a syntax error shows
// at once and a program that cannot compile never starts a run.
//
// The check has no input dependency, so its cost is bounded by the program's
// length alone — a pathological input cannot put it on the hot path. For jq and
// yq it is gojq's parse + compile, the same two steps Run starts with; for xmq
// it is the shell-word split runXMQ starts with, the only part of an xmq
// command line the playground can judge without running the binary. An empty
// program compiles in every dialect: Run treats it as idle (jq, yq) or as the
// bare pretty-print (xmq). Check is the same check with the error's position
// in the program (#2781).
func Compile(d Dialect, program string) string { return Check(d, program).Msg }

// runtimeError renders the error a jq program raised. A cancelled context
// surfaces as its own message: gojq reports the abort as an ordinary error
// value, and "context canceled" would read as a jq diagnostic.
func runtimeError(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return contextError(ctx)
	}
	return err.Error()
}

// contextError names why the run was cut short.
func contextError(ctx context.Context) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Sprintf("the program did not finish within %s — it may loop forever", EvalTimeout)
	}
	return "evaluation cancelled"
}

// encodeJSON pretty-prints one output value in jq's JSON flavour. gojq.Marshal
// emits jq's escaping (which differs from encoding/json: no `&` for
// `&`); json.Indent then lays the value out over lines, so a nested object
// reads as a document instead of as one very long row.
func encodeJSON(v any) string {
	compact, err := gojq.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, compact, "", "  "); err != nil {
		return string(compact)
	}
	return pretty.String()
}

// Size is the byte length of the result document (#2776), the joined outputs
// the result buffer shows — what the info row reports next to the count.
func (r Result) Size() int { return len(r.Text()) }
