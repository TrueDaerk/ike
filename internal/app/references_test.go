package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	ilsp "ike/internal/lsp"
	"ike/internal/palette"
)

func sampleRefs() []ilsp.Reference {
	return []ilsp.Reference{
		{Path: "/proj/a.go", Line: 4, Col: 2, Preview: "foo := Bar()"},
		{Path: "/proj/b.go", Line: 9, Col: 0, Preview: "Bar() // caller"},
		{Path: "/proj/c.go", Line: 0, Col: 1, Preview: strings.Repeat("x", 80)},
	}
}

func TestRefsModeItems(t *testing.T) {
	r := &refsMode{}
	r.Set(sampleRefs())
	items := r.Results("", palette.Context{})
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %+v", items)
	}
	if items[0].Title != "/proj/a.go:5" {
		t.Errorf("title should be path:1-based-line, got %q", items[0].Title)
	}
	msg, ok := items[0].Msg.(ilsp.DefinitionMsg)
	if !ok || msg.Path != "/proj/a.go" || msg.Line != 4 || msg.Col != 2 {
		t.Errorf("activation should carry the navigation target, got %#v", items[0].Msg)
	}
	if len([]rune(items[2].Detail)) > previewMax {
		t.Errorf("preview should be capped, got %q", items[2].Detail)
	}
}

func TestRefsModeFiltersByTitleAndPreview(t *testing.T) {
	r := &refsMode{}
	r.Set(sampleRefs())
	if items := r.Results("b.go", palette.Context{}); len(items) == 0 || items[0].Title != "/proj/b.go:10" {
		t.Fatalf("title filter failed: %+v", items)
	}
	if items := r.Results("caller", palette.Context{}); len(items) != 1 || items[0].Title != "/proj/b.go:10" {
		t.Fatalf("preview filter failed: %+v", items)
	}
}

func TestReferencesMsgRouting(t *testing.T) {
	m := sized(t, 100, 40)

	// Nothing found: a toast, no palette.
	out, _ := m.Update(ilsp.ReferencesMsg{})
	m = out.(Model)
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[0].text, "no usages") {
		t.Fatalf("empty result should toast, got %+v", m.toasts)
	}
	if m.palette.IsOpen() {
		t.Fatal("empty result must not open the palette")
	}

	// A single usage navigates straight there.
	dir := t.TempDir()
	file := filepath.Join(dir, "one.go")
	if err := os.WriteFile(file, []byte("package one\nvar X int\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ = m.Update(ilsp.ReferencesMsg{Refs: []ilsp.Reference{{Path: file, Line: 1, Col: 4}}})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("single result must navigate, not list")
	}
	ed := m.panes.Get(m.activeEditorKey()).Editor()
	if ed.Path() != file {
		t.Fatalf("single result should open the target, got %q", ed.Path())
	}

	// Multiple usages open the locked list.
	out, _ = m.Update(ilsp.ReferencesMsg{Refs: sampleRefs()})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("multiple results should open the references list")
	}
}
