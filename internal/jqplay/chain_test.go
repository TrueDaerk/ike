package jqplay

import (
	"context"
	"errors"
	"testing"
)

func TestChainRunsTheNextProgramOverTheResult(t *testing.T) {
	res := Evaluate(".data.items", `{"data":{"items":[{"ok":true,"n":1},{"ok":false,"n":2}]}}`)
	in, text, err := res.Chain()
	if err != nil {
		t.Fatalf("Chain: %v", err)
	}
	if in.Len() != 1 || in.Dialect() != DialectJQ || in.Origin() != "chained" {
		t.Fatalf("input = %d values, %v, origin %q", in.Len(), in.Dialect(), in.Origin())
	}
	if text != res.Text() || in.Size() != len(text) {
		t.Fatalf("text = %q (size %d), want the result text", text, in.Size())
	}
	next := Run(context.Background(), "map(select(.ok)) | .[].n", in)
	if next.Err != "" || next.Text() != "1" {
		t.Fatalf("next run = %q / %q", next.Text(), next.Err)
	}
}

func TestChainKeepsAStreamAsValues(t *testing.T) {
	res := Evaluate(".[]", `[1,"a",{"b":2}]`)
	in, _, err := res.Chain()
	if err != nil || in.Len() != 3 {
		t.Fatalf("Chain = %v values, %v", in.Len(), err)
	}
	next := Run(context.Background(), "type", in)
	if got := next.Text(); got != "\"number\"\n\"string\"\n\"object\"" {
		t.Fatalf("types = %q", got)
	}
}

// A raw or compact rendering is a view of the values: the chained input and
// its sample text are the default spelling.
func TestChainIgnoresTheOutputForm(t *testing.T) {
	in0, _ := Parse(`{"a":["x","y"]}`)
	res := RunWith(context.Background(), ".a[]", in0, Options{Raw: true})
	if res.Text() != "x\ny" {
		t.Fatalf("raw text = %q", res.Text())
	}
	in, text, err := res.Chain()
	if err != nil {
		t.Fatal(err)
	}
	if text != "\"x\"\n\"y\"" {
		t.Fatalf("sample text = %q", text)
	}
	if got := Run(context.Background(), "ascii_upcase", in).Text(); got != "\"X\"\n\"Y\"" {
		t.Fatalf("next = %q", got)
	}
}

func TestChainYAML(t *testing.T) {
	res := EvaluateWith(DialectYQ, ".items", "items:\n  - name: a\n  - name: b\n")
	in, text, err := res.Chain()
	if err != nil || in.Dialect() != DialectYQ {
		t.Fatalf("Chain = %v, %v", in.Dialect(), err)
	}
	if _, perr := DialectYQ.Parse(text); perr != nil {
		t.Fatalf("sample text does not parse as YAML: %v", perr)
	}
	if got := Run(context.Background(), ".[1].name", in).Text(); got != "b" {
		t.Fatalf("next = %q", got)
	}
}

func TestChainRefusals(t *testing.T) {
	if _, _, err := Evaluate("empty", `{}`).Chain(); !errors.Is(err, ErrChainEmpty) {
		t.Fatalf("empty result: %v", err)
	}
	xmq := Result{dialect: DialectXMQ, Outputs: []string{"{}"}}
	if _, _, err := xmq.Chain(); !errors.Is(err, ErrChainXMQ) {
		t.Fatalf("xmq result: %v", err)
	}
	capped := Evaluate(".", `1`)
	capped.Truncated = true
	if in, _, err := capped.Chain(); err != nil || in.Origin() != "chained from a capped result" {
		t.Fatalf("capped: %v %v", in, err)
	}
}
