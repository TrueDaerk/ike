package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/archview"
	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/largefile"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
)

// tarBytes builds a tar holding name/body pairs.
func tarBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, name := range sortedNames(files) {
		body := files[name]
		h := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(body)),
			ModTime:  time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// sortedNames orders a fixture map deterministically.
func sortedNames(files map[string]string) []string {
	out := make([]string, 0, len(files))
	for n := range files {
		out = append(out, n)
	}
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// writeTestArchive drops a tar (gzipped when name ends in .gz/.tgz) in a temp
// dir and returns its path.
func writeTestArchive(t *testing.T, name string, files map[string]string) string {
	t.Helper()
	data := tarBytes(t, files)
	if strings.HasSuffix(name, ".gz") || strings.HasSuffix(name, ".tgz") {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		data = buf.Bytes()
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// archiveKeys returns the keys of every archive pane in the active workspace.
func archiveKeys(m Model) []string {
	var out []string
	for _, key := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindArchive {
			out = append(out, key)
		}
	}
	return out
}

// dispatched walks a Cmd tree collecting the messages it produces.
func dispatched(cmd tea.Cmd) []tea.Msg {
	var out []tea.Msg
	var walk func(tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range batch {
				walk(sub)
			}
			return
		}
		out = append(out, msg)
	}
	walk(cmd)
	return out
}

// TestOpenPathRoutesArchivesToHandler: .tar/.tgz/.tar.gz never land in a raw
// text buffer — the handler claims them and dispatches OpenArchiveMsg.
func TestOpenPathRoutesArchivesToHandler(t *testing.T) {
	for _, name := range []string{"src.tar", "src.tgz", "src.tar.gz"} {
		t.Run(name, func(t *testing.T) {
			m := newSized()
			p := writeTestArchive(t, name, map[string]string{"main.go": "package main\n"})
			_, cmd := m.openPath(p, false)
			found := false
			for _, msg := range dispatched(cmd) {
				if open, ok := msg.(OpenArchiveMsg); ok && open.Path == p {
					found = true
				}
			}
			if !found {
				t.Fatalf("openPath on %s must dispatch OpenArchiveMsg", name)
			}
		})
	}
}

// TestPlainGzipIsNotClaimed: a .gz that holds no tar stays with the gz
// handler (the sibling viewer) — the sniff has to look inside.
func TestPlainGzipIsNotClaimed(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte("2026-08-10 boot ok\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "app.log.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	h, ok := m.reg.ResolveHandler(p, readHead(p))
	if !ok {
		t.Fatal("a plain gzip must be claimed by the gz handler")
	}
	if h.Owner == "archives" {
		t.Fatal("a plain gzip must not be claimed by the archive handler")
	}
}

// TestOpenArchivePaneListsEntries: the pane opens focused, bound to the file,
// and shows the entry list.
func TestOpenArchivePaneListsEntries(t *testing.T) {
	m := newSized()
	p := writeTestArchive(t, "src.tar", map[string]string{
		"cmd/main.go": "package main\n",
		"README.md":   "# hi\n",
	})
	tm, _ := m.Update(OpenArchiveMsg{Path: p})
	m = tm.(Model)
	keys := archiveKeys(m)
	if len(keys) != 1 {
		t.Fatalf("expected one archive pane, got %v", keys)
	}
	inst := m.activeWS().Panes.Get(keys[0])
	if inst.Archive().Path() != p {
		t.Fatalf("pane bound to %q", inst.Archive().Path())
	}
	if m.activeWS().Panes.Focused() != keys[0] {
		t.Error("the archive pane must take focus")
	}
	if inst.Archive().Entries() != 2 {
		t.Fatalf("entries = %d", inst.Archive().Entries())
	}
	// The flat entry list became a tree: the directory groups its file.
	av := inst.Archive()
	var rows []string
	for i := 0; i < av.Rows(); i++ {
		rows = append(rows, av.RowName(i))
	}
	if len(rows) != 3 || rows[0] != "cmd" || rows[1] != "cmd/main.go" || rows[2] != "README.md" {
		t.Fatalf("rows = %v", rows)
	}
	if !strings.Contains(inst.View(), "src.tar") {
		t.Errorf("the header must name the archive:\n%s", inst.View())
	}
	// A second open of the same path refocuses instead of duplicating.
	tm, _ = m.Update(OpenArchiveMsg{Path: p})
	m = tm.(Model)
	if got := archiveKeys(m); len(got) != 1 {
		t.Fatalf("same path must not duplicate, got %v", got)
	}
}

// readOnlyEditor finds the editor tab holding the virtual path.
func readOnlyEditor(m Model, vpath string) *editor.Model {
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if ed.HasFile() && ed.Path() == vpath {
				return ed
			}
		}
	}
	return nil
}

