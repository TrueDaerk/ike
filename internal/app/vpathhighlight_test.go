package app

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/archview"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/watch"
)

// vpathhighlight_test.go pins #1853: a read-only buffer whose path is virtual
// — "<archive>!<entry>" for an archive member, "<file>.gz!<inner>" for a gzip
// preview — is highlighted exactly like a file on disk with the same tail, and
// stays highlighted when the content is re-installed. Nothing about it reaches
// the LSP: no on-disk file answers to the path, so a didOpen for one would
// name a document no server can ever read.

// vpathExt is an extension no language plugin claims, so these tests own the
// language a virtual path resolves to. Registration is process-global (the
// plugin registry has no test scope), which is why the extension is unique.
const vpathExt = ".ike1853"

// registerVPathLang registers vpathExt with a Go span producer (#1585) that
// captures the first word of every line. Go-produced spans need no Tree-sitter
// grammar, so the assertions below hold in the CGo build and the stub alike —
// what is under test is the scheduling and routing of the parse, not any
// particular grammar.
func registerVPathLang(t *testing.T) {
	t.Helper()
	lang.Register(lang.Language{
		ID:         "vpath-test",
		Extensions: []string{"ike1853"},
		Spans: func(lines []string) []lang.Span {
			var out []lang.Span
			for i, line := range lines {
				n := 0
				for n < len(line) && line[n] != ' ' {
					n++
				}
				if n > 0 {
					out = append(out, lang.Span{Line: i, EndCol: n, Capture: "keyword"})
				}
			}
			return out
		},
	})
}

// vpathBody is the buffer content every case below installs: the first word is
// what the test language captures.
const vpathBody = "keyword tail\nsecond line\n"

// wantHighlighted fails unless the parse landed in the buffer's span index.
func wantHighlighted(t *testing.T, m Model, vpath, what string) {
	t.Helper()
	ed := readOnlyEditor(m, vpath)
	if ed == nil {
		t.Fatalf("%s: no read-only buffer for %q", what, vpath)
	}
	if got := ed.SyntaxCapture(0, 0); got != "keyword" {
		t.Fatalf("%s: capture at 0,0 = %q, want %q — the buffer is not highlighted", what, got, "keyword")
	}
}

