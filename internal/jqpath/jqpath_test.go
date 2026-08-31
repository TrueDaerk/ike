package jqpath

import (
	"context"
	"strings"
	"testing"
)

// find is the test entry: Find with a background context, failing the test on
// an unexpected error.
func find(t *testing.T, langID, text, program string) []Span {
	t.Helper()
	spans, _, err := Find(context.Background(), langID, text, program)
	if err != nil {
		t.Fatalf("Find(%q) error: %v", program, err)
	}
	return spans
}

const jsonDoc = `{
  "users": [
    {"name": "ada", "age": 36},
    {"name": "grace", "age": 45}
  ],
  "count": 2
}`

// TestJSONScalarSpans (#2363): a query selecting scalars maps each to the
// token it occupies in the source.
func TestJSONScalarSpans(t *testing.T) {
	spans := find(t, "json", jsonDoc, ".users[].name")
	want := []Span{
		{Line: 2, Start: 13, End: 18}, // "ada"
		{Line: 3, Start: 13, End: 20}, // "grace"
	}
	eqSpans(t, spans, want)
}

// TestJSONFilterQuery (#2363): full jq (select, comparisons) works, since the
// query runs wrapped in path().
func TestJSONFilterQuery(t *testing.T) {
	spans := find(t, "json", jsonDoc, `.users[] | select(.age > 40) | .name`)
	eqSpans(t, spans, []Span{{Line: 3, Start: 13, End: 20}})
}

// TestJSONContainerSpan (#2363): selecting a container highlights its first
// line from the opening token.
func TestJSONContainerSpan(t *testing.T) {
	spans := find(t, "json", jsonDoc, ".users")
	eqSpans(t, spans, []Span{{Line: 1, Start: 11, End: 12}})
}

// TestJSONIdentity (#2363): "." selects the document root.
func TestJSONIdentity(t *testing.T) {
	spans := find(t, "json", jsonDoc, ".")
	eqSpans(t, spans, []Span{{Line: 0, Start: 0, End: 1}})
}

// TestJSONMissingKeyYieldsNoMatch (#2363): path() emits paths to locations a
// document does not contain; those resolve to no source node and no match.
func TestJSONMissingKeyYieldsNoMatch(t *testing.T) {
	if spans := find(t, "json", jsonDoc, ".missing"); len(spans) != 0 {
		t.Fatalf("spans = %v, want none", spans)
	}
}

// TestJSONRecursiveDescent (#2363): `..` selects every node exactly once.
func TestJSONRecursiveDescent(t *testing.T) {
	spans := find(t, "json", `{"a": {"b": 1}}`, "..")
	// root, .a, .a.b — three nodes.
	if len(spans) != 3 {
		t.Fatalf("len(spans) = %d, want 3 (%v)", len(spans), spans)
	}
}

// TestJSONStreamAbsolutePositions (#2363): an ndjson stream evaluates per
// value with positions absolute in the buffer.
func TestJSONStreamAbsolutePositions(t *testing.T) {
	spans := find(t, "ndjson", "{\"id\": 1}\n{\"id\": 2}", ".id")
	want := []Span{
		{Line: 0, Start: 7, End: 8},
		{Line: 1, Start: 7, End: 8},
	}
	eqSpans(t, spans, want)
}

// TestJSONStringEscapes (#2363): escaped keys decode like JSON, so a query
// addressing the decoded key resolves.
func TestJSONStringEscapes(t *testing.T) {
	spans := find(t, "json", `{"a\nb": "x"}`, `.["a` + "\n" + `b"]`)
	eqSpans(t, spans, []Span{{Line: 0, Start: 9, End: 12}})
}

// TestInvalidProgramErrors (#2363): a syntax error surfaces as an error, not
// as zero silent matches.
func TestInvalidProgramErrors(t *testing.T) {
	_, _, err := Find(context.Background(), "json", jsonDoc, ".users[")
	if err == nil || !strings.HasPrefix(err.Error(), "jq: ") {
		t.Fatalf("err = %v, want a jq error", err)
	}
}

// TestNonPathQueryErrors (#2363): a query computing a value rather than
// selecting locations fails path() with a clear runtime error.
func TestNonPathQueryErrors(t *testing.T) {
	_, _, err := Find(context.Background(), "json", jsonDoc, ".count + 1")
	if err == nil || !strings.HasPrefix(err.Error(), "jq: ") {
		t.Fatalf("err = %v, want a jq error", err)
	}
}

// TestInvalidJSONErrors (#2363): a broken document reports the parse failure
// with its line.
func TestInvalidJSONErrors(t *testing.T) {
	_, _, err := Find(context.Background(), "json", "{\n  \"a\": ,\n}", ".a")
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("err = %v, want a line-2 JSON error", err)
	}
}

