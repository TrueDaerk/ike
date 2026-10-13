//go:build cgo

package complexity

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ike/internal/highlight"

	_ "ike/plugins/languages/go"
	_ "ike/plugins/languages/php"
	_ "ike/plugins/languages/python"
	_ "ike/plugins/languages/shell"
	_ "ike/plugins/languages/web"
)

// golden_test.go pins the engine's numbers per language (#2915): every
// testdata/<lang>/fixture.<ext> is measured and compared with the
// fixture.golden.json next to it. `go test ./internal/complexity -update`
// rewrites the goldens from the engine; review the diff before committing.
//
// Cross-check. The golden values were verified against the reference tools on
// the dev machine (2026-10-12): gocyclo v0.6.0 and lizard 1.24.1 for CCN,
// gocognit v1.3.0 (Go) and complexipy 8.0.1 (Python) for cognitive
// complexity. Where a tool differs the engine follows the rule the epic
// states, and the difference is a property of the tool:
//
//   - Nested units are their own CCN (lizard splits Go/TS/JS closures the
//     same way); gocyclo folds a closure's branches into the enclosing
//     function, lizard folds Python lambdas and PHP closures into theirs.
//   - A nested unit raises the cognitive nesting of the enclosing function
//     (Sonar; gocognit agrees for closures). complexipy keeps a nested `def`
//     at the enclosing level — nested_closures reads 5 here, 3 there.
//   - Boolean sequences are flattened through parentheses (Sonar, gocognit:
//     `a && (b || c) && d` = 3 runs); complexipy restarts the run at a
//     parenthesis. The Python fixture carries no parenthesised chain.
//   - A direct recursive call counts by its last identifier segment, so
//     `$this->handle()` and `self.m()` count; gocognit and complexipy count a
//     plain-identifier call only.
//   - lizard counts `finally` (Python, PHP) and every `?`-shaped token
//     (`?.` optional chaining, `??` as two) as a condition, does not count
//     PHP's `and` / `or` / `??` / `?:` and knows no PHP 8 `match`; its Go
//     reader misreads a type switch and a method with named results, and its
//     TypeScript reader ends a function one line late (NLOC +1).
//   - NLOC spans the unit's whole range here; lizard subtracts the lines of
//     nested functions from the enclosing one.
//   - Naming: `Receiver.Name` / `Class.method` / `Class::method` (PHP, as
//     lizard and complexipy print it) and `(anonymous)#n`; lizard names an
//     anonymous function after the variable it is assigned to.

var update = flag.Bool("update", false, "rewrite the complexity golden files from the engine")

// golden is the on-disk shape of fixture.golden.json.
type golden struct {
	Lang      string     `json:"lang"`
	Functions []Function `json:"functions"`
}

// fixtureLang maps a testdata directory to the language id it is measured
// as: JavaScript resolves to the typescript id (one TSX grammar serves both).
var fixtureLang = map[string]string{
	"go":         "go",
	"python":     "python",
	"php":        "php",
	"typescript": "typescript",
	"javascript": "typescript",
	"shell":      "shell",
}

