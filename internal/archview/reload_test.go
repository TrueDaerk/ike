package archview

import (
	"archive/tar"
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

// rewriteArchive replaces the tar at p with one holding names.
func rewriteArchive(t *testing.T, p string, names ...string) {
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
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(n)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestReloadPicksUpTheRewrittenArchive (#2314): the pane re-reads its file, so
// a re-packed archive shows its new members in place.
func TestReloadPicksUpTheRewrittenArchive(t *testing.T) {
	p := writeArchive(t, "cmd/main.go", "README.md")
	m := newPane(t, p)
	if got := rowNames(&m); len(got) != 3 {
		t.Fatalf("rows = %v", got)
	}
	rewriteArchive(t, p, "cmd/main.go", "cmd/extra.go", "README.md")
	m.Reload()
	got := strings.Join(rowNames(&m), ",")
	if got != "cmd,cmd/extra.go,cmd/main.go,README.md" {
		t.Fatalf("rows after reload = %s", got)
	}
	if m.Entries() != 3 {
		t.Fatalf("entries = %d, want 3", m.Entries())
	}
	if m.Err() != nil {
		t.Fatalf("reload error: %v", m.Err())
	}
}

// TestReloadKeepsFoldsAndClampsTheCursor: the view state a user built up
// survives where it still applies — a collapsed directory stays collapsed, and
// a cursor past the end of the shorter listing lands on the last row instead
// of pointing nowhere.
func TestReloadKeepsFoldsAndClampsTheCursor(t *testing.T) {
	p := writeArchive(t, "cmd/main.go", "cmd/aux.go", "README.md")
	m := newPane(t, p)
	m.Update(key("space")) // collapse "cmd" under the cursor
	if got := rowNames(&m); len(got) != 2 {
		t.Fatalf("rows after collapse = %v", got)
	}
	m.Update(key("j"))
	if m.Cursor() != 1 {
		t.Fatalf("cursor = %d", m.Cursor())
	}
	rewriteArchive(t, p, "cmd/main.go")
	m.Reload()
	if got := rowNames(&m); len(got) != 1 || got[0] != "cmd" {
		t.Fatalf("rows after reload = %v, want the still-collapsed cmd only", got)
	}
	if m.Cursor() != 0 {
		t.Fatalf("cursor = %d, want it clamped to the last row", m.Cursor())
	}
}

// TestReloadOfABrokenArchiveReportsTheError: a half-written file degrades to
// the pane's own notice, the way opening one does — no stale rows left behind.
func TestReloadOfABrokenArchiveReportsTheError(t *testing.T) {
	p := writeArchive(t, "a.txt")
	m := newPane(t, p)
	if err := os.WriteFile(p, []byte("not a tar at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.Reload()
	if m.Err() == nil {
		t.Fatal("a broken archive must surface its listing error")
	}
	if m.Rows() != 0 {
		t.Fatalf("rows = %v, want none", rowNames(&m))
	}
}
