package search

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
)

const structDoc = "{\n  \"users\": [\n    {\"name\": \"ada\"},\n    {\"name\": \"grace\"}\n  ]\n}"

func structuralQuery(t *testing.T, doc, program string) (Query, *buffer.Buffer) {
	t.Helper()
	b := buffer.FromString(doc)
	q := CompileStructural("json", program)
	q.SyncStructural(b, 1)
	return q, b
}

// TestStructuralMatches (#2363): a query selecting N nodes yields N spans,
// served through the same LineMatches/AllMatches surface text queries use.
func TestStructuralMatches(t *testing.T) {
	q, b := structuralQuery(t, structDoc, ".users[].name")
	all := q.AllMatches(b)
	if len(all) != 2 {
		t.Fatalf("AllMatches = %v, want 2 spans", all)
	}
	if got := q.LineMatches(b, 2); len(got) != 1 || got[0].Start != 13 {
		t.Fatalf("LineMatches(2) = %v, want the ada span at col 13", got)
	}
	if got := q.LineMatches(b, 0); got != nil {
		t.Fatalf("LineMatches(0) = %v, want none", got)
	}
}

// TestStructuralNextWraps (#2363): n/N step through structural matches with
// wrap-around, via the unchanged Next.
func TestStructuralNextWraps(t *testing.T) {
	q, b := structuralQuery(t, structDoc, ".users[].name")
	p, ok := q.Next(b, buffer.Position{Line: 0, Col: 0}, Forward, 1)
	if !ok || p.Line != 2 {
		t.Fatalf("first Next = %v %v, want line 2", p, ok)
	}
	p, ok = q.Next(b, p, Forward, 1)
	if !ok || p.Line != 3 {
		t.Fatalf("second Next = %v %v, want line 3", p, ok)
	}
	p, ok = q.Next(b, p, Forward, 1)
	if !ok || p.Line != 2 {
		t.Fatalf("wrap Next = %v %v, want line 2 again", p, ok)
	}
}

// TestStructuralTally (#2363): the tally counts structural matches like text
// ones, index included.
func TestStructuralTally(t *testing.T) {
	q, b := structuralQuery(t, structDoc, ".users[].name")
	tal := q.CountMatches(b, buffer.Position{Line: 3, Col: 13}, 0, 0)
	if tal.Index != 2 || tal.Total != 2 || tal.Capped {
		t.Fatalf("tally = %+v, want 2/2", tal)
	}
}

// TestStructuralError (#2363): an invalid query carries its error and matches
// nothing.
func TestStructuralError(t *testing.T) {
	q, b := structuralQuery(t, structDoc, ".users[")
	if err := q.StructuralErr(); !strings.HasPrefix(err, "jq: ") {
		t.Fatalf("StructuralErr = %q, want a jq error", err)
	}
	if all := q.AllMatches(b); len(all) != 0 {
		t.Fatalf("AllMatches = %v, want none on error", all)
	}
}

// TestStructuralNoLangError (#2363): compiling without a document language
// reports the gate instead of matching nothing.
func TestStructuralNoLangError(t *testing.T) {
	b := buffer.FromString("plain text")
	q := CompileStructural("", ".a")
	q.SyncStructural(b, 1)
	if err := q.StructuralErr(); !strings.Contains(err, "JSON or YAML") {
		t.Fatalf("StructuralErr = %q, want the language gate", err)
	}
}

// TestStructuralResyncOnVersion (#2363): a new document version re-evaluates
// through the shared state; the same version does not.
func TestStructuralResyncOnVersion(t *testing.T) {
	q, b := structuralQuery(t, structDoc, ".users[].name")
	if len(q.AllMatches(b)) != 2 {
		t.Fatal("want 2 matches before the edit")
	}
	b.ReplaceAll("{\n  \"users\": [\n    {\"name\": \"ada\"}\n  ]\n}")
	q.SyncStructural(b, 1) // same version: stale by design
	if len(q.AllMatches(b)) != 2 {
		t.Fatal("same version must keep the cached spans")
	}
	q.SyncStructural(b, 2)
	if all := q.AllMatches(b); len(all) != 1 {
		t.Fatalf("AllMatches after resync = %v, want 1", all)
	}
}

// TestStructuralID (#2363): structural queries key caches apart from text
// queries with the same pattern.
func TestStructuralID(t *testing.T) {
	s := CompileStructural("json", ".a")
	txt := Compile(".a", false, CaseSmart)
	if s.ID() == txt.ID() {
		t.Fatalf("structural and text ID collide: %q", s.ID())
	}
}
