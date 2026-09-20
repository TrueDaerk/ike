package symbols

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/complete"
	"ike/internal/lang"

	// The grammars the language-scoping tests (#2652) index with. A no-cgo
	// build registers them without a grammar and the tests skip.
	_ "ike/plugins/languages/go"
	_ "ike/plugins/languages/markdown"
	_ "ike/plugins/languages/python"
	_ "ike/plugins/languages/web"
)

// lang_test.go guards #2652: the symbol index offers declarations of the
// request's language only, an embedded fence's declarations belong to the
// fence language, and the project scan runs per language on demand.

func requireGrammar(t *testing.T, id string) {
	t.Helper()
	if l, ok := lang.ByID(id); !ok || l.Grammar == nil {
		t.Skipf("no %s grammar (no-cgo build)", id)
	}
}

func write(t *testing.T, dir, name, text string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func has(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// TestSymbolsScopedByLanguage: a Go function is offered in a Go buffer and
// not in a Python one, whether it comes from another open buffer or from
// the project scan.
func TestSymbolsScopedByLanguage(t *testing.T) {
	requireGrammar(t, "go")
	requireGrammar(t, "python")
	dir := t.TempDir()
	write(t, dir, "lib.go", "package x\n\nfunc DiskHelper() {}\n")
	s := New(dir)
	s.Observe(change(filepath.Join(dir, "open.go"), "package x\n\nfunc BufferHelper() {}\n"))

	py := filepath.Join(dir, "a.py")
	s.Observe(change(py, "def PyThing(): pass\n\nhel"))
	pyReq := complete.Request{Path: py, Line: 2, Col: 3}
	labels(t, s, pyReq)
	waitScan(t, s, "python")
	if got := labels(t, s, pyReq); has(got, "BufferHelper") || has(got, "DiskHelper") {
		t.Fatalf("python buffer got %v, must not see Go functions", got)
	}
	if got := s.ScannedLangs(); has(got, "go") {
		t.Fatalf("a Python request must not start the Go scan: %v", got)
	}

	goPath := filepath.Join(dir, "b.go")
	s.Observe(change(goPath, "package x\n\nvar _ = hel"))
	goReq := complete.Request{Path: goPath, Line: 2, Col: 11}
	labels(t, s, goReq)
	waitScan(t, s, "go")
	got := labels(t, s, goReq)
	if !has(got, "BufferHelper") || !has(got, "DiskHelper") {
		t.Fatalf("go buffer got %v, want both Go helpers", got)
	}
	if has(got, "PyThing") {
		t.Fatalf("go buffer got %v, must not see the Python function", got)
	}
}

// TestFenceSymbolsBelongToFenceLanguage: a Go function declared in a ```go
// fence of a README is a Go symbol — offered in a Go buffer, and the README
// buffer itself gets it when the cursor's effective language is go.
func TestFenceSymbolsBelongToFenceLanguage(t *testing.T) {
	requireGrammar(t, "go")
	requireGrammar(t, "markdown")
	dir := t.TempDir()
	write(t, dir, "README.md", "# Doc\n\n```go\nfunc FencedFunc() {}\n```\n")
	s := New(dir)
	goPath := filepath.Join(dir, "a.go")
	s.Observe(change(goPath, "package x\n\nvar _ = fen"))
	req := complete.Request{Path: goPath, Line: 2, Col: 11}
	labels(t, s, req)
	waitScan(t, s, "go")
	if got := labels(t, s, req); !has(got, "FencedFunc") {
		t.Fatalf("go buffer got %v, want FencedFunc from the README fence", got)
	}
	md := filepath.Join(dir, "README.md")
	s.Observe(change(md, "# Doc\n\n```go\nfunc FencedFunc() {}\nvar _ = fen\n```\n"))
	if got := labels(t, s, complete.Request{Path: md, Line: 4, Col: 11, Lang: "go"}); !has(got, "FencedFunc") {
		t.Fatalf("inside the fence got %v, want FencedFunc", got)
	}
	if got := labels(t, s, complete.Request{Path: md, Line: 0, Col: 5}); has(got, "FencedFunc") {
		t.Fatalf("markdown prose got %v, must not see the fence's Go symbol", got)
	}
}

// TestStyleFragmentClassesReachHTML: a <style> block's classes index under
// css through the fragment layer and answer an HTML class= attribute.
func TestStyleFragmentClassesReachHTML(t *testing.T) {
	requireGrammar(t, "html")
	requireGrammar(t, "css")
	dir := t.TempDir()
	write(t, dir, "page.html", "<style>.hero-card { color: red }</style>\n")
	s := New(dir)
	other := filepath.Join(dir, "other.html")
	s.Observe(change(other, `<div class="he`))
	req := complete.Request{Path: other, Line: 0, Col: 14}
	labels(t, s, req)
	waitScan(t, s, "css")
	if got := labels(t, s, req); !has(got, "hero-card") {
		t.Fatalf("got %v, want hero-card from the <style> fragment", got)
	}
}
