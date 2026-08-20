package jqplay

// raw.go is the single-value form of an evaluation: run a jq program over a
// JSON document and hand back its first output the way `jq -r` would print
// it — a string as itself, everything else in its JSON spelling. The
// playground shows *all* outputs pretty-printed; a caller that wants one
// value to put somewhere (the .http client's `# @capture name = <jq>`
// directive, #1993) wants exactly the opposite, and sharing the engine here
// keeps gojq behind one package.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/itchyny/gojq"
)

// ErrNoValue reports that the program ran but produced nothing usable — an
// empty iterator (`.items[] | select(false)`) or only nulls (`.missing`).
// It is the "the path does not match this body" case, and it is a failure
// rather than an empty string: silently capturing "" would send the next
// request with a hole in it.
var ErrNoValue = errors.New("the expression matched no value")

// EvaluateRaw runs program over the JSON in text and returns its first
// non-null output in `jq -r` spelling: a string unquoted, a number, boolean
// or null in its JSON form, an object or array as compact JSON. Nulls are
// skipped rather than returned, so a program run over a JSON stream (an
// NDJSON body is several top-level values) yields the first line that
// actually has the value.
//
// Every failure mode is an error with a sentence a reader can act on: an
// *InputError for text that is not JSON (the caller renames "input" to
// whatever it handed in), a compile error, a runtime error, a run that
// exceeds EvalTimeout, and ErrNoValue for an expression that matched nothing.
func EvaluateRaw(program, text string) (string, error) {
	code, err := compileRaw(program)
	if err != nil {
		return "", err
	}
	in, err := Parse(text)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), EvalTimeout)
	defer cancel()
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
					break // `halt`: a clean stop, not a failure
				}
				return "", errors.New(runtimeError(ctx, err))
			}
			if out == nil {
				continue
			}
			return rawString(out), nil
		}
		if ctx.Err() != nil {
			return "", errors.New(contextError(ctx))
		}
	}
	return "", ErrNoValue
}

// compileRaw parses and compiles the program, naming both failures the same
// way — to the reader of a capture directive they are one thing: the
// expression is not a jq program.
func compileRaw(program string) (*gojq.Code, error) {
	query, err := gojq.Parse(program)
	if err != nil {
		return nil, fmt.Errorf("invalid jq expression: %v", err)
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return nil, fmt.Errorf("invalid jq expression: %v", err)
	}
	return code, nil
}

// rawString renders one output value the way `jq -r` prints it: a string is
// its own text (no quotes, no escaping — it is going into a request, not into
// a JSON document), anything else keeps its JSON spelling on a single line.
func rawString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	}
	out, err := gojq.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(out)
}