// TestOpenArchiveEntryReadOnly: enter on an entry extracts it into a
// read-only buffer whose language resolves from the entry's file name, and
// editing/saving is refused.
func TestOpenArchiveEntryReadOnly(t *testing.T) {
	m := newSized()
	p := writeTestArchive(t, "src.tar", map[string]string{"cmd/main.go": "package main\n"})
	tm, cmd := m.Update(archview.OpenEntryMsg{Archive: p, Entry: "cmd/main.go"})
	m = tm.(Model)
	m = drainCmd(m, cmd)

	vpath := archiveEntryPath(p, "cmd/main.go")
	ed := readOnlyEditor(m, vpath)
	if ed == nil {
		t.Fatal("the entry must open in an editor tab")
	}
	if !ed.ReadOnly() {
		t.Fatal("an archive entry opens read-only")
	}
	if got := ed.Text(); got != "package main" {
		t.Fatalf("buffer = %q", got)
	}
	// Highlighting resolves from the entry's own name, not the archive's:
	// the virtual path ends in "main.go", so the extension lookup answers.
	// (Language plugins are registered by cmd/ike, so the test registers the
	// one it asserts on.)
	lang.Register(lang.Language{ID: "go-archive-test", Extensions: []string{"go"}})
	l, ok := lang.ByPath(ed.Path())
	if !ok || l.ID != "go-archive-test" {
		t.Fatalf("language for %q = %v/%v", ed.Path(), l.ID, ok)
	}
	// Editing is refused and nothing is written anywhere.
	*ed, _ = ed.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if ed.Text() != "package main" {
		t.Fatalf("the buffer was edited: %q", ed.Text())
	}
	if ed.Dirty() {
		t.Fatal("a refused edit must not dirty the buffer")
	}
	if _, err := os.Stat(vpath); err == nil {
		t.Fatalf("the virtual path became a file: %q", vpath)
	}
	// Re-opening the same entry activates the existing tab.
	tm, cmd = m.Update(archview.OpenEntryMsg{Archive: p, Entry: "cmd/main.go"})
	m = tm.(Model)
	m = drainCmd(m, cmd)
	n := 0
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, e := range inst.Editors() {
			if e.HasFile() && e.Path() == vpath {
				n++
			}
		}
	}
	if n != 1 {
		t.Fatalf("the entry opened %d times, want 1", n)
	}
}

// TestArchiveLimitIsTheLargeFileThreshold: an extracted entry is held to the
// same ceiling as a file on disk (#149) — the refusal itself is covered by
// internal/archive's ErrTooLarge test.
func TestArchiveLimitIsTheLargeFileThreshold(t *testing.T) {
	m := newSized()
	if got := m.largeFileLimit(); got != largefile.DefaultMaxKB*1024 {
		t.Fatalf("largeFileLimit = %d, want %d", got, largefile.DefaultMaxKB*1024)
	}
}

// TestOpenArchiveEntryBinaryRefused: a binary member never reaches a text
// buffer — the whole point of routing archives away from the editor.
func TestOpenArchiveEntryBinaryRefused(t *testing.T) {
	m := newSized()
	p := writeTestArchive(t, "bin.tar", map[string]string{"a.bin": "\x00\x01\x02binary\x00"})
	cmd := m.openArchiveEntry(p, "a.bin")
	if cmd != nil {
		t.Fatal("a binary entry must not open a buffer")
	}
	if readOnlyEditor(m, archiveEntryPath(p, "a.bin")) != nil {
		t.Fatal("a binary entry must not reach an editor tab")
	}
}

// TestOpenArchiveEntryMissingReports: a vanished member reports instead of
// opening an empty buffer.
func TestOpenArchiveEntryMissingReports(t *testing.T) {
	m := newSized()
	p := writeTestArchive(t, "src.tar", map[string]string{"a.txt": "hi\n"})
	if cmd := m.openArchiveEntry(p, "gone.txt"); cmd != nil {
		t.Fatal("a missing entry must not open a buffer")
	}
	if readOnlyEditor(m, archiveEntryPath(p, "gone.txt")) != nil {
		t.Fatal("a missing entry must not reach an editor tab")
	}
}

