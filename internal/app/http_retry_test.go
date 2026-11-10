package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/httpfile"
)

// http_retry_test.go covers the host side of `# @retry` (#3116): the attempt
// messages restarting the countdown, the pane showing the final attempt with
// its indicator and the earlier attempts, and captures reading the final one.

// retryEvents runs a dispatch command and collects its messages in order —
// following the event channel the way the update loop does — until the
// finalizing HTTPResponseMsg.
func retryEvents(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("dispatch returned no command")
	}
	// The batch holds the flight tick and the dispatch; both run off-test and
	// only the dispatch's messages are kept.
	out := make(chan tea.Msg, 16)
	var follow func(tea.Cmd)
	follow = func(c tea.Cmd) {
		switch msg := c().(type) {
		case HTTPRetryAttemptMsg:
			out <- msg
			follow(nextHTTPEvent(msg.events))
		case HTTPResponseMsg:
			out <- msg
		}
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				go follow(c)
			}
		}
	}
	var msgs []tea.Msg
	for {
		select {
		case msg := <-out:
			msgs = append(msgs, msg)
			if _, done := msg.(HTTPResponseMsg); done {
				return msgs
			}
		case <-time.After(30 * time.Second):
			t.Fatal("the dispatch never finished")
			return nil
		}
	}
}

func TestHTTPRetryPaneShowsFinalAttempt(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) < 3 {
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"id":"busy"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"final"}`))
	}))
	defer srv.Close()
	m := httpApp(t)
	source := filepath.Join(t.TempDir(), "api.http")
	f := httpfile.Parse("# @retry 3\n# @capture id = .id\nGET " + srv.URL + "/flaky\n")
	if len(f.Requests) != 1 {
		t.Fatalf("parse: %v", f.Errors)
	}

	msgs := retryEvents(t, m.dispatchHTTPRequest(source, f, f.Requests[0]))
	if len(msgs) != 3 {
		t.Fatalf("messages = %d (%T…), want two attempt messages and the response", len(msgs), msgs)
	}
	for i, want := range []int{2, 3} {
		a, ok := msgs[i].(HTTPRetryAttemptMsg)
		if !ok || a.N != want || a.Max != 4 {
			t.Fatalf("message %d = %+v, want attempt %d/4", i, msgs[i], want)
		}
	}

	// The attempt message restarts the pane's countdown and names the attempt.
	m.openHTTPPanel()
	key := httpFlightKey(source, f.Requests[0].Key())
	m.httpFlight[key].started = time.Now().Add(-time.Minute)
	out, _ := m.Update(msgs[1])
	m = out.(Model)
	p := m.httpPanel()
	if p == nil {
		t.Fatal("no response pane")
	}
	if p.PendingAttempt() != "attempt 3/4" {
		t.Errorf("pending attempt = %q, want attempt 3/4", p.PendingAttempt())
	}
	if view := p.View(); !strings.Contains(view, "attempt 3/4 · waiting 0") {
		t.Errorf("the countdown must restart for the attempt:\n%s", view)
	}

	out, _ = m.Update(msgs[2])
	m = out.(Model)
	p = m.httpPanel()
	if p.Pending() != "" {
		t.Error("the flight must end with the response")
	}
	view := p.View()
	for _, want := range []string{"200 OK", "· attempt 3/4", "2 earlier attempts", "↻ #1  503 Service Unavailable"} {
		if !strings.Contains(view, want) {
			t.Errorf("pane misses %q:\n%s", want, view)
		}
	}
	// Captures read the final attempt only.
	resp := msgs[2].(HTTPResponseMsg).Resp
	if len(resp.Captures) != 1 || resp.Captures[0].Value != "final" {
		t.Errorf("captures = %+v, want id=final", resp.Captures)
	}
}

// A series whose last attempt times out names the attempt in the message.
func TestHTTPRetryTimeoutNamesAttempt(t *testing.T) {
	srv := hangingHTTPServer(t)
	m := httpApp(t)
	source := filepath.Join(t.TempDir(), "api.http")
	f := httpfile.Parse("# @timeout 100ms\n# @retry 1\nGET " + srv.URL + "/slow\n")
	msgs := retryEvents(t, m.dispatchHTTPRequest(source, f, f.Requests[0]))
	out, _ := m.Update(msgs[len(msgs)-1])
	m = out.(Model)
	p := m.httpPanel()
	if p == nil {
		t.Fatal("the timeout must open the response pane")
	}
	if failure := p.Failure(); !strings.Contains(failure, "timed out after 100ms") || !strings.Contains(failure, "(attempt 2/2)") {
		t.Errorf("failure = %q, want the timeout at attempt 2/2", failure)
	}
}
