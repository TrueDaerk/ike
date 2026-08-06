//go:build cgo

package langweb

import (
	"strings"
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// Template-literal injections (#1625): the TSX grammar's injection query
// marks string_fragment chunks as HTML/SQL fragments when the joined text
// passes the content heuristic.

func TestFragmentsHTMLTemplateLiteral(t *testing.T) {
	lines := []string{
		"const page = `",
		"<html>",
		"  <body><p>hi</p></body>",
		"</html>",
		"`;",
	}
	frags := highlight.Fragments("typescript", lines)
	if len(frags) != 1 {
		t.Fatalf("Fragments = %d fragments, want 1: %+v", len(frags), frags)
	}
	f := frags[0]
	if f.Lang != "html" {
		t.Errorf("Lang = %q, want html", f.Lang)
	}
	want := "\n<html>\n  <body><p>hi</p></body>\n</html>\n"
	if got := strings.Join(f.Lines, "\n"); got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

// TestFragmentsHTMLTemplateWithSubstitution: ${…} splits the template into
// chunks; the heuristic judges them joined (#1625) — no single chunk of this
// template looks like HTML on its own — and each chunk injects while the
// substitution expression stays TypeScript.
func TestFragmentsHTMLTemplateWithSubstitution(t *testing.T) {
	lines := []string{
		"const row = `<tr><td>${name}</td></tr>`;",
	}
	frags := highlight.Fragments("typescript", lines)
	if len(frags) != 2 {
		t.Fatalf("Fragments = %d fragments, want 2: %+v", len(frags), frags)
	}
	if frags[0].Lang != "html" || frags[1].Lang != "html" {
		t.Fatalf("Langs = %q/%q, want html/html", frags[0].Lang, frags[1].Lang)
	}
	first := lines[0][frags[0].StartCol:frags[0].EndCol]
	second := lines[0][frags[1].StartCol:frags[1].EndCol]
	if first != "<tr><td>" || second != "</td></tr>" {
		t.Errorf("chunks = %q + %q, want %q + %q", first, second, "<tr><td>", "</td></tr>")
	}
}

func TestFragmentsSQLTemplateLiteral(t *testing.T) {
	lines := []string{
		"const q = `SELECT id FROM users WHERE id = $1`;",
	}
	frags := highlight.Fragments("typescript", lines)
	if len(frags) != 1 {
		t.Fatalf("Fragments = %d fragments, want 1: %+v", len(frags), frags)
	}
	f := frags[0]
	if f.Lang != "sql" {
		t.Errorf("Lang = %q, want sql", f.Lang)
	}
	wantContent := "SELECT id FROM users WHERE id = $1"
	if got := strings.Join(f.Lines, "\n"); got != wantContent {
		t.Errorf("content = %q, want %q", got, wantContent)
	}
	if got := lines[0][f.StartCol:f.EndCol]; got != wantContent {
		t.Errorf("host range = %q, want %q", got, wantContent)
	}
}

func TestFragmentsPlainTemplateIgnored(t *testing.T) {
	lines := []string{
		"const s = `hello ${name}, welcome`;",
		"const t = `x < y > z`;",
	}
	if frags := highlight.Fragments("typescript", lines); len(frags) != 0 {
		t.Fatalf("plain templates produced fragments: %+v", frags)
	}
}

// TestTemplateInjectionComposes: end-to-end via Highlight — HTML tag names
// inside the template highlight with the HTML grammar while the substitution
// expression keeps its TypeScript capture.
func TestTemplateInjectionComposes(t *testing.T) {
	if _, ok := lang.ByID("html"); !ok {
		t.Fatal("html not registered")
	}
	lines := []string{
		"const row = `<tr><td>${name}</td></tr>`;",
	}
	spans := highlight.Highlight("row.ts", lines)
	ix := highlight.NewIndex(spans)
	col := strings.Index(lines[0], "tr>")
	if got := ix.CaptureAt(0, col); got != "tag" {
		t.Errorf("tag name: got capture %q, want tag", got)
	}
	// The chunk after the substitution injects too. Parsed standalone it is
	// all end tags — the grammar cannot pair them, so the tag names stay
	// uncaptured (the host's string shows through) but the tag punctuation
	// still colours as HTML.
	col = strings.Index(lines[0], "</td>")
	if got := ix.CaptureAt(0, col); got != "punctuation.bracket" {
		t.Errorf("end-tag punctuation after substitution: got capture %q, want punctuation.bracket", got)
	}
	// The keyword outside the template stays TypeScript.
	if got := ix.CaptureAt(0, 0); got != "keyword" {
		t.Errorf("const: got capture %q, want keyword", got)
	}
}

// Regex injections (#1631): /…/ literals and RegExp construction inject the
// built-in regex mini-grammar.

func TestFragmentsRegexLiteral(t *testing.T) {
	lines := []string{
		`const re = /^[a-z]+$/gi;`,
	}
	frags := highlight.Fragments("typescript", lines)
	if len(frags) != 1 {
		t.Fatalf("Fragments = %d fragments, want 1: %+v", len(frags), frags)
	}
	f := frags[0]
	if f.Lang != "regex" {
		t.Errorf("Lang = %q, want regex", f.Lang)
	}
	if got := strings.Join(f.Lines, "\n"); got != `^[a-z]+$` {
		t.Errorf("content = %q, want ^[a-z]+$", got)
	}
	if want := len(`const re = /`); f.StartCol != want {
		t.Errorf("StartCol = %d, want %d", f.StartCol, want)
	}
}

func TestFragmentsNewRegExp(t *testing.T) {
	lines := []string{
		`const re = new RegExp("a|b", "g");`,
	}
	frags := highlight.Fragments("typescript", lines)
	if len(frags) != 1 {
		t.Fatalf("Fragments = %d fragments, want 1: %+v", len(frags), frags)
	}
	if frags[0].Lang != "regex" {
		t.Errorf("Lang = %q, want regex", frags[0].Lang)
	}
	if got := strings.Join(frags[0].Lines, "\n"); got != `a|b` {
		t.Errorf("content = %q, want a|b", got)
	}
}

func TestFragmentsBareRegExpCall(t *testing.T) {
	lines := []string{
		`const re = RegExp("x+");`,
	}
	frags := highlight.Fragments("typescript", lines)
	if len(frags) != 1 {
		t.Fatalf("Fragments = %d fragments, want 1: %+v", len(frags), frags)
	}
	if frags[0].Lang != "regex" {
		t.Errorf("Lang = %q, want regex", frags[0].Lang)
	}
}

func TestFragmentsPlainStringNotRegex(t *testing.T) {
	lines := []string{
		`const s = "a|b";`,
		`const t = other("x+");`,
	}
	if frags := highlight.Fragments("typescript", lines); len(frags) != 0 {
		t.Fatalf("plain strings produced fragments: %+v", frags)
	}
}
