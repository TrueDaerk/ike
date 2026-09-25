package pane

import (
	"testing"

	"ike/internal/host"
)

// TestHTMLPreviewFollowsImagesConfig (#2743): preview.html_images reaches a
// freshly opened HTML preview (on by default, off when persisted off), the
// tab-restore constructor, and — on a reload — the panes already open.
func TestHTMLPreviewFollowsImagesConfig(t *testing.T) {
	r := NewRegistry(host.MapConfig{}, nil)
	on := r.Get(r.AddHTMLPreview("/tmp/a.html")).HTMLPreview()
	if !on.ImagesEnabled() {
		t.Fatal("images default to on without the key")
	}
	if !r.HTMLPreviewsMinted() {
		t.Fatal("opening an HTML preview mints one")
	}
	r2 := NewRegistry(host.MapConfig{"preview.html_images": "false"}, nil)
	if r2.HTMLPreviewsMinted() {
		t.Fatal("a fresh registry has minted no HTML preview")
	}
	if r2.Get(r2.AddHTMLPreview("/tmp/a.html")).HTMLPreview().ImagesEnabled() {
		t.Fatal("the persisted off must reach a new pane")
	}
	if r2.AddHTMLPreviewKey("htmlpreview:5", "/tmp/c.html").HTMLPreview().ImagesEnabled() {
		t.Fatal("the layout restore must apply the setting too")
	}
	if inst := r2.NewContentPane(KindHTMLPreview, "/tmp/b.html", "", "", ""); inst.HTMLPreview().ImagesEnabled() {
		t.Fatal("the tab restore constructor must apply the setting too")
	}
	r.Reconfigure(host.MapConfig{"preview.html_images": "false"})
	if on.ImagesEnabled() {
		t.Fatal("a reload must reach an open pane")
	}
}
