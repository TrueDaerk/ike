package pathglob

import "testing"

// The matcher's own cases. The LSP watcher's registration globs (#1144) are
// exercised through internal/lsp/manager; these cover the vocabulary itself,
// including what the conceal file filter leans on (#1704).

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		// `**` spans any number of segments, including none.
		{"**/*.php", "src/Foo.php", true},
		{"**/*.php", "Foo.php", true},
		{"**/*.php", "a/b/c/Foo.php", true},
		{"**/*.php", "src/Foo.txt", false},
		{"**", "anything/at/all.go", true},
		{"src/**/*.go", "src/a/b/x.go", true},
		{"src/**/*.go", "src/x.go", true},
		{"src/**/*.go", "lib/x.go", false},
		{"a/**", "a/b/c", true},
		{"a/**", "b/c", false},
		// `*` and `?` never cross a separator.
		{"*.go", "main.go", true},
		{"*.go", "sub/main.go", false},
		{"main.?o", "main.go", true},
		{"main.?o", "main.gxo", false},
		{"?", "a/b", false},
		// Brace alternation, including nested.
		{"**/*.{ts,tsx}", "app/x.ts", true},
		{"**/*.{ts,tsx}", "app/x.tsx", true},
		{"**/*.{ts,tsx}", "app/x.tso", false},
		{"x.{a,{b,c}}", "x.c", true},
		{"x.{a,{b,c}}", "x.d", false},
		// An unterminated group is literal, not an error.
		{"x.{a", "x.{a", true},
		// Character classes, with ranges and negation.
		{"[a-c].go", "b.go", true},
		{"[a-c].go", "d.go", false},
		{"[!a-c].go", "d.go", true},
		{"[^a-c].go", "b.go", false},
		// An unterminated class is a literal bracket.
		{"[abc.go", "[abc.go", true},
		// Literals and edges.
		{"", "", true},
		{"", "x", false},
		{"*", "", true},
		{"Makefile", "Makefile", true},
		{"/abs/**/*.go", "/abs/pkg/x.go", true},
	}
	for _, c := range cases {
		if got := Match(c.pattern, c.path); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

// TestStarMatch covers the star-only matcher shared by the number-hint unit
// overrides (numhint) and the secret-pattern key matcher (secret): unlike
// Match, `/` and `\` are ordinary characters here and there is no `**`, `?`,
// `[...]` or `{a,b}`.
func TestStarMatch(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"", "", true},
		{"", "x", false},
		{"exact", "exact", true},
		{"exact", "exacter", false},
		{"*", "anything", true},
		{"db_pass*", "db_password", true},
		{"db_pass*", "db_user", false},
		{"*_KEY", "API_KEY", true},
		{"*_KEY", "API_KEYS", false},
		{"*_v*_final", "release_v2_final", true},
		{"*_v*_final", "release_v2", false},
		// `/` and `\` are ordinary characters, unlike in Match.
		{"path/*/file", "path/to/file", true},
		{"path/*/file", "path/file", false},
		{"a\\*b", "a\\xb", true},
		// `?`, `[...]` and `{a,b}` have no special meaning here.
		{"a?c", "a?c", true},
		{"a?c", "abc", false},
		{"[a-c]", "[a-c]", true},
		{"[a-c]", "b", false},
	}
	for _, c := range cases {
		if got := StarMatch(c.pattern, c.s); got != c.want {
			t.Errorf("StarMatch(%q, %q) = %v, want %v", c.pattern, c.s, got, c.want)
		}
	}
}
