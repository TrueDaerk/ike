package fuzzy

import (
	"reflect"
	"strings"
	"testing"
)

func TestClassifyPerClass(t *testing.T) {
	file := ClassOptions{Case: CaseFirstLetter, Extension: true}
	loose := ClassOptions{Case: CaseFirstLetter, Extension: true, Permissive: true}
	plain := ClassOptions{Case: CaseFirstLetter}
	cases := []struct {
		name          string
		pattern, text string
		opts          ClassOptions
		want          Class
		positions     []int
	}{
		// exact
		{"exact whole name", "file_mode.go", "file_mode.go", file, ClassExact, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}},
		{"exact without extension", "file_mode", "file_mode.go", file, ClassExact, []int{0, 1, 2, 3, 4, 5, 6, 7, 8}},
		{"exact ignoring separators", "filemode", "file_mode.go", file, ClassExact, []int{0, 1, 2, 3, 5, 6, 7, 8}},
		{"exact ignoring the dash", "filemode", "file-mode", plain, ClassExact, []int{0, 1, 2, 3, 5, 6, 7, 8}},
		{"exact ignoring the space", "filemode", "file mode", plain, ClassExact, []int{0, 1, 2, 3, 5, 6, 7, 8}},
		{"exact camelCase", "mycoolclass", "MyCoolClass", plain, ClassExact, nil},
		{"exact needs the extension option", "file_mode", "file_mode.go", plain, ClassPrefix, nil},
		{"exact keeps a dotfile whole", "gitignore", ".gitignore", file, ClassExact, nil},

		// prefix
		{"prefix", "file_m", "file_mode.go", file, ClassPrefix, []int{0, 1, 2, 3, 4, 5}},
		{"prefix camelCase", "myco", "MyCoolClass", plain, ClassPrefix, []int{0, 1, 2, 3}},
		{"prefix with digits", "html2", "html2canvas.js", file, ClassPrefix, []int{0, 1, 2, 3, 4}},
		{"prefix is the run, not the scorer's split", "tool", "toolhost_layout_test.go", file, ClassPrefix, []int{0, 1, 2, 3}},

		// contains
		{"contains at a word start", "mode", "file_mode.go", file, ClassContains, []int{5, 6, 7, 8}},
		{"contains at a camel hump", "cool", "MyCoolClass", plain, ClassContains, []int{2, 3, 4, 5}},
		{"contains at a digit boundary", "2canvas", "html2canvas.js", file, ClassContains, []int{4, 5, 6, 7, 8, 9, 10}},
		{"contains across a separator", "e_mo", "the_file_mode.go", loose, ClassContains, []int{7, 8, 9, 10}},
		{"contains prefers the word start", "mode", "commode_mode.go", file, ClassContains, []int{8, 9, 10, 11}},
		{"contains mid-word needs the permissive filter", "ilter", "savedfilter.go", loose, ClassContains, []int{6, 7, 8, 9, 10}},
		{"contains beats a split alignment", "mode", "my_ode_mode.go", file, ClassContains, []int{7, 8, 9, 10}},
		{"contains mid-word next to a hump match", "ab", "xab_a_b", plain, ClassContains, []int{1, 2}},

		// initials
		{"initials camelCase", "mcc", "MyCoolClass", plain, ClassInitials, []int{0, 2, 6}},
		{"initials with runs", "fimo", "file_mode.go", file, ClassInitials, []int{0, 1, 5, 6}},
		{"initials across the extension", "fmg", "file_mode.go", file, ClassInitials, []int{0, 5, 10}},
		{"initials at a digit boundary", "h2c", "html2canvas.js", file, ClassInitials, []int{0, 4, 5}},
		{"initials after an acronym", "gur", "GotoURLResolver", plain, ClassInitials, []int{0, 4, 7}},

		// fuzzy
		{"fuzzy scattered", "flmd", "file_mode.go", loose, ClassFuzzy, []int{0, 2, 5, 7}},
		{"fuzzy letters in order", "smry", "summary.go", loose, ClassFuzzy, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, r, ok := Classify(tc.pattern, tc.text, tc.opts)
			if !ok {
				t.Fatalf("Classify(%q, %q) rejected, want %s", tc.pattern, tc.text, tc.want)
			}
			if got != tc.want {
				t.Fatalf("Classify(%q, %q) = %s, want %s", tc.pattern, tc.text, got, tc.want)
			}
			if tc.positions != nil && !reflect.DeepEqual(r.Positions, tc.positions) {
				t.Errorf("positions = %v, want %v", r.Positions, tc.positions)
			}
		})
	}
}

