package editorconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// write creates dir/.editorconfig with the given content.
func write(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMatchPatterns(t *testing.T) {
	cases := []struct {
		pattern, rel string
		want         bool
	}{
		// * does not cross separators; ** does.
		{"*.go", "main.go", true},
		{"*.go", "cmd/main.go", true}, // bare names match at any depth
		{"*", "main.go", true},
		{"lib/*.js", "lib/foo.js", true},
		{"lib/*.js", "lib/sub/foo.js", false},
		{"lib/**.js", "lib/sub/foo.js", true},
		{"/*.go", "main.go", true},
		{"/*.go", "cmd/main.go", false}, // leading / anchors
		// ? single non-separator character.
		{"?.go", "a.go", true},
		{"?.go", "ab.go", false},
		{"a?c", "a/c", false},
		// Character classes.
		{"[ab].go", "a.go", true},
		{"[ab].go", "c.go", false},
		{"[!ab].go", "c.go", true},
		{"[!ab].go", "a.go", false},
		{"[a-c].go", "b.go", true},
		// Brace alternation, nesting, literal single braces.
		{"*.{js,ts}", "x.ts", true},
		{"*.{js,ts}", "x.go", false},
		{"{a,{b,c}}.go", "c.go", true},
		{"{single}.go", "{single}.go", true},
		{"{single}.go", "single.go", false},
		// Numeric ranges.
		{"file{1..3}.txt", "file2.txt", true},
		{"file{1..3}.txt", "file4.txt", false},
		{"file{9..11}.txt", "file10.txt", true},
		// Escapes.
		{`\*.go`, "*.go", true},
		{`\*.go`, "a.go", false},
		// Unclosed constructs are literal.
		{"[ab.go", "[ab.go", true},
		{"{a,b.go", "{a,b.go", true},
	}
	for _, c := range cases {
		if got := match(c.pattern, c.rel); got != c.want {
			t.Errorf("match(%q, %q) = %v, want %v", c.pattern, c.rel, got, c.want)
		}
	}
}

func TestParseSectionsAndComments(t *testing.T) {
	f := parse("# comment\n; also comment\nroot = true\n\n[*]\nindent_style = space\nindent_size = 4\n\n[*.md]\nindent_size = 2\nbroken line without equals\n")
	if !f.root {
		t.Fatal("root = true not parsed")
	}
	if len(f.sections) != 2 {
		t.Fatalf("want 2 sections, got %d", len(f.sections))
	}
	if f.sections[0].pattern != "*" || f.sections[1].pattern != "*.md" {
		t.Fatalf("patterns wrong: %+v", f.sections)
	}
	if len(f.sections[1].pairs) != 1 {
		t.Fatalf("malformed line should be skipped: %+v", f.sections[1].pairs)
	}
}

func TestResolveIndentMatrix(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "root = true\n\n[*]\nindent_style = space\nindent_size = 4\n\n[*.go]\nindent_style = tab\ntab_width = 8\n\n[Makefile]\nindent_style = tab\n")
	var r Resolver

	s := r.Resolve(filepath.Join(dir, "a.py"))
	if sp, ok := s.UseSpaces(); !ok || !sp {
		t.Errorf("a.py: want spaces, got %v %v", sp, ok)
	}
	if w, ok := s.IndentWidth(); !ok || w != 4 {
		t.Errorf("a.py: want width 4, got %d %v", w, ok)
	}

	s = r.Resolve(filepath.Join(dir, "sub", "b.go"))
	if sp, ok := s.UseSpaces(); !ok || sp {
		t.Errorf("b.go: want tabs, got %v %v", sp, ok)
	}
	if w, ok := s.IndentWidth(); !ok || w != 8 {
		t.Errorf("b.go: want width 8, got %d %v", w, ok)
	}

	s = r.Resolve(filepath.Join(dir, "Makefile"))
	if sp, ok := s.UseSpaces(); !ok || sp {
		t.Errorf("Makefile: want tabs, got %v %v", sp, ok)
	}
}

func TestResolveLaterSectionWins(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "[*]\nindent_size = 4\n\n[*.md]\nindent_size = 2\n")
	var r Resolver
	if w, _ := r.Resolve(filepath.Join(dir, "x.md")).IndentWidth(); w != 2 {
		t.Errorf("later section should win: got %d", w)
	}
	if w, _ := r.Resolve(filepath.Join(dir, "x.go")).IndentWidth(); w != 4 {
		t.Errorf("non-matching later section must not apply: got %d", w)
	}
}

