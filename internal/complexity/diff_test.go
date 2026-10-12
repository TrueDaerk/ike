package complexity

import (
	"reflect"
	"testing"
)

// diff_test.go covers the pure diff of #2918: matching by name and ordinal
// (duplicate names included), the changed / added / removed classification,
// the |ΔCCN| order, the verdict and its cognitive tiebreak, and the empty
// diff.

func unit(name string, line, ccn, cog int) Function {
	return Function{Name: name, Kind: KindFunction, Line: line, EndLine: line + 5, NLOC: 5, Params: 1, CCN: ccn, Cognitive: cog, Depth: 1}
}

func TestDiffFunctionsMatchesByNameAndOrdinal(t *testing.T) {
	base := []Function{
		unit("a", 0, 3, 2),
		unit("dup", 10, 1, 0),
		unit("dup", 20, 4, 3),
		unit("gone", 30, 7, 9),
	}
	head := []Function{
		unit("dup", 0, 1, 0),  // first dup: unchanged (moved only)
		unit("a", 10, 5, 2),   // CCN 3 -> 5
		unit("dup", 20, 4, 6), // second dup: cognitive 3 -> 6
		unit("dup", 30, 2, 2), // third dup: added
		unit("new", 40, 1, 0),
	}
	d := DiffFunctions(base, head)
	if len(d.Changed) != 2 {
		t.Fatalf("changed = %+v, want 2", d.Changed)
	}
	// |ΔCCN| 2 first, then the cognitive-only change.
	if d.Changed[0].Before.Name != "a" || d.Changed[0].DeltaCCN() != 2 || d.Changed[0].After.Line != 10 {
		t.Errorf("changed[0] = %+v", d.Changed[0])
	}
	if d.Changed[1].Before.Name != "dup" || d.Changed[1].Before.Line != 20 || d.Changed[1].DeltaCognitive() != 3 || d.Changed[1].DeltaCCN() != 0 {
		t.Errorf("changed[1] = %+v", d.Changed[1])
	}
	if got := namesOf(d.Added); !reflect.DeepEqual(got, []string{"dup", "new"}) || d.Added[0].Line != 30 {
		t.Errorf("added = %v (%+v)", got, d.Added)
	}
	if got := namesOf(d.Removed); !reflect.DeepEqual(got, []string{"gone"}) {
		t.Errorf("removed = %v", got)
	}
}

func TestDiffFunctionsMovedOnlyIsEmpty(t *testing.T) {
	base := []Function{unit("a", 0, 3, 2), unit("b", 10, 1, 1)}
	head := []Function{unit("b", 0, 1, 1), unit("a", 50, 3, 2)}
	if d := DiffFunctions(base, head); !d.Empty() {
		t.Fatalf("moved functions must not count as changed: %+v", d)
	}
	if d := DiffFunctions(nil, nil); !d.Empty() {
		t.Fatalf("nil vs nil = %+v", d)
	}
}

func TestDiffFunctionsEveryMetricCounts(t *testing.T) {
	b := unit("f", 0, 2, 1)
	for name, mut := range map[string]func(*Function){
		"ccn":    func(f *Function) { f.CCN++ },
		"cog":    func(f *Function) { f.Cognitive++ },
		"nloc":   func(f *Function) { f.NLOC++ },
		"params": func(f *Function) { f.Params++ },
		"depth":  func(f *Function) { f.Depth++ },
	} {
		h := b
		mut(&h)
		if d := DiffFunctions([]Function{b}, []Function{h}); len(d.Changed) != 1 {
			t.Errorf("%s: a differing %s must classify as changed: %+v", name, name, d)
		}
	}
	h := b
	h.Kind, h.EndLine = KindMethod, 99
	if d := DiffFunctions([]Function{b}, []Function{h}); !d.Empty() {
		t.Errorf("kind / range are not metrics: %+v", d)
	}
}

func TestDiffProjectSummariesAndVerdict(t *testing.T) {
	base := map[string][]Function{
		"a.go": {unit("f", 0, 5, 4), unit("g", 10, 2, 1)},
		"d.go": {unit("old", 0, 3, 3)}, // deleted file
	}
	head := map[string][]Function{
		"a.go": {unit("f", 0, 8, 4), unit("g", 10, 2, 1)},
		"n.go": {unit("fresh", 0, 1, 0)}, // new file
	}
	d := DiffProject(base, head, 15, 15)
	if d.Base.Functions != 3 || d.Base.CCNSum != 10 || d.Head.Functions != 3 || d.Head.CCNSum != 11 {
		t.Fatalf("summaries = base %+v head %+v", d.Base, d.Head)
	}
	if d.DeltaCCN() != 1 || d.Verdict != MoreComplex || d.Verdict.String() != "more complex" {
		t.Fatalf("delta %d verdict %v", d.DeltaCCN(), d.Verdict)
	}
	c, a, r := d.Counts()
	if c != 1 || a != 1 || r != 1 {
		t.Fatalf("counts = %d %d %d", c, a, r)
	}
	if got := d.Paths(); !reflect.DeepEqual(got, []string{"a.go", "d.go", "n.go"}) {
		t.Fatalf("paths = %v", got)
	}
	if len(d.Files["d.go"].Removed) != 1 || len(d.Files["n.go"].Added) != 1 {
		t.Fatalf("files = %+v", d.Files)
	}
}

func TestDiffProjectVerdictTiebreakAndEmpty(t *testing.T) {
	// ΣCCN equal (one up, one down), Σcognitive down: less complex.
	base := map[string][]Function{"a.go": {unit("f", 0, 5, 9), unit("g", 10, 5, 1)}}
	head := map[string][]Function{"a.go": {unit("f", 0, 6, 2), unit("g", 10, 4, 1)}}
	d := DiffProject(base, head, 0, 0)
	if d.DeltaCCN() != 0 || d.DeltaCognitive() != -7 || d.Verdict != LessComplex {
		t.Fatalf("tiebreak: Δccn %d Δcog %d verdict %v", d.DeltaCCN(), d.DeltaCognitive(), d.Verdict)
	}
	// Cognitive up on equal CCN: more complex.
	head["a.go"][0].Cognitive = 20
	if d := DiffProject(base, head, 0, 0); d.Verdict != MoreComplex {
		t.Fatalf("cognitive rise must read more complex: %v", d.Verdict)
	}
	// Identical sides: unchanged, no files, empty.
	same := DiffProject(base, base, 0, 0)
	if !same.Empty() || same.Verdict != Unchanged || len(same.Files) != 0 || same.Verdict.String() != "unchanged" {
		t.Fatalf("identical sides = %+v", same)
	}
	// Nothing at all.
	none := DiffProject(nil, nil, 0, 0)
	if !none.Empty() || none.Base.Functions != 0 || none.Head.Functions != 0 {
		t.Fatalf("nil diff = %+v", none)
	}
}

func namesOf(fns []Function) []string {
	out := make([]string, 0, len(fns))
	for _, f := range fns {
		out = append(out, f.Name)
	}
	return out
}
