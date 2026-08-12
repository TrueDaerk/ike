package httphistory

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ike/internal/httpclient"
)

// snapshot is the as-sent request of the test entries (#1832).
func snapshot() *httpclient.RequestSnapshot {
	return &httpclient.RequestSnapshot{
		Method:  "POST",
		URL:     "https://example.test/things?v=1",
		Headers: http.Header{"Content-Type": {"application/json"}, "X-Token": {"s3cret"}},
		Body:    []byte(`{"name":"example"}`),
	}
}

// TestAppendStoresRequestSnapshot: the as-sent request survives the round trip
// through .ike/http/ and stays readable on disk (#1832).
func TestAppendStoresRequestSnapshot(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	e := entry(1)
	e.Request = snapshot()
	s.Append("/p/req.http", "create", e)

	got := s.List("/p/req.http", "create")
	if len(got) != 1 {
		t.Fatalf("entries: %d, want 1", len(got))
	}
	snap := got[0].Request
	if snap == nil {
		t.Fatal("the stored entry must carry the as-sent request")
	}
	if snap.Method != "POST" || snap.URL != "https://example.test/things?v=1" {
		t.Errorf("request line: %s %s", snap.Method, snap.URL)
	}
	if snap.Headers.Get("X-Token") != "s3cret" {
		t.Errorf("headers: %v", snap.Headers)
	}
	if string(snap.Body) != `{"name":"example"}` {
		t.Errorf("body: %q", snap.Body)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("history files: %v (%v)", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"request"`) {
		t.Errorf("the file must hold the request snapshot:\n%s", data)
	}
	if !strings.Contains(string(data), `"bodyText": "{\"name\":\"example\"}"`) {
		t.Errorf("a text request body must be stored readable, not base64:\n%s", data)
	}
}

// TestBinaryRequestBodyRoundTrips: a non-text payload falls back to the base64
// field, exactly like a binary response body (#1832).
func TestBinaryRequestBodyRoundTrips(t *testing.T) {
	s := New(t.TempDir())
	e := entry(1)
	e.Request = snapshot()
	e.Request.Body = []byte{0x00, 0x01, 0xff}
	s.Append("req.http", "0", e)

	got := s.List("req.http", "0")
	if len(got) != 1 || got[0].Request == nil {
		t.Fatal("the snapshot must survive")
	}
	if string(got[0].Request.Body) != string([]byte{0x00, 0x01, 0xff}) {
		t.Errorf("binary body: %v", got[0].Request.Body)
	}
}

// TestLegacyEntryLoadsWithoutSnapshot: history files written before the
// capture existed keep loading; they simply have no request, which is what
// disables re-send for them (#1832).
func TestLegacyEntryLoadsWithoutSnapshot(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	legacy := `[{"time":"2026-07-27T12:00:00Z","status":"200 OK","statusCode":200,` +
		`"proto":"HTTP/1.1","headers":{"Content-Type":["application/json"]},` +
		`"bodyText":"{\"ok\":true}","duration":3000000}]`
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := s.file("/p/req.http", "create")
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	got := s.List("/p/req.http", "create")
	if len(got) != 1 {
		t.Fatalf("entries: %d, want the legacy entry to load", len(got))
	}
	if string(got[0].Body) != `{"ok":true}` || got[0].Status != "200 OK" {
		t.Errorf("legacy entry damaged: %+v", got[0])
	}
	if got[0].Request != nil {
		t.Error("a legacy entry must have no request snapshot")
	}
	if got[0].Response("create").Request != nil {
		t.Error("the restored response must have no snapshot either")
	}
	if got[0].Time.IsZero() || got[0].Duration != 3*time.Millisecond {
		t.Errorf("legacy metadata: %v / %v", got[0].Time, got[0].Duration)
	}
}

// TestFromResponseCarriesSnapshot: what the dispatcher captured is what gets
// stored (#1832).
func TestFromResponseCarriesSnapshot(t *testing.T) {
	resp := &httpclient.Response{Status: "200 OK", StatusCode: 200, Request: snapshot()}
	e := FromResponse(resp, time.Now())
	if e.Request == nil || e.Request.URL != resp.Request.URL {
		t.Fatalf("snapshot lost: %+v", e.Request)
	}
	if back := e.Response("one").Request; back == nil || back.URL != resp.Request.URL {
		t.Fatalf("snapshot lost on the way back: %+v", back)
	}
}
