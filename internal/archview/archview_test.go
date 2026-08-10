package archview

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/theme"
)

// writeArchive builds a tar holding the named members (a trailing "/" makes a
// directory entry) and returns its path.
func writeArchive(t *testing.T, names ...string) string {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, n := range names {
		h := &tar.Header{
			Name:     n,
			Mode:     0o644,
			ModTime:  time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
			Typeflag: tar.TypeReg,
			Size:     int64(len(n)),
		}
		if strings.HasSuffix(n, "/") {
			h.Typeflag, h.Size, h.Mode = tar.TypeDir, 0, 0o755
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(n)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "src.tar")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// newPane builds a sized, focused pane over the archive at p.
func newPane(t *testing.T, p string) Model {
	t.Helper()
	m := New("archive", p, theme.DefaultPalette())
	m.SetSize(80, 20)
	m.SetFocused(true)
	return m
}

// key builds the key press for one chord name, matching the panel test
// convention elsewhere in the tree.
func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// rowNames flattens the visible rows for assertions.
func rowNames(m *Model) []string {
	out := make([]string, m.Rows())
	for i := range out {
		out[i] = m.RowName(i)
	}
	return out
}

// TestTreeGroupsDirectories: the flat entry list becomes a tree with
// directories first, and directories the archive never named are synthesized.
func TestTreeGroupsDirectories(t *testing.T) {
	p := writeArchive(t, "README.md", "src/main.go", "docs/guide/intro.md")
	m := newPane(t, p)
	want := []string{"docs", "docs/guide", "docs/guide/intro.md", "src", "src/main.go", "README.md"}
	if got := rowNames(&m); !equal(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if m.Entries() != 3 {
		t.Fatalf("entries = %d, want 3", m.Entries())
	}
}

// TestNavigationAndOpen: j/k move the cursor, enter on a file asks the root
// model to open that entry.
func TestNavigationAndOpen(t *testing.T) {
	p := writeArchive(t, "README.md", "src/main.go")
	m := newPane(t, p)
	// rows: src, src/main.go, README.md
	if m.Cursor() != 0 {
		t.Fatalf("cursor starts at %d", m.Cursor())
	}
	m.Update(key("j"))
	if m.RowName(m.Cursor()) != "src/main.go" {
		t.Fatalf("j landed on %q", m.RowName(m.Cursor()))
	}
	m.Update(key("k"))
	if m.RowName(m.Cursor()) != "src" {
		t.Fatalf("k landed on %q", m.RowName(m.Cursor()))
	}
	// Enter on the directory folds it instead of opening anything.
	if cmd := m.Update(key("enter")); cmd != nil {
		t.Fatal("enter on a directory must not emit an open")
	}
	if got := rowNames(&m); !equal(got, []string{"src", "README.md"}) {
		t.Fatalf("collapsed rows = %v", got)
	}
	// Expand again and open the file.
	m.Update(key("enter"))
	m.Update(key("j"))
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter on a file must emit an open command")
	}
	msg, ok := cmd().(OpenEntryMsg)
	if !ok {
		t.Fatalf("emitted %T", cmd())
	}
	if msg.Archive != p || msg.Entry != "src/main.go" {
		t.Fatalf("open msg = %+v", msg)
	}
}

// TestCollapseKeepsCursorAndParentJump: h collapses an open directory, then
// jumps to the parent from a nested row.
func TestCollapseKeepsCursorAndParentJump(t *testing.T) {
	p := writeArchive(t, "src/deep/a.go")
	m := newPane(t, p)
	m.Update(key("j")) // src/deep
	m.Update(key("j")) // src/deep/a.go
	if m.RowName(m.Cursor()) != "src/deep/a.go" {
		t.Fatalf("cursor at %q", m.RowName(m.Cursor()))
	}
	m.Update(key("h")) // a file: jump to the parent directory
	if m.RowName(m.Cursor()) != "src/deep" {
		t.Fatalf("h from a file landed on %q", m.RowName(m.Cursor()))
	}
	m.Update(key("h")) // an expanded directory: collapse it
	if got := rowNames(&m); !equal(got, []string{"src", "src/deep"}) {
		t.Fatalf("rows after collapse = %v", got)
	}
}

// TestViewShowsMetadata renders name, size and mtime columns per entry.
func TestViewShowsMetadata(t *testing.T) {
	p := writeArchive(t, "src/main.go")
	m := newPane(t, p)
	v := m.View()
	for _, want := range []string{"src.tar", "tar · 1 entry", "main.go", "11 B", "2026-08-10", "-rw-r--r--"} {
		if !strings.Contains(v, want) {
			t.Errorf("view is missing %q:\n%s", want, v)
		}
	}
	if !strings.Contains(v, "▾") {
		t.Errorf("an expanded directory needs its fold glyph:\n%s", v)
	}
}

// TestCorruptArchiveShowsNotice: a file that claims to be a tar but is not
// degrades to an error notice instead of crashing or rendering junk.
func TestCorruptArchiveShowsNotice(t *testing.T) {
	junk := bytes.Repeat([]byte{0x7f}, 2048)
	copy(junk[257:], "ustar")
	p := filepath.Join(t.TempDir(), "broken.tar")
	if err := os.WriteFile(p, junk, 0o644); err != nil {
		t.Fatal(err)
	}
	m := newPane(t, p)
	if m.Err() == nil {
		t.Fatal("a corrupt archive must record its read error")
	}
	if m.Rows() != 0 {
		t.Fatalf("rows = %d, want none", m.Rows())
	}
	v := m.View()
	if !strings.Contains(v, "cannot read archive") || !strings.Contains(v, "broken.tar") {
		t.Fatalf("view must explain itself:\n%s", v)
	}
	// Keys on an empty pane are inert, not panics.
	m.Update(key("j"))
	m.Update(key("enter"))
}

// TestMissingArchiveShowsNotice: a vanished file (session restore) opens the
// pane with a notice rather than breaking the layout.
func TestMissingArchiveShowsNotice(t *testing.T) {
	m := newPane(t, filepath.Join(t.TempDir(), "gone.tar"))
	if m.Err() == nil {
		t.Fatal("a missing archive must record its error")
	}
	if !strings.Contains(m.View(), "cannot read archive") {
		t.Fatalf("view must explain itself:\n%s", m.View())
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
