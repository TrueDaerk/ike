package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/httpfile"
)

// http_timeout_test.go covers the request timeout (#2630): the configured
// deadline reaching the dispatcher, the in-flight header counting against it,
// and the explicit message a timed-out flight leaves in the response pane.

// httpTimeoutApp is an app with a known http.timeout_ms.
func httpTimeoutApp(t *testing.T, ms int) Model {
	t.Helper()
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	c := *old
	c.HTTP.TimeoutMs = ms
	config.Set(&c)
	return httpApp(t)
}

// hangingHTTPServer never answers until the test ends.
func hangingHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(done); srv.Close() })
	return srv
}

// awaitHTTPResponse runs a dispatch command tree and returns the finalizing
// HTTPResponseMsg — the message the update loop would receive.
func awaitHTTPResponse(t *testing.T, cmd tea.Cmd) HTTPResponseMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("dispatch returned no command")
	}
	out := make(chan HTTPResponseMsg, 4)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, sub := range msg {
				go run(sub)
			}
		case HTTPResponseMsg:
			out <- msg
		}
	}
	go run(cmd)
	select {
	case msg := <-out:
		return msg
	case <-time.After(30 * time.Second):
		t.Fatal("the dispatch never finished — the deadline did not fire")
		return HTTPResponseMsg{}
	}
}

// TestHTTPTimeoutShowsExplicitMessageInPane: a server that never answers and
// a one-second http.timeout_ms end the flight with the explicit timeout text
// in the response pane — not a generic transport error, and not the previous
// response left standing.
func TestHTTPTimeoutShowsExplicitMessageInPane(t *testing.T) {
	srv := hangingHTTPServer(t)
	m := httpTimeoutApp(t, 1000)

	source := filepath.Join(t.TempDir(), "api.http")
	f := httpfile.Parse("GET " + srv.URL + "/slow\n")
	if len(f.Requests) != 1 {
		t.Fatalf("requests = %d", len(f.Requests))
	}
	start := time.Now()
	msg := awaitHTTPResponse(t, m.dispatchHTTPRequest(source, f, f.Requests[0]))
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("waited %v — the setting did not bound the dispatch", took)
	}
	if msg.Err == nil {
		t.Fatal("a server that never answers must fail the dispatch")
	}

	out, _ := m.Update(msg)
	m = out.(Model)
	p := m.httpPanel()
	if p == nil {
		t.Fatal("the timeout must open the response pane")
	}
	failure := p.Failure()
	for _, want := range []string{"timed out after 1 s", "raise http.timeout_ms", "@timeout"} {
		if !strings.Contains(failure, want) {
			t.Errorf("pane failure %q must mention %q", failure, want)
		}
	}
	if view := p.View(); !strings.Contains(view, "timed out after 1 s") {
		t.Errorf("the pane view must say it timed out:\n%s", view)
	}
}

// The directive bounds one request without touching the setting: a generous
// http.timeout_ms and a tight `# @timeout` still fail fast.
func TestHTTPTimeoutDirectiveBeatsTheSetting(t *testing.T) {
	srv := hangingHTTPServer(t)
	m := httpTimeoutApp(t, 600000)

	source := filepath.Join(t.TempDir(), "api.http")
	f := httpfile.Parse("# @timeout 1s\nGET " + srv.URL + "/slow\n")
	if len(f.Errors) != 0 {
		t.Fatalf("parse: %v", f.Errors)
	}
	start := time.Now()
	msg := awaitHTTPResponse(t, m.dispatchHTTPRequest(source, f, f.Requests[0]))
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("waited %v — the directive did not bound the dispatch", took)
	}
	out, _ := m.Update(msg)
	m = out.(Model)
	if got := m.httpPanel().Failure(); !strings.Contains(got, "timed out after 1 s") {
		t.Errorf("pane failure = %q, want the directive's 1 s", got)
	}
}

// The flight's deadline reaches the pane, so the in-flight header can count
// the wait against it.
func TestHTTPFlightCarriesItsLimitToThePane(t *testing.T) {
	m := httpTimeoutApp(t, 30000)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)

	m.httpFlight = map[string]*httpFlightEntry{
		httpFlightKey("", "one"): {
			request: "one", label: "GET /one", limit: 30 * time.Second,
			started: time.Now().Add(-12 * time.Second), cancel: func() {},
		},
	}
	m.markHTTPPending()
	p := m.httpPanel()
	if got := p.PendingLimit(); got != 30*time.Second {
		t.Fatalf("pane limit = %v, want 30s", got)
	}
	if view := p.View(); !strings.Contains(view, "waiting 12.0s / 30 s") {
		t.Errorf("the header must show elapsed against the limit:\n%s", view)
	}
}

// httpFlightLimit reads the directive first and the setting otherwise — the
// two sources the header can know about.
func TestHTTPFlightLimitPrefersTheDirective(t *testing.T) {
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	c := *old
	c.HTTP.TimeoutMs = 45000
	config.Set(&c)

	if got := httpFlightLimit(0); got != 45*time.Second {
		t.Errorf("without a directive = %v, want the setting's 45s", got)
	}
	if got := httpFlightLimit(3 * time.Second); got != 3*time.Second {
		t.Errorf("with a directive = %v, want 3s", got)
	}
}
