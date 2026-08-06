//go:build cgo

package langpython

import (
	"strings"
	"testing"

	"ike/internal/highlight"
)

func TestFragmentsSQLString(t *testing.T) {
	lines := []string{
		`import db`,
		``,
		`q = "SELECT id, name FROM users WHERE id = ?"`,
	}
	frags := highlight.Fragments("python", lines)
	if len(frags) != 1 {
		t.Fatalf("Fragments = %d fragments, want 1: %+v", len(frags), frags)
	}
	f := frags[0]
	if f.Lang != "sql" {
		t.Errorf("Lang = %q, want sql", f.Lang)
	}
	if f.StartLine != 2 || f.EndLine != 2 {
		t.Errorf("lines = %d..%d, want 2..2", f.StartLine, f.EndLine)
	}
	wantContent := `SELECT id, name FROM users WHERE id = ?`
	if got := strings.Join(f.Lines, "\n"); got != wantContent {
		t.Errorf("content = %q, want %q", got, wantContent)
	}
	// Content starts right after the opening quote and ends before the closing one.
	if want := len(`q = "`); f.StartCol != want {
		t.Errorf("StartCol = %d, want %d", f.StartCol, want)
	}
	if want := len(lines[2]) - 1; f.EndCol != want {
		t.Errorf("EndCol = %d, want %d", f.EndCol, want)
	}
	// The fragment text must be exactly the host text in its range.
	if got := lines[2][f.StartCol:f.EndCol]; got != wantContent {
		t.Errorf("host range = %q, want %q", got, wantContent)
	}
}

func TestFragmentsTripleQuoted(t *testing.T) {
	lines := []string{
		`q = """`,
		`SELECT *`,
		`FROM users`,
		`"""`,
	}
	frags := highlight.Fragments("python", lines)
	if len(frags) != 1 {
		t.Fatalf("Fragments = %d fragments, want 1: %+v", len(frags), frags)
	}
	f := frags[0]
	if f.Lang != "sql" {
		t.Errorf("Lang = %q, want sql", f.Lang)
	}
	if f.StartLine != 0 || f.EndLine != 3 {
		t.Errorf("lines = %d..%d, want 0..3", f.StartLine, f.EndLine)
	}
	want := "\nSELECT *\nFROM users\n"
	if got := strings.Join(f.Lines, "\n"); got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestFragmentsPlainStringIgnored(t *testing.T) {
	lines := []string{`msg = "hello there, general"`}
	if frags := highlight.Fragments("python", lines); len(frags) != 0 {
		t.Fatalf("plain string produced fragments: %+v", frags)
	}
}

// TestFragmentsHTMLTripleQuoted (#1625): a triple-quoted HTML template
// becomes an html fragment via the tag-shape heuristic.
func TestFragmentsHTMLTripleQuoted(t *testing.T) {
	lines := []string{
		`page = """`,
		`<html>`,
		`  <body><p>hi</p></body>`,
		`</html>`,
		`"""`,
	}
	frags := highlight.Fragments("python", lines)
	if len(frags) != 1 {
		t.Fatalf("Fragments = %d fragments, want 1: %+v", len(frags), frags)
	}
	f := frags[0]
	if f.Lang != "html" {
		t.Errorf("Lang = %q, want html", f.Lang)
	}
	if f.StartLine != 0 || f.EndLine != 4 {
		t.Errorf("lines = %d..%d, want 0..4", f.StartLine, f.EndLine)
	}
	want := "\n<html>\n  <body><p>hi</p></body>\n</html>\n"
	if got := strings.Join(f.Lines, "\n"); got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

// TestFragmentsAngleBracketStringIgnored: incidental angle brackets don't
// pass the HTML heuristic.
func TestFragmentsAngleBracketStringIgnored(t *testing.T) {
	lines := []string{
		`a = "<nil>"`,
		`b = "x < y > z"`,
	}
	if frags := highlight.Fragments("python", lines); len(frags) != 0 {
		t.Fatalf("angle-bracket strings produced fragments: %+v", frags)
	}
}

// Regex call-site injections (#1631): the first string argument of the
// re-module matchers becomes a fragment.regex fragment.

func TestFragmentsReCompile(t *testing.T) {
	lines := []string{
		`import re`,
		``,
		`pat = re.compile(r"^[a-z]+$")`,
	}
	frags := highlight.Fragments("python", lines)
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
	if want := len(`pat = re.compile(r"`); f.StartCol != want {
		t.Errorf("StartCol = %d, want %d", f.StartCol, want)
	}
}

func TestFragmentsReSubFirstArg(t *testing.T) {
	lines := []string{
		`import re`,
		``,
		`out = re.sub(r"\d+", "N", text)`,
	}
	frags := highlight.Fragments("python", lines)
	if len(frags) != 1 {
		t.Fatalf("Fragments = %d fragments, want 1: %+v", len(frags), frags)
	}
	if frags[0].Lang != "regex" {
		t.Errorf("Lang = %q, want regex", frags[0].Lang)
	}
	if got := strings.Join(frags[0].Lines, "\n"); got != `\d+` {
		t.Errorf("content = %q, want \\d+", got)
	}
}

func TestFragmentsNonReCallIgnored(t *testing.T) {
	lines := []string{
		`import os`,
		``,
		`p = os.path.join("a[b]", "c")`,
		`q = re.escape("[x]")`,
	}
	if frags := highlight.Fragments("python", lines); len(frags) != 0 {
		t.Fatalf("non-matcher calls produced fragments: %+v", frags)
	}
}
