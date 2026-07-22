//go:build cgo

package langgo

import (
	"strings"
	"testing"

	"ike/internal/highlight"
)

// The blank init() registers Go, so highlight.Highlight resolves the grammar.
func TestGoHighlighting(t *testing.T) {
	lines := []string{
		"package main",
		"",
		`func main() { println("hi") }`,
	}
	spans := highlight.Highlight("main.go", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for Go source, got none")
	}
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 0); got != "keyword" { // "package"
		t.Errorf("package keyword: got capture %q", got)
	}
}

// TestHighlightFenced guards #379: a hover markdown fence tag ("```go")
// resolves to the registered grammar, both as a language id and an extension.
func TestHighlightFenced(t *testing.T) {
	lines := []string{"func Printf(format string, a ...any) (n int, err error)"}
	for _, tag := range []string{"go", "Go"} {
		if spans := highlight.HighlightFenced(tag, lines); len(spans) == 0 {
			t.Errorf("HighlightFenced(%q) returned no spans", tag)
		}
	}
	if spans := highlight.HighlightFenced("no-such-lang", lines); spans != nil {
		t.Errorf("unknown fence tag should yield nil spans, got %d", len(spans))
	}
}

// TestGoScopes guards sticky scroll (#168): the scope-collecting parse yields
// the enclosing declarations of Go source, pre-ordered and multi-line only.
func TestGoScopes(t *testing.T) {
	lines := []string{
		"package main",     // 0
		"",                 // 1
		"func outer() {",   // 2
		"\tf := func() {",  // 3
		"\t\tprintln(1)",   // 4
		"\t}",              // 5
		"\tf()",            // 6
		"}",                // 7
		"",                 // 8
		"type T struct {",  // 9
		"\tX int",          // 10
		"}",                // 11
		"",                 // 12
		"func single() {}", // 13 — single line, no scope
	}
	_, scopes, _ := highlight.HighlightScoped("main.go", lines)
	want := []highlight.Scope{
		{HeaderLine: 2, EndLine: 7},  // outer
		{HeaderLine: 3, EndLine: 5},  // func literal
		{HeaderLine: 9, EndLine: 11}, // type T
	}
	if len(scopes) != len(want) {
		t.Fatalf("scopes = %v, want %v", scopes, want)
	}
	for i := range want {
		if scopes[i] != want[i] {
			t.Errorf("scope[%d] = %v, want %v", i, scopes[i], want[i])
		}
	}
	if got := highlight.EnclosingScopes(scopes, 4); len(got) != 2 {
		t.Errorf("EnclosingScopes(line 4) = %v, want outer + literal", got)
	}
}

// TestGoFolds guards code folding (#144): the fold-collecting parse yields
// the collapsible regions of Go source, pre-ordered and multi-line only, with
// same-header nodes (declaration + body block) collapsed into one fold.
func TestGoFolds(t *testing.T) {
	lines := []string{
		"package main",       // 0
		"",                   // 1
		"import (",           // 2
		"\t\"fmt\"",          // 3
		")",                  // 4
		"",                   // 5
		"func outer() {",     // 6
		"\tif true {",        // 7
		"\t\tfmt.Println(1)", // 8
		"\t}",                // 9
		"}",                  // 10
		"",                   // 11
		"func single() {}",   // 12 — single line, nothing to hide
	}
	_, _, folds := highlight.HighlightScoped("main.go", lines)
	want := []highlight.Fold{
		{HeaderLine: 2, EndLine: 4},  // import list
		{HeaderLine: 6, EndLine: 10}, // func outer (declaration + block merged)
		{HeaderLine: 7, EndLine: 9},  // if block
	}
	if len(folds) != len(want) {
		t.Fatalf("folds = %v, want %v", folds, want)
	}
	for i := range want {
		if folds[i] != want[i] {
			t.Errorf("fold[%d] = %v, want %v", i, folds[i], want[i])
		}
	}
}

// TestGoRainbowBrackets (#789): bracket tokens carry depth-cycled rainbow
// captures from the real Go grammar, first-covering so they win over the
// grammar's own punctuation captures; disabling the toggle removes them.
func TestGoRainbowBrackets(t *testing.T) {
	lines := []string{"func f() { g([]int{1}) }"}
	spans := highlight.Highlight("main.go", lines)
	depths := map[string]bool{}
	for _, s := range spans {
		if strings.HasPrefix(s.Capture, "rainbow.") {
			depths[s.Capture] = true
		}
	}
	// f() and {} at depth 0, ([...]) at 1, []int at 2... at least 3 depths.
	for _, want := range []string{"rainbow.0", "rainbow.1", "rainbow.2"} {
		if !depths[want] {
			t.Errorf("missing %s in %v", want, depths)
		}
	}
	// The index resolves a bracket cell to its rainbow capture (first wins).
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 6); got != "rainbow.0" { // the "(" of f()
		t.Errorf("CaptureAt bracket = %q, want rainbow.0", got)
	}

	highlight.SetRainbow(false)
	defer highlight.SetRainbow(true)
	for _, s := range highlight.Highlight("main.go", lines) {
		if strings.HasPrefix(s.Capture, "rainbow.") {
			t.Fatalf("rainbow span %q present while disabled", s.Capture)
		}
	}
}