// TestUnsupportedLanguageErrors (#2363): a buffer without a document language
// reports why instead of matching nothing.
func TestUnsupportedLanguageErrors(t *testing.T) {
	_, _, err := Find(context.Background(), "go", "{}", ".")
	if err == nil {
		t.Fatal("err = nil, want an unsupported-language error")
	}
}

// TestEmptyProgramIsIdle (#2363): an empty program yields nothing, like the
// playground's idle state.
func TestEmptyProgramIsIdle(t *testing.T) {
	spans, capped, err := Find(context.Background(), "json", jsonDoc, "  ")
	if err != nil || capped || spans != nil {
		t.Fatalf("Find(empty) = %v, %v, %v; want idle", spans, capped, err)
	}
}

// TestMatchCap (#2363): a query selecting more than MaxMatches nodes caps the
// list and reports it.
func TestMatchCap(t *testing.T) {
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < MaxMatches+100; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("1")
	}
	b.WriteString("]")
	spans, capped, err := Find(context.Background(), "json", b.String(), ".[]")
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}
	if !capped || len(spans) != MaxMatches {
		t.Fatalf("len = %d capped = %v, want %d capped", len(spans), capped, MaxMatches)
	}
}

const yamlDoc = `spec:
  containers:
    - name: web
      image: nginx:1.25
    - name: sidecar
      image: envoy
  replicas: 3`

// TestYAMLScalarSpans (#2363): plain YAML scalars highlight exactly their
// token.
func TestYAMLScalarSpans(t *testing.T) {
	spans := find(t, "yaml", yamlDoc, ".spec.containers[].name")
	want := []Span{
		{Line: 2, Start: 12, End: 15}, // web
		{Line: 4, Start: 12, End: 19}, // sidecar
	}
	eqSpans(t, spans, want)
}

// TestYAMLContainerSpan (#2363): a block mapping highlights its first line.
func TestYAMLContainerSpan(t *testing.T) {
	spans := find(t, "yaml", yamlDoc, ".spec.containers[0]")
	eqSpans(t, spans, []Span{{Line: 2, Start: 6, End: 15}})
}

// TestYAMLNumbersCompare (#2363): YAML ints decode as numbers, so numeric
// comparison in the query works.
func TestYAMLNumbersCompare(t *testing.T) {
	spans := find(t, "yaml", yamlDoc, ".spec | select(.replicas == 3) | .replicas")
	eqSpans(t, spans, []Span{{Line: 6, Start: 12, End: 13}})
}

// TestYAMLMultiDocAbsoluteLines (#2363): a `---` stream evaluates per document
// with absolute buffer lines.
func TestYAMLMultiDocAbsoluteLines(t *testing.T) {
	spans := find(t, "yaml", "id: 1\n---\nid: 2", ".id")
	want := []Span{
		{Line: 0, Start: 4, End: 5},
		{Line: 2, Start: 4, End: 5},
	}
	eqSpans(t, spans, want)
}

// TestYAMLMergeKeys (#2363): a key folded in via `<<: *base` resolves to the
// anchored source it is written at.
func TestYAMLMergeKeys(t *testing.T) {
	doc := "base: &base\n  region: eu\nprod:\n  <<: *base\n  size: big"
	spans := find(t, "yaml", doc, ".prod.region")
	eqSpans(t, spans, []Span{{Line: 1, Start: 10, End: 12}})
}

// TestYAMLAliasSite (#2363): selecting an aliased value itself highlights the
// alias site, not the anchor.
func TestYAMLAliasSite(t *testing.T) {
	doc := "base: &base\n  region: eu\nprod: *base"
	spans := find(t, "yaml", doc, ".prod")
	eqSpans(t, spans, []Span{{Line: 2, Start: 6, End: 11}})
}

// TestYAMLQuotedScalarSpansLine (#2363): a quoted scalar's source width is not
// its value's, so it highlights through the end of the line.
func TestYAMLQuotedScalarSpansLine(t *testing.T) {
	spans := find(t, "yaml", `name: "a b"`, ".name")
	eqSpans(t, spans, []Span{{Line: 0, Start: 6, End: 11}})
}

// TestInvalidYAMLErrors (#2363): a broken YAML document reports the decoder's
// complaint.
func TestInvalidYAMLErrors(t *testing.T) {
	_, _, err := Find(context.Background(), "yaml", "a: [1,", ".a")
	if err == nil || !strings.Contains(err.Error(), "YAML") {
		t.Fatalf("err = %v, want a YAML error", err)
	}
}

func eqSpans(t *testing.T, got, want []Span) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("spans = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("spans[%d] = %v, want %v (all: %v)", i, got[i], want[i], got)
		}
	}
}