func TestGolden(t *testing.T) {
	dirs, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		langID, ok := fixtureLang[d.Name()]
		if !ok {
			t.Errorf("testdata/%s: no fixtureLang entry", d.Name())
			continue
		}
		seen++
		t.Run(d.Name(), func(t *testing.T) {
			dir := filepath.Join("testdata", d.Name())
			src, goldenPath := fixturePaths(t, dir)
			b, err := os.ReadFile(src)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Measure(langID, strings.Split(string(b), "\n"))
			if err != nil {
				t.Fatalf("Measure: %v", err)
			}
			if len(got) == 0 {
				t.Fatal("fixture measured no functions")
			}
			if *update {
				out, _ := json.MarshalIndent(golden{Lang: langID, Functions: got}, "", "  ")
				if err := os.WriteFile(goldenPath, append(out, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			raw, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			var want golden
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			if want.Lang != langID {
				t.Errorf("golden lang = %q, want %q", want.Lang, langID)
			}
			if len(got) != len(want.Functions) {
				t.Fatalf("measured %d functions, golden has %d:\n%s", len(got), len(want.Functions), render(got))
			}
			for i := range got {
				if !reflect.DeepEqual(got[i], want.Functions[i]) {
					t.Errorf("function %d:\n got  %+v\n want %+v", i, got[i], want.Functions[i])
				}
			}
		})
	}
	if seen != len(fixtureLang) {
		t.Errorf("%d fixture directories, want %d (one per fixtureLang entry)", seen, len(fixtureLang))
	}
}

// fixturePaths finds the one fixture.<ext> source and its golden in dir.
func fixturePaths(t *testing.T, dir string) (src, golden string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "fixture.") && !strings.HasSuffix(name, ".golden.json") {
			src = filepath.Join(dir, name)
		}
	}
	if src == "" {
		t.Fatalf("%s: no fixture.<ext> source", dir)
	}
	return src, filepath.Join(dir, "fixture.golden.json")
}

func render(fns []Function) string {
	var b strings.Builder
	for _, f := range fns {
		b.WriteString(f.Name)
		b.WriteString(" ")
		b.WriteString(f.Kind)
		b.WriteString("\n")
	}
	return b.String()
}

