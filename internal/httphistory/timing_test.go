package httphistory

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"ike/internal/httpclient"
)

// TestTimingRoundTrip: the phase breakdown is stored with the entry (#2404),
// so a browsed history response and a D/P diff show the same
// dns/connect/tls/ttfb/transfer line the fresh answer did.
func TestTimingRoundTrip(t *testing.T) {
	resp := &httpclient.Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
		Headers: http.Header{"X-A": {"1"}}, Body: []byte("{}"),
		Duration:   250 * time.Millisecond,
		RequestKey: "create",
		Timing: &httpclient.Timing{
			DNS: 2 * time.Millisecond, Connect: 11 * time.Millisecond,
			TLS: 34 * time.Millisecond, TTFB: 210 * time.Millisecond,
			Transfer: 4 * time.Millisecond, Reused: false,
		},
	}
	dir := t.TempDir()
	s := New(dir)
	s.Append("req.http", "create", FromResponse(resp, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)))

	got := s.List("req.http", "create")
	if len(got) != 1 {
		t.Fatal("entry missing")
	}
	back := got[0].Response("create").Timing
	if back == nil {
		t.Fatal("timing lost in the round trip")
	}
	if *back != *resp.Timing {
		t.Fatalf("timing = %+v, want %+v", *back, *resp.Timing)
	}
	if line := back.String(); !strings.Contains(line, "ttfb 210ms") {
		t.Fatalf("restored breakdown renders as %q", line)
	}
}

// TestEntryWithoutTimingStillLoads: files written before #2404 carry no
// "timing" key and must read back as "no breakdown", not as zeros.
func TestEntryWithoutTimingStillLoads(t *testing.T) {
	s := New(t.TempDir())
	s.Append("req.http", "create", entry(1))
	got := s.List("req.http", "create")
	if len(got) != 1 {
		t.Fatal("entry missing")
	}
	if tm := got[0].Response("create").Timing; tm != nil {
		t.Fatalf("timing = %+v, want nil", tm)
	}
}
