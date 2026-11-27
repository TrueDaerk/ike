package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"ike/internal/config"
	"ike/internal/fuzzy"
	ilsp "ike/internal/lsp"
	"ike/internal/palette"
)

func symbolHit(name, path string) ilsp.SymbolHit {
	return ilsp.SymbolHit{Name: name, Ref: ilsp.Reference{Path: path, Line: 16}}
}

// #377: an exact-name match inside the project must be the top hit, above
// stdlib/dependency symbols the server returned first and scored higher on
// pure fuzzy terms.
func TestSymbolResultsProjectExactMatchFirst(t *testing.T) {
	t.Parallel()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	s := &symbolMode{}
	q := "add"
	s.lastSent = q
	s.SetHits(q, []ilsp.SymbolHit{
		symbolHit("SendDir", "/usr/lib/go/src/internal/abi/addtype.go"),
		symbolHit("Add", "/usr/lib/go/src/math/bits/bits.go"),
		symbolHit("add", filepath.Join(cwd, "main.go")),
		symbolHit("addAll", filepath.Join(cwd, "internal", "sum.go")),
	})
	got := s.Results(q, palette.Context{})
	if len(got) != 4 {
		t.Fatalf("want 4 results, got %d", len(got))
	}
	if got[0].Title != "add" {
		t.Fatalf("want exact project match first, got %q", got[0].Title)
	}
	if got[1].Title != "addAll" {
		t.Fatalf("want project symbols above non-project, got %q second", got[1].Title)
	}
	for _, it := range got[2:] {
		if it.Score >= got[1].Score {
			t.Fatalf("non-project symbol %q must score below every project symbol, got %d >= %d", it.Title, it.Score, got[1].Score)
		}
		if it.Score > -symbolNonProjectMalus/2 {
			t.Fatalf("non-project symbol %q must carry the tier malus, got %d", it.Title, it.Score)
		}
	}
}

// Non-project symbols stay reachable — ranked below, not dropped.
func TestSymbolResultsKeepsNonProjectHits(t *testing.T) {
	t.Parallel()
	s := &symbolMode{}
	q := "senddir"
	s.lastSent = q
	s.SetHits(q, []ilsp.SymbolHit{
		symbolHit("SendDir", "/usr/lib/go/src/internal/abi/type.go"),
	})
	got := s.Results(q, palette.Context{})
	if len(got) != 1 || got[0].Title != "SendDir" {
		t.Fatalf("non-project hit must survive ranking, got %v", got)
	}
}

func TestInsideProject(t *testing.T) {
	t.Parallel()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if !insideProject(filepath.Join(cwd, "main.go")) {
		t.Fatal("file under cwd must count as project")
	}
	if !insideProject("main.go") {
		t.Fatal("relative path must count as project")
	}
	if insideProject(filepath.Join(cwd, "..", "elsewhere", "x.go")) {
		t.Fatal("sibling of cwd must not count as project")
	}
	if insideProject("/usr/lib/go/src/runtime/proc.go") {
		t.Fatal("stdlib path must not count as project")
	}
}

// TestSetHitsDeduplicatesIdenticalRows guards #1121: a query matching one
// line several times yields several hits on the same (name, path, line) —
// only one row survives; distinct lines and names never collapse.
func TestSetHitsDeduplicatesIdenticalRows(t *testing.T) {
	t.Parallel()
	s := &symbolMode{}
	q := "dup"
	s.lastSent = q
	same := symbolHit("dup", "/proj/a.go")
	otherLine := symbolHit("dup", "/proj/a.go")
	otherLine.Ref.Line = 7
	otherName := symbolHit("dupOther", "/proj/a.go")
	s.SetHits(q, []ilsp.SymbolHit{same, same, otherLine, same, otherName})
	if len(s.items) != 3 {
		t.Fatalf("items = %d, want 3 (dup@0, dup@7, dupOther@0)", len(s.items))
	}
	if s.items[0].item.Title != "dup" || s.items[1].item.Title != "dup" || s.items[2].item.Title != "dupOther" {
		t.Fatalf("order/titles wrong: %q %q %q", s.items[0].item.Title, s.items[1].item.Title, s.items[2].item.Title)
	}
}

// setSymbolMatchOrder installs search.match_order for the duration of the test.
func setSymbolMatchOrder(t *testing.T, order ...string) {
	t.Helper()
	prev := config.Get()
	c := *prev
	c.Search.MatchOrder = order
	config.Set(&c)
	t.Cleanup(func() { config.Set(prev) })
}

