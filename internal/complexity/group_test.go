package complexity

import (
	"reflect"
	"testing"
)

// groupFixture is two files, two classes and free functions: a.go holds
// Server's two methods, a free function and a lambda inside a method;
// b.py holds Client's method and a free function.
func groupFixture() map[string][]Function {
	return map[string][]Function{
		"a.go": {
			{Name: "Server.Handle", Class: "Server", Kind: KindMethod, Line: 10, NLOC: 20, CCN: 8, Cognitive: 12, CommentLines: 2},
			{Name: "(anonymous)#1", Kind: KindLambda, Line: 12, NLOC: 4, CCN: 2, Cognitive: 1},
			{Name: "Server.Close", Class: "Server", Kind: KindMethod, Line: 40, NLOC: 5, CCN: 1, Cognitive: 0, CommentLines: 1},
			{Name: "helper", Kind: KindFunction, Line: 2, NLOC: 6, CCN: 3, Cognitive: 2},
		},
		"b.py": {
			{Name: "Client.send", Class: "Client", Kind: KindMethod, Line: 5, NLOC: 30, CCN: 16, Cognitive: 20},
			{Name: "main", Kind: KindFunction, Line: 50, NLOC: 3, CCN: 1, Cognitive: 0},
		},
	}
}

func TestGroupFunctionsByClass(t *testing.T) {
	gs := GroupFunctions(groupFixture(), ByClass, Uniform(Thresholds{CCN: 15, Cog: 15, Cmt: 0}))
	if len(gs) != 2 {
		t.Fatalf("got %d class groups, want 2: %+v", len(gs), gs)
	}
	// CCN sum descending: Client (16) before Server (9).
	c, s := gs[0], gs[1]
	if c.File != "b.py" || c.Class != "Client" || s.File != "a.go" || s.Class != "Server" {
		t.Fatalf("order = %s/%s, %s/%s; want b.py/Client, a.go/Server", c.File, c.Class, s.File, s.Class)
	}
	// Server: its two methods only — the lambda and the free function
	// belong to no class.
	if s.Functions != 2 || s.NLOC != 25 || s.CCNSum != 9 || s.CCNMax != 8 || s.CogSum != 12 || s.CogMax != 12 ||
		s.CommentLines != 3 || s.CCNAvg != 4.5 || s.Line != 10 {
		t.Errorf("Server = %+v", s)
	}
	if c.OverThreshold != 1 || c.Line != 5 {
		t.Errorf("Client over %d line %d, want 1 and 5", c.OverThreshold, c.Line)
	}
}

func TestGroupFunctionsByFile(t *testing.T) {
	files := groupFixture()
	gs := GroupFunctions(files, ByFile, Uniform(Thresholds{CCN: 15, Cog: 15, Cmt: 0}))
	if len(gs) != 2 {
		t.Fatalf("got %d file groups, want 2", len(gs))
	}
	// Every group equals Summarize over its file, and the groups' sums add
	// up to the flat list's.
	var all []Function
	sumCCN, sumCog, sumNLOC, sumFns := 0, 0, 0, 0
	for _, g := range gs {
		if g.Class != "" {
			t.Errorf("file group %s has class %q", g.File, g.Class)
		}
		if want := Summarize(files[g.File], Thresholds{CCN: 15, Cog: 15, Cmt: 0}); !reflect.DeepEqual(g.Summary, want) {
			t.Errorf("%s summary = %+v, want %+v", g.File, g.Summary, want)
		}
		all = append(all, files[g.File]...)
		sumCCN += g.CCNSum
		sumCog += g.CogSum
		sumNLOC += g.NLOC
		sumFns += g.Functions
	}
	flat := Summarize(all, Thresholds{CCN: 15, Cog: 15, Cmt: 0})
	if sumCCN != flat.CCNSum || sumCog != flat.CogSum || sumNLOC != flat.NLOC || sumFns != flat.Functions {
		t.Errorf("group sums CCN %d cog %d NLOC %d fns %d, flat %+v", sumCCN, sumCog, sumNLOC, sumFns, flat)
	}
	if gs[0].File != "b.py" || gs[0].Line != 5 || gs[1].Line != 2 {
		t.Errorf("order/lines = %s:%d, %s:%d", gs[0].File, gs[0].Line, gs[1].File, gs[1].Line)
	}
}

func TestGroupFunctionsTiesAndNone(t *testing.T) {
	files := map[string][]Function{
		"b.go": {{Name: "B.x", Class: "B", CCN: 2}},
		"a.go": {{Name: "Z.x", Class: "Z", CCN: 2}, {Name: "A.x", Class: "A", CCN: 2}},
	}
	gs := GroupFunctions(files, ByClass, Uniform(Thresholds{CCN: 15, Cog: 15, Cmt: 0}))
	var got []string
	for _, g := range gs {
		got = append(got, g.File+"/"+g.Class)
	}
	if want := []string{"a.go/A", "a.go/Z", "b.go/B"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tie order = %v, want %v", got, want)
	}
	if GroupFunctions(files, ByNone, Uniform(Thresholds{CCN: 15, Cog: 15, Cmt: 0})) != nil {
		t.Error("ByNone should yield no groups")
	}
}

func TestGroupDepthParamsMax(t *testing.T) {
	gs := GroupFunctions(map[string][]Function{"a.go": {
		{Name: "S.a", Class: "S", Depth: 3, Params: 1, CCN: 2},
		{Name: "S.b", Class: "S", Depth: 1, Params: 4, CCN: 1},
		{Name: "f", Depth: 7, Params: 9, CCN: 1},
	}}, ByClass, Uniform(Thresholds{CCN: 15, Cog: 15, Cmt: 0}))
	if len(gs) != 1 || gs[0].DepthMax != 3 || gs[0].ParamsMax != 4 {
		t.Errorf("class group = %+v", gs)
	}
	gs = GroupFunctions(map[string][]Function{"a.go": {{Depth: 3, Params: 1}, {Depth: 7, Params: 9}}}, ByFile, Uniform(Thresholds{CCN: 15, Cog: 15, Cmt: 0}))
	if len(gs) != 1 || gs[0].DepthMax != 7 || gs[0].ParamsMax != 9 {
		t.Errorf("file group = %+v", gs)
	}
}

func TestParseLevel(t *testing.T) {
	for _, l := range []Level{ByNone, ByClass, ByFile} {
		if got, ok := ParseLevel(l.String()); !ok || got != l {
			t.Errorf("ParseLevel(%q) = %v, %v", l.String(), got, ok)
		}
	}
	if l, ok := ParseLevel("none"); !ok || l != ByNone {
		t.Error("none should parse as ByNone")
	}
	if _, ok := ParseLevel("method"); ok {
		t.Error("an unknown level parsed")
	}
}

// TestHot: commits times the excess, the CCN sum when nothing exceeds, 0
// without commits (#2957).
func TestHot(t *testing.T) {
	cases := []struct {
		commits int
		s       Summary
		want    int
	}{
		{3, Summary{Excess: 7, CCNSum: 40}, 21},
		{3, Summary{CCNSum: 40}, 120},
		{0, Summary{Excess: 7, CCNSum: 40}, 0},
	}
	for _, c := range cases {
		if got := Hot(c.commits, c.s); got != c.want {
			t.Errorf("Hot(%d, %+v) = %d, want %d", c.commits, c.s, got, c.want)
		}
	}
}
