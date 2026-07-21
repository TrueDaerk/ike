package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/lang"
)

// TestLoadSniffsShebang guards the editor side of #893: opening an
// extensionless file with a shebang associates its language in the registry,
// so every path-keyed consumer (highlighting, LSP, statusline) resolves it;
// a file without a shebang stays language-less.
func TestLoadSniffsShebang(t *testing.T) {
	lang.Register(lang.Language{
		ID:           "snifflang",
		Extensions:   []string{"snf"},
		Interpreters: []string{"snf"},
	})
	dir := t.TempDir()

	script := filepath.Join(dir, "deploy")
	if err := os.WriteFile(script, []byte("#!/usr/bin/env snf\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(script); err != nil {
		t.Fatal(err)
	}
	if l, ok := lang.ByPath(script); !ok || l.ID != "snifflang" {
		t.Errorf("ByPath after load = %v/%v, want snifflang", l.ID, ok)
	}

	plain := filepath.Join(dir, "notes")
	if err := os.WriteFile(plain, []byte("no shebang here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m2 := New()
	if err := m2.Load(plain); err != nil {
		t.Fatal(err)
	}
	if _, ok := lang.ByPath(plain); ok {
		t.Error("file without shebang must stay language-less")
	}
}

// TestLoadContextSnifferOverridesExtension guards the editor side of #897: a
// registered context sniffer wins over what the extension says (the Ansible
// case — role-tree .yml is ansible, not yaml).
func TestLoadContextSnifferOverridesExtension(t *testing.T) {
	lang.Register(lang.Language{ID: "sniffbase", Extensions: []string{"sfy"}})
	lang.Register(lang.Language{ID: "sniffspecial"})
	lang.RegisterSniffer(func(path string) (string, bool) {
		if strings.Contains(path, "sniffspecialdir") {
			return "sniffspecial", true
		}
		return "", false
	})
	dir := filepath.Join(t.TempDir(), "sniffspecialdir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "play.sfy")
	if err := os.WriteFile(path, []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	if l, _ := lang.ByPath(path); l.ID != "sniffspecial" {
		t.Errorf("context sniffer overridden: got %s, want sniffspecial", l.ID)
	}
	// The same extension outside the sniffed context keeps its language.
	plain := filepath.Join(t.TempDir(), "plain.sfy")
	if err := os.WriteFile(plain, []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m2 := New()
	if err := m2.Load(plain); err != nil {
		t.Fatal(err)
	}
	if l, _ := lang.ByPath(plain); l.ID != "sniffbase" {
		t.Errorf("plain path: got %s, want sniffbase", l.ID)
	}
}

// TestLoadSniffDoesNotOverrideExtension: a path the static indexes already
// resolve is never re-associated, even when the first line looks like a
// shebang for another language.
func TestLoadSniffDoesNotOverrideExtension(t *testing.T) {
	lang.Register(lang.Language{ID: "sniffext", Extensions: []string{"sfx"}})
	lang.Register(lang.Language{ID: "sniffother-lang", Interpreters: []string{"sniffother"}})
	dir := t.TempDir()
	path := filepath.Join(dir, "tool.sfx")
	if err := os.WriteFile(path, []byte("#!/usr/bin/env sniffother\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	if l, _ := lang.ByPath(path); l.ID != "sniffext" {
		t.Errorf("extension mapping overridden: got %s, want sniffext", l.ID)
	}
}
