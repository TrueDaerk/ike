package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/config"
	"ike/internal/lang"
)

func assocPage(t *testing.T) (*AssocPage, config.Options) {
	t.Helper()
	restoreConfig(t)
	lang.Register(lang.Language{ID: "assoc-page-test", Extensions: []string{"apgt"}})
	opts := config.Options{
		UserPath:    filepath.Join(t.TempDir(), "settings.toml"),
		ProjectRoot: t.TempDir(),
	}
	p := NewAssocPage(opts)
	p.SetSubPanelHost(&stubHost{})
	return p, opts
}

func assocFormOf(t *testing.T, p *AssocPage) *assocForm {
	t.Helper()
	f, ok := p.host.(*stubHost).top().(*assocForm)
	if !ok {
		t.Fatal("expected an open association form sub-panel")
	}
	return f
}

func typeAssoc(f *assocForm, s string) {
	for _, r := range s {
		f.Update(key(string(r)))
	}
}

// addAssociation drives the full add flow: open form, fill pattern/language,
// save.
func addAssociation(t *testing.T, p *AssocPage, pattern, id string) {
	t.Helper()
	p.Update(key("a"))
	f := assocFormOf(t, p)
	typeAssoc(f, pattern)
	f.Update(key("tab"))
	typeAssoc(f, id)
	apply(t, f.Update(key("enter")))
	if p.host.(*stubHost).top() != nil {
		t.Fatal("save must pop the form")
	}
}

// TestAssocPageAddEditDelete guards #1365: the page's CRUD persists
// [files.associations] at user scope and reloads.
func TestAssocPageAddEditDelete(t *testing.T) {
	p, opts := assocPage(t)
	addAssociation(t, p, "*.mytool", "assoc-page-test")
	if got := config.Get().Files.Associations["*.mytool"]; got != "assoc-page-test" {
		t.Fatalf("associations after add = %v", config.Get().Files.Associations)
	}
	// User scope: the write landed in the user settings file.
	data, err := os.ReadFile(opts.UserPath)
	if err != nil || !strings.Contains(string(data), "*.mytool") {
		t.Fatalf("user settings file = %q, %v", data, err)
	}

	// Edit the pattern: the old key must be removed, the new one written.
	p.Update(key("enter"))
	f := assocFormOf(t, p)
	// Cycle back to the pattern field so the cursor sits at the end.
	f.Update(key("tab"))
	f.Update(key("tab"))
	for range "*.mytool" {
		f.Update(key("backspace"))
	}
	typeAssoc(f, "*.othertool")
	apply(t, f.Update(key("enter")))
	got := config.Get().Files.Associations
	if _, stale := got["*.mytool"]; stale || got["*.othertool"] != "assoc-page-test" {
		t.Fatalf("associations after edit = %v", got)
	}

	// Delete empties the map.
	p.Update(key("d"))
	apply(t, confirmVia(t, p.host.(*stubHost)))
	if got := config.Get().Files.Associations; len(got) != 0 {
		t.Fatalf("associations after delete = %v", got)
	}
}

// TestAssocPageValidation: empty fields, unknown language ids and duplicate
// patterns are refused with a note; the form stays open.
func TestAssocPageValidation(t *testing.T) {
	p, _ := assocPage(t)
	p.Update(key("a"))
	f := assocFormOf(t, p)
	if cmd := f.Update(key("enter")); cmd != nil {
		t.Fatal("empty form must not write")
	}
	if p.host.(*stubHost).top() == nil || f.note == "" {
		t.Fatal("invalid form must stay open with a note")
	}

	// Unknown language id.
	typeAssoc(f, "*.x")
	f.Update(key("tab"))
	typeAssoc(f, "definitely-not-registered")
	if cmd := f.Update(key("enter")); cmd != nil {
		t.Fatal("unknown language id must not write")
	}
	if !strings.Contains(f.note, "unknown language id") {
		t.Fatalf("note = %q", f.note)
	}
	f.Update(key("esc"))

	// Duplicate pattern.
	addAssociation(t, p, "*.x", "assoc-page-test")
	p.Update(key("a"))
	f = assocFormOf(t, p)
	typeAssoc(f, "*.x")
	f.Update(key("tab"))
	typeAssoc(f, "assoc-page-test")
	if cmd := f.Update(key("enter")); cmd != nil {
		t.Fatal("duplicate pattern must not write")
	}
	if !strings.Contains(f.note, "already exists") {
		t.Fatalf("note = %q", f.note)
	}
}
