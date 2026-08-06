//go:build cgo

package langgo

import (
	"strings"
	"testing"

	"ike/internal/highlight"
)

func TestFragmentsSQLRawString(t *testing.T) {
	lines := []string{
		`package main`,
		``,
		"var q = `SELECT id, name FROM users WHERE id = ?`",
	}
	frags := highlight.Fragments("go", lines)
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
	// Content starts right after the opening backtick and ends before the closing one.
	if want := len("var q = `"); f.StartCol != want {
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

func TestFragmentsSQLInterpretedString(t *testing.T) {
	lines := []string{
		`package main`,
		``,
		`var q = "SELECT id FROM users"`,
	}
	frags := highlight.Fragments("go", lines)
	if len(frags) != 1 {
		t.Fatalf("Fragments = %d fragments, want 1: %+v", len(frags), frags)
	}
	f := frags[0]
	if f.Lang != "sql" {
		t.Errorf("Lang = %q, want sql", f.Lang)
	}
	wantContent := `SELECT id FROM users`
	if got := strings.Join(f.Lines, "\n"); got != wantContent {
		t.Errorf("content = %q, want %q", got, wantContent)
	}
}

func TestFragmentsMultilineRawString(t *testing.T) {
	lines := []string{
		"package main",
		"",
		"var q = `",
		"SELECT *",
		"FROM users",
		"`",
	}
	frags := highlight.Fragments("go", lines)
	if len(frags) != 1 {
		t.Fatalf("Fragments = %d fragments, want 1: %+v", len(frags), frags)
	}
	f := frags[0]
	if f.Lang != "sql" {
		t.Errorf("Lang = %q, want sql", f.Lang)
	}
	if f.StartLine != 2 || f.EndLine != 5 {
		t.Errorf("lines = %d..%d, want 2..5", f.StartLine, f.EndLine)
	}
	want := "\nSELECT *\nFROM users\n"
	if got := strings.Join(f.Lines, "\n"); got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestFragmentsPlainStringIgnored(t *testing.T) {
	lines := []string{
		`package main`,
		``,
		`var msg = "hello there, general"`,
		"var raw = `not sql either`",
	}
	if frags := highlight.Fragments("go", lines); len(frags) != 0 {
		t.Fatalf("plain strings produced fragments: %+v", frags)
	}
}

// Regex call-site injections (#1631): the first argument of regexp compile
// calls becomes a fragment.regex fragment for the built-in mini-grammar.

func TestFragmentsRegexpMustCompile(t *testing.T) {
	lines := []string{
		`package main`,
		``,
		`import "regexp"`,
		``,
		"var re = regexp.MustCompile(`^[a-z]+$`)",
	}
	frags := highlight.Fragments("go", lines)
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
	if want := len("var re = regexp.MustCompile(`"); f.StartCol != want {
		t.Errorf("StartCol = %d, want %d", f.StartCol, want)
	}
}

func TestFragmentsRegexpCompileInterpreted(t *testing.T) {
	lines := []string{
		`package main`,
		``,
		`import "regexp"`,
		``,
		`var re, _ = regexp.CompilePOSIX("a|b")`,
	}
	frags := highlight.Fragments("go", lines)
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

func TestFragmentsNonRegexpCallIgnored(t *testing.T) {
	lines := []string{
		`package main`,
		``,
		`var s = other.MustCompile("^[a-z]+$")`,
		`var t = regexp.QuoteMeta("^[a-z]+$")`,
	}
	if frags := highlight.Fragments("go", lines); len(frags) != 0 {
		t.Fatalf("non-compile calls produced fragments: %+v", frags)
	}
}

func TestFragmentsRegexpSecondArgIgnored(t *testing.T) {
	lines := []string{
		`package main`,
		``,
		`var s = regexp.MustCompile(pat)`,
	}
	if frags := highlight.Fragments("go", lines); len(frags) != 0 {
		t.Fatalf("non-literal first arg produced fragments: %+v", frags)
	}
}
