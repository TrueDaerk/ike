package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
	"ike/internal/palette"
)

func kindHit(name, path string, kind int) ilsp.SymbolHit {
	h := symbolHit(name, path)
	h.Kind = kind
	return h
}

// primed returns a symbol cache holding hits for query q.
func primed(t *testing.T, q string, hits ...ilsp.SymbolHit) *symbolMode {
	t.Helper()
	s := &symbolMode{}
	s.lastSent = q
	s.SetHits(q, hits)
	return s
}

// The class category lists only class-like kinds — class, struct, interface,
// enum (#1849) — while the symbol seat of search everywhere lists the rest, so
// nothing appears twice in the composed list.
func TestClassModeFiltersClassLikeKinds(t *testing.T) {
	q := "user"
	s := primed(t, q,
		kindHit("User", "/proj/user.go", protocol.SymKindStruct),
		kindHit("UserStore", "/proj/store.go", protocol.SymKindInterface),
		kindHit("UserKind", "/proj/kind.go", protocol.SymKindEnum),
		kindHit("UserClass", "/proj/user.py", protocol.SymKindClass),
		kindHit("userOf", "/proj/of.go", protocol.SymKindFunction),
		kindHit("userVar", "/proj/var.go", protocol.SymKindVariable),
		kindHit("userMethod", "/proj/m.go", protocol.SymKindMethod),
	)
	classes := titles(newClassMode(s).Results(q, palette.Context{}))
	wantClasses := []string{"User", "UserStore", "UserKind", "UserClass"}
	if !sameSet(classes, wantClasses) {
		t.Fatalf("class rows = %v, want %v", classes, wantClasses)
	}
	syms := titles(newNonClassSymbolMode(s).Results(q, palette.Context{}))
	wantSyms := []string{"userOf", "userVar", "userMethod"}
	if !sameSet(syms, wantSyms) {
		t.Fatalf("symbol rows = %v, want %v", syms, wantSyms)
	}
	// The standalone go-to-symbol mode keeps every kind.
	if got := s.Results(q, palette.Context{}); len(got) != 7 {
		t.Fatalf("standalone symbol mode = %d rows, want all 7", len(got))
	}
}

// Every symbol row carries its kind as a badge, so a class is tellable from a
// function at a glance.
func TestSymbolRowsCarryKindBadge(t *testing.T) {
	q := "user"
	s := primed(t, q,
		kindHit("User", "/proj/user.go", protocol.SymKindStruct),
		kindHit("userOf", "/proj/of.go", protocol.SymKindFunction),
		kindHit("userless", "/proj/x.go", 0), // server sent no kind
	)
	want := map[string]string{"User": "struct", "userOf": "func", "userless": ""}
	for _, it := range s.Results(q, palette.Context{}) {
		if it.Badge != want[it.Title] {
			t.Errorf("%q badge = %q, want %q", it.Title, it.Badge, want[it.Title])
		}
	}
}

// #377 carries over to the class category: an exact project class outranks a
// dependency class the server returned first.
func TestClassModeRanksProjectExactMatchFirst(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	q := "buffer"
	s := primed(t, q,
		kindHit("BufferPool", "/usr/lib/go/src/bytes/buffer.go", protocol.SymKindStruct),
		kindHit("Buffer", "/usr/lib/go/src/bytes/buffer.go", protocol.SymKindStruct),
		kindHit("Buffer", filepath.Join(cwd, "internal", "editor", "buffer.go"), protocol.SymKindStruct),
	)
	got := newClassMode(s).Results(q, palette.Context{})
	if len(got) != 3 {
		t.Fatalf("want 3 class rows, got %d", len(got))
	}
	if got[0].Title != "Buffer" || !strings.HasPrefix(got[0].Detail, "internal") {
		t.Fatalf("want the project Buffer first, got %q (%s)", got[0].Title, got[0].Detail)
	}
	if got[0].Score < 0 {
		t.Fatalf("project class must keep an unpenalised score, got %d", got[0].Score)
	}
}

// A '/'-scoped query narrows search everywhere to classes alone (#1417), and
// the scoped list is uncapped.
func TestSearchAllScopesToClasses(t *testing.T) {
	q := "item"
	var hits []ilsp.SymbolHit
	for i := 0; i < 12; i++ {
		hits = append(hits, kindHit("Item"+strconv.Itoa(i), "/proj/i"+strconv.Itoa(i)+".go", protocol.SymKindStruct))
	}
	hits = append(hits, kindHit("itemOf", "/proj/of.go", protocol.SymKindFunction))
	s := primed(t, q, hits...)
	all := palette.NewSearchAllMode(newClassMode(s), newNonClassSymbolMode(s))
	got := all.Results(string(classesPrefix)+q, palette.Context{})
	if len(got) != 12 {
		t.Fatalf("scoped class list = %d rows, want all 12 (uncapped)", len(got))
	}
	for _, it := range got {
		if !strings.HasPrefix(it.Title, string(classesPrefix)+" Item") {
			t.Fatalf("scoped row %q must be a glyphed class row", it.Title)
		}
	}
}

// The per-source cap cannot bury a matching class any more: the class category
// has its own seats next to the symbol source (#1849).
func TestSearchAllKeepsClassBesideManySymbols(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	q := "render"
	var hits []ilsp.SymbolHit
	for i := 0; i < 20; i++ {
		hits = append(hits, kindHit("render"+strconv.Itoa(i), filepath.Join(cwd, "f"+strconv.Itoa(i)+".go"), protocol.SymKindFunction))
	}
	hits = append(hits, kindHit("Render", filepath.Join(cwd, "render.go"), protocol.SymKindClass))
	s := primed(t, q, hits...)
	all := palette.NewSearchAllMode(newClassMode(s), newNonClassSymbolMode(s))
	got := all.Results(q, palette.Context{})
	if len(got) == 0 || got[0].Title != string(classesPrefix)+" Render" {
		t.Fatalf("want the exact class first, got %v", titles(got))
	}
}

// Both views share one cache, so a keystroke costs one workspace/symbol
// request however many of them search everywhere forwards it to.
func TestSymbolViewsShareOneRequest(t *testing.T) {
	s := &symbolMode{}
	sent := 0
	s.SetRequest(func(string) tea.Cmd { sent++; return nil })
	all := palette.NewSearchAllMode(newClassMode(s), newNonClassSymbolMode(s))
	all.QueryChanged("user", palette.Context{})
	if sent != 1 {
		t.Fatalf("workspace/symbol sent %d times, want 1", sent)
	}
	if s.lastSent != "user" {
		t.Fatalf("lastSent = %q, want %q", s.lastSent, "user")
	}
	// A fresh palette open forgets the sent query, so re-typing re-queries.
	s.Refresh()
	all.QueryChanged("user", palette.Context{})
	if sent != 2 {
		t.Fatalf("after Refresh: sent %d times, want 2", sent)
	}
}

func titles(items []palette.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
	}
	return out
}

func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(got))
	for _, g := range got {
		seen[g] = true
	}
	for _, w := range want {
		if !seen[w] {
			return false
		}
	}
	return true
}