// writeGzNamed gzips body into dir/name, recording inner as the gzip header's
// original-name field — the only thing that names the content when the file
// name itself strips to nothing useful.
func writeGzNamed(t *testing.T, dir, name, inner string, body []byte) string {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Name = inner
	if _, err := zw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestArchiveEntryBufferIsHighlighted: opening an entry from the archive pane
// schedules the parse and the result lands in the entry's own buffer, keyed by
// the virtual path.
func TestArchiveEntryBufferIsHighlighted(t *testing.T) {
	registerVPathLang(t)
	m := newSized()
	p := writeTestArchive(t, "src.tar.gz", map[string]string{"cmd/main" + vpathExt: vpathBody})

	tm, cmd := m.Update(archview.OpenEntryMsg{Archive: p, Entry: "cmd/main" + vpathExt})
	m = drainCmd(tm.(Model), cmd)

	wantHighlighted(t, m, archiveEntryPath(p, "cmd/main"+vpathExt), "archive entry")
}

// TestArchiveEntryBufferIsHighlightedBesideOpenTabs: the entry lands in a
// fresh tab when the editor already holds a file, which is the ordinary case —
// the parse must be scheduled for that tab, not for whatever was active.
func TestArchiveEntryBufferIsHighlightedBesideOpenTabs(t *testing.T) {
	registerVPathLang(t)
	m := newSized()
	dir := t.TempDir()
	other := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(other, []byte("plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPath(other, false)
	m = drainCmd(tm.(Model), cmd)

	p := writeTestArchive(t, "src.tar", map[string]string{"main" + vpathExt: vpathBody})
	tm, cmd = m.Update(archview.OpenEntryMsg{Archive: p, Entry: "main" + vpathExt})
	m = drainCmd(tm.(Model), cmd)

	wantHighlighted(t, m, archiveEntryPath(p, "main"+vpathExt), "archive entry in a second tab")
}

// TestGzipBufferIsHighlighted: the gz viewer's buffer is highlighted from the
// inner file name the same way.
func TestGzipBufferIsHighlighted(t *testing.T) {
	registerVPathLang(t)
	m := newSized()
	p := writeGzFile(t, "payload"+vpathExt+".gz", []byte(vpathBody))

	tm, cmd := m.Update(OpenGzipMsg{Path: p})
	m = drainCmd(tm.(Model), cmd)

	wantHighlighted(t, m, archiveEntryPath(p, "payload"+vpathExt), "gz buffer")
}

// TestGzipBufferKeepsHighlightingAfterRefresh is the link that actually broke
// (#1853): the watcher's re-install goes through ShowReadOnly, which drops the
// cached spans and advances the document version. A read-only buffer can never
// schedule a parse of its own, so without one here the preview stays plain for
// the rest of the session.
func TestGzipBufferKeepsHighlightingAfterRefresh(t *testing.T) {
	registerVPathLang(t)
	m := newSized()
	dir := t.TempDir()
	p := writeGzInto(t, filepath.Join(dir, "payload"+vpathExt+".gz"), []byte(vpathBody))

	tm, cmd := m.Update(OpenGzipMsg{Path: p})
	m = drainCmd(tm.(Model), cmd)

	writeGzInto(t, p, []byte("keyword changed\nsecond line\n"))
	tm, cmd = m.Update(watch.EventMsg{Path: p, Kind: watch.FileChanged})
	m = drainCmd(tm.(Model), cmd)

	vpath := archiveEntryPath(p, "payload"+vpathExt)
	if ed := readOnlyEditor(m, vpath); ed == nil || ed.Text() != "keyword changed\nsecond line" {
		t.Fatalf("the refresh did not re-install the content")
	}
	wantHighlighted(t, m, vpath, "refreshed gz buffer")
}

// TestGzipHeaderNamesTheContentWhenTheFileNameCannot: dump.gz says nothing
// about what it holds, so the gzip header's original-name field is the only
// thing that can give the buffer a language.
func TestGzipHeaderNamesTheContentWhenTheFileNameCannot(t *testing.T) {
	registerVPathLang(t)
	m := newSized()
	p := writeGzNamed(t, t.TempDir(), "dump.gz", "payload"+vpathExt, []byte(vpathBody))

	tm, cmd := m.Update(OpenGzipMsg{Path: p})
	m = drainCmd(tm.(Model), cmd)

	wantHighlighted(t, m, archiveEntryPath(p, "payload"+vpathExt), "header-named gz buffer")
}

// TestVirtualPathBufferWithoutALanguageStaysPlain: an entry whose name carries
// no known extension is shown as plain text — no spans, no crash.
func TestVirtualPathBufferWithoutALanguageStaysPlain(t *testing.T) {
	m := newSized()
	p := writeTestArchive(t, "src.tar", map[string]string{"NOTICE": vpathBody})

	tm, cmd := m.Update(archview.OpenEntryMsg{Archive: p, Entry: "NOTICE"})
	m = drainCmd(tm.(Model), cmd)

	ed := readOnlyEditor(m, archiveEntryPath(p, "NOTICE"))
	if ed == nil {
		t.Fatal("the entry must still open")
	}
	if got := ed.SyntaxCapture(0, 0); got != "" {
		t.Fatalf("capture at 0,0 = %q, want none for a language-less entry", got)
	}
}

// TestVirtualPathBuffersNeverReachTheLSP: EventFileOpened is the didOpen
// trigger (#332). No file on disk answers to "<archive>!<entry>", so a virtual
// path must never be announced — the buffers are highlighted by Tree-sitter
// alone, LSP support for them is explicitly out of scope.
func TestVirtualPathBuffersNeverReachTheLSP(t *testing.T) {
	registerVPathLang(t)
	var opened []string
	reg := registry.New()
	reg.Add(fakePlugin{id: "p", caps: plugin.Capabilities{
		Hooks: []plugin.Hook{{
			ID: "p.hook", Event: plugin.EventFileOpened,
			Notify: func(h host.API, payload any) tea.Cmd {
				opened = append(opened, payload.(string))
				return nil
			},
		}},
	}})
	m := NewWith(reg, host.MapConfig{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)

	arch := writeTestArchive(t, "src.tar", map[string]string{"main" + vpathExt: vpathBody})
	m = drainCmd(m, m.openArchiveEntry(arch, "main"+vpathExt))

	gz := writeGzFile(t, "payload"+vpathExt+".gz", []byte(vpathBody))
	m = drainCmd(m, m.openGzipFile(gz))

	if len(opened) != 0 {
		t.Fatalf("virtual-path buffers announced to the LSP: %v", opened)
	}
}
