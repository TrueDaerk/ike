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
