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

// TestPairUnitsAndBaseOf: PairUnits is DiffFunctions' (name, ordinal)
// pairing, and BaseOf reports the base side of a covered file (#2949).
func TestPairUnitsAndBaseOf(t *testing.T) {
	base := []Function{unit("a", 0, 3, 2), unit("dup", 10, 1, 0), unit("dup", 20, 4, 3)}
	head := []Function{unit("dup", 0, 1, 0), unit("a", 10, 5, 2), unit("dup", 20, 4, 6), unit("dup", 30, 2, 2)}
	if got := PairUnits(base, head); !reflect.DeepEqual(got, []int{1, 0, 2, -1}) {
		t.Fatalf("pairs = %v", got)
	}
	d := DiffProject(map[string][]Function{"m.go": base}, map[string][]Function{"m.go": head, "n.go": head}, Uniform(Thresholds{CCN: 15, Cog: 15, Cmt: 40}))
	if b, ok := d.BaseOf("m.go"); !ok || len(b) != 3 {
		t.Errorf("BaseOf(m.go) = %v, %v", b, ok)
	}
	if b, ok := d.BaseOf("n.go"); !ok || b != nil {
		t.Errorf("an added file is covered with a nil base: %v, %v", b, ok)
	}
	if _, ok := d.BaseOf("x.go"); ok {
		t.Error("a file outside the diff is not covered")
	}
	if _, ok := (Diff{}).BaseOf("m.go"); ok {
		t.Error("a diff not built by DiffProject covers nothing")
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
	d := DiffProject(base, head, Uniform(Thresholds{CCN: 15, Cog: 15, Cmt: 0}))
	if d.Base.Functions != 3 || d.Base.CCNSum != 10 || d.Head.Functions != 3 || d.Head.CCNSum != 11 {
		t.Fatalf("summaries = base %+v head %+v", d.Base, d.Head)
	}
	// No excess either side; Σcognitive 8 → 5 decides over ΣCCN +1 (#2945).
	if d.DeltaCCN() != 1 || d.DeltaCognitive() != -3 || d.Verdict != LessComplex || d.Verdict.String() != "less complex" {
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
	d := DiffProject(base, head, Uniform(Thresholds{CCN: 0, Cog: 0, Cmt: 0}))
	if d.DeltaCCN() != 0 || d.DeltaCognitive() != -7 || d.Verdict != LessComplex {
		t.Fatalf("tiebreak: Δccn %d Δcog %d verdict %v", d.DeltaCCN(), d.DeltaCognitive(), d.Verdict)
	}
	// Cognitive up on equal CCN: more complex.
	head["a.go"][0].Cognitive = 20
	if d := DiffProject(base, head, Uniform(Thresholds{CCN: 0, Cog: 0, Cmt: 0})); d.Verdict != MoreComplex {
		t.Fatalf("cognitive rise must read more complex: %v", d.Verdict)
	}
	// Identical sides: unchanged, no files, empty.
	same := DiffProject(base, base, Uniform(Thresholds{CCN: 0, Cog: 0, Cmt: 0}))
	if !same.Empty() || same.Verdict != Unchanged || len(same.Files) != 0 || same.Verdict.String() != "unchanged" {
		t.Fatalf("identical sides = %+v", same)
	}
	// Nothing at all.
	none := DiffProject(nil, nil, Uniform(Thresholds{CCN: 0, Cog: 0, Cmt: 0}))
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

func TestDiffProjectVerdictReadsExcessFirst(t *testing.T) {
	// Extracting a nested block: ΣCCN rises by one, the excess drops to 0.
	base := map[string][]Function{"a.go": {unit("f", 0, 20, 30)}}
	head := map[string][]Function{"a.go": {unit("f", 0, 12, 14), unit("fBlock", 20, 9, 6)}}
	d := DiffProject(base, head, Uniform(Thresholds{CCN: 15, Cog: 15, Cmt: 0}))
	if d.DeltaCCN() != 1 || d.DeltaExcess() != -20 || d.Verdict != LessComplex {
		t.Fatalf("extraction: Δccn %d Δexcess %d verdict %v", d.DeltaCCN(), d.DeltaExcess(), d.Verdict)
	}
	// A trivial helper added to an otherwise unchanged file: unchanged.
	head = map[string][]Function{"a.go": {unit("f", 0, 20, 30), unit("helper", 40, 1, 0)}}
	if d := DiffProject(base, head, Uniform(Thresholds{CCN: 15, Cog: 15, Cmt: 0})); d.Verdict != Unchanged || d.Empty() {
		t.Fatalf("trivial helper: verdict %v Δccn %d", d.Verdict, d.DeltaCCN())
	}
	// Equal excess and cognitive, one more branch: more complex.
	head = map[string][]Function{"a.go": {unit("f", 0, 21, 30)}}
	if d := DiffProject(base, head, Uniform(Thresholds{CCN: 0, Cog: 0, Cmt: 0})); d.DeltaDecisions() != 1 || d.Verdict != MoreComplex {
		t.Fatalf("decision tiebreak: %d %v", d.DeltaDecisions(), d.Verdict)
	}
	// One function crossing the threshold: 14 → 16 is excess 0 → 1.
	base = map[string][]Function{"a.go": {unit("f", 0, 14, 3), unit("g", 10, 2, 1)}}
	head = map[string][]Function{"a.go": {unit("f", 0, 16, 3), unit("g", 10, 2, 1)}}
	if d := DiffProject(base, head, Uniform(Thresholds{CCN: 15, Cog: 15, Cmt: 0})); d.DeltaExcess() != 1 || d.Verdict != MoreComplex {
		t.Fatalf("threshold crossing: Δexcess %d verdict %v", d.DeltaExcess(), d.Verdict)
	}
}

// TestDiffByFile: ByFile sums each side of every classified file — a
// removed unit on the base side only, an added one on the head side only —
// reports the counts, leaves a file without a classified unit out, and
// sorts by ΔCCN descending (#2955).
func TestDiffByFile(t *testing.T) {
	base := map[string][]Function{
		"a.go":    {unit("keep", 0, 2, 1), unit("grow", 10, 3, 2), unit("gone", 20, 4, 4)},
		"b.go":    {unit("shrink", 0, 9, 8)},
		"same.go": {unit("same", 0, 5, 5)},
	}
	head := map[string][]Function{
		"a.go":    {unit("keep", 0, 2, 1), unit("grow", 10, 7, 6), unit("new", 30, 2, 3)},
		"b.go":    {unit("shrink", 0, 4, 3)},
		"same.go": {unit("same", 3, 5, 5)},
	}
	fs := DiffProject(base, head, Uniform(Thresholds{CCN: 15, Cog: 15, Cmt: 40})).ByFile()
	if len(fs) != 2 {
		t.Fatalf("ByFile = %+v, want a.go and b.go", fs)
	}
	a, b := fs[0], fs[1]
	if a.File != "a.go" || a.Changed != 1 || a.Added != 1 || a.Removed != 1 {
		t.Errorf("a.go counts = %+v", a)
	}
	// Base: keep 2 + grow 3 + gone 4; head: keep 2 + grow 7 + new 2.
	if a.Before.CCNSum != 9 || a.After.CCNSum != 11 || a.DeltaCCN() != 2 {
		t.Errorf("a.go ccn %d→%d", a.Before.CCNSum, a.After.CCNSum)
	}
	if a.Before.CogSum != 7 || a.After.CogSum != 10 || a.DeltaCognitive() != 3 {
		t.Errorf("a.go cog %d→%d", a.Before.CogSum, a.After.CogSum)
	}
	if a.Before.Functions != 3 || a.After.Functions != 3 || a.DeltaNLOC() != 0 {
		t.Errorf("a.go functions %d→%d nloc Δ%d", a.Before.Functions, a.After.Functions, a.DeltaNLOC())
	}
	if b.File != "b.go" || b.Changed != 1 || b.DeltaCCN() != -5 || b.DeltaCognitive() != -5 {
		t.Errorf("b.go = %+v", b)
	}
}

// TestDiffByFileWithoutMeasurements: a diff not built by DiffProject sums
// the classified units alone.
func TestDiffByFileWithoutMeasurements(t *testing.T) {
	d := Diff{Files: map[string]FileDiff{"x.go": {
		Changed: []Change{{Before: unit("f", 0, 1, 0), After: unit("f", 0, 4, 2)}},
		Added:   []Function{unit("g", 10, 2, 0)},
		Removed: []Function{unit("h", 20, 3, 1)},
	}}}
	fs := d.ByFile()
	if len(fs) != 1 || fs[0].Before.CCNSum != 4 || fs[0].After.CCNSum != 6 || fs[0].Before.Functions != 2 {
		t.Fatalf("ByFile = %+v", fs)
	}
}
