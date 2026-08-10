package app

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/registry"
	"ike/internal/watch"
)

// writeGzFile gzips body into a fresh temp dir under name and returns its path.
func writeGzFile(t *testing.T, name string, body []byte) string {
	t.Helper()
	return writeGzInto(t, filepath.Join(t.TempDir(), name), body)
}

// writeGzInto gzips body to an exact path, for the reload test that has to
// rewrite the same file twice.
func writeGzInto(t *testing.T, p string, body []byte) string {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestOpenPathRoutesPlainGzipToHandler: a .gz never lands in a raw text
// buffer — the handler claims it and dispatches OpenGzipMsg.
func TestOpenPathRoutesPlainGzipToHandler(t *testing.T) {
	m := newSized()
	p := writeGzFile(t, "app.log.gz", []byte("2026-08-10 boot ok\n"))
	_, cmd := m.openPath(p, false)
	for _, msg := range dispatched(cmd) {
		if open, ok := msg.(OpenGzipMsg); ok && open.Path == p {
			return
		}
	}
	t.Fatal("openPath on app.log.gz must dispatch OpenGzipMsg")
}

// TestOpenPathRoutesTarGzToTheArchiveViewer is the other half of the routing
// contract (#1762/#1763): the gz handler must not steal a tarball.
func TestOpenPathRoutesTarGzToTheArchiveViewer(t *testing.T) {
	for _, name := range []string{"src.tar.gz", "src.tgz"} {
		t.Run(name, func(t *testing.T) {
			m := newSized()
			p := writeTestArchive(t, name, map[string]string{"main.go": "package main\n"})
			_, cmd := m.openPath(p, false)
			for _, msg := range dispatched(cmd) {
				if _, ok := msg.(OpenGzipMsg); ok {
					t.Fatalf("%s must go to the archive viewer, not the gz viewer", name)
				}
			}
		})
	}
}

// TestOpenGzipFileReadOnly: the buffer holds the decompressed text, refuses
// edits, names the archive in its title, and resolves its language from the
// *inner* file name so a .log.gz gets log mode.
func TestOpenGzipFileReadOnly(t *testing.T) {
	m := newSized()
	body := "2026-08-10 boot ok\n2026-08-10 ready\n"
	p := writeGzFile(t, "app.log.gz", []byte(body))

	tm, cmd := m.Update(OpenGzipMsg{Path: p})
	m = tm.(Model)
	m = drainCmd(m, cmd)

	vpath := archiveEntryPath(p, "app.log")
	ed := readOnlyEditor(m, vpath)
	if ed == nil {
		t.Fatalf("no buffer at %q", vpath)
	}
	if !ed.ReadOnly() {
		t.Fatal("a gz preview opens read-only")
	}
	if got := ed.Text(); got != strings.TrimSuffix(body, "\n") && got != body {
		t.Fatalf("buffer = %q, want the decompressed text", got)
	}
	// The title names what is being previewed and where it came from.
	title, ok := archiveEntryTitle(vpath)
	if !ok || title != "app.log (app.log.gz)" {
		t.Fatalf("title = %q/%v", title, ok)
	}
	if got := m.editorTitle(ed); !strings.Contains(got, "[RO]") {
		t.Fatalf("editorTitle = %q, want a read-only marker", got)
	}
	// Language resolves from "app.log", not from the archive's ".gz".
	lang.Register(lang.Language{ID: "log-gz-test", Extensions: []string{"log"}})
	l, ok := lang.ByPath(ed.Path())
	if !ok || l.ID != "log-gz-test" {
		t.Fatalf("language for %q = %v/%v", ed.Path(), l.ID, ok)
	}
	// Editing is refused and nothing is written anywhere.
	before := ed.Text()
	*ed, _ = ed.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if ed.Text() != before {
		t.Fatalf("the buffer was edited: %q", ed.Text())
	}
	if ed.Dirty() {
		t.Fatal("a refused edit must not dirty the buffer")
	}
	if _, err := os.Stat(vpath); err == nil {
		t.Fatalf("the virtual path became a file: %q", vpath)
	}
	// Re-opening the same archive activates the existing tab.
	tm, cmd = m.Update(OpenGzipMsg{Path: p})
	m = drainCmd(tm.(Model), cmd)
	if n := countTabsForPath(m, vpath); n != 1 {
		t.Fatalf("the file opened %d times, want 1", n)
	}
}

// countTabsForPath counts editor tabs showing vpath across every pane.
func countTabsForPath(m Model, vpath string) int {
	n := 0
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			if ed := inst.TabEditor(i); ed != nil && ed.HasFile() && ed.Path() == vpath {
				n++
			}
		}
	}
	return n
}

// newCapped builds a model whose large-file byte threshold is kb kilobytes.
func newCapped(t *testing.T, kb int) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.New(), host.MapConfig{"files.large_file_kb": strconv.Itoa(kb)})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return tm.(Model)
}