// TestArchivePaneRestoresByPath guards session restore (#1762): the layout
// store records the pane by path and a fresh model re-lists the archive.
func TestArchivePaneRestoresByPath(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	p := writeTestArchive(t, "src.tar", map[string]string{"a.txt": "hi\n"})

	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	m.openArchivePane(p)
	key := m.activeWS().Panes.Focused()
	if inst := m.activeWS().Panes.Get(key); inst == nil || inst.Kind() != pane.KindArchive {
		t.Fatalf("setup: focused = %q", key)
	}
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)

	_, ids, ok := loadLayout()
	if !ok {
		t.Fatal("the layout must persist")
	}
	if id := ids[key]; id.Kind != "archive" || id.Path != p {
		t.Fatalf("persisted identity = %+v", id)
	}

	m2 := NewWith(registry.New(), host.MapConfig{})
	inst := m2.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindArchive {
		t.Fatalf("the archive pane did not restore under %q", key)
	}
	if inst.Archive().Path() != p || inst.Archive().Entries() != 1 {
		t.Fatalf("restored pane = %q with %d entries", inst.Archive().Path(), inst.Archive().Entries())
	}
	found := false
	for _, leaf := range layout.Leaves(m2.activeWS().Tree) {
		if leaf == key {
			found = true
		}
	}
	if !found {
		t.Fatal("the archive leaf is missing from the restored tree")
	}
}

// TestArchiveEntryTitle labels a preview by entry and archive.
func TestArchiveEntryTitle(t *testing.T) {
	got, ok := archiveEntryTitle("/tmp/src.tar!cmd/main.go")
	if !ok || got != "main.go (src.tar)" {
		t.Fatalf("title = %q/%v", got, ok)
	}
	if _, ok := archiveEntryTitle("/tmp/plain.go"); ok {
		t.Fatal("a path with no entry separator is not an archive entry")
	}
}

// gzMember gzips body for use as a tar member's content.
func gzMember(t *testing.T, body []byte) string {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// TestOpenArchiveGzipEntryDecompressed is the composition of the two viewers
// (#1948): a .log.gz *inside* a tar opens as decompressed text with the inner
// name's language, instead of being refused as a binary blob.
func TestOpenArchiveGzipEntryDecompressed(t *testing.T) {
	m := newSized()
	body := "2026-08-10 boot ok\n2026-08-10 ready\n"
	p := writeTestArchive(t, "backup.tar", map[string]string{
		"logs/app.log.gz": gzMember(t, []byte(body)),
	})
	tm, cmd := m.Update(archview.OpenEntryMsg{Archive: p, Entry: "logs/app.log.gz"})
	m = drainCmd(tm.(Model), cmd)

	// The virtual path keeps the member's directory and drops the .gz, so the
	// tab, the title and the language all resolve from app.log.
	vpath := archiveEntryPath(p, "logs/app.log")
	ed := readOnlyEditor(m, vpath)
	if ed == nil {
		t.Fatalf("no read-only buffer at %q", vpath)
	}
	if want := strings.TrimSuffix(body, "\n"); ed.Text() != want {
		t.Fatalf("buffer = %q, want the decompressed %q", ed.Text(), want)
	}
	if !ed.ReadOnly() {
		t.Fatal("an archive gz preview must be read-only")
	}
	if title, ok := archiveEntryTitle(vpath); !ok || title != "app.log (backup.tar)" {
		t.Fatalf("title = %q/%v", title, ok)
	}
	lang.Register(lang.Language{ID: "log-archive-test", Extensions: []string{"log"}})
	if l, ok := lang.ByPath(ed.Path()); !ok || l.ID != "log-archive-test" {
		t.Fatalf("language for %q = %v/%v", ed.Path(), l.ID, ok)
	}
	// Re-opening the same member activates the existing tab.
	tm, cmd = m.Update(archview.OpenEntryMsg{Archive: p, Entry: "logs/app.log.gz"})
	m = drainCmd(tm.(Model), cmd)
	if n := countReadOnly(m, vpath); n != 1 {
		t.Fatalf("the member opened %d times, want 1", n)
	}
}

// countReadOnly counts the editor tabs holding vpath.
func countReadOnly(m Model, vpath string) int {
	n := 0
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if ed.HasFile() && ed.Path() == vpath {
				n++
			}
		}
	}
	return n
}

// TestOpenArchiveGzipEntryTruncatesAtTheCap: the bomb guard counts
// *decompressed* bytes for a member too — a member that fits the limit
// compressed must not blow the buffer up when it is unpacked.
func TestOpenArchiveGzipEntryTruncatesAtTheCap(t *testing.T) {
	m := newCapped(t, 3) // 3 KB of output; the compressed member is ~1 KB
	p := writeTestArchive(t, "backup.tar", map[string]string{
		"big.txt.gz": gzMember(t, bytes.Repeat([]byte("a"), 1<<20)),
	})
	tm, cmd := m.Update(archview.OpenEntryMsg{Archive: p, Entry: "big.txt.gz"})
	m = drainCmd(tm.(Model), cmd)

	ed := readOnlyEditor(m, archiveEntryPath(p, "big.txt"))
	if ed == nil {
		t.Fatal("an oversized gz member still opens, truncated")
	}
	if n := len(ed.Text()); n > 3*1024 {
		t.Fatalf("buffer holds %d bytes, want at most the 3072-byte cap", n)
	}
}

