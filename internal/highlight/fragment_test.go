package highlight

import (
	"testing"

	"ike/internal/lang"
)

func TestFragmentCapture(t *testing.T) {
	cases := []struct {
		name string
		lang string
		mode string
		ok   bool
	}{
		{"fragment.sql", "sql", modePlain, true},
		{"fragment.sql.guess", "sql", modeGuess, true},
		{"fragment.css", "css", modePlain, true},
		{"fragment.css.partial", "css", modePartial, true},
		{"fragment.typescript.partial", "typescript", modePartial, true},
		// An unknown mode weakens the rule to a plain fragment rather than
		// dropping it, so a typo still highlights something.
		{"fragment.sql.wat", "sql", modePlain, true},
		{"fragment.", "", modePlain, false},
		{"fragment", "", modePlain, false},
		{"string.special", "", modePlain, false},
	}
	for _, c := range cases {
		lang, mode, ok := fragmentCapture(c.name)
		if lang != c.lang || mode != c.mode || ok != c.ok {
			t.Errorf("fragmentCapture(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.name, lang, mode, ok, c.lang, c.mode, c.ok)
		}
	}
}

// TestFragmentWrapper: only languages whose snippets do not stand alone
// register a wrapper (#2329).
func TestFragmentWrapper(t *testing.T) {
	prefix, suffix, ok := fragmentWrapper("css")
	if !ok || prefix != "*{" || suffix != "}" {
		t.Errorf("fragmentWrapper(css) = (%q, %q, %v), want (\"*{\", \"}\", true)", prefix, suffix, ok)
	}
	if _, _, ok := fragmentWrapper("typescript"); ok {
		t.Error("fragmentWrapper(typescript) = ok, want none: a handler body is already valid TS")
	}
}

// TestWrapFragment covers the wrapper's line arithmetic: content lines shift
// by exactly one and columns never move, and a fragment that is not partial
// (or whose language has no wrapper) parses unchanged.
func TestWrapFragment(t *testing.T) {
	f := Fragment{Lang: "css", Lines: []string{"color:", "  red"}, Partial: true}
	lines, wrapped := wrapFragment(f)
	if !wrapped {
		t.Fatal("wrapFragment: wrapped = false, want true")
	}
	want := []string{"*{", "color:", "  red", "}"}
	if len(lines) != len(want) {
		t.Fatalf("wrapFragment = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("wrapFragment = %q, want %q", lines, want)
		}
	}

	for _, plain := range []Fragment{
		{Lang: "css", Lines: []string{"a{}"}},
		{Lang: "typescript", Lines: []string{"f()"}, Partial: true},
	} {
		if got, wrapped := wrapFragment(plain); wrapped || len(got) != 1 {
			t.Errorf("wrapFragment(%+v) = (%q, %v), want the lines unchanged", plain, got, wrapped)
		}
	}
}

// TestUnwrapSpans: spans on the wrapper's own lines vanish, content spans
// shift up one line and keep their columns.
func TestUnwrapSpans(t *testing.T) {
	spans := []Span{
		{Line: 0, StartCol: 0, EndCol: 1, Capture: "tag"},               // the "*" of the wrapper
		{Line: 1, StartCol: 0, EndCol: 5, Capture: "property"},          // content
		{Line: 2, StartCol: 2, EndCol: 5, Capture: "number"},            // content
		{Line: 3, StartCol: 0, EndCol: 1, Capture: "punctuation.brack"}, // the "}" of the wrapper
	}
	got := unwrapSpans(spans, 2)
	if len(got) != 2 {
		t.Fatalf("unwrapSpans = %+v, want 2 spans", got)
	}
	if got[0].Line != 0 || got[0].Capture != "property" || got[0].StartCol != 0 {
		t.Errorf("first span = %+v, want line 0 col 0 property", got[0])
	}
	if got[1].Line != 1 || got[1].Capture != "number" || got[1].StartCol != 2 {
		t.Errorf("second span = %+v, want line 1 col 2 number", got[1])
	}
}

// TestUnwrapFolds: the synthetic rule's own fold is anchored on the wrapper's
// header line and disappears with it; a fold inside the content survives,
// clamped to the content's last line.
func TestUnwrapFolds(t *testing.T) {
	folds := []Fold{
		{HeaderLine: 0, EndLine: 3}, // the synthetic *{…} rule
		{HeaderLine: 1, EndLine: 2}, // a fold within the content
	}
	got := unwrapFolds(folds, 2)
	if len(got) != 1 {
		t.Fatalf("unwrapFolds = %+v, want 1 fold", got)
	}
	if got[0].HeaderLine != 0 || got[0].EndLine != 1 {
		t.Errorf("fold = %+v, want {0, 1}", got[0])
	}
}

func TestLooksLikeSQL(t *testing.T) {
	yes := []string{
		"SELECT * FROM users",
		"  select id from t",
		"\n\tINSERT INTO t VALUES (1)",
		"WITH cte AS (SELECT 1) SELECT * FROM cte",
		"DELETE\nFROM t",
		"UPDATE t SET a = 1",
	}
	no := []string{
		"",
		"   ",
		"hello world",
		"SELECTION bias",   // keyword must end at a word boundary
		"WITHDRAW money",   // ditto
		"creates a widget", // lower-case prose, keyword not a prefix token
	}
	for _, s := range yes {
		if !looksLikeSQL(s) {
			t.Errorf("looksLikeSQL(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if looksLikeSQL(s) {
			t.Errorf("looksLikeSQL(%q) = true, want false", s)
		}
	}
}

func TestGuessFragmentUnknownLang(t *testing.T) {
	if guessFragment("nosuchlang", "SELECT 1") {
		t.Error("unknown guess language must never match")
	}
}

func TestFragmentsUnknownLanguage(t *testing.T) {
	if got := Fragments("nosuchlang", []string{"SELECT 1"}); got != nil {
		t.Errorf("Fragments(nosuchlang) = %v, want nil", got)
	}
}

// TestRegionDetectorWinsOverInjections guards #1303: a host language may
// resolve its embedded regions in Go when an injection query cannot express
// the rule, and the fragment's Lines are exactly the host text in that range.
func TestRegionDetectorWinsOverInjections(t *testing.T) {
	lang.Register(lang.Language{ID: "json", Extensions: []string{"json"}})
	lang.Register(lang.Language{
		ID:         "regionhost",
		Extensions: []string{"regionhost"},
		Regions: func(lines []string) []lang.Region {
			return []lang.Region{{Lang: "json", StartLine: 1, EndLine: 2, EndCol: len(lines[2])}}
		},
	})
	lines := []string{"header", `{"a": 1,`, `"b": 2}`, "trailer"}
	got := Fragments("regionhost", lines)
	if len(got) != 1 {
		t.Fatalf("fragments = %+v, want one", got)
	}
	f := got[0]
	if f.Lang != "json" || f.StartLine != 1 || f.EndLine != 2 {
		t.Fatalf("fragment = %+v, want json over lines 1–2", f)
	}
	if len(f.Lines) != 2 || f.Lines[0] != lines[1] || f.Lines[1] != lines[2] {
		t.Fatalf("fragment lines = %q, want the host text verbatim", f.Lines)
	}
}

// TestRegionsOutsideTheBufferAreDropped: a detector may report optimistically;
// the consumer clamps or drops rather than panicking.
func TestRegionsOutsideTheBufferAreDropped(t *testing.T) {
	lang.Register(lang.Language{ID: "json", Extensions: []string{"json"}})
	lang.Register(lang.Language{
		ID: "sloppyhost", Extensions: []string{"sloppyhost"},
		Regions: func([]string) []lang.Region {
			return []lang.Region{
				{Lang: "json", StartLine: 99, EndLine: 120},
				{Lang: "json", StartLine: 0, EndLine: 99},
				{Lang: "", StartLine: 0, EndLine: 0},
			}
		},
	})
	got := Fragments("sloppyhost", []string{"a", "b"})
	if len(got) != 1 || got[0].EndLine != 1 {
		t.Fatalf("fragments = %+v, want one clamped to the buffer", got)
	}
}

func TestLooksLikeHTML(t *testing.T) {
	cases := []struct {
		content string
		want    bool
	}{
		{"<!DOCTYPE html><html></html>", true},
		{"<!doctype html>", true},
		{"<!-- comment -->", true},
		{"<div class=\"x\">hi</div>", true},
		{"  \n\t<ul>\n  <li>a</li>\n</ul>", true},
		{"<br/>", true},
		{"<nil>", false},     // no closing marker
		{"<br>", false},      // lone void tag, no closing marker
		{"a < b > c", false}, // does not open with a tag
		{"</div>", false},    // closing tag first is not a document
		{"<3 you", false},    // tag name must be a letter
		{"SELECT * FROM users", false},
		{"", false},
		{"  ", false},
	}
	for _, c := range cases {
		if got := looksLikeHTML(c.content); got != c.want {
			t.Errorf("looksLikeHTML(%q) = %v, want %v", c.content, got, c.want)
		}
	}
}

func TestGuessFragmentHTML(t *testing.T) {
	if !guessFragment("html", "<p>hi</p>") {
		t.Error("guessFragment(html) rejected an HTML snippet")
	}
	if guessFragment("html", "plain text") {
		t.Error("guessFragment(html) accepted plain text")
	}
}
