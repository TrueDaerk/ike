package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
)

func debugMapPage(t *testing.T) (*DebugMapPage, config.Options) {
	t.Helper()
	restoreConfig(t)
	opts := config.Options{
		UserPath:    filepath.Join(t.TempDir(), "settings.toml"),
		ProjectRoot: t.TempDir(),
	}
	return NewDebugMapPage(opts), opts
}

func typeMap(p *DebugMapPage, s string) {
	for _, r := range s {
		p.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
}

// addMapping drives the full add flow: open form, fill server/local, save.
func addMapping(t *testing.T, p *DebugMapPage, server, local string) {
	t.Helper()
	p.Update(key("a"))
	if !p.Capturing() {
		t.Fatal("a must open the form and capture keys")
	}
	typeMap(p, server)
	p.Update(key("tab"))
	typeMap(p, local)
	apply(t, p.Update(key("enter")))
}

// TestDebugMapPageAddEditDelete guards #832: the page's CRUD persists
// [[debug.php.path_mappings]] at project scope and reloads.
func TestDebugMapPageAddEditDelete(t *testing.T) {
	p, opts := debugMapPage(t)
	addMapping(t, p, "/var/www/html", ".")
	got := config.Get().Debug.PHP.PathMappings
	if len(got) != 1 || got[0].Server != "/var/www/html" || got[0].Local != "." {
		t.Fatalf("mappings after add = %+v", got)
	}
	// Project scope: the write landed in <root>/.ike/settings.toml.
	data, err := os.ReadFile(filepath.Join(opts.ProjectRoot, ".ike", "settings.toml"))
	if err != nil || !strings.Contains(string(data), "/var/www/html") {
		t.Fatalf("project settings file = %q, %v", data, err)
	}

	// Edit the local side.
	p.Update(key("enter"))
	p.Update(key("tab")) // to local
	p.Update(key("backspace"))
	typeMap(p, "src")
	apply(t, p.Update(key("enter")))
	got = config.Get().Debug.PHP.PathMappings
	if len(got) != 1 || got[0].Local != "src" {
		t.Fatalf("mappings after edit = %+v", got)
	}

	// Delete empties the list.
	apply(t, p.Update(key("d")))
	if got := config.Get().Debug.PHP.PathMappings; len(got) != 0 {
		t.Fatalf("mappings after delete = %+v", got)
	}
}

// TestDebugMapPageValidation: empty fields and duplicate server prefixes are
// refused with a note, the form stays open.
func TestDebugMapPageValidation(t *testing.T) {
	p, _ := debugMapPage(t)
	p.Update(key("a"))
	if cmd := p.Update(key("enter")); cmd != nil {
		t.Fatal("empty form must not write")
	}
	if !p.Capturing() || p.note == "" {
		t.Fatal("invalid form must stay open with a note")
	}
	p.Update(key("esc"))

	addMapping(t, p, "/srv/app", ".")
	p.Update(key("a"))
	typeMap(p, "/srv/app")
	p.Update(key("tab"))
	typeMap(p, "other")
	if cmd := p.Update(key("enter")); cmd != nil {
		t.Fatal("duplicate server prefix must not write")
	}
	if !strings.Contains(p.note, "already exists") {
		t.Fatalf("note = %q", p.note)
	}
}

// TestWriteDebugMapping guards the #832 suggestion seam: append + persist,
// no-op on an existing server prefix.
func TestWriteDebugMapping(t *testing.T) {
	_, opts := debugMapPage(t)
	apply(t, WriteDebugMapping(opts, "/var/www", "/proj"))
	if got := config.Get().Debug.PHP.PathMappings; len(got) != 1 || got[0].Local != "/proj" {
		t.Fatalf("mappings = %+v", got)
	}
	if cmd := WriteDebugMapping(opts, "/var/www", "/elsewhere"); cmd != nil {
		t.Fatal("existing server prefix must be a no-op")
	}
}
