package highlight

import (
	"strings"
	"testing"

	"ike/internal/lang"
)

// segments_test.go covers the grammar-free parts of the completion index
// view (#2652): fragment resolution through a region detector, the
// innermost-fragment pick, and code-text masking. The grammar-backed path is
// exercised with real grammars in internal/complete/words and /symbols.

func init() {
	lang.Register(lang.Language{ID: "seghost", Extensions: []string{"segh"}, Regions: func(lines []string) []lang.Region {
		return []lang.Region{
			{Lang: "segembed", StartLine: 1, StartCol: 2, EndLine: 2, EndCol: 3},
			{Lang: "regex", StartLine: 3, StartCol: 0, EndLine: 3, EndCol: 4},
			{Lang: "seginline", StartLine: 4, StartCol: 0, EndLine: 4, EndCol: 4},
		}
	}})
	lang.Register(lang.Language{ID: "segembed", Extensions: []string{"sege"}})
	// An internal grammar: no extensions, like markdown_inline.
	lang.Register(lang.Language{ID: "seginline"})
}

var segLines = []string{"host0", "h emb", "edd rest", "rgx", "inln"}

func TestEmbeddedResolvesRegions(t *testing.T) {
	got := Embedded("seghost", segLines)
	if len(got) != 3 || got[0].Lang != "segembed" || got[1].Lang != "regex" || got[2].Lang != "seginline" {
		t.Fatalf("Embedded = %+v, want the three regions", got)
	}
	if got[0].StartLine != 1 || got[0].StartCol != 2 || got[0].EndLine != 2 || got[0].EndCol != 3 {
		t.Fatalf("region coords = %+v", got[0])
	}
	if langs := EmbeddedLangs("seghost", segLines); len(langs) != 3 {
		t.Fatalf("EmbeddedLangs = %v", langs)
	}
	if Embedded("nosuch", segLines) != nil {
		t.Fatal("an unregistered host has no fragments")
	}
}

func TestEmbeddedAtPicksBufferLanguages(t *testing.T) {
	for _, tc := range []struct {
		line, col int
		want      string
		ok        bool
	}{
		{0, 3, "", false},
		{1, 1, "", false},        // before the region's start column
		{1, 2, "segembed", true}, // region start
		{2, 3, "segembed", true}, // region end, inclusive
		{2, 4, "", false},
		{3, 2, "", false}, // regex: not a registered language
		{4, 2, "", false}, // seginline: no extensions, not a buffer language
	} {
		f, ok := EmbeddedAt("seghost", segLines, tc.line, tc.col)
		if ok != tc.ok || f.Lang != tc.want {
			t.Errorf("(%d,%d) = (%q,%v), want (%q,%v)", tc.line, tc.col, f.Lang, ok, tc.want, tc.ok)
		}
	}
}

func TestInnermostAtPrefersDeepest(t *testing.T) {
	lang.Register(lang.Language{ID: "segouter", Extensions: []string{"sego"}})
	frags := []Fragment{
		{Lang: "segouter", StartLine: 0, StartCol: 0, EndLine: 5, EndCol: 0},
		{Lang: "segembed", StartLine: 2, StartCol: 0, EndLine: 3, EndCol: 0},
	}
	if f, ok := InnermostAt(frags, 2, 1); !ok || f.Lang != "segembed" {
		t.Fatalf("nested = (%+v,%v), want the inner fragment", f, ok)
	}
	if f, ok := InnermostAt(frags, 1, 0); !ok || f.Lang != "segouter" {
		t.Fatalf("outer = (%+v,%v), want the outer fragment", f, ok)
	}
}

func TestCodeLanguageRules(t *testing.T) {
	if CodeLanguage("seghost") {
		t.Error("a language without a grammar is not a code language")
	}
	if CodeLanguage("nosuch") {
		t.Error("an unregistered id is not a code language")
	}
}

func TestSegmentCodeTextMasks(t *testing.T) {
	lines := []string{`x := "str" // c`, `y := 1`, `z`}
	seg := Segment{
		Lang: "t", StartLine: 0, StartCol: 0, EndLine: 2, EndCol: 1,
		Masked: []Span{
			{Line: 0, StartCol: 5, EndCol: 10, Capture: "string"},
			{Line: 0, StartCol: 11, EndCol: 15, Capture: "comment"},
		},
	}
	got := seg.CodeText(lines)
	want := "x :=" + strings.Repeat(" ", 11) + "\ny := 1\nz"
	if got != want {
		t.Fatalf("CodeText = %q, want %q", got, want)
	}
	// A fragment segment keeps its own columns on the first and last line.
	frag := Segment{Lang: "t", StartLine: 0, StartCol: 5, EndLine: 1, EndCol: 4}
	if got := frag.CodeText(lines); got != `"str" // c`+"\ny :=" {
		t.Fatalf("fragment CodeText = %q", got)
	}
}

func TestChildRegionsMaskNestedFragments(t *testing.T) {
	lines := []string{"aaaa", "bbbb", "cccc"}
	frags := []deepFragment{
		{Fragment: Fragment{Lang: "x", StartLine: 0, StartCol: 2, EndLine: 2, EndCol: 1}, depth: 1, parent: -1},
		{Fragment: Fragment{Lang: "y", StartLine: 1, StartCol: 0, EndLine: 1, EndCol: 2}, depth: 2, parent: 0},
	}
	got := childRegions(frags, -1, lines)
	want := []Span{
		{Line: 0, StartCol: 2, EndCol: 4, Capture: "fragment"},
		{Line: 1, StartCol: 0, EndCol: 4, Capture: "fragment"},
		{Line: 2, StartCol: 0, EndCol: 1, Capture: "fragment"},
	}
	if len(got) != len(want) {
		t.Fatalf("childRegions = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("region %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if nested := childRegions(frags, 0, lines); len(nested) != 1 || nested[0].Line != 1 || nested[0].EndCol != 2 {
		t.Fatalf("nested childRegions = %+v", nested)
	}
}
