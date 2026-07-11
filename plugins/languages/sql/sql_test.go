//go:build cgo

package langsql

import (
	"testing"

	"ike/internal/highlight"

	// Registers Python, the host language of the injection test below.
	_ "ike/plugins/languages/python"
)

// The package init() registers SQL, so highlight.Highlight resolves the grammar.
func TestSQLHighlighting(t *testing.T) {
	lines := []string{
		"-- users by id",
		"SELECT id, name FROM users WHERE id = 1;",
	}
	spans := highlight.Highlight("query.sql", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for SQL source, got none")
	}
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 0); got != "comment" {
		t.Errorf("comment line: got capture %q", got)
	}
	if got := ix.CaptureAt(1, 0); got != "keyword" { // "SELECT"
		t.Errorf("SELECT keyword: got capture %q", got)
	}
}

// TestInjectedSQLHighlighting is the issue #299 end-to-end path: a Python
// buffer with an SQL string gets SQL spans inside the string, shifted into
// host coordinates and winning over the host's string capture.
func TestInjectedSQLHighlighting(t *testing.T) {
	lines := []string{
		`import db`,
		``,
		`q = "SELECT id FROM users"`,
	}
	spans := highlight.Highlight("app.py", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for Python source, got none")
	}
	ix := highlight.NewIndex(spans)
	// col 5 = the S of SELECT, right after `q = "`.
	if got := ix.CaptureAt(2, 5); got != "keyword" {
		t.Errorf("injected SELECT: got capture %q, want keyword", got)
	}
	// The closing quote is host territory: still the Python string capture.
	if got := ix.CaptureAt(2, len(lines[2])-1); got != "string" {
		t.Errorf("closing quote: got capture %q, want string", got)
	}
	// Host code outside the fragment is untouched.
	if got := ix.CaptureAt(0, 0); got != "keyword" { // "import"
		t.Errorf("host import keyword: got capture %q", got)
	}
}

// TestInjectedSQLMultiline covers a triple-quoted SQL string: spans land on
// the fragment's own lines with no column shift after the first line.
func TestInjectedSQLMultiline(t *testing.T) {
	lines := []string{
		`q = """`,
		`SELECT *`,
		`FROM users`,
		`"""`,
	}
	spans := highlight.Highlight("app.py", lines)
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(1, 0); got != "keyword" { // SELECT
		t.Errorf("SELECT: got capture %q, want keyword", got)
	}
	if got := ix.CaptureAt(2, 0); got != "keyword" { // FROM
		t.Errorf("FROM: got capture %q, want keyword", got)
	}
}