// TestGoldenAcceptance spells out the epic's acceptance criteria on the Go
// fixture, so a golden regeneration cannot quietly move them.
func TestGoldenAcceptance(t *testing.T) {
	b, err := os.ReadFile("testdata/go/fixture.go")
	if err != nil {
		t.Fatal(err)
	}
	fns, err := Measure("go", strings.Split(string(b), "\n"))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Function{}
	for _, f := range fns {
		byName[f.Name] = f
	}
	// A nested closure's branches do not count toward its parent's CCN but
	// raise the parent's cognitive nesting for the structures inside it:
	// nestedClosures holds two closures with an if and a for; CCN stays 1,
	// cognitive is if at level 2 (3) + for at level 1 (2) = 5.
	if f := byName["nestedClosures"]; f.CCN != 1 || f.Cognitive != 5 {
		t.Errorf("nestedClosures CCN %d cog %d, want 1 and 5", f.CCN, f.Cognitive)
	}
	if f := byName["(anonymous)#2"]; f.CCN != 2 || f.Cognitive != 3 {
		t.Errorf("outer closure CCN %d cog %d, want 2 and 3", f.CCN, f.Cognitive)
	}
	// Boolean sequences: four ifs; `a && b && c` → CCN +2, cognitive +1;
	// `a && b || c` → CCN +2, cognitive +2; `a && (b || c) && d` → +3 / +3;
	// `a || b && c` → +2 / +2.
	if f := byName["booleanSequences"]; f.CCN != 14 || f.Cognitive != 12 {
		t.Errorf("booleanSequences CCN %d cog %d, want 14 and 12", f.CCN, f.Cognitive)
	}
	if f := byName["Server.Handle"]; f.Kind != KindMethod {
		t.Errorf("Server.Handle kind %q, want method", f.Kind)
	}
	if f := byName["factorial"]; f.Cognitive != 2 {
		t.Errorf("factorial cognitive %d, want 2 (if + recursion)", f.Cognitive)
	}
	// The sites (#2946). An else-if chain: the if and the two else-ifs are
	// flat sites (Nesting 0), the else is a CCN-less site, and the if in the
	// else body nests once.
	wantSites := map[string][]Site{
		"elseIfChain": {
			{Line: 17, Col: 1, EndLine: 29, Kind: "if", CCN: 1, Cognitive: 1},
			{Line: 19, Col: 8, EndLine: 29, Kind: "else if", CCN: 1, Cognitive: 1},
			{Line: 21, Col: 8, EndLine: 29, Kind: "else if", CCN: 1, Cognitive: 1},
			{Line: 23, Col: 8, EndLine: 29, Kind: "else", Cognitive: 1},
			{Line: 26, Col: 2, EndLine: 28, Kind: "if", CCN: 1, Cognitive: 2, Nesting: 1, Level: 1},
		},
		// A switch with a default: one cognitive site for the switch, one
		// CCN-only site per arm (the default is not an arm), the ifs in the
		// arm bodies one level in.
		"switchArms": {
			{Line: 36, Col: 1, EndLine: 47, Kind: "switch", Cognitive: 1},
			{Line: 37, Col: 1, EndLine: 41, Kind: "case", CCN: 1, Level: 1},
			{Line: 38, Col: 2, EndLine: 40, Kind: "if", CCN: 1, Cognitive: 2, Nesting: 1, Level: 1},
			{Line: 41, Col: 1, EndLine: 43, Kind: "case", CCN: 1, Level: 1},
			{Line: 44, Col: 2, EndLine: 46, Kind: "if", CCN: 1, Cognitive: 2, Nesting: 1, Level: 1},
		},
		// A nested closure's structures are the closure's own sites (CCN
		// there); the enclosing function records them CCN-less at its own
		// nesting, which is what its cognitive total counts.
		"nestedClosures": {
			{Line: 127, Col: 3, EndLine: 129, Kind: "if", Cognitive: 3, Nesting: 2, Level: 2},
			{Line: 131, Col: 2, EndLine: 133, Kind: "for", Cognitive: 2, Nesting: 1, Level: 1},
		},
		"(anonymous)#2": {
			{Line: 127, Col: 3, EndLine: 129, Kind: "if", Cognitive: 2, Nesting: 1, Level: 1},
			{Line: 131, Col: 2, EndLine: 133, Kind: "for", CCN: 1, Cognitive: 1},
		},
		"factorial": {
			{Line: 140, Col: 1, EndLine: 142, Kind: "if", CCN: 1, Cognitive: 1},
			{Line: 143, Col: 12, EndLine: 143, Kind: "recursion", Cognitive: 1},
		},
	}
	for name, want := range wantSites {
		if got := byName[name].Sites; !reflect.DeepEqual(got, want) {
			t.Errorf("%s sites:\n got  %+v\n want %+v", name, got, want)
		}
	}
	// A boolean run is one site per run of like operators at its first
	// operator, its CCN the run's operator count: `a && b || c` is two
	// sites of one CCN each; `a && b && c` one site of CCN 2.
	bs := byName["booleanSequences"].Sites
	runs := []Site{}
	for _, s := range bs {
		if s.Kind != "if" {
			runs = append(runs, s)
		}
	}
	wantRuns := []Site{
		{Line: 106, Col: 6, EndLine: 106, Kind: "&&", CCN: 2, Cognitive: 1},
		{Line: 109, Col: 6, EndLine: 109, Kind: "&&", CCN: 1, Cognitive: 1},
		{Line: 109, Col: 11, EndLine: 109, Kind: "||", CCN: 1, Cognitive: 1},
		{Line: 112, Col: 6, EndLine: 112, Kind: "&&", CCN: 1, Cognitive: 1},
		{Line: 112, Col: 12, EndLine: 112, Kind: "||", CCN: 1, Cognitive: 1},
		{Line: 112, Col: 18, EndLine: 112, Kind: "&&", CCN: 1, Cognitive: 1},
		{Line: 115, Col: 6, EndLine: 115, Kind: "||", CCN: 1, Cognitive: 1},
		{Line: 115, Col: 11, EndLine: 115, Kind: "&&", CCN: 1, Cognitive: 1},
	}
	if !reflect.DeepEqual(runs, wantRuns) {
		t.Errorf("booleanSequences runs:\n got  %+v\n want %+v", runs, wantRuns)
	}
}

