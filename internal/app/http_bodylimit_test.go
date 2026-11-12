package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/httpfile"
	"ike/internal/httppane"
)

// http_bodylimit_test.go covers the host side of `# @stream` and
// `# @max-body` (#3117): the head on show while the body still arrives, and
// the truncated marker of a cut body.

// bodyEvents runs a dispatch command and forwards its messages — following
// the event channel the way the update loop does — until the finalizing
// HTTPResponseMsg. The caller applies them to the model at its own pace.
func bodyEvents(t *testing.T, cmd tea.Cmd) <-chan tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("dispatch returned no command")
	}
	out := make(chan tea.Msg, 64)
	var follow func(tea.Cmd)
	follow = func(c tea.Cmd) {
		switch msg := c().(type) {
		case HTTPStreamStartMsg:
			out <- msg
			follow(nextHTTPEvent(msg.events))
		case HTTPStreamChunkMsg:
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
	return out
}

// nextBodyEvent waits for the next forwarded message.
func nextBodyEvent(t *testing.T, events <-chan tea.Msg) tea.Msg {
	t.Helper()
	select {
	case msg := <-events:
		return msg
	case <-time.After(10 * time.Second):
		t.Fatal("the dispatch stalled")
		return nil
	}
}

func TestHTTPStreamDirectiveShowsHeadEarly(t *testing.T) {
	// serial: waits on the wall clock (load-sensitive)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("first line of the export\n"))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-time.After(10 * time.Second):
		}
		_, _ = w.Write([]byte("last line of the export\n"))
	}))
	defer srv.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	m := httpApp(t)
	source := filepath.Join(t.TempDir(), "api.http")
	f := httpfile.Parse("# @stream\nGET " + srv.URL + "/export\n")
	events := bodyEvents(t, m.dispatchHTTPRequest(source, f, f.Requests[0]))

	// The server holds the rest back: the start and the head chunk arrive
	// anyway, and the pane shows the head while the body is incomplete.
	sawHead := false
	for !sawHead {
		msg := nextBodyEvent(t, events)
		if _, final := msg.(HTTPResponseMsg); final {
			t.Fatal("the response finished before the server sent the rest")
		}
		out, _ := m.Update(msg)
		m = out.(Model)
		_, sawHead = msg.(HTTPStreamChunkMsg)
	}
	p := m.httpPanel()
	if p == nil || !p.Streaming() {
		t.Fatal("the pane must be showing the live head")
	}
	if view := p.View(); !strings.Contains(view, "first line of the export") || strings.Contains(view, "last line") {
		t.Fatalf("pane must show the head only:\n%s", view)
	}

	close(release)
	for {
		msg := nextBodyEvent(t, events)
		out, _ := m.Update(msg)
		m = out.(Model)
		if _, final := msg.(HTTPResponseMsg); final {
			break
		}
	}
	p = m.httpPanel()
	if p.Streaming() {
		t.Fatal("the final response must end the live view")
	}
	resp := p.CurrentResponse()
	if resp == nil || !resp.Spooled() || resp.BodyBytes() != len("first line of the export\nlast line of the export\n") {
		t.Fatalf("final response = %+v, want the whole body, spooled", resp)
	}
	if view := p.View(); !strings.Contains(view, "last line of the export") {
		t.Errorf("the completed body must be on show:\n%s", view)
	}
}

func TestHTTPMaxBodyDirectiveMarksPane(t *testing.T) {
	// serial: nextBodyEvent: waits on the wall clock (load-sensitive)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(strings.Repeat("x", 10<<10)))
	}))
	defer srv.Close()
	m := httpApp(t)
	source := filepath.Join(t.TempDir(), "api.http")
	f := httpfile.Parse("# @max-body 1kb\n# @assert body contains xxx\nGET " + srv.URL + "/dump\n")
	events := bodyEvents(t, m.dispatchHTTPRequest(source, f, f.Requests[0]))
	for {
		msg := nextBodyEvent(t, events)
		out, _ := m.Update(msg)
		m = out.(Model)
		if _, final := msg.(HTTPResponseMsg); final {
			break
		}
	}
	p := m.httpPanel()
	if p == nil {
		t.Fatal("no response pane")
	}
	resp := p.CurrentResponse()
	if resp == nil || !resp.Truncated || len(resp.Body) != 1024 {
		t.Fatalf("response = %+v, want the first 1024 bytes, truncated", resp)
	}
	view := p.View()
	for _, want := range []string{"· " + httppane.TruncatedMarker, "body truncated at 1024 bytes", "@max-body 1kb"} {
		if !strings.Contains(view, want) {
			t.Errorf("pane misses %q:\n%s", want, view)
		}
	}
}
