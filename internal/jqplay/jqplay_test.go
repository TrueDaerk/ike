package jqplay

import (
	"context"
	"strings"
	"testing"
	"time"
)

// jqplay_test.go covers the evaluation core of the jq playground (#1936):
// programs, compile and runtime errors, the output cap, input parsing and
// cancellation.

// compact strips the pretty-printer's layout, so a test can assert on the
// value rather than on the indentation.
func compact(s string) string {
	return strings.Join(strings.Fields(s), "")
}

// TestEvaluateSelect is the acceptance case from the issue: a filter program
// over an open JSON buffer yields the matching elements.
func TestEvaluateSelect(t *testing.T) {
	const doc = `{"foo":[{"bar":1},{"bar":4},{"bar":9}]}`
	res := Evaluate(".foo[] | select(.bar > 3)", doc)
	if res.Err != "" {
		t.Fatalf("valid program reported %q", res.Err)
	}
	if len(res.Outputs) != 2 {
		t.Fatalf("got %d outputs, want 2: %v", len(res.Outputs), res.Outputs)
	}
	if got := compact(res.Outputs[0]); got != `{"bar":4}` {
		t.Errorf("first output = %q", got)
	}
	if got := compact(res.Outputs[1]); got != `{"bar":9}` {
		t.Errorf("second output = %q", got)
	}
}

// TestEvaluatePrettyPrints: the identity program lays the document out over
// lines — that is what the playground shows before a program is written.
func TestEvaluatePrettyPrints(t *testing.T) {
	res := Evaluate(".", `{"a":{"b":1}}`)
	if res.Err != "" {
		t.Fatalf("identity reported %q", res.Err)
	}
	if len(res.Lines()) < 4 {
		t.Fatalf("identity should pretty-print, got %q", res.Text())
	}
}

// TestEmptyProgramIsIdle: an empty query line is not an error. The playground
// opens on one whenever the seed path is blank, and painting it red would be
// noise.
func TestEmptyProgramIsIdle(t *testing.T) {
	for _, p := range []string{"", "   "} {
		res := Evaluate(p, `{"a":1}`)
		if res.Err != "" || len(res.Outputs) != 0 {
			t.Fatalf("Evaluate(%q) = %+v, want idle", p, res)
		}
	}
}

// TestCompileErrorIsReported: a program that does not parse or does not
// compile comes back as a message, not as a panic and not as output.
func TestCompileErrorIsReported(t *testing.T) {
	for _, p := range []string{".foo[", "nosuchfunc", "$undefined"} {
		res := Evaluate(p, `{"a":1}`)
		if res.Err == "" {
			t.Errorf("Evaluate(%q) reported no error", p)
		}
		if len(res.Outputs) != 0 {
			t.Errorf("Evaluate(%q) produced output despite failing: %v", p, res.Outputs)
		}
	}
}

// TestRuntimeErrorKeepsEarlierOutputs: jq prints what it produced before a
// value blew up, and the playground shows both — the error line plus the rows
// that made it out.
func TestRuntimeErrorKeepsEarlierOutputs(t *testing.T) {
	res := Evaluate(".[] | .x", `[{"x":1},3]`)
	if res.Err == "" {
		t.Fatal("indexing a number must report a runtime error")
	}
	if len(res.Outputs) != 1 || compact(res.Outputs[0]) != "1" {
		t.Fatalf("outputs before the error = %v, want [1]", res.Outputs)
	}
}

// TestOutputCap: an infinite generator stops at MaxOutputs and says so, which
// is what keeps a stray `repeat` from growing the model without bound.
func TestOutputCap(t *testing.T) {
	res := Evaluate("range(infinite)", "null")
	if !res.Truncated {
		t.Error("an infinite program must report a truncated result")
	}
	if len(res.Outputs) != MaxOutputs {
		t.Errorf("collected %d outputs, want the cap %d", len(res.Outputs), MaxOutputs)
	}
	if res.Err != "" {
		t.Errorf("a capped run is not an error, got %q", res.Err)
	}
}

// TestResultByteCap: few values, each huge — the byte budget stops the run
// even though the value count is nowhere near MaxOutputs.
func TestResultByteCap(t *testing.T) {
	res := Evaluate(`range(100) | [range(20000)]`, "null")
	if !res.Truncated {
		t.Error("a run past MaxResultBytes must report a truncated result")
	}
	if len(res.Outputs) >= MaxOutputs {
		t.Errorf("the byte cap should stop before the value cap, got %d values", len(res.Outputs))
	}
}

