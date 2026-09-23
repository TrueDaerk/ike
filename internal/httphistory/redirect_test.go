package httphistory

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"ike/internal/httpclient"
)

// TestRedirectChainRoundTrip: the chain is stored with the entry (#2716), so
// a browsed history response shows the same block the fresh answer did.
func TestRedirectChainRoundTrip(t *testing.T) {
	resp := &httpclient.Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
		Headers: http.Header{"X-A": {"1"}}, Body: []byte("{}"),
		Duration:   250 * time.Millisecond,
		RequestKey: "create",
		Redirects: []httpclient.Hop{
			{Method: "POST", URL: "http://example.com/a", Status: 301,
				Location: "https://example.com/b", DNSAddrs: []string{"93.184.216.34"},
				RemoteAddr: "93.184.216.34:80", MethodChanged: true},
			{Method: "GET", URL: "https://example.com/b", Status: 200,
				RemoteAddr: "93.184.216.34:443", TLSServerName: "example.com",
				Proto: "h2", Reused: true},
		},
		FinalURL: "https://example.com/b",
	}
	dir := t.TempDir()
	s := New(dir)
	s.Append("req.http", "create", FromResponse(resp, time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)))

	got := s.List("req.http", "create")
	if len(got) != 1 {
		t.Fatal("entry missing")
	}
	back := got[0].Response("create")
	if !reflect.DeepEqual(back.Redirects, resp.Redirects) {
		t.Fatalf("chain = %+v, want %+v", back.Redirects, resp.Redirects)
	}
	if back.FinalURL != resp.FinalURL {
		t.Errorf("FinalURL = %q, want %q", back.FinalURL, resp.FinalURL)
	}
	if back.RedirectCount() != 1 {
		t.Errorf("RedirectCount = %d, want 1", back.RedirectCount())
	}

	// The stored file stays readable: the chain is plain JSON, not base64.
	names, err := os.ReadDir(dir)
	if err != nil || len(names) == 0 {
		t.Fatalf("store dir: %v (%d entries)", err, len(names))
	}
	raw, err := os.ReadFile(filepath.Join(dir, names[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"redirects"`, `"finalUrl"`, `"methodChanged"`, "example.com"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("stored file misses %s:\n%s", want, raw)
		}
	}
}

// TestRedirectsOffRoundTrip: a declined redirect is stored too, so a restored
// entry still says the 3xx was not followed (#2716).
func TestRedirectsOffRoundTrip(t *testing.T) {
	resp := &httpclient.Response{
		Status: "301 Moved Permanently", StatusCode: 301, Proto: "HTTP/1.1",
		Headers:  http.Header{"Location": {"https://example.com/b"}},
		Duration: time.Millisecond, RequestKey: "create",
		FinalURL: "http://example.com/a", RedirectsOff: true,
	}
	s := New(t.TempDir())
	s.Append("req.http", "create", FromResponse(resp, time.Now()))

	back := s.List("req.http", "create")[0].Response("create")
	if !back.RedirectsOff {
		t.Error("RedirectsOff lost in the round trip")
	}
	if len(back.Redirects) != 0 {
		t.Errorf("Redirects = %+v, want none", back.Redirects)
	}
}

// TestEntryWithoutRedirectsStillLoads: files written before #2716 carry no
// "redirects" key and must read back as "not recorded", which the pane then
// renders exactly as it always did.
func TestEntryWithoutRedirectsStillLoads(t *testing.T) {
	s := New(t.TempDir())
	s.Append("req.http", "create", entry(1))
	back := s.List("req.http", "create")[0].Response("create")
	if len(back.Redirects) != 0 || back.FinalURL != "" || back.RedirectsOff {
		t.Fatalf("legacy entry invented a chain: %+v / %q / %v", back.Redirects, back.FinalURL, back.RedirectsOff)
	}
}
