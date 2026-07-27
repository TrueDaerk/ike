package httphistory

import (
	"net/http"
	"os"
	"strconv"
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
