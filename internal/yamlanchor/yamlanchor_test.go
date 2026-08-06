package yamlanchor

import (
	"reflect"
	"strings"
	"testing"

	"ike/internal/highlight"
)

func lines(s string) []string { return strings.Split(s, "\n") }

const k8sish = `defaults: &defaults
  adapter: postgres
  host: localhost
development:
  <<: *defaults
  database: dev
test:
  <<: *defaults
  database: test
`

// TestScanPairsAnchorsAndAliases (#1629): the scanner finds the anchor and
// both aliases, resolves each alias to the anchor's mark and keeps rune
// coordinates covering sigil plus name.
func TestScanPairsAnchorsAndAliases(t *testing.T) {
	marks := Scan(lines(k8sish))
	if len(marks) != 3 {
		t.Fatalf("got %d marks, want 3: %+v", len(marks), marks)
	}
	a := marks[0]
	if a.Alias || a.Name != "defaults" || a.Line != 0 || a.Col != 10 || a.Len != len("&defaults") {
		t.Errorf("anchor mark = %+v", a)
	}
	for _, al := range marks[1:] {
		if !al.Alias || al.Unresolved || al.Anchor != 0 {
			t.Errorf("alias mark = %+v, want resolved to mark 0", al)
		}
	}
}

// TestScanSkipsCommentsQuotesAndBlockScalars (#1629): a * or & inside a
// comment, a quoted scalar or a block-scalar body is text, not a token, and a
// * mid plain scalar is never an alias.
func TestScanSkipsCommentsQuotesAndBlockScalars(t *testing.T) {
	src := `a: &real 1
b: "not an *alias"
c: 'nor &this'
d: | # comment with *alias
  text with *alias and &anchor
e: *real # a *comment
f: a * b
`
	marks := Scan(lines(src))
	if len(marks) != 2 {
		t.Fatalf("got %d marks, want 2 (&real, *real): %+v", len(marks), marks)
	}
	if marks[0].Name != "real" || marks[0].Alias || marks[1].Name != "real" || !marks[1].Alias {
		t.Errorf("marks = %+v", marks)
	}
}

// TestScanDocumentBoundary (#1629): --- resets the anchor table — an alias
// in the second document never resolves to the first document's anchor.
func TestScanDocumentBoundary(t *testing.T) {
	src := `a: &x 1
b: *x
---
c: *x
`
	marks := Scan(lines(src))
	if len(marks) != 3 {
		t.Fatalf("got %d marks, want 3", len(marks))
	}
	if marks[1].Unresolved || marks[1].Anchor != 0 {
		t.Errorf("same-doc alias = %+v, want resolved", marks[1])
	}
	if !marks[2].Unresolved {
		t.Errorf("cross-doc alias = %+v, want unresolved", marks[2])
	}
}

// TestScanForwardAliasUnresolved (#1629): YAML aliases refer back; an alias
// before its anchor is an error and marks as unresolved.
func TestScanForwardAliasUnresolved(t *testing.T) {
	marks := Scan(lines("a: *later\nb: &later 1\n"))
	if len(marks) != 2 || !marks[0].Unresolved {
		t.Fatalf("marks = %+v, want the forward alias unresolved", marks)
	}
}

// TestSpansShareSlotPerName (#1629): the anchor and its aliases carry the
// same rainbow capture, a different name hashes (here) to a different one,
// and the unresolved alias carries the error capture.
func TestSpansShareSlotPerName(t *testing.T) {
	src := `a: &one 1
b: *one
c: &two 2
d: *missing
`
	spans := Spans(lines(src))
	if len(spans) != 4 {
		t.Fatalf("got %d spans, want 4", len(spans))
	}
	if spans[0].Capture != spans[1].Capture {
		t.Errorf("&one=%q *one=%q, want the same capture", spans[0].Capture, spans[1].Capture)
	}
	if !strings.HasPrefix(spans[0].Capture, "rainbow.") {
		t.Errorf("capture %q, want a rainbow slot", spans[0].Capture)
	}
	if spans[3].Capture != Unresolved {
		t.Errorf("unresolved capture = %q, want %q", spans[3].Capture, Unresolved)
	}
	if Slot("one") == Slot("two") {
		t.Skip("hash collision between test names; slot distinctness covered by Capture form")
	}
	if spans[0].Capture == spans[2].Capture {
		t.Errorf("distinct names share capture %q", spans[0].Capture)
	}
}

// TestSlotStable (#1629): the slot is a pure hash — stable across calls and
// within the palette range.
func TestSlotStable(t *testing.T) {
	if Slot("defaults") != Slot("defaults") {
		t.Error("slot not stable")
	}
	if s := Slot("defaults"); s < 0 || s >= highlight.RainbowColors {
		t.Errorf("slot %d out of palette range", s)
	}
}

// TestDefinitionAt (#1629): an alias resolves to its anchor's position; an
// anchor or plain text claims nothing.
func TestDefinitionAt(t *testing.T) {
	ls := lines(k8sish)
	mk, ok := DefinitionAt(ls, 4, 7) // on *defaults
	if !ok || mk.Line != 0 || mk.Col != 10 {
		t.Fatalf("definition = %+v/%v, want the anchor at 0:10", mk, ok)
	}
	if _, ok := DefinitionAt(ls, 0, 12); ok {
		t.Error("an anchor position must not claim a definition")
	}
	if _, ok := DefinitionAt(ls, 1, 4); ok {
		t.Error("plain text must not claim a definition")
	}
}

