package httphistory

import (
	"bytes"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"ike/internal/httpclient"
)

func entry(n int) Entry {
	return Entry{
		Time:       time.Date(2026, 7, 27, 12, 0, n, 0, time.UTC),
		Status:     "200 OK",
		StatusCode: 200,
		Proto:      "HTTP/1.1",
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       []byte("body-" + strconv.Itoa(n)),
		Duration:   time.Duration(n) * time.Millisecond,
	}
}

func TestAppendAndListNewestFirst(t *testing.T) {
	s := New(t.TempDir() + "/http")
	for i := 0; i < 3; i++ {
		s.Append("/p/req.http", "create", entry(i))
	}
	got := s.List("/p/req.http", "create")
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d", len(got))
	}
	if string(got[0].Body) != "body-2" || string(got[2].Body) != "body-0" {
		t.Errorf("order wrong: %s .. %s", got[0].Body, got[2].Body)
	}
	if got[0].Headers.Get("Content-Type") != "application/json" {
		t.Errorf("headers lost: %v", got[0].Headers)
	}
}

func TestPruneBeyondMax(t *testing.T) {
	s := New(t.TempDir())
	for i := 0; i < MaxPerRequest+3; i++ {
		s.Append("req.http", "0", entry(i))
	}
	got := s.List("req.http", "0")
	if len(got) != MaxPerRequest {
		t.Fatalf("want %d entries, got %d", MaxPerRequest, len(got))
	}
	if string(got[0].Body) != "body-7" {
		t.Errorf("newest must survive: %s", got[0].Body)
	}
	if string(got[MaxPerRequest-1].Body) != "body-3" {
		t.Errorf("oldest kept: %s", got[MaxPerRequest-1].Body)
	}
}

func TestKeysAreIndependent(t *testing.T) {
	s := New(t.TempDir())
	s.Append("req.http", "a", entry(1))
	s.Append("req.http", "b", entry(2))
	s.Append("other.http", "a", entry(3))
	if n := len(s.List("req.http", "a")); n != 1 {
		t.Errorf("req/a: %d", n)
	}
	if n := len(s.List("req.http", "b")); n != 1 {
		t.Errorf("req/b: %d", n)
	}
	if n := len(s.List("other.http", "a")); n != 1 {
		t.Errorf("other/a: %d", n)
	}
	if got := s.List("req.http", "missing"); got != nil {
		t.Errorf("missing key must be nil, got %v", got)
	}
}

func TestCorruptFileYieldsNil(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	s.Append("req.http", "0", entry(1))
	if err := os.WriteFile(s.file("req.http", "0"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := s.List("req.http", "0"); got != nil {
		t.Errorf("corrupt file must yield nil, got %v", got)
	}
}

func TestRoundTripResponse(t *testing.T) {
	resp := &httpclient.Response{
		Status: "201 Created", StatusCode: 201, Proto: "HTTP/1.1",
		Headers: http.Header{"X-A": {"1"}}, Body: []byte{0x00, 0x01, 0xff},
		Truncated: true, Duration: 5 * time.Millisecond,
		RequestKey: "create", Warnings: []string{"w"},
	}
	at := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	s := New(t.TempDir())
	s.Append("req.http", "create", FromResponse(resp, at))
	got := s.List("req.http", "create")
	if len(got) != 1 {
		t.Fatal("entry missing")
	}
	back := got[0].Response("create")
	if back.StatusCode != 201 || back.Status != "201 Created" || !back.Truncated {
		t.Errorf("round trip: %+v", back)
	}
	if string(back.Body) != string(resp.Body) {
		t.Errorf("binary body must round-trip: %v", back.Body)
	}
	if back.Warnings[0] != "w" || back.Headers.Get("X-A") != "1" {
		t.Errorf("fields lost: %+v", back)
	}
	if !got[0].Time.Equal(at) {
		t.Errorf("time: %v", got[0].Time)
	}
}

// TestTextBodyStoredReadable: a text body lands on disk as a JSON string, so
// the history file is readable and diffable without base64 decoding (#1267).
func TestTextBodyStoredReadable(t *testing.T) {
	s := New(t.TempDir())
	body := `{"name":"exämple","n":1}`
	s.Append("/p/req.http", "one", Entry{Time: time.Now(), Status: "200 OK", Body: []byte(body)})

	raw, err := os.ReadFile(s.file("/p/req.http", "one"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"bodyText"`) {
		t.Errorf("text body must be stored under bodyText:\n%s", raw)
	}
	if strings.Contains(string(raw), `"body"`) {
		t.Errorf("no base64 body field expected:\n%s", raw)
	}
	// The body itself is legible in the file (JSON-escaped quotes aside).
	if !strings.Contains(string(raw), `exämple`) && !strings.Contains(string(raw), "exämple") {
		t.Errorf("body text must appear verbatim:\n%s", raw)
	}
	if got := s.List("/p/req.http", "one"); len(got) != 1 || string(got[0].Body) != body {
		t.Errorf("round trip: %+v", got)
	}
}

// TestBinaryBodyStaysBase64: a body that is not valid UTF-8 (or carries NUL)
// keeps the base64 field so binary responses survive.
func TestBinaryBodyStaysBase64(t *testing.T) {
	s := New(t.TempDir())
	body := []byte{0x00, 0xff, 0xfe, 'a'}
	s.Append("/p/req.http", "bin", Entry{Time: time.Now(), Status: "200 OK", Body: body})

	raw, err := os.ReadFile(s.file("/p/req.http", "bin"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"bodyText"`) {
		t.Errorf("binary body must not be stored as text:\n%s", raw)
	}
	got := s.List("/p/req.http", "bin")
	if len(got) != 1 || !bytes.Equal(got[0].Body, body) {
		t.Errorf("binary round trip: %+v", got)
	}
}

// TestLegacyBase64FileStillLoads: files written before the split (text bodies
// as base64 "body") keep loading — the reader is tolerant.
func TestLegacyBase64FileStillLoads(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	legacy := `[{"time":"2026-07-27T10:00:00Z","status":"200 OK","statusCode":200,` +
		`"proto":"HTTP/1.1","body":"eyJvayI6dHJ1ZX0=","duration":1000000}]`
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.file("/p/req.http", "old"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	got := s.List("/p/req.http", "old")
	if len(got) != 1 {
		t.Fatalf("legacy file must load, got %d entries", len(got))
	}
	if string(got[0].Body) != `{"ok":true}` {
		t.Errorf("legacy body: %q", got[0].Body)
	}
	if got[0].Status != "200 OK" || got[0].StatusCode != 200 {
		t.Errorf("legacy metadata: %+v", got[0])
	}
}

// TestHistoryFileIsIndented keeps the file human-readable as a whole.
func TestHistoryFileIsIndented(t *testing.T) {
	s := New(t.TempDir())
	s.Append("/p/req.http", "one", Entry{Time: time.Now(), Status: "200 OK", Body: []byte("hi")})
	raw, err := os.ReadFile(s.file("/p/req.http", "one"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "\n  ") {
		t.Errorf("history file must be indented:\n%s", raw)
	}
}
