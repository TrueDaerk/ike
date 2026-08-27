package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/httphistory"
	"ike/internal/httppane"
	"ike/internal/pane"
)

// httprerun_test.go covers re-running a request straight from its history
// entry and the comparison that follows (#2247).

// bodyServer answers every request with whatever body returns, so a test can
// change what the "same" request answers between two runs.
func bodyServer(t *testing.T, body func() string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"v":"` + body() + `"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestHTTPRerunKeyRoutes: "R" in the focused viewer asks the host to re-run —
// the pane reaches neither the .http file nor the environment.
func TestHTTPRerunKeyRoutes(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Source: "/p/req.http", Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'R', Text: "R", Mod: tea.ModShift})
	if cmd == nil {
		t.Fatal("R must emit a command")
	}
	if _, ok := cmd().(httppane.RerunMsg); !ok {
		t.Fatalf("message type: %T", cmd())
	}
}

// TestHTTPRerunUsesCurrentRequest is the point of the re-run (#2247), and the
// difference to the verbatim re-send (#1832): the block is re-read from the
// .http file, so an edit made since the stored response counts. The answer
// lands in the pane and in the history like any dispatch.
func TestHTTPRerunUsesCurrentRequest(t *testing.T) {
	var log []recorded
	srv := resendServer(t, &log)

	m := httpApp(t)
	path := filepath.Join(t.TempDir(), "req.http")
	first := "### one\nGET " + srv.URL + "/a\nX-Token: first\n"
	if err := os.WriteFile(path, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)
	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	resp := drainHTTPResponse(t, cmd)
	if resp.Err != nil {
		t.Fatal(resp.Err)
	}
	out, _ = m.Update(resp)
	m = out.(Model)

	// The request changes; a re-run must pick the new form up.
	second := "### one\nGET " + srv.URL + "/b\nX-Token: second\n"
	if err := os.WriteFile(path, []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}
	m.activeEditor().RestoreText(second)

	m.setFocus(pane.HTTPKey)
	m.layout()
	out, cmd = m.Update(httppane.RerunMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("the re-run must dispatch")
	}
	again := drainHTTPResponse(t, cmd)
	if again.Err != nil {
		t.Fatal(again.Err)
	}
	if again.Request != "one" || again.Source != path {
		t.Errorf("re-run routed as %q from %q", again.Request, again.Source)
	}
	if len(log) != 2 {
		t.Fatalf("server saw %d requests, want 2", len(log))
	}
	if log[1].path != "/b" || log[1].token != "second" {
		t.Errorf("re-ran %s %s (X-Token %q), want the current GET /b with second",
			log[1].method, log[1].path, log[1].token)
	}

	out, _ = m.Update(again)
	m = out.(Model)
	if _, n := m.httpPanel().HistoryIndex(); n != 2 {
		t.Errorf("history entries in the pane: %d, want 2", n)
	}
	if n := len(httphistory.New(httpHistoryDir()).List(path, "one")); n != 2 {
		t.Errorf("stored entries: %d, want 2", n)
	}
}

// TestHTTPRerunOpensPreviousRunDiff: with http.diff_after_rerun on (the
// default) the comparison of the re-run against the run before it opens by
// itself once the answer is stored (#2247) — the whole point of re-running.
func TestHTTPRerunOpensPreviousRunDiff(t *testing.T) {
	var body = "one"
	srv := bodyServer(t, func() string { return body })

	m := httpApp(t)
	path := filepath.Join(t.TempDir(), "req.http")
	if err := os.WriteFile(path, []byte("### one\nGET "+srv.URL+"/a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)
	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	out, _ = m.Update(drainHTTPResponse(t, cmd))
	m = out.(Model)
	if _, _, _, ok := m.diffSlot(); ok {
		t.Fatal("a first run has nothing to compare with and must open no diff")
	}

	body = "two"
	m.setFocus(pane.HTTPKey)
	m.layout()
	out, cmd = m.Update(httppane.RerunMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("the re-run must dispatch")
	}
	out, _ = m.Update(drainHTTPResponse(t, cmd))
	m = out.(Model)

	inst, _, _, ok := m.diffSlot()
	if !ok {
		t.Fatal("the re-run must open the previous-vs-new diff")
	}
	left, right := inst.Diff().Titles()
	if !strings.HasPrefix(left, "one 2/2") || !strings.HasPrefix(right, "one 1/2") {
		t.Fatalf("titles: %q vs %q, want the previous run on the left", left, right)
	}
	if inst.Diff().HunkCount() == 0 {
		t.Fatal("the changed body must diff to at least one hunk")
	}
}

// TestHTTPRerunDiffFiltersVolatileHeaders: the auto-diff compares two runs
// that differ only in Date and a request id as unchanged (#2247) — that noise
// is what made previous-vs-new unreadable.
func TestHTTPRerunDiffFiltersVolatileHeaders(t *testing.T) {
	m := httpApp(t)
	path := httpPickerFile(t)
	store := httphistory.New(httpHistoryDir())
	for i, id := range []string{"req-1", "req-2"} {
		store.Append(path, "first", httphistory.Entry{
			Time:  time.Now().Add(time.Duration(i) * time.Second),
			Proto: "HTTP/1.1", Status: "200 OK", StatusCode: 200,
			Headers: http.Header{
				"Content-Type": {"application/json"},
				"Date":         {"Mon, 0" + id[len(id)-1:] + " Jan 2024 00:00:00 GMT"},
				"X-Request-Id": {id},
			},
			Body: []byte(`{"a":1}`),
		})
	}
	m = showStored(t, m, path, "first")

	out, _ := m.Update(DiffHTTPEntriesMsg{Source: path, Request: "first", Shown: 0, Other: 1})
	m = out.(Model)
	inst, _, _, ok := m.diffSlot()
	if !ok {
		t.Fatal("the diff viewer must open")
	}
	if n := inst.Diff().HunkCount(); n != 0 {
		t.Fatalf("volatile headers only must diff to nothing, got %d hunks", n)
	}
}

// TestHTTPRerunMissingRequestNotifies: a block renamed or deleted since the
// response was stored cannot be re-run — say so (and name the re-send)
// instead of dispatching something else (#2247).
func TestHTTPRerunMissingRequestNotifies(t *testing.T) {
	m := httpApp(t)
	path := filepath.Join(t.TempDir(), "req.http")
	if err := os.WriteFile(path, []byte("### renamed\nGET http://localhost/a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := httphistory.New(httpHistoryDir())
	store.Append(path, "one", httphistory.FromResponse(sampleResponse("one"), time.Now()))

	out, _ := m.Update(ShowStoredHTTPResponseMsg{Source: path, Request: "one"})
	m = out.(Model)
	if m.httpPanel() == nil {
		t.Fatal("the stored response must be on show")
	}
	out, _ = m.Update(HTTPRerunMsg{})
	m = out.(Model)
	if len(m.httpFlight) != 0 {
		t.Errorf("nothing may go in flight: %v", m.httpFlight)
	}
}

// TestHTTPRerunWithoutPaneNotifies: the palette entry is reachable with no
// viewer open (#2247).
func TestHTTPRerunWithoutPaneNotifies(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPRerunMsg{})
	m = out.(Model)
	if len(m.httpFlight) != 0 {
		t.Errorf("no pane means no dispatch: %v", m.httpFlight)
	}
	if m.activeWS().Panes.Has(pane.HTTPKey) {
		t.Error("the re-run must not open a pane by itself")
	}
}