// TestUsagesAt (#1629): from the anchor and from an alias alike, usages list
// every mark of the name in document order.
func TestUsagesAt(t *testing.T) {
	ls := lines(k8sish)
	name, refs, ok := UsagesAt(ls, 0, 11)
	if !ok || name != "defaults" || len(refs) != 3 {
		t.Fatalf("usages from anchor = %q/%d/%v, want defaults/3", name, len(refs), ok)
	}
	wantLines := []int{0, 4, 7}
	for i, r := range refs {
		if r.Line != wantLines[i] {
			t.Errorf("ref %d on line %d, want %d", i, r.Line, wantLines[i])
		}
	}
	if _, refs2, ok := UsagesAt(ls, 7, 7); !ok || !reflect.DeepEqual(refs, refs2) {
		t.Error("usages from an alias must match usages from the anchor")
	}
	if _, _, ok := UsagesAt(ls, 1, 4); ok {
		t.Error("plain text must not claim usages")
	}
}

// TestResolveInlineScalar (#1629): an anchor with an inline value previews as
// that value, comment cut.
func TestResolveInlineScalar(t *testing.T) {
	ls := lines("a: &x hello world # note\nb: *x\n")
	name, val, ok := ResolveAt(ls, 1, 4)
	if !ok || name != "x" {
		t.Fatalf("resolve = %q/%v", name, ok)
	}
	if !reflect.DeepEqual(val, []string{"hello world"}) {
		t.Errorf("value = %q, want [hello world]", val)
	}
}

// TestResolveBlockValue (#1629): a block anchor previews the indented block,
// dedented.
func TestResolveBlockValue(t *testing.T) {
	ls := lines(k8sish)
	_, val, ok := ResolveAt(ls, 4, 7)
	if !ok {
		t.Fatal("alias did not resolve")
	}
	want := []string{"adapter: postgres", "host: localhost"}
	if !reflect.DeepEqual(val, want) {
		t.Errorf("value = %q, want %q", val, want)
	}
}

// TestResolveMergeKey (#1629): a `<<:` line inside the anchored block splices
// the merged anchor's value at the merge key's indent — recursively through a
// chain.
func TestResolveMergeKey(t *testing.T) {
	src := `base: &base
  adapter: postgres
mid: &mid
  <<: *base
  pool: 5
use:
  final: *mid
`
	ls := lines(src)
	_, val, ok := ResolveAt(ls, 6, 10)
	if !ok {
		t.Fatal("alias did not resolve")
	}
	want := []string{"adapter: postgres", "pool: 5"}
	if !reflect.DeepEqual(val, want) {
		t.Errorf("value = %q, want %q", val, want)
	}
}

// TestResolveMergeList (#1629): the sequence form `<<: [*a, *b]` splices both
// values in order.
func TestResolveMergeList(t *testing.T) {
	src := `a: &a
  one: 1
b: &b
  two: 2
c: &c
  <<: [*a, *b]
  three: 3
d: *c
`
	ls := lines(src)
	_, val, ok := ResolveAt(ls, 7, 4)
	if !ok {
		t.Fatal("alias did not resolve")
	}
	want := []string{"one: 1", "two: 2", "three: 3"}
	if !reflect.DeepEqual(val, want) {
		t.Errorf("value = %q, want %q", val, want)
	}
}

// TestResolveCycleTerminates (#1629): two anchors merging each other must not
// recurse forever; the preview simply stops expanding.
func TestResolveCycleTerminates(t *testing.T) {
	src := `a: &a
  <<: *b
  x: 1
b: &b
  <<: *a
  y: 2
c: *b
`
	ls := lines(src)
	_, val, ok := ResolveAt(ls, 6, 4)
	if !ok || len(val) == 0 {
		t.Fatalf("cyclic merge must still resolve, got %q/%v", val, ok)
	}
}

// TestResolveUnresolvedAliasFails (#1629): no anchor, no preview — the claim
// passes so nothing pretends to know the value.
func TestResolveUnresolvedAliasFails(t *testing.T) {
	if _, _, ok := ResolveAt(lines("a: *ghost\n"), 0, 4); ok {
		t.Error("an unresolved alias must not resolve")
	}
}

// TestResolveCapsPreview (#1629): a long block truncates with an ellipsis
// line.
func TestResolveCapsPreview(t *testing.T) {
	var b strings.Builder
	b.WriteString("long: &long\n")
	for i := 0; i < 40; i++ {
		b.WriteString("  key: value\n")
	}
	b.WriteString("use: *long\n")
	ls := lines(b.String())
	_, val, ok := ResolveAt(ls, 41, 6)
	if !ok {
		t.Fatal("alias did not resolve")
	}
	if len(val) != previewCap+1 || val[len(val)-1] != "…" {
		t.Errorf("capped preview = %d lines ending %q, want %d + ellipsis", len(val), val[len(val)-1], previewCap)
	}
}
