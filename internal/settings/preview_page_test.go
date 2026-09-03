package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// preview_page_test.go guards the Markdown Preview page (#2421):
// preview.diagrams picks how a fenced diagram block is drawn, so its option
// set must be the three modes — and editing it must persist, render and
// validate like any enum.

// previewDiagramsEntry returns the shipped preview.diagrams schema entry.
func previewDiagramsEntry(t *testing.T) Entry {
	t.Helper()
	for _, p := range BasePages([]string{"default"}, nil, nil) {
		for _, e := range p.Entries {
			if e.Key == "preview.diagrams" {
				return e
			}
		}
	}
	t.Fatal("the schema must expose preview.diagrams")
	return Entry{}
}

// TestPreviewDiagramsOptions: the enum offers exactly the three modes.
func TestPreviewDiagramsOptions(t *testing.T) {
	e := previewDiagramsEntry(t)
	want := []string{"ascii", "image", "off"}
	if len(e.Options) != len(want) {
		t.Fatalf("preview.diagrams options = %v, want %v", e.Options, want)
	}
	for i, v := range want {
		if e.Options[i] != v {
			t.Fatalf("preview.diagrams options = %v, want %v", e.Options, want)
		}
	}
	if config.Defaults()["preview.diagrams"] != "ascii" {
		t.Fatalf("the shipped default must be ascii, got %q", config.Defaults()["preview.diagrams"])
	}
}

// TestPreviewDiagramsRoundTrip: setting the mode through the panel persists
// it, the reloaded config reads it back, the entry row shows it, and an
// invalid value snaps back to the default.
func TestPreviewDiagramsRoundTrip(t *testing.T) {
	restoreConfig(t)
	e := previewDiagramsEntry(t)
	m := New([]Page{{Title: "Markdown Preview", Entries: []Entry{e}}}, testOpts(t))
	m.SetSize(90, 24)
	m.Open()

	m.writeValue(e, "image")
	commit(t, m)
	if got := config.Get().Preview.Diagrams; got != "image" {
		t.Fatalf("persisted preview.diagrams = %q, want image", got)
	}
	if got := m.value("preview.diagrams"); got != "image" {
		t.Fatalf("panel value = %q, want image", got)
	}
	if v := m.View(); !strings.Contains(v, "image") {
		t.Fatalf("the entry row must render the value:\n%s", v)
	}

	m.writeValue(e, "sixel")
	commit(t, m)
	if got := config.Get().Preview.Diagrams; got != "ascii" {
		t.Fatalf("rejected preview.diagrams = %q, want the ascii default", got)
	}
}
