package jqplay

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// yaml_test.go covers the yq playground's input and output path (#2039):
// what a YAML buffer decodes into, what an evaluation renders back, and how
// the failures read. The program language itself is jq's and is covered by
// jqplay_test.go — running the same program over both dialects is exactly
// what the shared engine is for.

// TestYQEvaluateRendersYAML is the issue's core case: a YAML buffer, a jq
// program, YAML back out.
func TestYQEvaluateRendersYAML(t *testing.T) {
	in := "name: ike\nversion: 3\ntags:\n  - tui\n  - go\n"
	if got := EvaluateWith(DialectYQ, ".tags", in).Text(); got != "- tui\n- go" {
		t.Errorf("`.tags` = %q, want the sequence as YAML", got)
	}
	got := EvaluateWith(DialectYQ, "{n: .name, count: (.tags | length)}", in)
	if got.Err != "" {
		t.Fatalf("unexpected error %q", got.Err)
	}
	if want := "count: 2\nn: ike"; got.Text() != want {
		t.Errorf("constructed object = %q, want %q", got.Text(), want)
	}
}

// TestYQDialectIsCarried: the dialect rides on the parsed input and on the
// result it produced, so a host holding either never has to remember which
// playground it opened.
func TestYQDialectIsCarried(t *testing.T) {
	in, err := DialectYQ.Parse("a: 1\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if in.Dialect() != DialectYQ {
		t.Errorf("Input.Dialect() = %v, want DialectYQ", in.Dialect())
	}
	if res := EvaluateWith(DialectYQ, ".", "a: 1\n"); res.Dialect() != DialectYQ {
		t.Errorf("Result.Dialect() = %v, want DialectYQ", res.Dialect())
	}
	if res := Evaluate(".", `{"a":1}`); res.Dialect() != DialectJQ {
		t.Errorf("the jq path must stay DialectJQ, got %v", res.Dialect())
	}
}

// TestYQMultiDocument: a `---`-separated file is a stream, the way a `.jsonl`
// export is — the program runs over every document, and the outputs come back
// separated by the document marker rather than merged into one mapping.
func TestYQMultiDocument(t *testing.T) {
	in := "kind: Service\n---\nkind: Deployment\n"
	parsed, err := DialectYQ.Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Len() != 2 {
		t.Fatalf("Len() = %d, want 2 documents", parsed.Len())
	}
	if got := EvaluateWith(DialectYQ, ".kind", in).Text(); got != "Service\n---\nDeployment" {
		t.Errorf("`.kind` over two documents = %q", got)
	}
}

// TestYQAnchorsAndMerge: anchors are followed and a merge key is folded in
// with YAML's own precedence — an explicit key beats the merged one.
func TestYQAnchorsAndMerge(t *testing.T) {
	in := "base: &b\n  image: alpine\n  pull: always\nsvc:\n  <<: *b\n  pull: never\n"
	res := EvaluateWith(DialectYQ, ".svc", in)
	if res.Err != "" {
		t.Fatalf("unexpected error %q", res.Err)
	}
	if want := "image: alpine\npull: never"; res.Text() != want {
		t.Errorf("merged mapping = %q, want %q", res.Text(), want)
	}
}

// TestYQAliasBombIsBounded: alias expansion is unbounded in the source size,
// so the decoder spends a node budget and reports an error line instead of
// eating the machine.
func TestYQAliasBombIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("a: &a [x, x, x, x, x, x, x, x, x, x]\n")
	for i := 'b'; i <= 'h'; i++ {
		prev := string(rune(i - 1))
		b.WriteString(string(i) + ": &" + string(i) + " [")
		for j := 0; j < 10; j++ {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString("*" + prev)
		}
		b.WriteString("]\n")
	}
	_, err := DialectYQ.Parse(b.String())
	if !errors.Is(err, ErrYAMLTooBig) {
		t.Fatalf("Parse of an alias bomb = %v, want ErrYAMLTooBig", err)
	}
}

// TestYQNumbers: YAML's integer spellings arrive as the numbers they mean, a
// float keeps the digits it was written with, and an id wider than int64
// keeps every one of them.
func TestYQNumbers(t *testing.T) {
	in := "hex: 0x1f\nsep: 1_000\nf: 1.50\nid: 123456789012345678901234567890\n"
	cases := map[string]string{".hex": "31", ".sep": "1000", ".f": "1.50", ".id": "123456789012345678901234567890"}
	for prog, want := range cases {
		if got := EvaluateWith(DialectYQ, prog, in).Text(); got != want {
			t.Errorf("%s = %q, want %q", prog, got, want)
		}
	}
	if got := EvaluateWith(DialectYQ, ".hex + 1", in).Text(); got != "32" {
		t.Errorf("`.hex + 1` = %q, want 32 — the value must arrive as a number", got)
	}
	parsed, err := DialectYQ.Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	obj := parsed.values[0].(map[string]any)
	if _, ok := obj["hex"].(json.Number); !ok {
		t.Errorf("hex decoded as %T, want json.Number", obj["hex"])
	}
}