// TestHaltStopsCleanly: `halt` ends the run without an error line.
func TestHaltStopsCleanly(t *testing.T) {
	res := Evaluate(`1, halt, 2`, "null")
	if res.Err != "" {
		t.Errorf("halt is a clean stop, got error %q", res.Err)
	}
	if len(res.Outputs) != 1 {
		t.Errorf("outputs before halt = %v, want [1]", res.Outputs)
	}
}

// TestParseRejectsNonJSON: the input error names the line, which a byte
// offset alone does not tell the reader of a pretty-printed document.
func TestParseRejectsNonJSON(t *testing.T) {
	_, err := Parse("{\n  \"a\": 1,\n  oops\n}")
	if err == nil {
		t.Fatal("malformed JSON must fail to parse")
	}
	if !strings.Contains(err.Error(), "line 3") {
		t.Errorf("the error should name the line, got %q", err)
	}
}

// TestParseRejectsEmpty: an empty buffer is a message, not a nil input the
// caller has to guard.
func TestParseRejectsEmpty(t *testing.T) {
	for _, text := range []string{"", "   \n\t"} {
		if _, err := Parse(text); err == nil {
			t.Errorf("Parse(%q) must fail", text)
		}
	}
}

// TestParseReadsJSONStream: a `.jsonl` export is many top-level values and
// the program runs against every one, as jq's own stdin does.
func TestParseReadsJSONStream(t *testing.T) {
	in, err := Parse("{\"a\":1}\n{\"a\":2}\n{\"a\":3}\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if in.Len() != 3 {
		t.Fatalf("Len = %d, want 3", in.Len())
	}
	res := Run(context.Background(), ".a", in)
	if got := compact(res.Text()); got != "123" {
		t.Errorf("outputs = %q, want the three values", got)
	}
}

// TestParseCapsInputValues: a stream longer than MaxInputValues is read to
// the cap and marked, rather than read whole.
func TestParseCapsInputValues(t *testing.T) {
	var b strings.Builder
	for i := 0; i < MaxInputValues+5; i++ {
		b.WriteString("1\n")
	}
	in, err := Parse(b.String())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !in.Truncated {
		t.Error("an over-long stream must report a truncated input")
	}
	if in.Len() != MaxInputValues {
		t.Errorf("Len = %d, want the cap %d", in.Len(), MaxInputValues)
	}
}

// TestLargeIntegersSurvive: gojq keeps arbitrary-precision integers, so an
// id that float64 would round comes out unchanged.
func TestLargeIntegersSurvive(t *testing.T) {
	res := Evaluate(".id", `{"id":12345678901234567890}`)
	if got := compact(res.Text()); got != "12345678901234567890" {
		t.Errorf("output = %q, want the exact integer", got)
	}
}

// TestRunHonoursCancellation: a cancelled context stops the run and reports
// the abort as its own message, not as a jq diagnostic — this is the path a
// superseded keystroke takes.
func TestRunHonoursCancellation(t *testing.T) {
	in, err := Parse("null")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := Run(ctx, "range(infinite)", in)
	if res.Err != "evaluation cancelled" {
		t.Errorf("Err = %q, want the cancellation message", res.Err)
	}
}

// TestRunHonoursDeadline: a program that never terminates ends on the
// deadline with a message naming the timeout, instead of spinning forever.
func TestRunHonoursDeadline(t *testing.T) {
	in, err := Parse("null")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	res := Run(ctx, "def f: f; f", in)
	if !strings.Contains(res.Err, "may loop forever") {
		t.Errorf("Err = %q, want the timeout message", res.Err)
	}
}

// TestRunWithoutInput: Run guards a nil input rather than dereferencing it —
// the playground calls it while a large snapshot is still parsing.
func TestRunWithoutInput(t *testing.T) {
	if res := Run(context.Background(), ".", nil); res.Err == "" {
		t.Error("Run with no input must report an error")
	}
}

// TestHistoryOrdersNewestFirst covers the per-session program history: newest
// first, a repeat moved to the front, blanks ignored, capped.
func TestHistoryOrdersNewestFirst(t *testing.T) {
	var h History
	h.Add(".a")
	h.Add("")
	h.Add("   ")
	h.Add(".b")
	h.Add(".a")
	if h.Len() != 2 {
		t.Fatalf("Len = %d, want 2", h.Len())
	}
	if got, _ := h.At(0); got != ".a" {
		t.Errorf("newest = %q, want .a", got)
	}
	if got, _ := h.At(1); got != ".b" {
		t.Errorf("second = %q, want .b", got)
	}
	if _, ok := h.At(2); ok {
		t.Error("At past the end must report !ok")
	}
	for i := 0; i < HistoryLimit+10; i++ {
		h.Add(strings.Repeat(".x", i+1))
	}
	if h.Len() != HistoryLimit {
		t.Errorf("Len = %d, want the cap %d", h.Len(), HistoryLimit)
	}
}
