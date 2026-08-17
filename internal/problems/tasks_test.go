package problems

import (
	"testing"

	ilsp "ike/internal/lsp"
)

// Task-source findings (#1915): per-source wholesale replacement, cleared on
// re-run, merged after server and lint findings.
func TestStoreTaskSourceSetReplaceAndClear(t *testing.T) {
	s := NewStore()
	s.SetTaskSource("make: build", map[string][]ilsp.Diagnostic{
		"/a.go": {diag(4, 1, 1, "boom", "")},
		"/b.go": {diag(0, 0, 2, "meh", "")},
	})
	if s.Len() != 2 || len(s.Get("/a.go")) != 1 {
		t.Fatalf("Len = %d, Get(/a.go) = %v", s.Len(), s.Get("/a.go"))
	}
	// A re-run replaces the source wholesale: /b.go's finding vanishes.
	s.SetTaskSource("make: build", map[string][]ilsp.Diagnostic{
		"/a.go": {diag(9, 0, 1, "still", "")},
	})
	if s.Get("/b.go") != nil || len(s.Get("/a.go")) != 1 || s.Get("/a.go")[0].Message != "still" {
		t.Fatalf("replace failed: a=%v b=%v", s.Get("/a.go"), s.Get("/b.go"))
	}
	s.ClearTaskSource("make: build")
	if s.Len() != 0 {
		t.Fatalf("cleared source must vanish, Len = %d", s.Len())
	}
}

func TestStoreTaskSourcesMergeWithServerFindings(t *testing.T) {
	s := NewStore()
	s.Set("/a.go", []ilsp.Diagnostic{diag(0, 0, 1, "server", "")})
	s.SetTaskSource("make: build", map[string][]ilsp.Diagnostic{"/a.go": {diag(4, 1, 1, "task", "")}})
	got := s.Get("/a.go")
	if len(got) != 2 || got[0].Message != "server" || got[1].Message != "task" {
		t.Fatalf("Get = %v", got)
	}
	if s.Len() != 1 {
		t.Fatalf("one path, Len = %d", s.Len())
	}
	// An empty replacement clears the source like Set does a path.
	s.SetTaskSource("make: build", nil)
	if len(s.Get("/a.go")) != 1 {
		t.Fatalf("task findings must clear, Get = %v", s.Get("/a.go"))
	}
}

func TestStoreDropRemovesTaskFindings(t *testing.T) {
	s := NewStore()
	s.SetTaskSource("npm: test", map[string][]ilsp.Diagnostic{
		"/proj/src/a.ts": {diag(1, 1, 1, "x", "")},
		"/proj/src/b.ts": {diag(2, 2, 1, "y", "")},
		"/other/c.ts":    {diag(3, 3, 1, "z", "")},
	})
	s.Drop("/proj/src/a.ts", false)
	if s.Get("/proj/src/a.ts") != nil {
		t.Fatal("dropped file's task findings must vanish")
	}
	s.Drop("/proj", true)
	if s.Get("/proj/src/b.ts") != nil || s.Get("/other/c.ts") == nil {
		t.Fatalf("directory drop wrong: b=%v c=%v", s.Get("/proj/src/b.ts"), s.Get("/other/c.ts"))
	}
}
