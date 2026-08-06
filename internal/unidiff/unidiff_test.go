package unidiff

import (
	"strings"
	"testing"

	"ike/internal/lang"
)

// gitPatch is a git-style patch with extension headers and two files.
var gitPatch = []string{
	"diff --git a/foo.go b/foo.go",          // 0 header
	"index 1234567..89abcde 100644",         // 1 meta
	"--- a/foo.go",                          // 2 header
	"+++ b/foo.go",                          // 3 header
	"@@ -1,3 +1,4 @@ func main()",           // 4 hunk
	" context",                              // 5 context
	"-old line",                             // 6 minus
	"+new line",                             // 7 plus
	"+added",                                // 8 plus
	" tail",                                 // 9 context
	"diff --git a/old.txt b/new.txt",        // 10 header
	"similarity index 90%",                  // 11 meta
	"rename from old.txt",                   // 12 meta
	"rename to new.txt",                     // 13 meta
}

func kindsOf(t *testing.T, lines []string) []kind {
	t.Helper()
	return parse(lines).kinds
}

func TestClassifyGitPatch(t *testing.T) {
	want := []kind{
		kindHeader, kindMeta, kindHeader, kindHeader, kindHunk,
		kindContext, kindMinus, kindPlus, kindPlus, kindContext,
		kindHeader, kindMeta, kindMeta, kindMeta,
	}
	got := kindsOf(t, gitPatch)
	for i, w := range want {
		if got[i] != w {
			t.Errorf("line %d %q: kind = %d, want %d", i, gitPatch[i], got[i], w)
		}
	}
}

// A removed line whose content starts with "-- " renders as "--- …" in the
// patch — inside a hunk body it must stay a removed line, not a file header.
func TestBodyLineIsNotFileHeader(t *testing.T) {
	lines := []string{
		"--- a/notes.txt",
		"+++ b/notes.txt",
		"@@ -1,2 +1,1 @@",
		"--- signature separator",
		" kept",
	}
	got := kindsOf(t, lines)
	want := []kind{kindHeader, kindHeader, kindHunk, kindMinus, kindContext}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("line %d %q: kind = %d, want %d", i, lines[i], got[i], w)
		}
	}
}

// Omitted counts default to 1; a zero old count means an all-added hunk.
func TestHunkHeaderCounts(t *testing.T) {
	cases := []struct {
		line       string
		oldN, newN int
		ok         bool
	}{
		{"@@ -1,4 +1,5 @@", 4, 5, true},
		{"@@ -1 +1,2 @@", 1, 2, true},
		{"@@ -0,0 +1,3 @@", 0, 3, true},
		{"@@ -5,2 +5 @@ context", 2, 1, true},
		{"@@ not a hunk @@", 0, 0, false},
		{"@@ -1,x +1 @@", 0, 0, false},
	}
	for _, c := range cases {
		o, n, ok := parseHunkHeader(c.line)
		if o != c.oldN || n != c.newN || ok != c.ok {
			t.Errorf("parseHunkHeader(%q) = %d, %d, %v; want %d, %d, %v", c.line, o, n, ok, c.oldN, c.newN, c.ok)
		}
	}
}

func TestAllAddedHunk(t *testing.T) {
	lines := []string{
		"@@ -0,0 +1,2 @@",
		"+first",
		"+second",
		"unrelated trailing prose",
	}
	got := kindsOf(t, lines)
	want := []kind{kindHunk, kindPlus, kindPlus, kindText}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("line %d: kind = %d, want %d", i, got[i], w)
		}
	}
}

// The "\ No newline at end of file" marker counts toward neither side and
// classifies as meta wherever it appears.
func TestNoNewlineMarker(t *testing.T) {
	lines := []string{
		"@@ -1,1 +1,1 @@",
		"-old",
		`\ No newline at end of file`,
		"+new",
		`\ No newline at end of file`,
	}
	got := kindsOf(t, lines)
	want := []kind{kindHunk, kindMinus, kindMeta, kindPlus, kindMeta}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("line %d: kind = %d, want %d", i, got[i], w)
		}
	}
}

// A truncated hunk body falls back to ordinary classification without
// swallowing the rest of the buffer.
func TestMalformedBodyRecovers(t *testing.T) {
	lines := []string{
		"@@ -1,5 +1,5 @@",
		" context",
		"diff --git a/x b/x", // hunk promised 5 lines but a new file starts
		"index 1111111..2222222 100644",
	}
	got := kindsOf(t, lines)
	want := []kind{kindHunk, kindContext, kindHeader, kindMeta}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("line %d: kind = %d, want %d", i, got[i], w)
		}
	}
}

