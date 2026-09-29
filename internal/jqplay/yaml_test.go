package jqplay

import (
	"context"
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

// rtManifest is the round-trip fixture (#2798): a commented, anchored
// manifest in the encoder's own layout, so a round-tripped edit is the
// fixture with one line changed.
const rtManifest = `# Deployment for the api
apiVersion: apps/v1
kind: Deployment # the kind
metadata:
  name: api
  labels: # inline labels comment
    app: api
  # annotations block
  annotations:
    team: "core"
spec:
  replicas: 1 # scale me
  template: &tpl
    image: 'alpine:3'
    ports: [80, 443]
  other: *tpl
`

// runRT runs program over text with the round-trip on.
func runRT(t *testing.T, program, text string) Result {
	t.Helper()
	return runOpts(t, DialectYQ, program, text, Options{RoundTrip: true})
}

// TestYQRoundTripEditKeepsEverythingElse is the acceptance case: an
// assignment on a commented manifest keeps every comment, anchor, alias,
// quoting style, flow sequence and key order; only that scalar changes.
func TestYQRoundTripEditKeepsEverythingElse(t *testing.T) {
	res := runRT(t, ".spec.replicas = 3", rtManifest)
	if res.Err != "" {
		t.Fatalf("unexpected error %q", res.Err)
	}
	want := strings.TrimRight(strings.Replace(rtManifest, "replicas: 1", "replicas: 3", 1), "\n")
	if res.Text() != want {
		t.Errorf("round-trip = \n%s\nwant\n%s", res.Text(), want)
	}
	if res.Note() != "" {
		t.Errorf("note = %q, want none", res.Note())
	}
	// Off, the plain serializer drops the comments and sorts the keys.
	plain := runOpts(t, DialectYQ, ".spec.replicas = 3", rtManifest, Options{})
	if strings.Contains(plain.Text(), "#") || !strings.HasPrefix(plain.Text(), "apiVersion") || strings.Contains(plain.Text(), "&tpl") {
		t.Errorf("plain = \n%s\nwant no comments and no anchors", plain.Text())
	}
}

// TestYQRoundTripDeleteTakesItsComment: deleting a mapping removes the
// mapping and its inline comment, nothing else — the head comment of the
// key after it stays.
func TestYQRoundTripDeleteTakesItsComment(t *testing.T) {
	res := runRT(t, "del(.metadata.labels)", rtManifest)
	if res.Err != "" {
		t.Fatalf("unexpected error %q", res.Err)
	}
	want := strings.TrimRight(strings.Replace(rtManifest, "  labels: # inline labels comment\n    app: api\n", "", 1), "\n")
	if res.Text() != want {
		t.Errorf("round-trip = \n%s\nwant\n%s", res.Text(), want)
	}
}

// TestYQRoundTripStringEditKeepsQuoting: a changed string keeps the quoting
// style it was written in, and its line comment.
func TestYQRoundTripStringEditKeepsQuoting(t *testing.T) {
	res := runRT(t, `.spec.template.image = "alpine:4"`, rtManifest)
	if !strings.Contains(res.Text(), "template: &tpl\n    image: 'alpine:4'\n    ports: [80, 443]\n") {
		t.Errorf("round-trip = \n%s\nwant the single-quoted style and the anchor kept", res.Text())
	}
	// The program ran over the expanded tree, so `other: *tpl` still holds
	// the old value: the alias is written out as a copy of it.
	if !strings.Contains(res.Text(), "  other:\n    image: 'alpine:3'\n    ports: [80, 443]") || res.Note() != "" {
		t.Errorf("round-trip = \n%s\n(note %q) want the alias written out with its old value", res.Text(), res.Note())
	}
	res = runRT(t, `.kind = "StatefulSet"`, rtManifest)
	if !strings.Contains(res.Text(), "kind: StatefulSet # the kind") {
		t.Errorf("round-trip = \n%s\nwant the line comment kept on the new value", res.Text())
	}
}

// TestYQRoundTripAddsNewKeysAtTheEnd: a key the program adds is appended
// after the written ones; the written order is kept.
func TestYQRoundTripAddsNewKeysAtTheEnd(t *testing.T) {
	res := runRT(t, `.metadata.namespace = "prod"`, rtManifest)
	want := "  annotations:\n    team: \"core\"\n  namespace: prod\nspec:"
	if !strings.Contains(res.Text(), want) {
		t.Errorf("round-trip = \n%s\nwant the new key appended to metadata", res.Text())
	}
}

// TestYQRoundTripSequenceEdits: deleting an item keeps its neighbours'
// comments, inserting one shifts the rest instead of rewriting them.
func TestYQRoundTripSequenceEdits(t *testing.T) {
	in := "items:\n  - a # first\n  - b # second\n  - c # third\n"
	res := runRT(t, "del(.items[0])", in)
	if want := "items:\n  - b # second\n  - c # third"; res.Text() != want {
		t.Errorf("delete = %q, want %q", res.Text(), want)
	}
	res = runRT(t, `.items = ["z"] + .items`, in)
	if want := "items:\n  - z\n  - a # first\n  - b # second\n  - c # third"; res.Text() != want {
		t.Errorf("insert = %q, want %q", res.Text(), want)
	}
	res = runRT(t, `.items[1] = "B"`, in)
	if want := "items:\n  - a # first\n  - B # second\n  - c # third"; res.Text() != want {
		t.Errorf("edit = %q, want %q", res.Text(), want)
	}
}

// TestYQRoundTripReshapeFallsBack: a program that reshapes the document, or
// produces several outputs, renders plainly with a note — never a crash.
func TestYQRoundTripReshapeFallsBack(t *testing.T) {
	cases := []struct{ program, note string }{
		{"to_entries", rtSkipReshaped},
		{".spec, .metadata", rtSkipMulti},
		{".metadata | keys", rtSkipReshaped},
		{"[.kind]", rtSkipReshaped},
		{"empty", ""},
	}
	for _, c := range cases {
		res := runRT(t, c.program, rtManifest)
		if res.Err != "" {
			t.Errorf("%s: unexpected error %q", c.program, res.Err)
		}
		plain := runOpts(t, DialectYQ, c.program, rtManifest, Options{})
		if res.Text() != plain.Text() {
			t.Errorf("%s: fallback = %q, want the plain form %q", c.program, res.Text(), plain.Text())
		}
		want := ""
		if c.note != "" {
			want = roundTripSkipped(c.note)
		}
		if res.Note() != want {
			t.Errorf("%s: note = %q, want %q", c.program, res.Note(), want)
		}
	}
}

// TestYQRoundTripVerifiesTheResult: a change the tree cannot express —
// deleting a key a merge supplies — falls back rather than showing a
// document that reads back differently; editing such a key writes it out
// explicitly, which wins over the merge.
func TestYQRoundTripVerifiesTheResult(t *testing.T) {
	in := "base: &b\n  image: alpine\n  pull: always\nsvc:\n  <<: *b\n  pull: never\n"
	res := runRT(t, "del(.svc.image)", in)
	if res.Note() != roundTripSkipped(rtSkipDiffers) {
		t.Errorf("note = %q, want the verification fallback", res.Note())
	}
	if strings.Contains(res.Text(), "<<") {
		t.Errorf("fallback = %q, want the plain form", res.Text())
	}
	res = runRT(t, `.svc.image = "busybox"`, in)
	if res.Note() != "" {
		t.Fatalf("note = %q, want none", res.Note())
	}
	if want := "base: &b\n  image: alpine\n  pull: always\nsvc:\n  <<: *b\n  pull: never\n  image: busybox"; res.Text() != want {
		t.Errorf("merged edit = %q, want %q", res.Text(), want)
	}
	// An untouched merged mapping stays as written — no explicit copies of
	// the keys the merge supplies.
	res = runRT(t, `.base.pull = "never"`, in)
	if want := "base: &b\n  image: alpine\n  pull: never\nsvc:\n  <<: *b\n  pull: never"; res.Text() != want {
		t.Errorf("anchor edit = %q, want %q", res.Text(), want)
	}
}

// TestYQRoundTripMultiDocument: every document of a stream is patched
// against its own tree.
func TestYQRoundTripMultiDocument(t *testing.T) {
	in := "# one\nkind: Service # svc\n---\n# two\nkind: Deployment\n"
	res := runRT(t, `.kind = "X"`, in)
	if want := "# one\nkind: X # svc\n---\n# two\nkind: X"; res.Text() != want {
		t.Errorf("round-trip = %q, want %q", res.Text(), want)
	}
	// One output per document, delivered page by page, patches the same way.
	parsed, err := DialectYQ.Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	p := Start(context.Background(), `.kind = "X"`, parsed, Options{RoundTrip: true})
	defer p.Stop()
	shape := p.Shape()
	for {
		pg, ok := p.Next(context.Background())
		if !ok {
			break
		}
		shape.Append(pg)
	}
	if want := "# one\nkind: X # svc\n---\n# two\nkind: X"; shape.Text() != want || shape.Note() != "" {
		t.Errorf("paged round-trip = %q (note %q)", shape.Text(), shape.Note())
	}
}

// TestYQRoundTripSkipsUnderOtherToggles: compact output and a slurped input
// have no document to patch; the note says which toggle is in the way. Raw
// output of a string is the string, as asked.
func TestYQRoundTripSkipsUnderOtherToggles(t *testing.T) {
	res := runOpts(t, DialectYQ, ".", rtManifest, Options{RoundTrip: true, Compact: true})
	if res.Note() != roundTripSkipped(rtSkipCompact) || strings.Contains(res.Text(), "#") {
		t.Errorf("compact: note %q text %q", res.Note(), res.Text())
	}
	res = runOpts(t, DialectYQ, ".", rtManifest, Options{RoundTrip: true, Slurp: true})
	if res.Note() != roundTripSkipped(rtSkipSlurp) || !strings.HasPrefix(res.Text(), "- ") {
		t.Errorf("slurp: note %q text %q", res.Note(), res.Text())
	}
	res = runOpts(t, DialectYQ, ".kind", rtManifest, Options{RoundTrip: true, Raw: true})
	if res.Text() != "Deployment" {
		t.Errorf("raw string = %q", res.Text())
	}
	if res := runOpts(t, DialectJQ, ".", `{"a":1}`, Options{RoundTrip: true}); res.Note() != "" || res.Text() != "{\n  \"a\": 1\n}" {
		t.Errorf("jq ignores the toggle: note %q text %q", res.Note(), res.Text())
	}
}

// TestYQRoundTripIdentityIsTheDocument: `.` round-trips to the document as
// written, comments and all — the whole point for a reader who only wants
// to look.
func TestYQRoundTripIdentityIsTheDocument(t *testing.T) {
	res := runRT(t, ".", rtManifest)
	if want := strings.TrimRight(rtManifest, "\n"); res.Text() != want {
		t.Errorf("identity = \n%s\nwant the document", res.Text())
	}
	// Numbers written in another base stay as written when untouched.
	res = runRT(t, ".b = 2", "a: 0x1f # hex\nb: 1\n")
	if want := "a: 0x1f # hex\nb: 2"; res.Text() != want {
		t.Errorf("hex kept = %q, want %q", res.Text(), want)
	}
}