func TestResolveRootStopsUpwardSearch(t *testing.T) {
	top := t.TempDir()
	mid := filepath.Join(top, "mid")
	leaf := filepath.Join(mid, "leaf")
	write(t, top, "[*]\nindent_size = 8\ncharset = latin1\n")
	write(t, mid, "root = true\n\n[*]\nindent_size = 2\n")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	var r Resolver
	s := r.Resolve(filepath.Join(leaf, "x.txt"))
	if w, _ := s.IndentWidth(); w != 2 {
		t.Errorf("mid root=true should apply: got width %d", w)
	}
	if _, ok := s.Charset(); ok {
		t.Error("top-level file beyond root=true must not contribute")
	}
}

func TestResolveCloserFileWins(t *testing.T) {
	top := t.TempDir()
	sub := filepath.Join(top, "sub")
	write(t, top, "root = true\n\n[*]\nindent_size = 8\ninsert_final_newline = true\n")
	write(t, sub, "[*]\nindent_size = 3\n")
	var r Resolver
	s := r.Resolve(filepath.Join(sub, "x.txt"))
	if w, _ := s.IndentWidth(); w != 3 {
		t.Errorf("closer file should win: got %d", w)
	}
	if v, ok := s.InsertFinalNewline(); !ok || !v {
		t.Error("outer keys not overridden must survive")
	}
}

func TestResolveUnset(t *testing.T) {
	top := t.TempDir()
	sub := filepath.Join(top, "sub")
	write(t, top, "root = true\n\n[*]\ntrim_trailing_whitespace = true\n")
	write(t, sub, "[*]\ntrim_trailing_whitespace = unset\n")
	var r Resolver
	if _, ok := r.Resolve(filepath.Join(sub, "x.txt")).TrimTrailingWhitespace(); ok {
		t.Error("unset should remove the key")
	}
}

func TestResolveNoFiles(t *testing.T) {
	var r Resolver
	if s := r.Resolve(filepath.Join(t.TempDir(), "x.txt")); len(s) != 0 {
		t.Errorf("want empty settings, got %v", s)
	}
}

func TestInvalidate(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "root = true\n\n[*]\nindent_size = 4\n")
	var r Resolver
	path := filepath.Join(dir, "x.txt")
	if w, _ := r.Resolve(path).IndentWidth(); w != 4 {
		t.Fatal("initial resolve failed")
	}
	// Change on disk; the cache still serves the old value until invalidated.
	write(t, dir, "root = true\n\n[*]\nindent_size = 7\n")
	if w, _ := r.Resolve(path).IndentWidth(); w != 4 {
		t.Fatal("cache should serve until invalidated")
	}
	r.Invalidate(filepath.Join(dir, FileName))
	if w, _ := r.Resolve(path).IndentWidth(); w != 7 {
		t.Error("invalidate should force a re-read")
	}
}

func TestSettingsAccessors(t *testing.T) {
	s := Settings{
		"indent_style":             "TAB", // values compared case-insensitively
		"indent_size":              "2",
		"trim_trailing_whitespace": "false",
		"insert_final_newline":     "true",
		"end_of_line":              "CRLF",
		"charset":                  "UTF-8-BOM",
	}
	if sp, ok := s.UseSpaces(); !ok || sp {
		t.Error("indent_style TAB should mean tabs")
	}
	if w, ok := s.IndentWidth(); !ok || w != 2 {
		t.Errorf("indent_size fallback: got %d", w)
	}
	s["tab_width"] = "8"
	if w, _ := s.IndentWidth(); w != 8 {
		t.Errorf("tab_width should beat indent_size: got %d", w)
	}
	if v, ok := s.TrimTrailingWhitespace(); !ok || v {
		t.Error("trim false not read")
	}
	if v, ok := s.InsertFinalNewline(); !ok || !v {
		t.Error("final newline true not read")
	}
	if v, ok := s.EndOfLine(); !ok || v != "crlf" {
		t.Errorf("end_of_line: got %q", v)
	}
	if v, ok := s.Charset(); !ok || v != "utf-8-bom" {
		t.Errorf("charset: got %q", v)
	}
	// indent_size = tab defers to tab_width.
	s2 := Settings{"indent_size": "tab", "tab_width": "5"}
	if w, ok := s2.IndentWidth(); !ok || w != 5 {
		t.Errorf("indent_size=tab: got %d %v", w, ok)
	}
	if _, ok := (Settings{"indent_size": "tab"}).IndentWidth(); ok {
		t.Error("indent_size=tab without tab_width has no width")
	}
	// Nil settings never panic and report nothing.
	var nilS Settings
	if _, ok := nilS.UseSpaces(); ok {
		t.Error("nil settings should report nothing")
	}
}
