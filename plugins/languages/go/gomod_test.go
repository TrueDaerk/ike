package langgo

import (
	"testing"

	"ike/internal/lang"
)

// TestModuleFileAssociation guards #1063: go.mod / go.work / go.sum resolve by
// exact base name to their own language ids — the wire languageIds gopls
// documents — while delegating to the "go" server.
func TestModuleFileAssociation(t *testing.T) {
	for _, name := range []string{"go.mod", "go.work", "go.sum"} {
		l, ok := lang.ByPath("/some/project/" + name)
		if !ok {
			t.Errorf("ByPath(%q): no language", name)
			continue
		}
		if l.ID != name {
			t.Errorf("ByPath(%q).ID = %q, want %q", name, l.ID, name)
		}
		if got := l.ServerLang(); got != "go" {
			t.Errorf("%s ServerLang() = %q, want go", name, got)
		}
		if !l.HasServer() {
			t.Errorf("%s HasServer() = false, want true (delegates to go/gopls)", name)
		}
	}
}

// TestModuleFileNoFalsePositives: only the exact base names match — a .mod
// extension elsewhere or a nested name must not resolve to the module files.
func TestModuleFileNoFalsePositives(t *testing.T) {
	for _, path := range []string{"/p/other.mod", "/p/ago.mod2", "/p/go.mode"} {
		if l, ok := lang.ByPath(path); ok && (l.ID == "go.mod" || l.ID == "go.work" || l.ID == "go.sum") {
			t.Errorf("ByPath(%q) unexpectedly resolved to %q", path, l.ID)
		}
	}
	// A .go file keeps the plain go language.
	if l, ok := lang.ByPath("/p/main.go"); !ok || l.ID != "go" {
		t.Errorf("ByPath(main.go) = %+v, want go", l)
	}
}

// TestModuleFileComments: go.mod and go.work support // comments (toggling
// works); go.sum has none.
func TestModuleFileComments(t *testing.T) {
	for _, name := range []string{"go.mod", "go.work"} {
		if line, _, ok := lang.Comments("/p/" + name); !ok || line != "//" {
			t.Errorf("Comments(%s) = %q,%v, want // true", name, line, ok)
		}
	}
	if _, _, ok := lang.Comments("/p/go.sum"); ok {
		t.Error("Comments(go.sum) ok = true, want false")
	}
}