// TestOpenArchiveGzipEntryBinaryShowsMetadata: a gzipped PNG has no text to
// show, so it degrades to the gz viewer's metadata notice — not mojibake, and
// not the old blanket binary refusal.
func TestOpenArchiveGzipEntryBinaryShowsMetadata(t *testing.T) {
	m := newSized()
	p := writeTestArchive(t, "backup.tar", map[string]string{
		"img/logo.png.gz": gzMember(t, append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)),
	})
	tm, cmd := m.Update(archview.OpenEntryMsg{Archive: p, Entry: "img/logo.png.gz"})
	m = drainCmd(tm.(Model), cmd)

	ed := readOnlyEditor(m, archiveEntryPath(p, "img/logo.png"))
	if ed == nil {
		t.Fatal("a binary gz member must still open its metadata notice")
	}
	for _, want := range []string{"Binary content", "logo.png", "Compressed:"} {
		if !strings.Contains(ed.Text(), want) {
			t.Fatalf("metadata notice missing %q:\n%s", want, ed.Text())
		}
	}
}

// TestOpenArchiveGzipEntryNestedArchiveNotice: an inner.tar.gz is an archive
// inside an archive. IKE has no nested view, so it says so — the one thing it
// must never do is refuse it as "binary archive entry".
func TestOpenArchiveGzipEntryNestedArchiveNotice(t *testing.T) {
	inner := tarBytes(t, map[string]string{"a.txt": "hi\n"})
	cases := []struct{ name, entry string }{
		// Named as a tarball: refused before a byte is decompressed.
		{"by name", "nested/inner.tar.gz"},
		// Named as nothing in particular: the tar header in the payload
		// decides.
		{"by payload", "nested/inner.gz"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newSized()
			p := writeTestArchive(t, "outer.tar", map[string]string{c.entry: gzMember(t, inner)})
			tm, cmd := m.Update(archview.OpenEntryMsg{Archive: p, Entry: c.entry})
			m = drainCmd(tm.(Model), cmd)
			for _, ed := range allReadOnly(m) {
				if strings.HasPrefix(ed.Path(), p+entrySep) {
					t.Fatalf("a nested archive must not open a buffer: %q", ed.Path())
				}
			}
		})
	}
}

// allReadOnly collects every read-only editor tab in the model.
func allReadOnly(m Model) []*editor.Model {
	var out []*editor.Model
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if ed.HasFile() && ed.ReadOnly() {
				out = append(out, ed)
			}
		}
	}
	return out
}

// TestOpenArchiveEntryPlainMembersUnaffected guards the seam: adding the gzip
// branch must not change what a plain text or plain binary member does.
func TestOpenArchiveEntryPlainMembersUnaffected(t *testing.T) {
	m := newSized()
	p := writeTestArchive(t, "src.tar", map[string]string{
		"a.txt": "hi\n",
		"a.bin": "\x00\x01\x02binary\x00",
	})
	tm, cmd := m.Update(archview.OpenEntryMsg{Archive: p, Entry: "a.txt"})
	m = drainCmd(tm.(Model), cmd)
	if ed := readOnlyEditor(m, archiveEntryPath(p, "a.txt")); ed == nil || ed.Text() != "hi" {
		t.Fatal("a plain text member must still open unchanged")
	}
	if cmd := m.openArchiveEntry(p, "a.bin"); cmd != nil {
		t.Fatal("a plain binary member must still be refused")
	}
}

// TestArchiveGzipInner keeps the decompressed name in the member's directory.
func TestArchiveGzipInner(t *testing.T) {
	cases := []struct{ entry, inner, want string }{
		{"logs/app.log.gz", "app.log", "logs/app.log"},
		{"app.log.gz", "app.log", "app.log"},
		{"a/b/c/dump.gz", "dump.sql", "a/b/c/dump.sql"},
	}
	for _, c := range cases {
		if got := archiveGzipInner(c.entry, c.inner); got != c.want {
			t.Errorf("archiveGzipInner(%q, %q) = %q, want %q", c.entry, c.inner, got, c.want)
		}
	}
}
