package langdiff

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// TestDiffRegistered asserts the registration facts: both extensions resolve
// to the language, structure is Go-computed (no grammar needed), and the
// Spans/Folds hooks are wired. Runs without cgo — the language has no
// Tree-sitter grammar at all.
func TestDiffRegistered(t *testing.T) {
	for _, path := range []string{"changes.diff", "fix.patch"} {
		l, ok := lang.ByPath(path)
		if !ok {
			t.Fatalf("no language for %q", path)
		}
		if l.ID != "diff" {
			t.Fatalf("language for %q = %q, want diff", path, l.ID)
		}
		if l.Spans == nil || l.Folds == nil {
			t.Fatalf("diff language missing Spans/Folds hooks")
		}
		if l.Grammar != nil {
			t.Fatalf("diff language should have no grammar")
		}
	}
}

// TestHighlightPipeline drives the full highlight path an editor uses: spans
// and folds for a .patch buffer come out of HighlightScoped without a grammar.
func TestHighlightPipeline(t *testing.T) {
	lines := []string{
		"diff --git a/a.txt b/a.txt",
		"index 1234567..89abcde 100644",
		"--- a/a.txt",
		"+++ b/a.txt",
		"@@ -1,2 +1,2 @@",
		" same",
		"-removed word here",
		"+removed text here",
	}
	if !highlight.Supported("fix.patch") {
		t.Fatal("fix.patch not Supported")
	}
	spans, _, folds := highlight.HighlightScoped("fix.patch", lines)
	if len(spans) == 0 {
		t.Fatal("no spans")
	}
	ix := highlight.NewIndex(spans)
	cases := []struct {
		line, col int
		capture   string
	}{
		{0, 0, "diff.header"},
		{1, 0, "diff.meta"},
		{4, 0, "diff.delta"},
		{6, 0, "diff.minus"},
		{7, 0, "diff.plus"},
		{6, 9, "diff.minus.emph"}, // "word" vs "text" — changed range wins
		{7, 9, "diff.plus.emph"},
	}
	for _, c := range cases {
		if got := ix.CaptureAt(c.line, c.col); got != c.capture {
			t.Errorf("CaptureAt(%d, %d) = %q, want %q", c.line, c.col, got, c.capture)
		}
	}
	// The hunk folds behind its @@ header: file section fold + hunk fold.
	if len(folds) != 2 {
		t.Fatalf("folds = %v, want file section + hunk", folds)
	}
	if folds[1].HeaderLine != 4 || folds[1].EndLine != 7 {
		t.Errorf("hunk fold = %v, want {4 7}", folds[1])
	}
}