// TestClassifyRejects: without Permissive the classifier accepts exactly what
// the hump filter accepts, and with it nothing that is not a subsequence.
func TestClassifyRejects(t *testing.T) {
	strict := ClassOptions{Case: CaseFirstLetter, Extension: true}
	loose := ClassOptions{Case: CaseFirstLetter, Extension: true, Permissive: true}
	cases := []struct {
		pattern, text string
		opts          ClassOptions
	}{
		{"flmd", "file_mode.go", strict},
		{"ilter", "savedfilter.go", strict},
		{"my", "summary.go", strict},
		{"log", "dialogBox.go", strict},
		{"xyz", "file_mode.go", loose},
		{"file_mode.go.bak", "file_mode.go", loose},
		{"file-mode", "file_mode.go", loose},
	}
	for _, tc := range cases {
		if class, _, ok := Classify(tc.pattern, tc.text, tc.opts); ok {
			t.Errorf("Classify(%q, %q) = %s, want it rejected", tc.pattern, tc.text, class)
		}
	}
	names := []string{"file_mode.go", "MyCoolClass", "html2canvas.js", "savedfilter.go", "GotoURLResolver", "x-z-q.go"}
	patterns := []string{"f", "fm", "mode", "mcc", "MCC", "h2c", "filter", "ilter", "gur", "xzq", "go", "e", "Class"}
	for _, c := range []Case{CaseFirstLetter, CaseNone, CaseAll} {
		for _, n := range names {
			for _, p := range patterns {
				_, humps := MatchHumpsCase(p, n, c)
				if _, _, ok := Classify(p, n, ClassOptions{Case: c}); ok != humps {
					t.Errorf("case %d: Classify(%q, %q) accepted = %v, hump filter = %v", c, p, n, ok, humps)
				}
			}
		}
	}
}

func TestClassifyPrecedence(t *testing.T) {
	file := ClassOptions{Case: CaseFirstLetter, Extension: true}
	cases := []struct {
		pattern, text string
		want          Class
	}{
		// A name that equals the query is exact, not prefix.
		{"mode", "mode.go", ClassExact},
		{"mode", "mode", ClassExact},
		// A name that starts with the query is prefix, not contains, even
		// when the query occurs again later.
		{"mode", "mode_mode.go", ClassPrefix},
		// A contiguous run is contains, not initials, although the runs of a
		// split alignment would start at word starts too.
		{"ab", "x_ab_a_b.go", ClassContains},
		// One rune at a word start is a run, not an initial.
		{"m", "file_mode.go", ClassContains},
	}
	for _, tc := range cases {
		if got, _, ok := Classify(tc.pattern, tc.text, file); !ok || got != tc.want {
			t.Errorf("Classify(%q, %q) = %s (ok %v), want %s", tc.pattern, tc.text, got, ok, tc.want)
		}
	}
	loose := ClassOptions{Case: CaseFirstLetter, Permissive: true}
	if got, _, _ := Classify("ile", "file_mile", loose); got != ClassContains {
		t.Errorf("a mid-word run is contains before it is fuzzy, got %s", got)
	}
}

func TestClassifyCaseRules(t *testing.T) {
	cases := []struct {
		pattern, text string
		rule          Case
		want          Class
		ok            bool
	}{
		// first_letter: a lowercase rune matches either case, an uppercase one
		// only an uppercase rune.
		{"cool", "MyCoolClass", CaseFirstLetter, ClassContains, true},
		{"Cool", "MyCoolClass", CaseFirstLetter, ClassContains, true},
		{"COOL", "MyCoolClass", CaseFirstLetter, 0, false},
		{"my", "MyCoolClass", CaseFirstLetter, ClassPrefix, true},
		{"My", "my_cool_class", CaseFirstLetter, 0, false},
		{"mcc", "MyCoolClass", CaseFirstLetter, ClassInitials, true},
		{"MCC", "my_cool_class", CaseFirstLetter, 0, false},
		// none: every rune folds.
		{"COOL", "MyCoolClass", CaseNone, ClassContains, true},
		{"MY", "my_cool_class", CaseNone, ClassPrefix, true},
		{"MCC", "my_cool_class", CaseNone, ClassInitials, true},
		// all: every rune is compared exactly.
		{"cool", "MyCoolClass", CaseAll, 0, false},
		{"Cool", "MyCoolClass", CaseAll, ClassContains, true},
		{"my", "MyCoolClass", CaseAll, 0, false},
		{"MCC", "MyCoolClass", CaseAll, ClassInitials, true},
		{"mcc", "MyCoolClass", CaseAll, 0, false},
		// exact is case-insensitive under every rule once the match is accepted.
		{"mycoolclass", "MyCoolClass", CaseFirstLetter, ClassExact, true},
		{"MYCOOLCLASS", "my_cool_class", CaseNone, ClassExact, true},
		{"MyCoolClass", "MyCoolClass", CaseAll, ClassExact, true},
	}
	for _, tc := range cases {
		got, _, ok := Classify(tc.pattern, tc.text, ClassOptions{Case: tc.rule})
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("case %d: Classify(%q, %q) = %s (ok %v), want %s (ok %v)", tc.rule, tc.pattern, tc.text, got, ok, tc.want, tc.ok)
		}
	}
	// A permissive search keeps a case mismatch: it is fuzzy, or exact when
	// the name is spelled whole.
	loose := ClassOptions{Case: CaseAll, Permissive: true}
	if got, _, ok := Classify("cool", "MyCoolClass", loose); !ok || got != ClassFuzzy {
		t.Errorf("permissive case mismatch = %s (ok %v), want fuzzy", got, ok)
	}
	if got, _, ok := Classify("mycoolclass", "MyCoolClass", loose); !ok || got != ClassExact {
		t.Errorf("permissive whole name = %s (ok %v), want exact", got, ok)
	}
}