func TestFolds(t *testing.T) {
	folds := Folds(gitPatch)
	want := []lang.FoldRange{
		{HeaderLine: 0, EndLine: 9},  // first file section
		{HeaderLine: 4, EndLine: 9},  // its hunk
		{HeaderLine: 10, EndLine: 13}, // rename-only file section
	}
	if len(folds) != len(want) {
		t.Fatalf("Folds = %v, want %v", folds, want)
	}
	for i, w := range want {
		if folds[i] != w {
			t.Errorf("fold %d = %v, want %v", i, folds[i], w)
		}
	}
	// Pre-order: outer file section before its hunks.
	for i := 1; i < len(folds); i++ {
		if folds[i].HeaderLine <= folds[i-1].HeaderLine {
			t.Errorf("folds not in pre-order: %v", folds)
		}
	}
}

// A trailing no-newline marker stays inside its hunk's fold.
func TestFoldIncludesTrailingNoNewline(t *testing.T) {
	lines := []string{
		"@@ -1,1 +1,1 @@",
		"-old",
		"+new",
		`\ No newline at end of file`,
	}
	folds := Folds(lines)
	if len(folds) != 1 || folds[0] != (lang.FoldRange{HeaderLine: 0, EndLine: 3}) {
		t.Fatalf("Folds = %v, want [{0 3}]", folds)
	}
}

func spanAt(spans []lang.Span, line, col int) (lang.Span, bool) {
	for _, s := range spans {
		if s.Line == line && col >= s.StartCol && col < s.EndCol {
			return s, true
		}
	}
	return lang.Span{}, false
}

func TestSpansLineColoring(t *testing.T) {
	spans := Spans(gitPatch)
	cases := []struct {
		line    int
		capture string
	}{
		{0, CaptureHeader},
		{1, CaptureMeta},
		{2, CaptureHeader},
		{3, CaptureHeader},
		{4, CaptureDelta},
		{6, CaptureMinus},
		{7, CapturePlus},
		{12, CaptureMeta},
	}
	for _, c := range cases {
		s, ok := spanAt(spans, c.line, 0)
		if !ok || s.Capture != c.capture {
			t.Errorf("line %d: capture = %q (found %v), want %q", c.line, s.Capture, ok, c.capture)
		}
		if ok && s.EndCol != len([]rune(gitPatch[c.line])) {
			t.Errorf("line %d: EndCol = %d, want full line %d", c.line, s.EndCol, len([]rune(gitPatch[c.line])))
		}
	}
	// Context lines carry no span.
	if s, ok := spanAt(spans, 5, 0); ok {
		t.Errorf("context line has span %v", s)
	}
}

// Word-level emphasis pairs the i-th removed with the i-th added line of a
// run and marks the changed range, shifted one column for the marker.
func TestWordSpans(t *testing.T) {
	lines := []string{
		"@@ -1,2 +1,2 @@",
		"-foo bar baz",
		"-unpaired removed extra",
		"+foo qux baz",
	}
	spans := Spans(lines)
	// "bar" sits at content runes [4,7) => line columns [5,8).
	s, ok := spanAt(spans, 1, 5)
	if !ok || s.Capture != MinusEmph || s.StartCol != 5 || s.EndCol != 8 {
		t.Errorf("minus emph = %v (found %v), want [5,8) %s", s, ok, MinusEmph)
	}
	s, ok = spanAt(spans, 3, 5)
	if !ok || s.Capture != PlusEmph || s.StartCol != 5 || s.EndCol != 8 {
		t.Errorf("plus emph = %v (found %v), want [5,8) %s", s, ok, PlusEmph)
	}
	// Emphasis spans precede the whole-line spans, so first-covering-wins
	// resolves the emphasized cells to the emph capture.
	if s, _ := spanAt(spans, 1, 0); s.Capture != CaptureMinus {
		t.Errorf("marker column capture = %q, want %q", s.Capture, CaptureMinus)
	}
	// The unpaired second removed line gets no emphasis.
	for _, s := range spans {
		if s.Line == 2 && strings.HasSuffix(s.Capture, ".emph") {
			t.Errorf("unpaired removed line has emph span %v", s)
		}
	}
}

func TestWordSpansToggle(t *testing.T) {
	SetWordHighlight(false)
	defer SetWordHighlight(true)
	spans := Spans([]string{"@@ -1,1 +1,1 @@", "-foo bar", "+foo qux"})
	for _, s := range spans {
		if strings.HasSuffix(s.Capture, ".emph") {
			t.Errorf("emph span %v with word highlight off", s)
		}
	}
}

// The no-newline marker between a removed run and its added run keeps the
// pairing intact.
func TestWordSpansAcrossNoNewline(t *testing.T) {
	lines := []string{
		"@@ -1,1 +1,1 @@",
		"-foo bar",
		`\ No newline at end of file`,
		"+foo qux",
	}
	spans := Spans(lines)
	if _, ok := spanAt(spans, 3, 5); !ok {
		t.Errorf("no plus emph span across no-newline marker: %v", spans)
	}
}
