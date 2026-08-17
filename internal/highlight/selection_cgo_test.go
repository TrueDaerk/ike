//go:build cgo

package highlight

// selection_cgo_test.go exercises the Tree-sitter extend-selection fallback
// (#1912, SelectionRangesAt in parse_cgo.go) against the real Go grammar. The
// language plugins are not linked into this package's tests, so the test
// registers a private language under its own extension — the same trick the
// fragment tests use for synthetic hosts.

import (
	"testing"

	ts "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"

	"ike/internal/lang"
)

// registerSelGo wires the Go grammar under the .selrgo extension (kept off
// "go" so no other test's association is disturbed).
func registerSelGo(t *testing.T) string {
	t.Helper()
	lang.Register(lang.Language{
		ID:         "selrgo",
		Extensions: []string{"selrgo"},
		Grammar:    NewGrammar(ts.NewLanguage(tsgo.Language()), "(package_clause) @keyword"),
	})
	return "main.selrgo"
}

func TestSelectionRangesAtLadder(t *testing.T) {
	path := registerSelGo(t)
	lines := []string{
		"package main",
		"",
		"func add(a, b int) int {",
		"\treturn a + b",
		"}",
	}
	// Cursor on the 'a' of "a + b" (line 3, rune col 8).
	got := SelectionRangesAt(path, lines, 3, 8)
	if len(got) < 3 {
		t.Fatalf("ladder = %+v, want at least identifier, expression, function", got)
	}
	// Innermost first: the identifier "a".
	if got[0] != (NodeRange{StartLine: 3, StartCol: 8, EndLine: 3, EndCol: 9}) {
		t.Errorf("ladder[0] = %+v, want the identifier a", got[0])
	}
	// Some step must be the binary expression "a + b".
	foundExpr := false
	for _, r := range got {
		if r == (NodeRange{StartLine: 3, StartCol: 8, EndLine: 3, EndCol: 13}) {
			foundExpr = true
		}
	}
	if !foundExpr {
		t.Errorf("ladder %+v misses the a + b expression", got)
	}
	// Outermost last: the whole source file.
	last := got[len(got)-1]
	if last.StartLine != 0 || last.EndLine < 4 {
		t.Errorf("ladder top = %+v, want the whole file", last)
	}
	// Strictly non-shrinking containment, innermost first.
	for i := 1; i < len(got); i++ {
		in, out := got[i-1], got[i]
		if out.StartLine > in.StartLine || (out.StartLine == in.StartLine && out.StartCol > in.StartCol) ||
			out.EndLine < in.EndLine || (out.EndLine == in.EndLine && out.EndCol < in.EndCol) {
			t.Fatalf("ladder[%d] %+v does not contain ladder[%d] %+v", i, out, i-1, in)
		}
		if in == out {
			t.Fatalf("ladder holds duplicate extents at %d: %+v", i, in)
		}
	}
}

func TestSelectionRangesAtUnicodeColumns(t *testing.T) {
	path := registerSelGo(t)
	// The ä is 2 bytes but 1 rune: the identifier after it proves byte→rune
	// conversion in both directions (request col in runes, results in runes).
	lines := []string{
		"package main",
		"",
		`var s = "ä" + name`,
	}
	// Cursor on the 'n' of "name": rune col 14.
	got := SelectionRangesAt(path, lines, 2, 14)
	if len(got) == 0 {
		t.Fatal("no ladder for a unicode line")
	}
	if got[0] != (NodeRange{StartLine: 2, StartCol: 14, EndLine: 2, EndCol: 18}) {
		t.Errorf("ladder[0] = %+v, want the name identifier at rune cols 14-18", got[0])
	}
}

func TestSelectionRangesAtOutsideAndUnknown(t *testing.T) {
	path := registerSelGo(t)
	if got := SelectionRangesAt(path, []string{"package main"}, 7, 0); got != nil {
		t.Errorf("out-of-range line must yield nil, got %+v", got)
	}
	if got := SelectionRangesAt("nope.unknownext", []string{"x"}, 0, 0); got != nil {
		t.Errorf("a path without a grammar must yield nil, got %+v", got)
	}
}
