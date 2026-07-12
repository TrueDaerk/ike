package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/editorconfig"
	"ike/internal/host"
	"ike/internal/watch"
)

// loadedInDir writes dir/.editorconfig plus dir/name and loads the file into
// a configured editor.
func loadedInDir(t *testing.T, cfg host.Config, ecContent, name, content string) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	if ecContent != "" {
		if err := os.WriteFile(filepath.Join(dir, editorconfig.FileName), []byte(ecContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if cfg != nil {
		m.Configure(cfg)
	}
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	return m, path
}

func TestEditorconfigOverridesIndent(t *testing.T) {
	cfg := host.MapConfig{"editor.tab_width": "4", "editor.use_spaces": "false"}
	m, _ := loadedInDir(t, cfg, "root = true\n\n[*.py]\nindent_style = space\nindent_size = 2\n", "x.py", "pass\n")
	if !m.useSpaces || m.tabWidth != 2 {
		t.Errorf("editorconfig should win over config: spaces=%v width=%d", m.useSpaces, m.tabWidth)
	}
	// A file no section matches keeps the config values.
	m2, _ := loadedInDir(t, cfg, "root = true\n\n[*.py]\nindent_style = space\nindent_size = 2\n", "x.go", "package x\n")
	if m2.useSpaces || m2.tabWidth != 4 {
		t.Errorf("unmatched file should keep config: spaces=%v width=%d", m2.useSpaces, m2.tabWidth)
	}
}

func TestEditorconfigAppliesWithoutHostConfig(t *testing.T) {
	m, _ := loadedInDir(t, nil, "root = true\n\n[*]\nindent_size = 3\nindent_style = tab\n", "x.txt", "hi\n")
	if m.useSpaces || m.tabWidth != 3 {
		t.Errorf("editorconfig should apply without cfg: spaces=%v width=%d", m.useSpaces, m.tabWidth)
	}
}

func TestEditorconfigDisabled(t *testing.T) {
	cfg := host.MapConfig{"editor.tab_width": "4", "editor.use_spaces": "false", "editor.editorconfig": "false"}
	m, _ := loadedInDir(t, cfg, "root = true\n\n[*]\nindent_style = space\nindent_size = 2\n", "x.py", "pass\n")
	if m.useSpaces || m.tabWidth != 4 {
		t.Errorf("editor.editorconfig=false should ignore the file: spaces=%v width=%d", m.useSpaces, m.tabWidth)
	}
}

func TestEditorconfigSavePolicies(t *testing.T) {
	cfg := host.MapConfig{"editor.trim_trailing_whitespace": "false", "editor.insert_final_newline": "false"}
	m, path := loadedInDir(t, cfg,
		"root = true\n\n[*]\ntrim_trailing_whitespace = true\ninsert_final_newline = true\n",
		"x.txt", "hi   ")
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "hi\n" {
		t.Errorf("save should trim and add final newline: %q", data)
	}
}

func TestEditorconfigEndOfLine(t *testing.T) {
	m, path := loadedInDir(t, nil, "root = true\n\n[*]\nend_of_line = crlf\n", "x.txt", "a\nb\n")
	if m.LineEnding() != "CRLF" {
		t.Fatalf("end_of_line should flip the stored flavor: %s", m.LineEnding())
	}
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "a\r\nb\r\n" {
		t.Errorf("save should write CRLF: %q", data)
	}
}

func TestEditorconfigNewFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, editorconfig.FileName),
		[]byte("root = true\n\n[*]\nend_of_line = crlf\ncharset = utf-8-bom\nindent_size = 2\nindent_style = space\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	m.NewFile(filepath.Join(dir, "new.txt"))
	if m.LineEnding() != "CRLF" || m.EncodingName() != "UTF-8 BOM" {
		t.Errorf("new file should take editorconfig eol/charset: %s %s", m.LineEnding(), m.EncodingName())
	}
	if !m.useSpaces || m.tabWidth != 2 {
		t.Errorf("new file should take indent settings: spaces=%v width=%d", m.useSpaces, m.tabWidth)
	}
}

func TestEditorconfigWatchInvalidation(t *testing.T) {
	m, path := loadedInDir(t, nil, "root = true\n\n[*]\nindent_size = 2\nindent_style = space\n", "x.txt", "hi\n")
	if m.tabWidth != 2 {
		t.Fatalf("initial width: %d", m.tabWidth)
	}
	ecPath := filepath.Join(filepath.Dir(path), editorconfig.FileName)
	if err := os.WriteFile(ecPath, []byte("root = true\n\n[*]\nindent_size = 8\nindent_style = space\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: ecPath})
	if m.tabWidth != 8 {
		t.Errorf("watch event should re-resolve: width=%d", m.tabWidth)
	}
}

func TestEditorconfigCharsetFallback(t *testing.T) {
	// "héllo" in ISO 8859-1: é = 0xE9, invalid as UTF-8, so the charset
	// fallback decides how the file decodes.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, editorconfig.FileName),
		[]byte("root = true\n\n[*]\ncharset = latin1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte{'h', 0xE9, 'l', 'l', 'o'}, 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.Text(), "héllo") {
		t.Errorf("charset fallback should decode latin1: %q", m.Text())
	}
	if m.EncodingName() != "ISO 8859-1" {
		t.Errorf("encoding: %s", m.EncodingName())
	}
}
