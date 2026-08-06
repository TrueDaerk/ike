package highlight

import (
	"testing"

	"ike/internal/lang"
)

func TestFragmentCapture(t *testing.T) {
	cases := []struct {
		name  string
		lang  string
		guess bool
		ok    bool
	}{
		{"fragment.sql", "sql", false, true},
		{"fragment.sql.guess", "sql", true, true},
		{"fragment.css", "css", false, true},
		{"fragment.", "", false, false},
		{"fragment", "", false, false},
		{"string.special", "", false, false},
	}
	for _, c := range cases {
		lang, guess, ok := fragmentCapture(c.name)
		if lang != c.lang || guess != c.guess || ok != c.ok {
			t.Errorf("fragmentCapture(%q) = (%q, %v, %v), want (%q, %v, %v)",
				c.name, lang, guess, ok, c.lang, c.guess, c.ok)
		}
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
