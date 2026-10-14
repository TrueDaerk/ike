package complexity

import (
	"testing"

	"ike/internal/config"
)

// overridesConfig is a [complexity] section with globals 15 / 15 / 40 and
// the given overrides.
func overridesConfig(overrides ...string) config.Complexity {
	return config.Complexity{CCNWarn: 15, CognitiveWarn: 15, CommentDensityWarn: 40, Overrides: overrides}
}

// TestThresholdsForFirstMatch (#2960): the first matching entry wins, its
// missing keys come from the globals, a lang: selector matches the path's
// language and no match returns the globals.
func TestThresholdsForFirstMatch(t *testing.T) {
	c := overridesConfig("legacy/** ccn=30 cog=30", "legacy/old.go ccn=99", "lang:php cog=25 cmt=0", "*_test.go ccn=40 cog=40")
	root := "/p"
	cases := []struct {
		path string
		want Thresholds
	}{
		{"/p/legacy/old.go", Thresholds{CCN: 30, Cog: 30, Cmt: 40, Source: "legacy/**"}},
		{"/p/legacy/a/b.php", Thresholds{CCN: 30, Cog: 30, Cmt: 40, Source: "legacy/**"}},
		{"/p/web/index.php", Thresholds{CCN: 15, Cog: 25, Cmt: 0, Source: "lang:php"}},
		{"/p/pkg/x_test.go", Thresholds{CCN: 40, Cog: 40, Cmt: 40, Source: "*_test.go"}},
		{"/p/pkg/x.go", Thresholds{CCN: 15, Cog: 15, Cmt: 40}},
	}
	res := ResolverFor(c, root)
	for _, tc := range cases {
		if got := ThresholdsFor(c, root, tc.path); got != tc.want {
			t.Errorf("ThresholdsFor(%s) = %+v, want %+v", tc.path, got, tc.want)
		}
		if got := res(tc.path); got != tc.want {
			t.Errorf("resolver(%s) = %+v, want %+v", tc.path, got, tc.want)
		}
	}
	if got := ThresholdsFor(overridesConfig(), root, "/p/legacy/old.go"); got != (Thresholds{CCN: 15, Cog: 15, Cmt: 40}) || got.Overridden() {
		t.Errorf("no overrides = %+v, want the globals", got)
	}
}

func TestThresholdsLabel(t *testing.T) {
	g := Thresholds{CCN: 15, Cog: 15, Cmt: 40}
	if got := g.Label(g); got != "" {
		t.Errorf("globals label = %q, want empty", got)
	}
	if got := (Thresholds{CCN: 30, Cog: 30, Cmt: 40, Source: "legacy/**"}).Label(g); got != "ccn 30 cog 30 (legacy/**)" {
		t.Errorf("label = %q", got)
	}
	if got := (Thresholds{CCN: 15, Cog: 25, Cmt: 0, Source: "lang:php"}).Label(g); got != "ccn 15 cog 25 cmt 0% (lang:php)" {
		t.Errorf("label = %q", got)
	}
}

// TestSummarizeFilesPerFileThresholds: each file's functions count against
// the thresholds resolved for it; a constant resolver is Summarize.
func TestSummarizeFilesPerFileThresholds(t *testing.T) {
	files := map[string][]Function{
		"/p/legacy/a.go": {{CCN: 20, Cognitive: 20}},
		"/p/src/b.go":    {{CCN: 20, Cognitive: 20}},
	}
	res := ResolverFor(overridesConfig("legacy/** ccn=30 cog=30"), "/p")
	s := SummarizeFiles(files, res)
	if s.OverThreshold != 1 || s.Excess != 10 || s.Functions != 2 {
		t.Fatalf("per-file summary = %+v, want 1 over, excess 10", s)
	}
	g := Thresholds{CCN: 15, Cog: 15, Cmt: 40}
	if s := SummarizeFiles(files, Uniform(g)); s.OverThreshold != 2 || s.Excess != 20 {
		t.Fatalf("global summary = %+v, want 2 over, excess 20", s)
	}
	var all []Function
	for _, fns := range files {
		all = append(all, fns...)
	}
	if a, b := SummarizeFiles(files, Uniform(g)), Summarize(all, g); a != b {
		t.Fatalf("SummarizeFiles(Uniform) = %+v, Summarize = %+v", a, b)
	}
	gs := GroupFunctions(files, ByFile, res)
	for _, gr := range gs {
		want := 1
		if gr.File == "/p/legacy/a.go" {
			want = 0
		}
		if gr.OverThreshold != want {
			t.Errorf("group %s over = %d, want %d", gr.File, gr.OverThreshold, want)
		}
	}
}

// TestDiffProjectPerFileThresholds: the diff's summaries and per-file folds
// judge each file by its own thresholds.
func TestDiffProjectPerFileThresholds(t *testing.T) {
	base := map[string][]Function{"/p/legacy/a.go": {{Name: "f", CCN: 10}}, "/p/src/b.go": {{Name: "g", CCN: 10}}}
	head := map[string][]Function{"/p/legacy/a.go": {{Name: "f", CCN: 20}}, "/p/src/b.go": {{Name: "g", CCN: 20}}}
	res := ResolverFor(overridesConfig("legacy/** ccn=30"), "/p")
	d := DiffProject(base, head, res)
	if d.Head.OverThreshold != 1 || d.Head.Excess != 5 || d.DeltaExcess() != 5 {
		t.Fatalf("head = %+v, want only src/b.go over (excess 5)", d.Head)
	}
	for _, fc := range d.ByFile() {
		want := 1
		if fc.File == "/p/legacy/a.go" {
			want = 0
		}
		if fc.After.OverThreshold != want {
			t.Errorf("%s after over = %d, want %d", fc.File, fc.After.OverThreshold, want)
		}
	}
	if w := d.Without(func(p string) bool { return p == "/p/src/b.go" }); w.Head.OverThreshold != 0 {
		t.Fatalf("Without keeps the resolver: head = %+v", w.Head)
	}
	if g := DiffProject(base, head, Uniform(Thresholds{CCN: 15, Cog: 15})); g.Head.OverThreshold != 2 {
		t.Fatalf("global diff head = %+v, want 2 over", g.Head)
	}
}