// TestOpenGzipTruncatesAtTheLargeFileCap is the decompression-bomb guard: the
// ceiling counts decompressed bytes, so a tiny .gz cannot blow up the buffer.
func TestOpenGzipTruncatesAtTheLargeFileCap(t *testing.T) {
	m := newCapped(t, 1) // 1 KB of output
	p := writeGzFile(t, "big.txt.gz", bytes.Repeat([]byte("a"), 1<<20))
	if got := m.largeFileLimit(); got != 1024 {
		t.Fatalf("limit = %d, want 1024", got)
	}

	tm, cmd := m.Update(OpenGzipMsg{Path: p})
	m = drainCmd(tm.(Model), cmd)

	ed := readOnlyEditor(m, archiveEntryPath(p, "big.txt"))
	if ed == nil {
		t.Fatal("an oversized gz still opens, truncated")
	}
	if n := len(ed.Text()); n > 1024 {
		t.Fatalf("buffer holds %d bytes, want at most the 1024-byte cap", n)
	}
}

// TestOpenGzipTruncatesAtTheLineCap: the other large-file threshold. A log
// compresses far below the byte cap and can still be unusably long.
func TestOpenGzipTruncatesAtTheLineCap(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.New(), host.MapConfig{"files.large_file_lines": "10"})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)

	p := writeGzFile(t, "many.log.gz", []byte(strings.Repeat("entry\n", 500)))
	tm, cmd := m.Update(OpenGzipMsg{Path: p})
	m = drainCmd(tm.(Model), cmd)

	ed := readOnlyEditor(m, archiveEntryPath(p, "many.log"))
	if ed == nil {
		t.Fatal("a long gz still opens, truncated")
	}
	if n := strings.Count(ed.Text(), "\n") + 1; n > 10 {
		t.Fatalf("buffer holds %d lines, want at most the 10-line cap", n)
	}
}

// TestOpenGzipBinaryShowsMetadata: binary content degrades to a description
// of the archive instead of a buffer full of mojibake.
func TestOpenGzipBinaryShowsMetadata(t *testing.T) {
	m := newSized()
	p := writeGzFile(t, "blob.bin.gz", []byte("\x00\x01\x02binary\x00payload"))

	tm, cmd := m.Update(OpenGzipMsg{Path: p})
	m = drainCmd(tm.(Model), cmd)

	ed := readOnlyEditor(m, archiveEntryPath(p, "blob.bin"))
	if ed == nil {
		t.Fatal("binary content still opens a notice buffer")
	}
	text := ed.Text()
	if strings.ContainsRune(text, 0) {
		t.Fatalf("the raw binary reached the buffer: %q", text)
	}
	for _, want := range []string{"Binary content", "blob.bin.gz", "blob.bin", "Compressed:", "Decompressed:", "Ratio:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("metadata notice missing %q:\n%s", want, text)
		}
	}
}

// TestGzipBufferRefreshesOnDiskChange: the buffer's path names content inside
// the archive, so the editor's own reload never fires — the root model
// re-decompresses on the watcher event instead.
func TestGzipBufferRefreshesOnDiskChange(t *testing.T) {
	m := newSized()
	p := filepath.Join(t.TempDir(), "app.log.gz")
	writeGzInto(t, p, []byte("first\n"))

	tm, cmd := m.Update(OpenGzipMsg{Path: p})
	m = drainCmd(tm.(Model), cmd)

	vpath := archiveEntryPath(p, "app.log")
	if ed := readOnlyEditor(m, vpath); ed == nil || !strings.Contains(ed.Text(), "first") {
		t.Fatal("the initial content did not open")
	}
	writeGzInto(t, p, []byte("second\n"))

	tm, cmd = m.Update(watch.EventMsg{Path: p, Kind: watch.FileChanged})
	m = drainCmd(tm.(Model), cmd)

	ed := readOnlyEditor(m, vpath)
	if ed == nil {
		t.Fatalf("the buffer vanished on reload")
	}
	if !strings.Contains(ed.Text(), "second") {
		t.Fatalf("buffer = %q, want the refreshed content", ed.Text())
	}
	if !ed.ReadOnly() {
		t.Fatal("a refreshed gz buffer stays read-only")
	}
}

// TestRefreshGzipBuffersIgnoresOtherFiles: an unrelated write must not touch
// a gz preview, and a non-gzip path must not be decompressed at all.
func TestRefreshGzipBuffersIgnoresOtherFiles(t *testing.T) {
	m := newSized()
	dir := t.TempDir()
	p := writeGzInto(t, filepath.Join(dir, "app.log.gz"), []byte("first\n"))
	other := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(other, []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.Update(OpenGzipMsg{Path: p})
	m = drainCmd(tm.(Model), cmd)

	writeGzInto(t, p, []byte("second\n"))
	m.refreshGzipBuffers(other) // the wrong path: nothing must change

	ed := readOnlyEditor(m, archiveEntryPath(p, "app.log"))
	if ed == nil || !strings.Contains(ed.Text(), "first") {
		t.Fatalf("an unrelated change refreshed the buffer: %q", ed.Text())
	}
}

// TestTruncateLines pins the line cap's edges: a text landing exactly on the
// cap keeps its trailing newline instead of being reported as truncated.
func TestTruncateLines(t *testing.T) {
	cases := []struct {
		in      string
		max     int
		want    string
		dropped bool
	}{
		{"a\nb\nc\n", 2, "a\nb", true},
		{"a\nb\n", 2, "a\nb\n", false},
		{"a\nb", 5, "a\nb", false},
		{"", 3, "", false},
	}
	for _, c := range cases {
		got, dropped := truncateLines(c.in, c.max)
		if got != c.want || dropped != c.dropped {
			t.Errorf("truncateLines(%q, %d) = %q/%v, want %q/%v", c.in, c.max, got, dropped, c.want, c.dropped)
		}
	}
}
