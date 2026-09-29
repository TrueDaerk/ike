package jqplay

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// runOpts parses text in d and runs program under opts.
func runOpts(t *testing.T, d Dialect, program, text string, opts Options) Result {
	t.Helper()
	in, err := d.Parse(text)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return RunWith(context.Background(), program, in, opts)
}

// TestRunWithRaw: -r prints a string output bare, one per line, and names the
// result `.txt` without folds; off, the quoted JSON form comes back.
func TestRunWithRaw(t *testing.T) {
	doc := `{"name":"alice","tags":["a","b"]}`
	if got := runOpts(t, DialectJQ, ".name", doc, Options{}).Text(); got != `"alice"` {
		t.Fatalf("default = %q, want the quoted form", got)
	}
	res := runOpts(t, DialectJQ, ".name", doc, Options{Raw: true})
	if res.Text() != "alice" {
		t.Fatalf("raw = %q, want alice", res.Text())
	}
	if res.Ext() != "txt" || res.ResultPath() != "jq result.txt" {
		t.Fatalf("raw ext = %q path = %q, want txt", res.Ext(), res.ResultPath())
	}
	if !res.Options().Raw {
		t.Fatal("result does not report its raw option")
	}
	if got := runOpts(t, DialectJQ, ".tags[]", doc, Options{Raw: true}).Text(); got != "a\nb" {
		t.Fatalf("raw stream = %q, want one per line", got)
	}
	// A non-string keeps its JSON spelling, and a raw result never folds.
	obj := runOpts(t, DialectJQ, ".", doc, Options{Raw: true})
	if !strings.Contains(obj.Text(), `"name": "alice"`) || obj.Folds() != nil {
		t.Fatalf("raw object = %q folds=%v", obj.Text(), obj.Folds())
	}
	// yq: raw strings drop the separator too.
	y := runOpts(t, DialectYQ, ".items[]", "items:\n  - \"true\"\n  - x\n", Options{Raw: true})
	if y.Text() != "true\nx" {
		t.Fatalf("yq raw = %q", y.Text())
	}
}

// TestRunWithCompact: -c puts every output on one line and disables folds.
func TestRunWithCompact(t *testing.T) {
	doc := `{"a":{"b":[1,2]},"c":"x"}`
	pretty := runOpts(t, DialectJQ, ".", doc, Options{})
	if len(pretty.Folds()) == 0 {
		t.Fatal("pretty result should fold")
	}
	res := runOpts(t, DialectJQ, ".a, .c", doc, Options{Compact: true})
	if res.Text() != "{\"b\":[1,2]}\n\"x\"" {
		t.Fatalf("compact = %q", res.Text())
	}
	if res.Folds() != nil {
		t.Fatalf("compact folds = %v, want none", res.Folds())
	}
	if res.Ext() != "json" {
		t.Fatalf("compact ext = %q", res.Ext())
	}
	if got := runOpts(t, DialectJQ, ".c", doc, Options{Compact: true, Raw: true}).Text(); got != "x" {
		t.Fatalf("-rc string = %q, want x", got)
	}
	y := runOpts(t, DialectYQ, ".", "a:\n  b: [1, 2]\n---\nc: 1\n", Options{Compact: true})
	if y.Text() != "{\"a\":{\"b\":[1,2]}}\n---\n{\"c\":1}" || y.Folds() != nil {
		t.Fatalf("yq compact = %q folds=%v", y.Text(), y.Folds())
	}
	if got := y.ValueStarts(); len(got) != 2 || got[1] != 2 {
		t.Fatalf("yq compact value starts = %v", got)
	}
}

// TestRunWithSlurp: -s runs the program once over one array of every input
// value — a JSONL stream becomes countable.
func TestRunWithSlurp(t *testing.T) {
	jsonl := "{\"n\":1}\n{\"n\":2}\n{\"n\":3}\n"
	if got := runOpts(t, DialectJQ, "length", jsonl, Options{}).Text(); got != "1\n1\n1" {
		t.Fatalf("unslurped length = %q", got)
	}
	if got := runOpts(t, DialectJQ, "length", jsonl, Options{Slurp: true}).Text(); got != "3" {
		t.Fatalf("slurped length = %q, want 3", got)
	}
	if got := runOpts(t, DialectJQ, "map(.n) | add", jsonl, Options{Slurp: true}).Text(); got != "6" {
		t.Fatalf("slurped sum = %q, want 6", got)
	}
	// The input itself is untouched: the next unslurped run sees three values.
	in, _ := DialectJQ.Parse(jsonl)
	RunWith(context.Background(), "length", in, Options{Slurp: true})
	if in.Len() != 3 {
		t.Fatalf("slurp mutated the input: %d values", in.Len())
	}
	if got := runOpts(t, DialectYQ, "length", "a: 1\n---\nb: 2\n", Options{Slurp: true}).Text(); got != "2" {
		t.Fatalf("yq slurped length = %q, want 2", got)
	}
}

// TestOptionsFlagsRoundTrip: Flags spells the toggles in command-line order and
// ParseFlags reads them back, ignoring unknown words.
func TestOptionsFlagsRoundTrip(t *testing.T) {
	all := Options{Raw: true, Compact: true, Slurp: true}
	if all.Flags() != "-r -c -s" {
		t.Fatalf("flags = %q", all.Flags())
	}
	if (Options{}).Flags() != "" {
		t.Fatal("zero options should spell as empty")
	}
	if got := ParseFlags("-s -x -r"); got != (Options{Raw: true, Slurp: true}) {
		t.Fatalf("parse = %+v", got)
	}
}

// TestLastProgramsFlagsPersist: the toggles are stored with the program and
// survive a reload; a plain Set clears them.
func TestLastProgramsFlagsPersist(t *testing.T) {
	file := filepath.Join(t.TempDir(), "last.json")
	l := NewLastPrograms(file)
	l.SetWithFlags("jq:file:/a.json", ".name", "-r -s")
	l.Set("jq:file:/b.json", ".x")
	re := NewLastPrograms(file)
	if got := re.Flags("jq:file:/a.json"); got != "-r -s" {
		t.Fatalf("reloaded flags = %q", got)
	}
	if got := re.Flags("jq:file:/b.json"); got != "" {
		t.Fatalf("b flags = %q, want none", got)
	}
	re.Set("jq:file:/a.json", ".name")
	if got := re.Flags("jq:file:/a.json"); got != "" {
		t.Fatalf("flags after plain Set = %q", got)
	}
}
