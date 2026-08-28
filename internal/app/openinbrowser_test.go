package app

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/registry"
)

// withOpenInBrowserTempDir points the scratch directory at a fresh t.TempDir
// for the duration of the test, so unpacking never touches the real OS temp
// directory or leaks between tests.
func withOpenInBrowserTempDir(t *testing.T) string {
	t.Helper()
	orig := openInBrowserTempDir
	dir := t.TempDir()
	openInBrowserTempDir = filepath.Join(dir, "ike-open-in-browser")
	t.Cleanup(func() { openInBrowserTempDir = orig })
	return openInBrowserTempDir
}

// writeGzipFixture gzips content into a file named name under dir, optionally
// carrying an original-name header (used for the header-fallback case).
func writeGzipFixture(t *testing.T, dir, name, header string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw, _ := gzip.NewWriterLevel(f, gzip.BestSpeed)
	zw.Name = header
	if _, err := zw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

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

// TestOpenInBrowserGzipViewable guards #2298: a browser-viewable gzip file
// (report.html.gz) is unpacked into ike's own scratch directory and the
// unpacked copy — not the .gz — is handed to the opener.
func TestOpenInBrowserGzipViewable(t *testing.T) {
	tmpDir := withOpenInBrowserTempDir(t)
	var opened string
	orig := browserOpen
	browserOpen = func(path string) error { opened = path; return nil }
	defer func() { browserOpen = orig }()

	m := sizedWith(t, registry.Global(), 100, 40)
	cwd, _ := cachedGetwd()
	src := writeGzipFixture(t, cwd, "openinbrowser_fixture.html.gz", "", []byte("<html>hi</html>"))
	t.Cleanup(func() { os.Remove(src) })
	out, cmd := m.openPath(src, false)
	m = drainCmd(out.(Model), cmd)

	m.openInBrowser()
	if opened == "" || opened == src {
		t.Fatalf("opened = %q, want an unpacked temp file", opened)
	}
	if !strings.HasPrefix(opened, tmpDir) {
		t.Fatalf("opened %q is not under the scratch dir %q", opened, tmpDir)
	}
	data, err := os.ReadFile(opened)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "<html>hi</html>" {
		t.Fatalf("unpacked content = %q", data)
	}

	// Reopening the same archive overwrites the unpacked copy rather than
	// accumulating a second one.
	first := opened
	m.openInBrowser()
	entries, err := os.ReadDir(filepath.Dir(first))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("scratch dir has %d entries after reopening, want 1", len(entries))
	}
}

// TestOpenInBrowserGzipNonHTML guards #2298: a gzip file whose inner content
// is not browser-viewable (app.log.gz) still declines the action, and never
// reaches the opener or the scratch directory.
func TestOpenInBrowserGzipNonHTML(t *testing.T) {
	withOpenInBrowserTempDir(t)
	var opened string
	orig := browserOpen
	browserOpen = func(path string) error { opened = path; return nil }
	defer func() { browserOpen = orig }()

	m := sizedWith(t, registry.Global(), 100, 40)
	cwd, _ := cachedGetwd()
	src := writeGzipFixture(t, cwd, "openinbrowser_fixture.log.gz", "", []byte("boot ok\n"))
	t.Cleanup(func() { os.Remove(src) })
	out, cmd := m.openPath(src, false)
	m = drainCmd(out.(Model), cmd)

	m.openInBrowser()
	if opened != "" {
		t.Fatalf("opener called for non-viewable gzip file: %q", opened)
	}
}

// TestOpenInBrowserGzipTarExcluded guards #2298: a compressed tar keeps
// belonging to the archive viewer, never the unpack-and-open path, even when
// it is named like an .html.gz.
func TestOpenInBrowserGzipTarExcluded(t *testing.T) {
	withOpenInBrowserTempDir(t)
	var opened string
	orig := browserOpen
	browserOpen = func(path string) error { opened = path; return nil }
	defer func() { browserOpen = orig }()

	m := sizedWith(t, registry.Global(), 100, 40)
	cwd, _ := cachedGetwd()
	src := writeGzipFixture(t, cwd, "openinbrowser_fixture.tar.gz", "", []byte("not really a tar, name is enough"))
	t.Cleanup(func() { os.Remove(src) })
	out, cmd := m.openPath(src, false)
	m = drainCmd(out.(Model), cmd)

	m.openInBrowser()
	if opened != "" {
		t.Fatalf("opener called for a compressed tar: %q", opened)
	}
}

// TestOpenInBrowserGzipLimit guards #2298: decompressed content past the
// large-file limit is refused with a clear notice instead of being opened
// partially — the gzip-bomb guard gzfile.Read already enforces.
func TestOpenInBrowserGzipLimit(t *testing.T) {
	withOpenInBrowserTempDir(t)
	var opened string
	orig := browserOpen
	browserOpen = func(path string) error { opened = path; return nil }
	defer func() { browserOpen = orig }()

	m := newCapped(t, 1) // 1 KB decompressed limit
	cwd, _ := cachedGetwd()
	src := writeGzipFixture(t, cwd, "openinbrowser_fixture_limit.html.gz", "", bytes.Repeat([]byte("a"), 1<<20))
	t.Cleanup(func() { os.Remove(src) })
	out, cmd := m.openPath(src, false)
	m = drainCmd(out.(Model), cmd)

	m.openInBrowser()
	if opened != "" {
		t.Fatalf("opener called despite the size limit: %q", opened)
	}
}

// TestOpenInBrowserGzipHeaderFallback guards #2298: an inner name is also
// resolved from the gzip header's original-name field when the archive's own
// name carries no usable extension (report.gz, header names report.html).
func TestOpenInBrowserGzipHeaderFallback(t *testing.T) {
	tmpDir := withOpenInBrowserTempDir(t)
	var opened string
	orig := browserOpen
	browserOpen = func(path string) error { opened = path; return nil }
	defer func() { browserOpen = orig }()

	m := sizedWith(t, registry.Global(), 100, 40)
	cwd, _ := cachedGetwd()
	src := writeGzipFixture(t, cwd, "openinbrowser_fixture_report.gz", "report.html", []byte("<html>report</html>"))
	t.Cleanup(func() { os.Remove(src) })
	out, cmd := m.openPath(src, false)
	m = drainCmd(out.(Model), cmd)

	m.openInBrowser()
	if opened == "" || !strings.HasPrefix(opened, tmpDir) {
		t.Fatalf("opened = %q, want an unpacked temp file under %q", opened, tmpDir)
	}
	if filepath.Base(opened) != "report.html" {
		t.Fatalf("unpacked file name = %q, want report.html", filepath.Base(opened))
	}
}
