package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// html_images_test.go guards the Settings-UI half of the HTML preview's
// inline images (#2743): preview.html_images is a real toggle on the Markdown
// Preview page, next to the diagram mode, that persists and renders.

// htmlImagesEntry returns the shipped preview.html_images schema entry.
func htmlImagesEntry(t *testing.T) Entry {
	t.Helper()
	for _, p := range BasePages([]string{"default"}, nil, nil) {
		for _, e := range p.Entries {
			if e.Key == "preview.html_images" {
				if p.Title != "Markdown Preview" {
					t.Fatalf("entry lives on page %q, want Markdown Preview", p.Title)
				}
				return e
			}
		}
	}
	t.Fatal("the schema must expose preview.html_images")
	return Entry{}
}

// TestPreviewHTMLImagesEntry: a titled, described, user-scoped toggle that
// ships on.
func TestPreviewHTMLImagesEntry(t *testing.T) {
	e := htmlImagesEntry(t)
	if e.Type != Bool || e.Scope != config.UserScope || e.Title != "Render images in HTML preview" || e.Description == "" {
		t.Fatalf("entry = %#v, want a titled, described user-scoped Bool", e)
	}
	if config.Defaults()["preview.html_images"] != "true" {
		t.Fatalf("the shipped default must be on, got %q", config.Defaults()["preview.html_images"])
	}
}

// TestPreviewHTMLImagesRoundTrip: toggling through the panel persists, the
// reloaded config reads it back and the entry row renders the value.
func TestPreviewHTMLImagesRoundTrip(t *testing.T) {
	restoreConfig(t)
	e := htmlImagesEntry(t)
	m := New([]Page{{Title: "Markdown Preview", Entries: []Entry{e}}}, testOpts(t))
	m.SetSize(140, 24)
	m.Open()

	m.writeValue(e, false)
	commit(t, m)
	if config.Get().Preview.HTMLImages {
		t.Fatal("persisted preview.html_images must be false")
	}
	if got := m.value("preview.html_images"); got != "false" {
		t.Fatalf("panel value = %q, want false", got)
	}
	if v := m.View(); !strings.Contains(v, " off") {
		t.Fatalf("the entry row must render the value:\n%s", v)
	}
	m.writeValue(e, true)
	commit(t, m)
	if !config.Get().Preview.HTMLImages {
		t.Fatal("re-enabling must persist true")
	}
}
