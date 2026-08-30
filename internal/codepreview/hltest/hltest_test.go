// Package hltest verifies the code-preview column's syntax highlighting end
// to end (#2327). It lives in its own package on purpose: the check requires a
// grammar plugin to be linked (as cmd/ike/main.go does), and linking one into
// internal/codepreview's own test binary would change the rendered output the
// other tests there assert on.
//
// The tests run in both build flavours: with CGo the Go grammar colours the
// excerpt; without it (or without the plugin linked) the preview must fall
// back to readable plain text instead of failing the frame.
package hltest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	// Linked like the real binary, so the excerpt has a grammar to parse with.
	_ "ike/plugins/languages/go"

	"ike/internal/codepreview"
	"ike/internal/highlight"
)

// source is a small Go file the grammar has plenty to colour in.
var source = []string{
	"package sample",
	"",
	"import \"fmt\"",
	"",
	"func Total(values []int) int {",
	"\tsum := 0",
	"\tfor _, v := range values {",
	"\t\tsum += v",
	"\t}",
	"\tfmt.Println(sum)",
	"\treturn sum",
	"}",
}

// sgrRE matches one SGR escape sequence.
var sgrRE = regexp.MustCompile("\x1b\\[[0-9;:]*m")

// write drops source into a temp file and returns its path.
func write(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sample.go")
	if err := os.WriteFile(path, []byte(strings.Join(source, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// afterGutter returns a rendered row without its styled line-number gutter.
func afterGutter(row string) string {
	if at := sgrRE.FindAllStringIndex(row, 2); len(at) == 2 {
		return row[at[1][1]:]
	}
	return row
}

// TestPreviewHighlightsSource is the acceptance criterion that the excerpt is
// syntax-coloured, not a plain dump — and, in a build without the tree-sitter
// parser, that it degrades to plain text instead of erroring.
func TestPreviewHighlightsSource(t *testing.T) {
	path := write(t)
	var c codepreview.Cache
	rows := c.Render(codepreview.Target{Path: path, Line: 7}, 60, 13, nil)
	if len(rows) != 13 {
		t.Fatalf("got %d rows, want 13", len(rows))
	}
	// Row 0 is line 1, "package sample" — a keyword the theme colours.
	body := afterGutter(rows[0])
	if got := strings.TrimRight(ansi.Strip(body), " "); got != "package sample" {
		t.Fatalf("row 0 = %q, want the file's first line", got)
	}
	if !highlight.Supported(path) {
		t.Fatal("the linked Go plugin must register a grammar for .go files")
	}
	if len(highlight.Highlight(path, source)) == 0 {
		// A build without CGo has the language but no parser: plain text is
		// the contract, and it must still be the right text.
		if ansi.Strip(body) != body {
			t.Fatalf("row 0 = %q, want plain text without a parser", body)
		}
		return
	}
	if ansi.Strip(body) == body {
		t.Fatalf("row 0 = %q, want syntax-coloured code", body)
	}
	// The colouring survives horizontal scrolling: the slice stays inside the
	// column and keeps its styling.
	var scrolled codepreview.Cache
	target := codepreview.Target{Path: path, Line: 7}
	scrolled.Render(target, 40, 13, nil)
	scrolled.SetFocus(true)
	scrolled.Key("l")
	out := scrolled.Render(target, 40, 13, nil)
	for i, r := range out {
		if w := ansi.StringWidth(r); w > 40 {
			t.Fatalf("scrolled row %d is %d cells wide, past the column", i, w)
		}
	}
	// The hit line still spans the column: slicing coloured rows must not
	// eat the current-line background.
	if w := ansi.StringWidth(out[6]); w != 40 {
		t.Fatalf("scrolled hit line is %d cells wide, want the full 40", w)
	}
	if !strings.Contains(ansi.Strip(out[6]), "range values") {
		t.Fatalf("scrolled hit line = %q, want the scrolled hit", ansi.Strip(out[6]))
	}
}

// TestPreviewHitLineKeepsSyntaxColours is the layering criterion: the hit
// line's background and its match emphasis sit on top of the capture colours
// rather than replacing them.
func TestPreviewHitLineKeepsSyntaxColours(t *testing.T) {
	path := write(t)
	if len(highlight.Highlight(path, source)) == 0 {
		t.Skip("no tree-sitter parser in this build")
	}
	var c codepreview.Cache
	// Line 6 is "\tsum := 0"; the match covers "sum" (runes 1…4 raw).
	plainHit := c.Render(codepreview.Target{Path: path, Line: 6}, 60, 11, nil)[5]
	marked := c.Render(codepreview.Target{
		Path:   path,
		Line:   6,
		Ranges: []codepreview.Range{{Start: 1, End: 4}},
	}, 60, 11, nil)[5]
	if ansi.Strip(plainHit) != ansi.Strip(marked) {
		t.Fatal("the match emphasis must change styling only, not the text")
	}
	if plainHit == marked {
		t.Fatal("the match range must be visible on top of the syntax colours")
	}
	if n := len(sgrRE.FindAllString(marked, -1)); n < 3 {
		t.Fatalf("the marked hit line carries %d style runs, want the syntax colours kept", n)
	}
}
