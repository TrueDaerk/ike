package httphistory

import (
	"encoding/json"
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

// TestRelativeStoreDirYieldsOpenablePaths is the #2385 regression: the
// production store is rooted at the *relative* ".ike/http", and appending a
// spooled entry used to record "../../bodies/…" (filepath.Rel of an
// already-relative adopted name against the store dir), which List then
// collapsed to a bare "bodies/…" the viewer could not open. Every path the
// store hands out must be absolute and openable — from any working directory.
func TestRelativeStoreDirYieldsOpenablePaths(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	s := New(filepath.Join(".ike", "http"))
	body := strings.Repeat("payload ", 500)
	s.Append("/p/req.http", "create", spoolEntry(t, 1, body))
	// A second append pushes the first entry through the List → serialize
	// round trip again — the loop the corruption used to happen in.
	s.Append("/p/req.http", "create", spoolEntry(t, 2, body))

	raw, err := os.ReadFile(filepath.Join(work, ".ike", "http", mustHistoryFileName(t, filepath.Join(work, ".ike", "http"))))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "..") {
		t.Errorf("the stored file records a ..-shaped body path:\n%s", raw)
	}

	// The viewer keeps paths across directory changes (#2385): what List
	// handed out must still open after a chdir.
	t.Chdir(t.TempDir())
	for _, e := range s.List("/p/req.http", "create") {
		if !filepath.IsAbs(e.BodyFile) {
			t.Fatalf("List handed out a relative body path: %q", e.BodyFile)
		}
		resp := e.Response("create")
		full, err := resp.FullBody()
		if err != nil || string(full) != body {
			t.Errorf("body file %q not openable after chdir: %v", resp.SpoolPath, err)
		}
	}
}

// TestListHealsLegacyDotDotPaths: entries written while #2385 corrupted the
// stored name to "../../bodies/<name>" must resolve to the file that actually
// exists in bodies/ — the base name is the whole identity of an adopted body.
func TestListHealsLegacyDotDotPaths(t *testing.T) {
	dir := t.TempDir() + "/http"
	s := New(dir)
	if err := os.MkdirAll(filepath.Join(dir, bodiesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, bodiesDir, "body-legacy.bin"), []byte("whole body"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := entry(1)
	e.BodyFile = "../../bodies/body-legacy.bin"
	e.BodySize = 10
	data, err := json.Marshal([]Entry{e})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.file("/p/req.http", "create"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	got := s.List("/p/req.http", "create")
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	if string(got[0].FullBody()) != "whole body" {
		t.Errorf("legacy path %q did not heal to the bodies/ file", got[0].BodyFile)
	}
}

// TestAdoptKeepsTheContentTypeExtension: the adopted copy keeps the spool
// file's extension (#2385) — it decides which language the editor opens the
// body under.
func TestAdoptKeepsTheContentTypeExtension(t *testing.T) {
	dir := t.TempDir() + "/http"
	s := New(dir)
	spool := filepath.Join(t.TempDir(), "body-123.json")
	if err := os.WriteFile(spool, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	e := entry(1)
	e.BodyFile, e.BodySize = spool, 7
	s.Append("/p/req.http", "create", e)

	got := s.List("/p/req.http", "create")
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	if filepath.Ext(got[0].BodyFile) != ".json" {
		t.Errorf("adopted body lost its extension: %q", got[0].BodyFile)
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
