//go:build cgo

package langphp

import (
	"strings"
	"testing"

	"ike/internal/highlight"
)

func TestFragmentsSQLSingleQuoted(t *testing.T) {
	lines := []string{
		`<?php`,
		`$q = 'SELECT id, name FROM users WHERE id = ?';`,
	}
	frags := highlight.Fragments("php", lines)
	if len(frags) != 1 {
		t.Fatalf("Fragments = %d fragments, want 1: %+v", len(frags), frags)
	}
	f := frags[0]
	if f.Lang != "sql" {
		t.Errorf("Lang = %q, want sql", f.Lang)
	}
	if f.StartLine != 1 || f.EndLine != 1 {
		t.Errorf("lines = %d..%d, want 1..1", f.StartLine, f.EndLine)
	}
	wantContent := `SELECT id, name FROM users WHERE id = ?`
	if got := strings.Join(f.Lines, "\n"); got != wantContent {
		t.Errorf("content = %q, want %q", got, wantContent)
	}
	// Content starts right after the opening quote and ends before the closing one.
	if want := len(`$q = '`); f.StartCol != want {
		t.Errorf("StartCol = %d, want %d", f.StartCol, want)
	}
	// The fragment text must be exactly the host text in its range.
	if got := lines[1][f.StartCol:f.EndCol]; got != wantContent {
		t.Errorf("host range = %q, want %q", got, wantContent)
	}
}

func TestFragmentsSQLDoubleQuoted(t *testing.T) {
	lines := []string{
		`<?php`,
		`$q = "SELECT id FROM users";`,
	}
	frags := highlight.Fragments("php", lines)
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

func TestFragmentsSQLHeredoc(t *testing.T) {
	lines := []string{
		`<?php`,
		`$q = <<<SQL`,
		`SELECT *`,
		`FROM users`,
		`SQL;`,
	}
	frags := highlight.Fragments("php", lines)
	if len(frags) != 1 {
		t.Fatalf("Fragments = %d fragments, want 1: %+v", len(frags), frags)
	}
	f := frags[0]
	if f.Lang != "sql" {
		t.Errorf("Lang = %q, want sql", f.Lang)
	}
	content := strings.Join(f.Lines, "\n")
	if !strings.Contains(content, "SELECT *") || !strings.Contains(content, "FROM users") {
		t.Errorf("content = %q, want SELECT/FROM lines", content)
	}
	if strings.Contains(content, "SQL") {
		t.Errorf("content = %q, must not include the heredoc delimiters", content)
	}
}

func TestFragmentsSQLNowdoc(t *testing.T) {
	lines := []string{
		`<?php`,
		`$q = <<<'SQL'`,
		`SELECT id`,
		`FROM users`,
		`SQL;`,
	}
	frags := highlight.Fragments("php", lines)
	if len(frags) != 1 {
		t.Fatalf("Fragments = %d fragments, want 1: %+v", len(frags), frags)
	}
	f := frags[0]
	if f.Lang != "sql" {
		t.Errorf("Lang = %q, want sql", f.Lang)
	}
	content := strings.Join(f.Lines, "\n")
	if !strings.Contains(content, "SELECT id") || !strings.Contains(content, "FROM users") {
		t.Errorf("content = %q, want SELECT/FROM lines", content)
	}
}

func TestFragmentsPlainStringIgnored(t *testing.T) {
	lines := []string{
		`<?php`,
		`$msg = 'hello there, general';`,
		`$msg2 = "still not sql";`,
	}
	if frags := highlight.Fragments("php", lines); len(frags) != 0 {
		t.Fatalf("plain strings produced fragments: %+v", frags)
	}
}
