package app

import (
	"os"
	"path/filepath"
	"testing"

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
	for _, it := range got[:2] {
		if it.Score < 0 {
			t.Fatalf("project symbol %q must keep a non-penalised score, got %d", it.Title, it.Score)
		}
	}
	for _, it := range got[2:] {
		if it.Score >= 0 {
			t.Fatalf("non-project symbol %q must carry the tier malus, got %d", it.Title, it.Score)
		}
	}
}

// Non-project symbols stay reachable — ranked below, not dropped.
func TestSymbolResultsKeepsNonProjectHits(t *testing.T) {
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