// classSymbols caches one project symbol per match class of the query "mcc"
// plus a non-project exact match.
func classSymbols(t *testing.T) (*symbolMode, string) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(cwd, "sym.go")
	s := &symbolMode{}
	q := "mcc"
	s.lastSent = q
	s.SetHits(q, []ilsp.SymbolHit{
		symbolHit("Mxcxc", proj),       // fuzzy
		symbolHit("MyCoolClass", proj), // initials
		symbolHit("ReadMcc", proj),     // contains
		symbolHit("MccReader", proj),   // prefix
		symbolHit("Mcc", "/usr/lib/go/src/x/mcc.go"),
		symbolHit("mcc", proj), // exact
	})
	return s, q
}

// TestSymbolResultsRankByMatchClass: with the default order a project symbol
// lists exact, prefix, contains, initials, fuzzy, and a non-project symbol of
// any class lists below every project one.
func TestSymbolResultsRankByMatchClass(t *testing.T) {
	// serial: config.Set (process-wide configuration)
	setSymbolMatchOrder(t, fuzzy.ClassNames()...)
	s, q := classSymbols(t)
	got := titles(s.Results(q, palette.Context{}))
	want := []string{"mcc", "MccReader", "ReadMcc", "MyCoolClass", "Mxcxc", "Mcc"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestSymbolResultsFollowConfiguredMatchOrder: a permuted search.match_order
// reorders the list on the next query, without a restart; the position of
// "directory" is ignored.
func TestSymbolResultsFollowConfiguredMatchOrder(t *testing.T) {
	// serial: config.Set (process-wide configuration)
	s, q := classSymbols(t)
	setSymbolMatchOrder(t, "fuzzy", "directory", "initials", "contains", "prefix", "exact")
	got := titles(s.Results(q, palette.Context{}))
	want := []string{"Mxcxc", "MyCoolClass", "ReadMcc", "MccReader", "mcc", "Mcc"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	setSymbolMatchOrder(t, "initials", "exact", "prefix", "contains", "fuzzy", "directory")
	got = titles(s.Results(q, palette.Context{}))
	want = []string{"MyCoolClass", "mcc", "MccReader", "ReadMcc", "Mxcxc", "Mcc"}
	if !slices.Equal(got, want) {
		t.Fatalf("order after change = %v, want %v", got, want)
	}
}

// TestSymbolResultsDetailMatchIsFuzzy: a row matched only through its
// location detail ranks as a fuzzy match, not as a directory match.
func TestSymbolResultsDetailMatchIsFuzzy(t *testing.T) {
	// serial: config.Set (process-wide configuration)
	setSymbolMatchOrder(t, "directory", "exact", "prefix", "contains", "initials", "fuzzy")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	s := &symbolMode{}
	q := "zork"
	s.lastSent = q
	s.SetHits(q, []ilsp.SymbolHit{
		symbolHit("Run", filepath.Join(cwd, "zork", "run.go")),
		symbolHit("ZxOxRxK", filepath.Join(cwd, "a.go")),
		symbolHit("ZorkRun", filepath.Join(cwd, "b.go")),
	})
	got := titles(s.Results(q, palette.Context{}))
	if got[0] != "ZorkRun" || got[len(got)-1] == "ZorkRun" {
		t.Fatalf("order = %v, want the prefix name match first", got)
	}
	idx := slices.Index(got, "Run")
	if idx < slices.Index(got, "ZorkRun") {
		t.Fatalf("detail-only match outranks a name match: %v", got)
	}
}

// fixedMode is a palette source returning one row with a fixed score.
type fixedMode struct {
	prefix rune
	title  string
	score  int
}

func (f fixedMode) Prefix() rune        { return f.prefix }
func (f fixedMode) Placeholder() string { return "" }
func (f fixedMode) Results(string, palette.Context) []palette.Item {
	return []palette.Item{{Title: f.title, Score: f.score}}
}

// TestSearchAllSinksNonProjectSymbols: in search everywhere an exact-name
// non-project symbol still lists below a weakly matched command row.
func TestSearchAllSinksNonProjectSymbols(t *testing.T) {
	t.Parallel()
	q := "mcc"
	s := &symbolMode{}
	s.lastSent = q
	s.SetHits(q, []ilsp.SymbolHit{symbolHit("mcc", "/usr/lib/go/src/x/mcc.go")})
	all := palette.NewSearchAllMode(fixedMode{prefix: ':', title: "cmd", score: 1}, newNonClassSymbolMode(s))
	got := all.Results(q, palette.Context{})
	if len(got) != 2 || got[len(got)-1].Title != string(symbolsPrefix)+" mcc" {
		t.Fatalf("order = %v, want the non-project symbol last", titles(got))
	}
}
