// Package jqplay is the evaluation core of the jq playground (#1936): parse a
// JSON buffer once, then run jq programs against it as the user types and
// report what came out — the values, the compile or runtime error, and
// whether the output had to be capped.
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

// MaxInputValues caps how many top-level values Parse reads out of a JSON
// stream (a `.jsonl` export is one value per line). Input.Truncated marks a
// capped read.
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

// Input is a parsed JSON input: the top-level values of a JSON stream, kept
// decoded so that typing in the query line re-runs the program without
// re-parsing the buffer. Numbers stay json.Number, which gojq understands
// natively, so a 64-bit id survives the round trip that float64 would round.
type Input struct {
	values []any
	size   int
	// Truncated reports that the stream held more than MaxInputValues values
	// and the tail was dropped.
	Truncated bool
}

// Parse decodes text as a JSON stream — one value for an ordinary document,
// many for a `.jsonl` export or a concatenated body. An empty or non-JSON
// text is an error the playground shows on its input line; it is never a
// crash.
func Parse(text string) (*Input, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("no JSON input — the buffer is empty")
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
			return nil, fmt.Errorf("input is not valid JSON: %s", decodeError(err, text))
		}
		in.values = append(in.values, v)
		if len(in.values) >= MaxInputValues {
			in.Truncated = true
			break
		}
	}
	if len(in.values) == 0 {
		return nil, errors.New("no JSON input — the buffer is empty")
	}
	return in, nil
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
}

// Text joins the outputs the way jq's stdout would, which is what the copy
// and open-as-scratch actions write.
func (r Result) Text() string { return strings.Join(r.Outputs, "\n") }

// Lines splits the result into display rows.
func (r Result) Lines() []string {
	if len(r.Outputs) == 0 {
		return nil
	}
	return strings.Split(r.Text(), "\n")
}

// Evaluate is Parse + Run against a fresh EvalTimeout context — the
// convenient form for tests and for callers that hold text rather than a
// parsed Input. A parse failure comes back as the Result's error, so one call
// covers both failure modes the playground renders identically.
func Evaluate(program, text string) Result {
	in, err := Parse(text)
	if err != nil {
		return Result{Err: err.Error()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), EvalTimeout)
	defer cancel()
	return Run(ctx, program, in)
}

// Run compiles program and runs it over every value of in, collecting the
// output until the iterators are exhausted, a runtime error stops the run, a
// cap is hit, or ctx ends.
//
// An empty program is idle, not an error: the playground opens on one, and a
// half-typed program should not paint the query line red before there is
// anything to compile.
func Run(ctx context.Context, program string, in *Input) Result {
	program = strings.TrimSpace(program)
	if program == "" {
		return Result{}
	}
	if in == nil || len(in.values) == 0 {
		return Result{Err: "no JSON input — the buffer is empty"}
	}
	query, err := gojq.Parse(program)
	if err != nil {
		return Result{Err: err.Error()}
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return Result{Err: err.Error()}
	}
	var res Result
	size := 0
	for _, v := range in.values {
		iter := code.RunWithContext(ctx, v)
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
			text := encode(out)
			size += len(text)
			res.Outputs = append(res.Outputs, text)
		}
		if ctx.Err() != nil {
			res.Err = contextError(ctx)
			return res
		}
	}
	return res
}

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

// encode pretty-prints one output value in jq's JSON flavour. gojq.Marshal
// emits jq's escaping (which differs from encoding/json: no `&` for
// `&`); json.Indent then lays the value out over lines, so a nested object
// reads as a document instead of as one very long row.
func encode(v any) string {
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

// HistoryLimit caps the per-session program history.
const HistoryLimit = 50

// History is the session-scoped list of programs the playground evaluated,
// newest first. It lives in memory only: a jq program under construction is
// scratch work, not something to persist into the project.
type History struct{ items []string }

// Add records program as the newest entry, moving a repeat to the front
// instead of duplicating it. Empty programs are ignored.
func (h *History) Add(program string) {
	program = strings.TrimSpace(program)
	if program == "" {
		return
	}
	for i, p := range h.items {
		if p == program {
			h.items = append(h.items[:i], h.items[i+1:]...)
			break
		}
	}
	h.items = append([]string{program}, h.items...)
	if len(h.items) > HistoryLimit {
		h.items = h.items[:HistoryLimit]
	}
}

// Len reports how many programs are remembered.
func (h *History) Len() int { return len(h.items) }

// At returns the i-th newest program; ok is false when i is out of range.
func (h *History) At(i int) (string, bool) {
	if i < 0 || i >= len(h.items) {
		return "", false
	}
	return h.items[i], true
}
