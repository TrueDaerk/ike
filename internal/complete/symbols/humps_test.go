package symbols

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/complete"
	"ike/internal/config"
)

// humps_test.go guards #2650: the symbol source's pre-filter is the hump
// matcher the popup uses, so "gur" reaches GotoURLResolver indexed from a
// project file and mid-word hits never leave the source.

// TestProjectSymbolsHumpMatched indexes a Go file from the project scan; the
// grammar gate mirrors TestBufferSymbolsGrammarGated (no-cgo builds skip).
func TestProjectSymbolsHumpMatched(t *testing.T) {
	dir := t.TempDir()
	src := "package x\n\nfunc GotoURLResolver() {}\n\nfunc catalogOf() {}\n\nfunc logger() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir)
	waitScan(t, s, "go")
	other := filepath.Join(dir, "a.go")
	s.Observe(change(other, "package x\n\nlog"))
	if got := labels(t, s, complete.Request{Path: other, Line: 2, Col: 3}); len(got) == 0 {
		t.Skip("no grammar captures (no-cgo build)")
	} else if len(got) != 1 || got[0] != "logger" {
		t.Fatalf("log → %v, want [logger] (catalogOf is a mid-word hit)", got)
	}
	s.Observe(change(other, "package x\n\ngur"))
	if got := labels(t, s, complete.Request{Path: other, Line: 2, Col: 3}); len(got) != 1 || got[0] != "GotoURLResolver" {
		t.Fatalf("gur → %v, want [GotoURLResolver]", got)
	}
}

// TestCSSClassesHumpMatched: the CSS class/id path shares the pre-filter, so
// "bp" reaches btn-primary over its "-" boundary and "tn" stays mid-word.
func TestCSSClassesHumpMatched(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "style.css"), []byte(".btn-primary { } .Button { }"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir)
	waitScan(t, s, "css")
	page := filepath.Join(dir, "index.html")
	s.Observe(change(page, `<div class="bp`))
	if got := labels(t, s, complete.Request{Path: page, Line: 0, Col: 14}); len(got) != 1 || got[0] != "btn-primary" {
		t.Fatalf("bp → %v, want [btn-primary]", got)
	}
	s.Observe(change(page, `<div class="tn`))
	if got := labels(t, s, complete.Request{Path: page, Line: 0, Col: 14}); len(got) != 0 {
		t.Fatalf("tn → %v, want none (mid-word)", got)
	}

	// completion.case_sensitivity = all: "b" no longer folds onto Button.
	prev := config.Get()
	c := *prev
	c.Completion.CaseSensitivity = "all"
	config.Set(&c)
	t.Cleanup(func() { config.Set(prev) })
	s.Observe(change(page, `<div class="B`))
	if got := labels(t, s, complete.Request{Path: page, Line: 0, Col: 13}); len(got) != 1 || got[0] != "Button" {
		t.Fatalf("all: B → %v, want [Button]", got)
	}
}
