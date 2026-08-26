package app

// replace_stale_test.go covers the #2154 apply-path guarantees: disk writes
// preserve the file's encoding and line endings, and a file whose mtime moved
// on since the scan is skipped whole with a notice — never silently
// corrupted. The finder-side capture of the mtimes is tested in
// internal/finder; here the request carries them directly.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ike/internal/locations"
	"ike/internal/search"
	"ike/internal/textenc"
)

func TestReplaceOnDiskPreservesEncodingAndEOL(t *testing.T) {
	m := newSized()
	path := filepath.Join(t.TempDir(), "u16.txt")
	data, err := textenc.Encode("a needle\nplain\n", textenc.UTF16LE, textenc.CRLF)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(replaceReq([]locations.Item{
		itemAt(path, 1, "a needle", 2, 8),
	}, "thread", search.Query{Pattern: "needle"}))
	_ = tm
	got, _ := os.ReadFile(path)
	want, err := textenc.Encode("a thread\nplain\n", textenc.UTF16LE, textenc.CRLF)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("UTF-16 LE + CRLF must survive the rewrite:\ngot  %x\nwant %x", got, want)
	}
}

func TestReplaceSkipsFileChangedSinceScan(t *testing.T) {
	m := newSized()
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("a needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := replaceReq([]locations.Item{
		itemAt(path, 1, "a needle", 2, 8),
	}, "thread", search.Query{Pattern: "needle"})
	// The recorded mtime predates the file: it "changed" after the scan.
	req.Mtimes = map[string]time.Time{path: time.Now().Add(-time.Hour)}
	tm, _ := m.Update(req)
	m = tm.(Model)
	data, _ := os.ReadFile(path)
	if string(data) != "a needle\n" {
		t.Fatalf("a changed file must not be touched: %q", data)
	}
	if len(m.history) == 0 || !strings.Contains(m.history[len(m.history)-1].text, "changed since the search") {
		t.Fatalf("summary must report the skipped file, history=%+v", m.history)
	}
}

func TestReplaceRefreshesMtimeAfterOwnWrite(t *testing.T) {
	m := newSized()
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("a needle\nb needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mtimes := map[string]time.Time{path: fi.ModTime()}
	q := search.Query{Pattern: "needle"}

	first := replaceReq([]locations.Item{itemAt(path, 1, "a needle", 2, 8)}, "thread", q)
	first.Mtimes = mtimes
	tm, _ := m.Update(first)
	m = tm.(Model)

	// The same scan's second batch applies too: our own write refreshed the
	// shared mtime baseline, so it is not read as an external change.
	second := replaceReq([]locations.Item{itemAt(path, 2, "b needle", 2, 8)}, "thread", q)
	second.Mtimes = mtimes
	tm, _ = m.Update(second)
	_ = tm
	data, _ := os.ReadFile(path)
	if string(data) != "a thread\nb thread\n" {
		t.Fatalf("both batches must apply: %q", data)
	}
}
