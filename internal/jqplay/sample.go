package jqplay

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// sample.go is a saved filter's self-test (#2792). A filter in the library is
// written against a shape of document, and nothing notices when that shape
// moves on: the program still compiles, it just answers `null` now. A sample is
// the smallest proof that it still works — the input it was written against
// and the output it produced there, captured by the save prompt from the
// playground's own snapshot and result. Running the program over the sample
// again and comparing is the check the picker shows as ✓ / ✗ / –.

// MaxSampleBytes is the capture budget, applied to the input and the expected
// output alike. A sample lives in the filter store, which is read on every
// picker open, so it has to stay small; and it is *refused* rather than cut
// when over budget, because half a JSON document does not parse and an
// expectation cut short never matches — a truncated sample would only ever
// check ✗. A larger document is sampled by selecting the part that matters
// before opening the playground: the selection is the input then.
const MaxSampleBytes = 8 << 10

// ErrSampleTooLarge is returned by NewSample for an input or expectation over
// MaxSampleBytes.
var ErrSampleTooLarge = errors.New("sample over budget")

// SelfTest is a filter's captured self-test. Input empty means none — every
// field is omitempty, so a filter without one stores exactly the pre-#2792
// shape. Expect may legitimately be empty (a `select` that matched nothing).
type SelfTest struct {
	// Input is the document text the program is run over.
	Input string `json:"sample,omitempty"`
	// Expect is the result text the program produced over Input.
	Expect string `json:"expect,omitempty"`
	// Flags are the -r / -c / -s toggles the expectation was produced under
	// (Options.Flags): the same program prints a different document with -r.
	Flags string `json:"flags,omitempty"`
	// CSV reads Input through the CSV adapter (#2791), Sep its separator (""
	// sniffs it, as the adapter does for an unknown dialect).
	CSV bool   `json:"csv,omitempty"`
	Sep string `json:"sep,omitempty"`
}

// NewSample builds the self-test from an input snapshot and the result a run
// produced over it, enforcing MaxSampleBytes on both.
func NewSample(input string, res Result, csv bool, sep rune) (SelfTest, error) {
	if err := SampleBudget("input", len(input)); err != nil {
		return SelfTest{}, err
	}
	expect := res.Text()
	if res.Truncated {
		return SelfTest{}, fmt.Errorf("%w: the result was cut short", ErrSampleTooLarge)
	}
	if err := SampleBudget("result", len(expect)); err != nil {
		return SelfTest{}, err
	}
	s := SelfTest{Input: input, Expect: expect, Flags: res.Options().Flags(), CSV: csv}
	if csv && sep != 0 {
		s.Sep = string(sep)
	}
	return s, nil
}

// SampleBudget is the MaxSampleBytes check on one side of a sample, what
// naming it in the message: nil within budget, an ErrSampleTooLarge that
// quotes both sizes otherwise. The host asks it directly for an input it did
// not keep because it was already too large.
func SampleBudget(what string, n int) error {
	if n <= MaxSampleBytes {
		return nil
	}
	return fmt.Errorf("%w: the %s is %s, the budget %s", ErrSampleTooLarge, what, byteSize(n), byteSize(MaxSampleBytes))
}

// byteSize renders a byte count the way the capture messages quote it.
func byteSize(n int) string {
	if n < 1<<10 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
}

// Has reports whether the filter carries a self-test at all.
func (s SelfTest) Has() bool { return s.Input != "" }

// CheckState is the outcome of a filter's self-test.
type CheckState int

// The three outcomes the picker renders.
const (
	// CheckNone: the filter has no sample to check against.
	CheckNone CheckState = iota
	// CheckPass: the program over the sample printed the expectation.
	CheckPass
	// CheckFail: it printed something else, or failed to run at all.
	CheckFail
)

// Glyph is the state's picker mark.
func (c CheckState) Glyph() string {
	switch c {
	case CheckPass:
		return "✓"
	case CheckFail:
		return "✗"
	}
	return "–"
}

// CheckFilter runs f's program over its sample in dialect d and compares the
// output with the expectation. It is a full evaluation — up to EvalTimeout, or
// an xmq process — so hosts call it off the event loop. A sample that no
// longer parses, a program that no longer compiles, a runtime error and a
// different output all fail; the expectation is compared modulo trailing
// newlines, which a hand-edited store gains and loses freely.
func CheckFilter(ctx context.Context, d Dialect, f Filter) CheckState {
	if !f.SelfTest.Has() {
		return CheckNone
	}
	var in *Input
	var err error
	if f.CSV && d == DialectJQ {
		var sep rune
		if r := []rune(f.Sep); len(r) == 1 {
			sep = r[0]
		}
		in, err = ParseCSV(f.SelfTest.Input, sep)
	} else {
		in, err = d.Parse(f.SelfTest.Input)
	}
	if err != nil {
		return CheckFail
	}
	ctx, cancel := context.WithTimeout(ctx, EvalTimeout)
	defer cancel()
	opts := ParseFlags(f.Flags)
	opts.Vars = f.Vars
	res := RunWith(ctx, f.Program, in, opts)
	if res.Err != "" || res.Truncated {
		return CheckFail
	}
	if strings.TrimRight(res.Text(), "\n") != strings.TrimRight(f.Expect, "\n") {
		return CheckFail
	}
	return CheckPass
}