// TestSitesSumToTotals asserts on every unit of every fixture that the
// sites account for the totals (#2946): 1 + Σ CCN == CCN and Σ Cognitive ==
// Cognitive, with the sites in source order and every nesting penalty
// inside its increment.
func TestSitesSumToTotals(t *testing.T) {
	for dir, langID := range fixtureLang {
		src, _ := fixturePaths(t, filepath.Join("testdata", dir))
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		fns, err := Measure(langID, strings.Split(string(b), "\n"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range fns {
			ccn, cog := 1, 0
			for i, s := range f.Sites {
				ccn += s.CCN
				cog += s.Cognitive
				if s.Nesting > s.Cognitive || s.Nesting > s.Level || s.Kind == "" {
					t.Errorf("%s %s: malformed site %+v", dir, f.Name, s)
				}
				if i > 0 && (s.Line < f.Sites[i-1].Line || s.Line == f.Sites[i-1].Line && s.Col < f.Sites[i-1].Col) {
					t.Errorf("%s %s: sites out of source order at %d: %+v", dir, f.Name, i, f.Sites)
				}
				if s.Line < f.Line || s.EndLine > f.EndLine {
					t.Errorf("%s %s: site outside the unit: %+v", dir, f.Name, s)
				}
			}
			if ccn != f.CCN || cog != f.Cognitive {
				t.Errorf("%s %s: sites sum to ccn %d cog %d, unit has %d and %d:\n%+v", dir, f.Name, ccn, cog, f.CCN, f.Cognitive, f.Sites)
			}
		}
	}
}

// TestCandidateInvariants checks the extraction candidates (#2948) of every
// unit of every fixture: at most MaxCandidates, each saving at least
// MinCandidateSaving and never more than the unit's cognitive total, sorted
// by saving then range, inside the unit, with RestCog + NewCog ==
// Cognitive − CogSaving and RestCCN + NewCCN == CCN + 1 — and every number
// equal to the slow site-by-site derivation from the unit's own site list.
func TestCandidateInvariants(t *testing.T) {
	seen := 0
	for dir, langID := range fixtureLang {
		src, _ := fixturePaths(t, filepath.Join("testdata", dir))
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		fns, err := Measure(langID, strings.Split(string(b), "\n"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range fns {
			if len(f.Candidates) > MaxCandidates {
				t.Errorf("%s %s: %d candidates", dir, f.Name, len(f.Candidates))
			}
			for i, c := range f.Candidates {
				seen++
				switch {
				case c.CogSaving < MinCandidateSaving || c.CogSaving > f.Cognitive:
					t.Errorf("%s %s: saving out of range: %+v (cog %d)", dir, f.Name, c, f.Cognitive)
				case c.RestCog+c.NewCog != f.Cognitive-c.CogSaving || c.RestCCN+c.NewCCN != f.CCN+1:
					t.Errorf("%s %s: identity broken: %+v (ccn %d cog %d)", dir, f.Name, c, f.CCN, f.Cognitive)
				case c.Line < f.Line || c.EndLine > f.EndLine || c.Line > c.EndLine || c.Kind == "":
					t.Errorf("%s %s: malformed candidate %+v", dir, f.Name, c)
				case i > 0 && (c.CogSaving > f.Candidates[i-1].CogSaving || c.CogSaving == f.Candidates[i-1].CogSaving && c.Line < f.Candidates[i-1].Line):
					t.Errorf("%s %s: candidates out of order at %d: %+v", dir, f.Name, i, f.Candidates)
				}
				if want, ok := deriveCandidate(f, c); !ok {
					t.Errorf("%s %s: candidate without a site: %+v", dir, f.Name, c)
				} else if want != c {
					t.Errorf("%s %s: candidate %+v, the site list gives %+v", dir, f.Name, c, want)
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("no fixture yields a candidate")
	}
}

// deriveCandidate recomputes c from f's site list the slow way: the
// node's own site is the first site with the candidate's range and kind,
// the sites inside are every other site on the node's lines; a structural
// node drops its own nesting penalty and `Nesting` of every inner site
// (bounded by it), an else body keeps its point and the inner sites drop
// one level more.
func deriveCandidate(f Function, c Candidate) (Candidate, bool) {
	own := -1
	for i, s := range f.Sites {
		if s.Line == c.Line && s.EndLine == c.EndLine && s.Kind == c.Kind {
			own = i
			break
		}
	}
	if own < 0 {
		return Candidate{}, false
	}
	o := f.Sites[own]
	body := o.Nesting == 0 && o.Kind == "else"
	drop := o.Nesting
	if body {
		drop = o.Level + 1
	}
	got := Candidate{Line: c.Line, EndLine: c.EndLine, Kind: c.Kind, NewCCN: 1}
	if !body {
		got.CogSaving, got.NewCog, got.NewCCN = o.Nesting, 1, 1+o.CCN
	}
	for i, s := range f.Sites {
		if i == own || s.Line < c.Line || s.Line > c.EndLine {
			continue
		}
		lose := s.Nesting
		if lose > drop {
			lose = drop
		}
		got.CogSaving += lose
		got.NewCCN += s.CCN
		got.NewCog += s.Cognitive - lose
	}
	got.RestCCN = f.CCN + 1 - got.NewCCN
	got.RestCog = f.Cognitive - got.CogSaving - got.NewCog
	return got, true
}

// TestGoldenClasses pins Function.Class (#2937) on every fixture: a Go
// receiver, a Python class, a PHP class and trait and a TypeScript /
// JavaScript class qualify their methods; every free function, lambda and
// nested def carries no class.
func TestGoldenClasses(t *testing.T) {
	want := map[string]map[string]string{
		"go":         {"Server.Handle": "Server"},
		"python":     {"Server.handle": "Server", "Server.send": "Server"},
		"php":        {"Server::handle": "Server", "Server::send": "Server", "Loggable::log": "Loggable"},
		"typescript": {"Server.handle": "Server", "Server.send": "Server"},
		"javascript": {"Server.handle": "Server", "Server.send": "Server"},
	}
	for dir, classes := range want {
		src, _ := fixturePaths(t, filepath.Join("testdata", dir))
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		fns, err := Measure(fixtureLang[dir], strings.Split(string(b), "\n"))
		if err != nil {
			t.Fatal(err)
		}
		seen := 0
		for _, f := range fns {
			c, isMethod := classes[f.Name]
			switch {
			case isMethod:
				seen++
				if f.Class != c {
					t.Errorf("%s: %s class %q, want %q", dir, f.Name, f.Class, c)
				}
			case f.Class != "":
				t.Errorf("%s: %s (%s) class %q, want none", dir, f.Name, f.Kind, f.Class)
			}
		}
		if seen != len(classes) {
			t.Errorf("%s: found %d of the %d expected methods", dir, seen, len(classes))
		}
	}
}

// TestMeasureUnsupported pins ErrUnsupported for a language without a spec
// and for an unknown language id.
func TestMeasureUnsupported(t *testing.T) {
	for _, id := range []string{"json", "no-such-language"} {
		fns, err := Measure(id, []string{"{}"})
		if err != ErrUnsupported || fns != nil {
			t.Errorf("Measure(%q) = %v, %v; want nil, ErrUnsupported", id, fns, err)
		}
	}
}

// TestMeasureNilTree pins the !cgo shape: a spec'd language whose parse yields
// no tree returns ErrUnsupported and never panics.
func TestMeasureNilTree(t *testing.T) {
	saved := syntaxTree
	defer func() { syntaxTree = saved }()
	syntaxTree = func(string, []string) *highlight.SyntaxNode { return nil }
	fns, err := Measure("go", []string{"package p", "func f() {}"})
	if err != ErrUnsupported || fns != nil {
		t.Errorf("Measure with a nil tree = %v, %v; want nil, ErrUnsupported", fns, err)
	}
}