// TestYQScalarTagsStayStrings: what the core schema does not cover — a
// timestamp, a custom tag — stays the text it was written as, which is both
// visible and computable-on as a string.
func TestYQScalarTagsStayStrings(t *testing.T) {
	in := "when: 2026-08-21\nref: !Ref bucket\nbool: yes\n"
	if got := EvaluateWith(DialectYQ, ".when | type", in).Text(); got != "string" {
		t.Errorf("a timestamp is a %s, want string", got)
	}
	if got := EvaluateWith(DialectYQ, ".ref", in).Text(); got != "bucket" {
		t.Errorf("a custom-tagged scalar = %q, want its text", got)
	}
	if got := EvaluateWith(DialectYQ, ".bool | type", in).Text(); got != "string" {
		t.Errorf("`yes` is a %s under the core schema, want string", got)
	}
}

// TestYQNonStringKeys: jq objects have string keys, so a numeric or boolean
// YAML key is reachable under its source text rather than being lost.
func TestYQNonStringKeys(t *testing.T) {
	in := "1: one\ntrue: yes it is\n"
	if got := EvaluateWith(DialectYQ, `."1"`, in).Text(); got != "one" {
		t.Errorf(`."1" = %q, want "one"`, got)
	}
	if got := EvaluateWith(DialectYQ, "keys", in).Text(); got != "- \"1\"\n- \"true\"" {
		t.Errorf("keys = %q, want the two stringified keys", got)
	}
}

// TestYQStringsRenderReadably: a value that would read back as another type is
// quoted, and a multi-line string becomes a literal block rather than one
// escape-riddled row.
func TestYQStringsRenderReadably(t *testing.T) {
	if got := EvaluateWith(DialectYQ, `{v: "123"}`, "a: 1\n").Text(); got != `v: "123"` {
		t.Errorf("a numeric-looking string = %q, want it quoted", got)
	}
	got := EvaluateWith(DialectYQ, `{script: "one\ntwo\n"}`, "a: 1\n").Text()
	if want := "script: |\n  one\n  two"; got != want {
		t.Errorf("a multi-line string = %q, want the literal block %q", got, want)
	}
}

// TestYQEmptyAndBrokenInput: both failures are sentences on the info row, not
// crashes, and both name YAML rather than JSON.
func TestYQEmptyAndBrokenInput(t *testing.T) {
	if _, err := DialectYQ.Parse("   \n"); err == nil || !strings.Contains(err.Error(), "YAML") {
		t.Errorf("empty input error = %v, want it to name YAML", err)
	}
	_, err := DialectYQ.Parse("a: 1\n\tb: 2\n")
	if err == nil {
		t.Fatal("a tab-indented document must not parse")
	}
	var input *InputError
	if !errors.As(err, &input) || input.Dialect != DialectYQ {
		t.Fatalf("error is %v (%T), want an *InputError carrying DialectYQ", err, err)
	}
	if strings.Contains(err.Error(), "JSON") {
		t.Errorf("the YAML failure reads %q — it must not name JSON", err.Error())
	}
	if strings.Contains(input.Detail, "yaml:") || strings.Contains(input.Detail, "\n") {
		t.Errorf("Detail = %q, want the decoder's complaint as one bare sentence", input.Detail)
	}
}

// TestYQEmptyDocumentIsNull: `---` with nothing under it is a document all the
// same — dropping it would shift every later document's position.
func TestYQEmptyDocumentIsNull(t *testing.T) {
	in, err := DialectYQ.Parse("a: 1\n---\n---\nb: 2\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if in.Len() != 3 {
		t.Fatalf("Len() = %d, want 3 documents", in.Len())
	}
	if in.values[1] != nil {
		t.Errorf("the empty document decoded to %#v, want nil", in.values[1])
	}
}

// TestJQPathIsUnchanged guards the acceptance criterion that the jq
// playground's behavior did not move: the same program over the same data
// still renders JSON and still joins its outputs with a plain newline.
func TestJQPathIsUnchanged(t *testing.T) {
	res := Evaluate(".[]", `[{"a":1},2]`)
	if want := "{\n  \"a\": 1\n}\n2"; res.Text() != want {
		t.Errorf("jq result = %q, want %q", res.Text(), want)
	}
	if _, err := Parse("nope"); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("the jq input error = %v, want the original JSON wording", err)
	}
}
