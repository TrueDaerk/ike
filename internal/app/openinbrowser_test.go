package app

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/registry"
)

// TestOpenInBrowserViewable guards #1429: a browser-viewable focused file is
// handed to the platform opener.
func TestOpenInBrowserViewable(t *testing.T) {
	var opened string
	orig := browserOpen
	browserOpen = func(path string) error { opened = path; return nil }
	defer func() { browserOpen = orig }()

	m := sizedWith(t, registry.New(), 100, 40)
	cwd, _ := cachedGetwd()
	path := filepath.Join(cwd, "openinbrowser_fixture.html")
	if err := os.WriteFile(path, []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	out, _ := m.openPath(path, false)
	m = out.(Model)

	m.openInBrowser()
	if opened != path {
		t.Fatalf("opened = %q want %q", opened, path)
	}
}

// TestOpenInBrowserNonViewable guards #1429: non-viewable types toast and do
// not reach the opener.
func TestOpenInBrowserNonViewable(t *testing.T) {
	var opened string
	orig := browserOpen
	browserOpen = func(path string) error { opened = path; return nil }
	defer func() { browserOpen = orig }()

	m := sizedWith(t, registry.New(), 100, 40)
	cwd, _ := cachedGetwd()
	path := filepath.Join(cwd, "openinbrowser_fixture.go")
	if err := os.WriteFile(path, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	out, _ := m.openPath(path, false)
	m = out.(Model)

	m.openInBrowser()
	if opened != "" {
		t.Fatalf("opener called for non-viewable file: %q", opened)
	}
}

// TestBrowserViewable guards the extension gate: markup, images and PDF are
// viewable; source, markdown and extension-less files are not.
func TestBrowserViewable(t *testing.T) {
	yes := []string{"a.html", "b.HTM", "c.svg", "d.png", "e.jpeg", "f.pdf", "g.webp"}
	for _, p := range yes {
		if !browserViewable(p) {
			t.Errorf("browserViewable(%q) = false, want true", p)
		}
	}
	no := []string{"a.go", "b.md", "c.txt", "Makefile", "d.pdf.bak"}
	for _, p := range no {
		if browserViewable(p) {
			t.Errorf("browserViewable(%q) = true, want false", p)
		}
	}
}
