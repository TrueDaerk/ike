package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/httpclient"
	"ike/internal/httphistory"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
)

func httpApp(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return out.(Model)
}

func sampleResponse(key string) *httpclient.Response {
	return &httpclient.Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       []byte(`{"ok":true}`),
		Duration:   3 * time.Millisecond,
		RequestKey: key,
	}
}

func TestHTTPResponseOpensAndReusesViewer(t *testing.T) {
	m := httpApp(t)
	if m.activeWS().Panes.Has(pane.HTTPKey) {
		t.Fatal("viewer must not exist initially")
	}

	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.HTTPKey) {
		t.Fatal("first response must open the viewer")
	}
	if got := m.httpPanel().Request(); got != "one" {
		t.Fatalf("request label: %q", got)
	}

	// Second dispatch reuses the pane, replacing its content.
	out, _ = m.Update(HTTPResponseMsg{Request: "two", Resp: sampleResponse("two")})
	m = out.(Model)
	keys := 0
	for _, k := range m.activeWS().Panes.Keys() {
		if k == pane.HTTPKey {
			keys++
		}
	}
	if keys != 1 {
		t.Fatalf("viewer panes: %d, want 1", keys)
	}
	if got := m.httpPanel().Request(); got != "two" {
		t.Fatalf("request label after reuse: %q", got)
	}
}

func TestHTTPRunDispatchesRequestUnderCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"path":"` + r.URL.Path + `"}`))
	}))
	defer srv.Close()

	m := httpApp(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "req.http")
	src := "### first\nGET " + srv.URL + "/a\n\n### second\nGET " + srv.URL + "/b\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)

	// Cursor sits on line 1 (first block) — http.run must dispatch "first".
	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("http.run on an .http file must return a dispatch command")
	}
	msg := cmd()
	resp, ok := msg.(HTTPResponseMsg)
	if !ok {
		t.Fatalf("dispatch result: %T", msg)
	}
	if resp.Err != nil {
		t.Fatal(resp.Err)
	}
	if resp.Request != "first" {
		t.Errorf("request key: %q, want first", resp.Request)
	}
	if !strings.Contains(string(resp.Resp.Body), "/a") {
		t.Errorf("body: %s", resp.Resp.Body)
	}

	// Routing the response opens the viewer.
	out, _ = m.Update(resp)
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.HTTPKey) {
		t.Fatal("viewer must open after the response lands")
	}
}

func TestHTTPRunGatesOnFileType(t *testing.T) {
	m := httpApp(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)

	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	// A notification timer command may come back; a dispatch must not.
	if cmd != nil {
		if _, ok := cmd().(HTTPResponseMsg); ok {
			t.Fatal("http.run on a non-.http file must not dispatch")
		}
	}
	if m.activeWS().Panes.Has(pane.HTTPKey) {
		t.Fatal("viewer must not open")
	}
}

func TestHTTPErrorNotifies(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "x", Err: os.ErrDeadlineExceeded})
	m = out.(Model)
	if m.activeWS().Panes.Has(pane.HTTPKey) {
		t.Fatal("an error must not open the viewer")
	}
}

func TestHTTPPanePersistsAsEmptySingleton(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)

	// fillHTTPPanel saved the layout; the pane's identity round-trips as
	// "http" so a restart restores the slot (empty, re-filled on the next
	// http.run dispatch).
	_, ids, ok := loadLayout()
	if !ok {
		t.Fatal("layout must have been saved")
	}
	if ids[pane.HTTPKey].Kind != "http" {
		t.Fatalf("persisted kind: %+v", ids[pane.HTTPKey])
	}
}

func TestHTTPResponsePersistsHistory(t *testing.T) {
	m := httpApp(t)
	for i := 0; i < 7; i++ {
		resp := sampleResponse("one")
		resp.Body = []byte(`{"n":` + strconv.Itoa(i) + `}`)
		out, _ := m.Update(HTTPResponseMsg{Source: "/p/req.http", Request: "one", Resp: resp})
		m = out.(Model)
	}

	// Persisted under the .ike-convention dir (IKE_CONFIG_DIR in tests),
	// pruned to the last 5.
	entries := httphistory.New(httpHistoryDir()).List("/p/req.http", "one")
	if len(entries) != httphistory.MaxPerRequest {
		t.Fatalf("stored entries: %d, want %d", len(entries), httphistory.MaxPerRequest)
	}
	if string(entries[0].Body) != `{"n":6}` {
		t.Errorf("newest first: %s", entries[0].Body)
	}

	// The viewer received the browsable history.
	if idx, n := m.httpPanel().HistoryIndex(); idx != 0 || n != httphistory.MaxPerRequest {
		t.Errorf("viewer history: %d/%d", idx, n)
	}
}

func TestHTTPResponseReopensViewerAfterClose(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.HTTPKey) {
		t.Fatal("first response must open the viewer")
	}

	// Close the pane the ordinary way (pane.close on the focused viewer).
	m.closePane(pane.HTTPKey)
	if leafHas(m.activeWS().Tree, pane.HTTPKey) {
		t.Fatal("close must remove the viewer leaf")
	}

	// A later dispatch must reopen the pane and show the response.
	out, _ = m.Update(HTTPResponseMsg{Request: "two", Resp: sampleResponse("two")})
	m = out.(Model)
	if !leafHas(m.activeWS().Tree, pane.HTTPKey) {
		t.Fatal("dispatch after close must reopen the viewer leaf")
	}
	if got := m.httpPanel().Request(); got != "two" {
		t.Fatalf("request label after reopen: %q", got)
	}
}

// leafHas reports whether the layout tree contains a leaf named key.
func leafHas(root layout.Node, key string) bool {
	for _, k := range layout.Leaves(root) {
		if k == key {
			return true
		}
	}
	return false
}

func TestHTTPResponseReattachesAfterHideAllTools(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)

	// hideAllTools removes the leaf but keeps the instance registered (#791).
	m.hideToolWindows()
	if leafHas(m.activeWS().Tree, pane.HTTPKey) {
		t.Fatal("hide must remove the viewer leaf")
	}
	if !m.activeWS().Panes.Has(pane.HTTPKey) {
		t.Fatal("hide must keep the instance registered")
	}

	// A dispatch while hidden must re-attach the leaf, not fill the void.
	out, _ = m.Update(HTTPResponseMsg{Request: "two", Resp: sampleResponse("two")})
	m = out.(Model)
	if !leafHas(m.activeWS().Tree, pane.HTTPKey) {
		t.Fatal("dispatch while hidden must re-attach the viewer leaf")
	}
	if got := m.httpPanel().Request(); got != "two" {
		t.Fatalf("request label after re-attach: %q", got)
	}
}

// TestHTTPPaneReceivesSearchKeys guards the routing (#1265): with the
// response viewer focused, "/" and the pattern reach the pane instead of
// being swallowed by a keymap binding or a global handler.
func TestHTTPPaneReceivesSearchKeys(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)

	for _, k := range []string{"/", "o", "k"} {
		out, _ = m.Update(tea.KeyPressMsg{Code: rune(k[0]), Text: k})
		m = out.(Model)
	}
	q, open := m.httpPanel().SearchQuery()
	if !open || q != "ok" {
		t.Fatalf("pane search state: query=%q open=%v", q, open)
	}
	if cur, total := m.httpPanel().MatchPosition(); cur != 1 || total == 0 {
		t.Errorf("match position: %d/%d", cur, total)
	}
}