// TestClassifyRunScores: a prefix scores the same whatever follows it, and a
// run at a word start out-scores the same run mid-word.
func TestClassifyRunScores(t *testing.T) {
	file := ClassOptions{Case: CaseFirstLetter, Extension: true}
	_, a, _ := Classify("tool", "tools.go", file)
	_, b, _ := Classify("tool", "toolhost_layout_test.go", file)
	if a.Score != b.Score || !a.Prefix || !b.Prefix {
		t.Errorf("prefix scores %d and %d (prefix %v, %v), want them equal", a.Score, b.Score, a.Prefix, b.Prefix)
	}
	loose := ClassOptions{Case: CaseFirstLetter, Extension: true, Permissive: true}
	_, word, _ := Classify("filter", "saved_filter.go", loose)
	_, mid, _ := Classify("filter", "savedfilter.go", loose)
	if word.Score <= mid.Score {
		t.Errorf("word-start run scores %d, mid-word run %d, want the word start higher", word.Score, mid.Score)
	}
	if hr, _ := MatchHumps("mode", "file_mode.go"); hr.Score != runScore([]rune("file_mode.go"), 5, 4) {
		t.Errorf("runScore disagrees with the matcher: %d", hr.Score)
	}
}

func TestClassifyEmptyPattern(t *testing.T) {
	got, r, ok := Classify("", "anything", ClassOptions{})
	if !ok || got != ClassPrefix || !r.Prefix || len(r.Positions) != 0 {
		t.Errorf("empty pattern = %s %+v (ok %v), want an empty prefix match", got, r, ok)
	}
}

func TestClassNames(t *testing.T) {
	want := []string{"exact", "prefix", "contains", "initials", "directory", "fuzzy"}
	if got := ClassNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ClassNames = %v, want %v", got, want)
	}
	for i, n := range want {
		c, ok := ParseClass(n)
		if !ok || int(c) != i || c.String() != n {
			t.Errorf("ParseClass(%q) = %d, %v", n, c, ok)
		}
	}
	if _, ok := ParseClass("scattered"); ok {
		t.Error("ParseClass accepted an unknown name")
	}
}

func TestParseClassOrder(t *testing.T) {
	def := DefaultClassOrder()
	parsed, err := ParseClassOrder(ClassNames())
	if err != nil || parsed != def {
		t.Fatalf("the default list must parse to the default order, got %v, %v", parsed, err)
	}
	for c := ClassExact; c < ClassFuzzy; c++ {
		if def.Rank(c) >= def.Rank(c+1) {
			t.Errorf("default order ranks %s at %d, %s at %d", c, def.Rank(c), c+1, def.Rank(c+1))
		}
	}
	o, err := ParseClassOrder([]string{"directory", "exact", "initials", "prefix", "contains", "fuzzy"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Rank(ClassDirectory) != 0 || o.Rank(ClassInitials) != 2 || o.Rank(ClassContains) != 4 || o.Rank(ClassFuzzy) != 5 {
		t.Errorf("permuted ranks = %+v", o)
	}

	bad := []struct {
		names []string
		msg   string
	}{
		{[]string{"exact", "prefix", "contains", "initials", "directory", "scattered"}, `unknown match class "scattered" (valid: exact, prefix, contains, initials, directory, fuzzy)`},
		{[]string{"exact", "exact", "contains", "initials", "directory", "fuzzy"}, `match class "exact" is listed twice`},
		{[]string{"exact", "prefix", "contains", "initials"}, "missing match class directory, fuzzy"},
		{nil, "missing match class exact, prefix, contains, initials, directory, fuzzy"},
	}
	for _, tc := range bad {
		_, err := ParseClassOrder(tc.names)
		if err == nil || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("ParseClassOrder(%v) = %v, want an error saying %q", tc.names, err, tc.msg)
		}
	}
}
