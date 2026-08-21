package lsp

import (
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/manager"
)

// tocLines is the shape a heading rename has to leave consistent: a table of
// contents, a prose link, and one link the author worded differently.
var tocLines = []string{
	"# Doc",
	"",
	"## Table of contents",
	"",
	"- [Old Heading](#old-heading)",
	"- [Other](#other)",
	"",
	"## Old Heading",
	"",
	"Some text linking to [Old Heading](#old-heading), and to",
	"[the section above](#old-heading) by another name.",
}

// marksmanEdits are the edits marksman returns for renaming the heading on
// line 7: the heading itself plus every same-document anchor, in the server's
// own (unsorted) order.
func marksmanEdits() []ilsp.FormatEdit {
	return []ilsp.FormatEdit{
		{StartLine: 9, StartCol: 36, EndLine: 9, EndCol: 47, Text: "new-heading"},
		{StartLine: 7, StartCol: 3, EndLine: 7, EndCol: 14, Text: "New Heading"},
		{StartLine: 4, StartCol: 17, EndLine: 4, EndCol: 28, Text: "new-heading"},
		{StartLine: 10, StartCol: 20, EndLine: 10, EndCol: 31, Text: "new-heading"},
	}
}

// TestHeadingLinkTitlesFollowRename is #2025's second half: the anchors are
// the server's job, the titles are ours — a TOC entry that spells the old
// heading is retitled, so the rename is visible where IKE conceals the
// destination.
func TestHeadingLinkTitlesFollowRename(t *testing.T) {
	got := headingLinkTitleEdits(tocLines, marksmanEdits(), "Old Heading", "New Heading")
	if len(got) != 2 {
		t.Fatalf("want the two matching titles retitled, got %#v", got)
	}
	want := map[int][2]int{4: {3, 14}, 9: {22, 33}} // line -> [startCol, endCol)
	for _, e := range got {
		span, ok := want[e.StartLine]
		if !ok {
			t.Fatalf("unexpected retitle on line %d: %#v", e.StartLine, e)
		}
		if e.StartCol != span[0] || e.EndCol != span[1] || e.EndLine != e.StartLine {
			t.Errorf("line %d span = [%d,%d), want [%d,%d)", e.StartLine, e.StartCol, e.EndCol, span[0], span[1])
		}
		if e.Text != "New Heading" {
			t.Errorf("line %d text = %q", e.StartLine, e.Text)
		}
		delete(want, e.StartLine)
	}
	if len(want) != 0 {
		t.Errorf("missing retitles for lines %v", want)
	}
}

// TestHeadingLinkTitlesLeaveOtherWordingAlone is the restraint half: a link
// the author titled differently keeps its wording (only its anchor moves,
// which the server already did), and the heading edit itself is never
// mistaken for a link.
func TestHeadingLinkTitlesLeaveOtherWordingAlone(t *testing.T) {
	for _, e := range headingLinkTitleEdits(tocLines, marksmanEdits(), "Old Heading", "New Heading") {
		if e.StartLine == 10 {
			t.Errorf("differently worded link must keep its title, got %#v", e)
		}
		if e.StartLine == 7 {
			t.Errorf("the heading edit is not a link title, got %#v", e)
		}
	}
}

// TestHeadingLinkTitleSpanShapes pins which destinations qualify: an inline
// anchor link with or without a file part, and nothing else — a bare "#" in
// prose, a wiki link or an already-consumed bracket must not produce an edit.
func TestHeadingLinkTitleSpanShapes(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		col   int // where the server's anchor edit starts
		title string
	}{
		{"same document", "see [Old Heading](#old-heading) below", 19, "Old Heading"},
		{"cross file", "see [Old Heading](doc.md#old-heading)", 25, "Old Heading"},
		{"empty title", "see [](#old-heading)", 8, ""},
		{"bare fragment", "the #old-heading anchor", 5, ""},
		{"wiki link", "see [[old-heading]]", 6, ""},
		{"no closing bracket", "see (#old-heading)", 6, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runes := []rune(tc.line)
			start, end, ok := linkTitleSpan(runes, tc.col)
			if !ok {
				if tc.title != "" {
					t.Fatalf("want title %q, got no span", tc.title)
				}
				return
			}
			if got := string(runes[start:end]); got != tc.title {
				t.Fatalf("title = %q, want %q", got, tc.title)
			}
		})
	}
}

// TestHeadingLinkTitleEditsClampOutOfRange guards the honest-input rule: edits
// addressing lines or columns the buffer no longer has are skipped, never
// panicked on.
func TestHeadingLinkTitleEditsClampOutOfRange(t *testing.T) {
	edits := []ilsp.FormatEdit{
		{StartLine: 99, StartCol: 3, EndLine: 99, EndCol: 5, Text: "x"},
		{StartLine: -1, StartCol: 0, EndLine: -1, EndCol: 1, Text: "x"},
		{StartLine: 4, StartCol: 999, EndLine: 4, EndCol: 1000, Text: "x"},
		{StartLine: 4, StartCol: 17, EndLine: 5, EndCol: 3, Text: "x"}, // multi-line
	}
	if got := headingLinkTitleEdits(tocLines, edits, "Old Heading", "New Heading"); len(got) != 0 {
		t.Fatalf("out-of-range edits must be skipped, got %#v", got)
	}
}

// TestMergeHeadingTitleEditsAugmentsRenamedFile is the wiring: the renamed
// Markdown file's slice of the WorkspaceEdit grows the title edits, other
// files are passed through untouched, and a non-Markdown rename is left
// entirely alone.
func TestMergeHeadingTitleEditsAugmentsRenamedFile(t *testing.T) {
	lang.Register(lang.Language{ID: "markdown", Extensions: []string{"md"}})
	b, path, _ := gateBridge(t, renameCaps(true))
	// The gate bridge's document is the single-heading file; sync the TOC
	// shape the server's edits address.
	if err := b.mgr.Open(path, "markdown", strings.Join(tocLines, "\n")); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(filepath.Dir(path), "other.md")
	files := []manager.FileEdits{
		{Path: path, Open: true, Edits: marksmanEdits()},
		{Path: other, Edits: []ilsp.FormatEdit{{StartLine: 0, StartCol: 3, EndLine: 0, EndCol: 14, Text: "new-heading"}}},
	}

	got := b.mergeHeadingTitleEdits(files, path, "Old Heading", "New Heading")
	if n := len(got[0].Edits); n != len(marksmanEdits())+2 {
		t.Fatalf("renamed file edits = %d, want the server's %d plus 2 titles", n, len(marksmanEdits()))
	}
	if n := len(got[1].Edits); n != 1 {
		t.Errorf("other files must pass through untouched, got %d edits", n)
	}
}

// TestMergeHeadingTitleEditsSkipsNonMarkdown keeps the completion where it
// belongs: a Go rename returns exactly what the server sent.
func TestMergeHeadingTitleEditsSkipsNonMarkdown(t *testing.T) {
	b, path, _ := gateBridge(t, renameCaps(true))
	goPath := filepath.Join(filepath.Dir(path), "main.go")
	files := []manager.FileEdits{{Path: goPath, Open: true, Edits: marksmanEdits()}}

	got := b.mergeHeadingTitleEdits(files, goPath, "Old Heading", "New Heading")
	if len(got[0].Edits) != len(marksmanEdits()) {
		t.Fatalf("non-Markdown rename must pass through, got %#v", got[0].Edits)
	}
}
