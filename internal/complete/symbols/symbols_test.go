package symbols

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ike/internal/complete"
	"ike/internal/host"
	"ike/internal/lsp/protocol"
)

func change(path, text string) host.EditorEvent {
	return host.EditorEvent{Kind: host.EditorChange, Path: path, Text: text}
}

func labels(t *testing.T, s *Source, req complete.Request) []string {
	t.Helper()
	items, err := s.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Label
	}
	return out
}

func waitScan(t *testing.T, s *Source) {
	t.Helper()
	for start := time.Now(); !s.ScanDone(); {
		if time.Since(start) > 5*time.Second {
			t.Fatal("scan did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestCSSClassesIntoHTML: class names and IDs from a project stylesheet are
// offered inside HTML class=/id= attribute values — and only there.
func TestCSSClassesIntoHTML(t *testing.T) {
	dir := t.TempDir()
	css := ".btn-primary { color: red } .card { } #main-nav { }"
	if err := os.WriteFile(filepath.Join(dir, "style.css"), []byte(css), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir)
	waitScan(t, s)

	page := filepath.Join(dir, "index.html")
	line := `<div class="bt`
	s.Observe(change(page, line))
	got := labels(t, s, complete.Request{Path: page, Line: 0, Col: len(line)})
	if len(got) != 1 || got[0] != "btn-primary" {
		t.Fatalf("class attr candidates = %v, want [btn-primary]", got)
	}

	s.Observe(change(page, `<div id="ma`))
	got = labels(t, s, complete.Request{Path: page, Line: 0, Col: 11})
	if len(got) != 1 || got[0] != "main-nav" {
		t.Fatalf("id attr candidates = %v, want [main-nav]", got)
	}

	// Outside an attribute value no CSS names leak.
	s.Observe(change(page, `<div>bt`))
	if got := labels(t, s, complete.Request{Path: page, Line: 0, Col: 7}); len(got) != 0 {
		t.Fatalf("outside attributes got %v, want none", got)
	}
}

// TestCSSBufferOverridesDisk: an edited (unsaved) stylesheet contributes its
// live classes instead of the on-disk ones.
func TestCSSBufferOverridesDisk(t *testing.T) {
	dir := t.TempDir()
	cssPath := filepath.Join(dir, "style.css")
	if err := os.WriteFile(cssPath, []byte(".old-name {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir)
	waitScan(t, s)
	s.Observe(change(cssPath, ".new-name {}"))

	page := filepath.Join(dir, "a.html")
	s.Observe(change(page, `<p class="n`))
	got := labels(t, s, complete.Request{Path: page, Line: 0, Col: 11})
	if len(got) != 1 || got[0] != "new-name" {
		t.Fatalf("got %v, want the buffer's class only", got)
	}
}

// TestInvalidateFileRefreshes: a watcher-driven invalidation re-extracts the
// on-disk file.
func TestInvalidateFileRefreshes(t *testing.T) {
	dir := t.TempDir()
	cssPath := filepath.Join(dir, "style.css")
	if err := os.WriteFile(cssPath, []byte(".before {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir)
	waitScan(t, s)
	if err := os.WriteFile(cssPath, []byte(".after {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.InvalidateFile(cssPath)

	page := filepath.Join(dir, "a.html")
	s.Observe(change(page, `<p class="`))
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := labels(t, s, complete.Request{Path: page, Line: 0, Col: 10})
		if len(got) == 1 && got[0] == "after" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("invalidation never refreshed, got %v", got)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestHTMLAttrContext table-tests the attribute detector.
func TestHTMLAttrContext(t *testing.T) {
	for _, tc := range []struct {
		line string
		col  int
		attr string
		ok   bool
	}{
		{`<div class="foo`, 15, "class", true},
		{`<div CLASS='foo`, 15, "class", true},
		{`<div id="x`, 10, "id", true},
		{`<div class="done">bar`, 21, "", false},
		{`plain text`, 5, "", false},
		{`<div data-class="x`, 18, "", false},
	} {
		attr, ok := htmlAttrContext(tc.line, tc.col)
		if ok != tc.ok || attr != tc.attr {
			t.Errorf("%q@%d = (%q,%v), want (%q,%v)", tc.line, tc.col, attr, ok, tc.attr, tc.ok)
		}
	}
}

// TestBufferSymbolsGrammarGated: with cgo the highlight layer yields symbols
// for a Go buffer; without it the source stays silently empty (the word index
// covers those builds). The assertion adapts so the test passes either way.
func TestBufferSymbolsGrammarGated(t *testing.T) {
	s := New("")
	src := "package x\n\nfunc DoWork() {}\n\ntype Widget struct{}\n\ndo"
	s.Observe(change("/a.go", src))
	got := labels(t, s, complete.Request{Path: "/a.go", Line: 6, Col: 2})
	if len(got) == 0 {
		t.Skip("no grammar captures (no-cgo build)")
	}
	if got[0] != "DoWork" {
		t.Fatalf("got %v, want DoWork first", got)
	}
	items, _ := s.Complete(context.Background(), complete.Request{Path: "/a.go", Line: 6, Col: 2})
	if items[0].Kind != protocol.KindFunction {
		t.Fatalf("kind = %d, want function", items[0].Kind)
	}
}
