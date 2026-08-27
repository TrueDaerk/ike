package httphistory

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"ike/internal/httpclient"
)

// spoolEntry is one entry whose body lives in a file, the way the dispatcher
// hands it over past httpclient.SpoolThreshold (#2157): Body is the head, the
// file holds all of it.
func spoolEntry(t *testing.T, n int, body string) Entry {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spool-"+strconv.Itoa(n)+".bin")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	e := entry(n)
	e.Body = []byte(body[:8])
	e.BodyFile = path
	e.BodySize = len(body)
	return e
}

// TestAppendAdoptsTheSpoolFile: the dispatcher's temp file dies with the
// process, so the store copies it into its own bodies/ directory and records
// a name relative to itself.
func TestAppendAdoptsTheSpoolFile(t *testing.T) {
	dir := t.TempDir() + "/http"
	s := New(dir)
	body := strings.Repeat("payload ", 500)
	e := spoolEntry(t, 1, body)
	s.Append("/p/req.http", "create", e)

	got := s.List("/p/req.http", "create")
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	if got[0].BodyFile == e.BodyFile {
		t.Error("the entry still points at the dispatcher's temp file")
	}
	if filepath.Dir(got[0].BodyFile) != filepath.Join(dir, bodiesDir) {
		t.Errorf("body file outside bodies/: %s", got[0].BodyFile)
	}
	if got[0].BodySizeBytes() != len(body) {
		t.Errorf("body size = %d, want %d", got[0].BodySizeBytes(), len(body))
	}
	if string(got[0].FullBody()) != body {
		t.Error("FullBody does not return the whole stored body")
	}
	// The head is still stored inline, so showing the entry costs no read.
	if string(got[0].Body) != body[:8] {
		t.Errorf("head = %q", got[0].Body)
	}

	// Recorded relative: the raw JSON must not carry an absolute temp path.
	raw, err := os.ReadFile(filepath.Join(dir, mustHistoryFileName(t, dir)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), e.BodyFile) {
		t.Error("the stored file records an absolute dispatcher path")
	}
	if !strings.Contains(string(raw), bodiesDir) {
		t.Errorf("no bodyFile recorded:\n%s", raw)
	}
}

// mustHistoryFileName finds the single history JSON in dir.
func mustHistoryFileName(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			return e.Name()
		}
	}
	t.Fatal("no history file")
	return ""
}

// TestPruneDropsBodyFiles: bodies/ follows the five-entry ring instead of
// growing without bound.
func TestPruneDropsBodyFiles(t *testing.T) {
	dir := t.TempDir() + "/http"
	s := New(dir)
	for i := 0; i < MaxPerRequest+3; i++ {
		s.Append("/p/req.http", "create", spoolEntry(t, i, strings.Repeat("x", 200)+strconv.Itoa(i)))
	}
	if got := len(s.List("/p/req.http", "create")); got != MaxPerRequest {
		t.Fatalf("entries = %d, want %d", got, MaxPerRequest)
	}
	files, err := os.ReadDir(filepath.Join(dir, bodiesDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != MaxPerRequest {
		t.Errorf("bodies/ holds %d files, want %d", len(files), MaxPerRequest)
	}
}

// TestResponseCarriesTheSpool: the viewer's Response shape keeps the file and
// the total size, so the pane can render the head and load the rest.
func TestResponseCarriesTheSpool(t *testing.T) {
	dir := t.TempDir() + "/http"
	s := New(dir)
	body := strings.Repeat("abcd", 400)
	s.Append("/p/req.http", "create", spoolEntry(t, 1, body))

	resp := s.List("/p/req.http", "create")[0].Response("create")
	if !resp.Spooled() {
		t.Fatal("the restored response is not spooled")
	}
	if resp.BodyBytes() != len(body) {
		t.Errorf("BodyBytes = %d, want %d", resp.BodyBytes(), len(body))
	}
	full, err := resp.FullBody()
	if err != nil || string(full) != body {
		t.Errorf("FullBody = %d bytes, %v", len(full), err)
	}
}

// TestFromResponseCarriesTheSpool: the conversion in the other direction keeps
// the dispatcher's file and size.
func TestFromResponseCarriesTheSpool(t *testing.T) {
	resp := &httpclient.Response{
		Status: "200 OK", Proto: "HTTP/1.1",
		Body: []byte("head"), SpoolPath: "/tmp/nope.bin", BodySize: 4096,
	}
	e := FromResponse(resp, time.Now())
	if e.BodyFile != "/tmp/nope.bin" || e.BodySize != 4096 {
		t.Errorf("entry = %q / %d", e.BodyFile, e.BodySize)
	}
}

// TestMissingSpoolDegradesToTheHead: a dispatcher file already gone when the
// entry is stored costs the entry its full body, never the entry itself.
func TestMissingSpoolDegradesToTheHead(t *testing.T) {
	dir := t.TempDir() + "/http"
	s := New(dir)
	e := entry(1)
	e.BodyFile = filepath.Join(t.TempDir(), "gone.bin")
	e.BodySize = 9999
	s.Append("/p/req.http", "create", e)

	got := s.List("/p/req.http", "create")
	if len(got) != 1 {
		t.Fatalf("the entry was lost with its body file")
	}
	if got[0].BodyFile != "" || got[0].BodySizeBytes() != len(got[0].Body) {
		t.Errorf("stale body file recorded: %q / %d", got[0].BodyFile, got[0].BodySizeBytes())
	}
	if string(got[0].FullBody()) != string(got[0].Body) {
		t.Error("FullBody should be the head when there is no file")
	}
}
