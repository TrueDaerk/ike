package words

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ike/internal/complete"
	"ike/internal/host"
	"ike/internal/lang"

	// The grammars the language-scoping tests (#2652) index with. A no-cgo
	// build registers them without a grammar and the tests skip.
	_ "ike/plugins/languages/go"
	_ "ike/plugins/languages/markdown"
	_ "ike/plugins/languages/php"
	_ "ike/plugins/languages/python"
)

// lang_test.go guards #2652: the word index offers code tokens of the
// request's language only — strings, comments and Markdown prose are not
// words, an embedded fence belongs to its own language, and the project scan
// runs per language on demand.

// requireGrammar skips when id has no compiled-in grammar (no-cgo build).
func requireGrammar(t *testing.T, id string) {
	t.Helper()
	if l, ok := lang.ByID(id); !ok || l.Grammar == nil {
		t.Skipf("no %s grammar (no-cgo build)", id)
	}
}

func write(t *testing.T, dir, name, text string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
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

// TestOnlyCodeTokensIndexed: a Go buffer with a comment word, a string word
// and a code identifier offers only the identifier.
func TestOnlyCodeTokensIndexed(t *testing.T) {
	requireGrammar(t, "go")
	s := New("")
	src := "package x\n\n// commentWord\nvar codeWord = \"stringWord\"\n\nvar y = co"
	s.Observe(change("/a.go", src))
	got := labels(t, s, complete.Request{Path: "/a.go", Line: 5, Col: 10})
	if len(got) != 1 || got[0] != "codeWord" {
		t.Fatalf("got %v, want [codeWord] — strings and comments are not words", got)
	}
	// A manual request at a word boundary lists everything indexed: still
	// no string or comment content.
	all := labels(t, s, complete.Request{Path: "/a.go", Line: 5, Col: 0})
	if has(all, "commentWord") || has(all, "stringWord") {
		t.Fatalf("index = %v, must not contain string/comment words", all)
	}
}

// TestPlainTextBufferKeepsPlainTokenizer: a buffer no language claims (Plain
// Text) still completes from its own text — the fallback the grammar gate
// must not remove.
func TestPlainTextBufferKeepsPlainTokenizer(t *testing.T) {
	s := New("")
	s.Observe(host.EditorEvent{Kind: host.EditorChange, Key: "\x00buffer/1", Text: "plainword \"quoted\"\npl"})
	got := labels(t, s, complete.Request{Key: "\x00buffer/1", Line: 1, Col: 2})
	if len(got) != 1 || got[0] != "plainword" {
		t.Fatalf("got %v, want [plainword]", got)
	}
}

// TestMarkdownFenceIndexedUnderFenceLanguage: README prose is not indexed,
// `$myObj` from its ```php fence is a PHP word — offered in a .php buffer and
// inside the fence itself, not in a .py buffer.
func TestMarkdownFenceIndexedUnderFenceLanguage(t *testing.T) {
	requireGrammar(t, "php")
	requireGrammar(t, "markdown")
	requireGrammar(t, "python")
	dir := t.TempDir()
	readme := "# Title\n\nproseword paragraph here\n\n```php\n<?php\n$myObj = new Thing();\n```\n"
	write(t, dir, "README.md", readme)

	s := New(dir)
	php := filepath.Join(dir, "a.php")
	s.Observe(change(php, "<?php\n$my"))
	phpReq := complete.Request{Path: php, Line: 1, Col: 3}
	labels(t, s, phpReq)
	waitScan(t, s, "php")
	if got := labels(t, s, phpReq); !has(got, "myObj") {
		t.Fatalf("php buffer got %v, want myObj from the README fence", got)
	}
	// Prose never enters any index: not the PHP one, not a Markdown one.
	if got := labels(t, s, complete.Request{Path: php, Line: 1, Col: 0}); has(got, "proseword") || has(got, "paragraph") {
		t.Fatalf("php buffer got %v, must not contain README prose", got)
	}

	py := filepath.Join(dir, "b.py")
	s.Observe(change(py, "my"))
	pyReq := complete.Request{Path: py, Line: 0, Col: 2}
	labels(t, s, pyReq)
	waitScan(t, s, "python")
	if got := labels(t, s, pyReq); has(got, "myObj") {
		t.Fatalf("python buffer got %v, must not see the PHP fence", got)
	}

	// Inside the fence itself: the engine resolves the effective language to
	// php there, and the README buffer's own fence words answer.
	mdPath := filepath.Join(dir, "README.md")
	s.Observe(change(mdPath, readme+"\n```php\n$my\n```\n"))
	inFence := complete.Request{Path: mdPath, Line: 10, Col: 3, Lang: "php"}
	if got := labels(t, s, inFence); !has(got, "myObj") {
		t.Fatalf("inside the fence got %v, want myObj", got)
	}
	if got := labels(t, s, complete.Request{Path: mdPath, Line: 10, Col: 0, Lang: "php"}); has(got, "proseword") {
		t.Fatalf("inside the fence got %v, must not contain the prose around it", got)
	}
}

// TestScanPerLanguageOnDemand: the scan for a language runs once, on the
// first request from a buffer of that language, and other languages'
// requests do not trigger it.
func TestScanPerLanguageOnDemand(t *testing.T) {
	requireGrammar(t, "go")
	requireGrammar(t, "python")
	dir := t.TempDir()
	write(t, dir, "x.go", "package x\n\nvar goword = 1\n")
	write(t, dir, "y.py", "pyword = 1\n")
	s := New(dir)
	if got := s.ScannedLangs(); len(got) != 0 {
		t.Fatalf("scanned before any request: %v", got)
	}

	goPath := filepath.Join(dir, "a.go")
	s.Observe(change(goPath, "package x\n\nvar z = "))
	goReq := complete.Request{Path: goPath, Line: 2, Col: 8}
	labels(t, s, goReq)
	if got := s.ScannedLangs(); len(got) != 1 || got[0] != "go" {
		t.Fatalf("after a Go request scanned = %v, want [go] only", got)
	}
	waitScan(t, s, "go")
	labels(t, s, goReq)
	if got := s.ScannedLangs(); len(got) != 1 {
		t.Fatalf("a second Go request must not start another scan: %v", got)
	}
	got := labels(t, s, goReq)
	if !has(got, "goword") || has(got, "pyword") {
		t.Fatalf("go buffer got %v, want goword and no pyword", got)
	}

	pyPath := filepath.Join(dir, "b.py")
	s.Observe(change(pyPath, ""))
	pyReq := complete.Request{Path: pyPath}
	labels(t, s, pyReq)
	if got := s.ScannedLangs(); len(got) != 2 || got[1] != "python" {
		t.Fatalf("after a Python request scanned = %v, want [go python]", got)
	}
	waitScan(t, s, "python")
	got = labels(t, s, pyReq)
	if !has(got, "pyword") || has(got, "goword") {
		t.Fatalf("python buffer got %v, want pyword and no goword", got)
	}
}

// TestInvalidateFileRefreshesLanguageIndex: a watcher-driven invalidation
// re-extracts the file for the scanned language.
func TestInvalidateFileRefreshesLanguageIndex(t *testing.T) {
	requireGrammar(t, "go")
	dir := t.TempDir()
	path := write(t, dir, "x.go", "package x\n\nvar beforeword = 1\n")
	s := New(dir)
	goPath := filepath.Join(dir, "a.go")
	s.Observe(change(goPath, "package x\n\nvar z = "))
	req := complete.Request{Path: goPath, Line: 2, Col: 8}
	labels(t, s, req)
	waitScan(t, s, "go")
	write(t, dir, "x.go", "package x\n\nvar afterword = 1\n")
	s.InvalidateFile(path)
	deadline := 200
	for ; deadline > 0; deadline-- {
		got := labels(t, s, req)
		if has(got, "afterword") && !has(got, "beforeword") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("invalidation never refreshed: %v", labels(t, s, req))
}
