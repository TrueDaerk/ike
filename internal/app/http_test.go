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
	"ike/internal/httppane"
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
	// The dispatch comes back batched with the in-flight tick (#1272).
	resp := drainHTTPResponse(t, cmd)
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

// TestHTTPPaneMouseSelectionCopies covers the app wiring of #1266: a press,
// motion and release over the response pane select text, and "y" hands it to
// the clipboard through the pane's CopyMsg.
func TestHTTPPaneMouseSelectionCopies(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)
	m.layout()

	r, ok := m.lay.Panes[pane.HTTPKey]
	if !ok {
		t.Fatal("the response pane must be laid out")
	}
	// The first body row inside the pane; coordinates are pane-local plus the
	// pane's own origin and content inset.
	p := m.httpPanel()
	bodyRow := -1
	for i := 0; i < p.Rows(); i++ {
		if p.RowText(i) == "{" {
			bodyRow = i
		}
	}
	if bodyRow < 0 {
		t.Fatal("no body row found")
	}
	x := r.X + paneContentX + 1
	y := r.Y + paneContentY + bodyRow + 1

	m = step(m, press(x, y))
	m = step(m, motion(x+4, y+1))
	m = step(m, release(x+4, y+1))
	if !p.HasSelection() {
		t.Fatal("the drag must select in the response pane")
	}

	var copied httppane.CopyMsg
	clipped := ""
	prev := clipboardWrite
	clipboardWrite = func(s string) { clipped = s }
	defer func() { clipboardWrite = prev }()

	out, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("y must emit a copy command")
	}
	msg, ok := cmd().(httppane.CopyMsg)
	if !ok {
		t.Fatalf("copy message type: %T", cmd())
	}
	copied = msg
	out, _ = m.Update(copied)
	m = out.(Model)
	if clipped == "" || clipped != copied.Text {
		t.Errorf("clipboard got %q, want %q", clipped, copied.Text)
	}
}

// TestHTTPCopyBodyCommand covers the palette entry (#1266).
func TestHTTPCopyBodyCommand(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)

	_, cmd := m.Update(HTTPCopyBodyMsg{})
	if cmd == nil {
		t.Fatal("http.copyBody must emit a copy command")
	}
	msg, ok := cmd().(httppane.CopyMsg)
	if !ok {
		t.Fatalf("message type: %T", cmd())
	}
	if !strings.Contains(msg.Text, `"ok"`) || msg.What != "response body" {
		t.Errorf("copy message: %+v", msg)
	}

	_, cmd = m.Update(HTTPCopyHeadersMsg{})
	if cmd == nil {
		t.Fatal("http.copyHeaders must emit a copy command")
	}
	hdr := cmd().(httppane.CopyMsg)
	if !strings.Contains(hdr.Text, "Content-Type: application/json") || strings.Contains(hdr.Text, `"ok"`) {
		t.Errorf("headers copy: %q", hdr.Text)
	}
}

// TestHTTPCopyWithoutPaneNotifies: the palette command is reachable with no
// pane open; it must say so instead of emitting an empty clipboard write.
func TestHTTPCopyWithoutPaneNotifies(t *testing.T) {
	m := httpApp(t)
	if _, cmd := m.Update(HTTPCopyBodyMsg{}); cmd != nil {
		if _, ok := cmd().(httppane.CopyMsg); ok {
			t.Fatal("no pane open: nothing may be copied")
		}
	}
}

// TestHTTPResponseHistoryCommand covers http.responseHistory (#1267): it
// focuses the viewer and reports the stored count.
func TestHTTPResponseHistoryCommand(t *testing.T) {
	m := httpApp(t)
	for i := 0; i < 3; i++ {
		resp := sampleResponse("one")
		resp.Body = []byte(`{"n":` + strconv.Itoa(i) + `}`)
		out, _ := m.Update(HTTPResponseMsg{Source: "/p/req.http", Request: "one", Resp: resp})
		m = out.(Model)
	}
	// Focus something else first, so the command has work to do.
	m.setFocus(m.activeEditorKey())

	out, _ := m.Update(HTTPResponseHistoryMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != pane.HTTPKey {
		t.Errorf("the command must focus the response pane, focused=%q", m.activeWS().Panes.Focused())
	}
	if idx, n := m.httpPanel().HistoryIndex(); idx != 0 || n != 3 {
		t.Errorf("history state: %d/%d, want 0/3", idx, n)
	}
}

// TestHTTPResponseHistoryWithoutPane must not panic or open anything.
func TestHTTPResponseHistoryWithoutPane(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseHistoryMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Has(pane.HTTPKey) {
		t.Error("the command must not conjure a viewer without a response")
	}
}

// TestHTTPPaneKeysInHelp: the viewer's pane-local keys reach the cheatsheet
// (#1267) — they belong to no registry command, so nothing else documents
// that history browsing exists.
func TestHTTPPaneKeysInHelp(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)

	g := m.paneKeysHelpGroup()
	if len(g.Entries) == 0 {
		t.Fatal("the focused response pane must contribute help entries")
	}
	found := false
	for _, e := range g.Entries {
		if e.Shortcut == "h / l" && strings.Contains(e.Title, "stored response") {
			found = true
		}
	}
	if !found {
		t.Errorf("history browsing must be documented: %+v", g.Entries)
	}

	// An editor-focused help overlay must not carry the pane keys.
	m.setFocus(m.activeEditorKey())
	if len(m.paneKeysHelpGroup().Entries) != 0 {
		t.Error("pane keys must be scoped to the focused pane")
	}
}

// TestHTTPFooterAlwaysShowsHistory: the hint is there from the first
// response, not only once a second one exists (#1267).
func TestHTTPFooterAlwaysShowsHistory(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Source: "/p/req.http", Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)
	if view := m.activeWS().Panes.Get(pane.HTTPKey).View(); !strings.Contains(view, "h/l history 1/1") {
		t.Errorf("footer must advertise history from the first response:\n%s", view)
	}
}

// slowServer answers only when release is closed, so a dispatch can be
// observed while it is in flight.
func slowServer(t *testing.T, release <-chan struct{}) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// httpFileApp opens a one-request .http file pointing at url and returns the
// app with the cursor on the request.
func httpFileApp(t *testing.T, url string) Model {
	t.Helper()
	m := httpApp(t)
	path := filepath.Join(t.TempDir(), "req.http")
	if err := os.WriteFile(path, []byte("### one\nGET "+url+"/slow\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	return out.(Model)
}

// TestHTTPInFlightIndicator covers #1272: while a dispatch runs the
// statusline shows it, the pane marks the pending request, and both clear
// when the response lands.
func TestHTTPInFlightIndicator(t *testing.T) {
	release := make(chan struct{})
	srv := slowServer(t, release)
	m := httpFileApp(t, srv.URL)

	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("http.run must dispatch")
	}
	if len(m.httpFlight) != 1 {
		t.Fatalf("in-flight entries: %d, want 1", len(m.httpFlight))
	}
	seg := m.httpFlightSegment()
	if !strings.Contains(seg, "GET /slow") {
		t.Errorf("statusline segment: %q", seg)
	}

	// Run the dispatch to completion and route the response back.
	close(release)
	msg := drainHTTPResponse(t, cmd)
	out, _ = m.Update(msg)
	m = out.(Model)
	if len(m.httpFlight) != 0 {
		t.Errorf("the indicator must clear on response: %+v", m.httpFlight)
	}
	if m.httpFlightSegment() != "" {
		t.Errorf("segment must be empty when idle: %q", m.httpFlightSegment())
	}
	if p := m.httpPanel(); p == nil || p.Pending() != "" {
		t.Error("the pane's pending marker must clear")
	}
}

// drainHTTPResponse runs cmd (possibly a batch) until the response message
// appears, discarding the tick commands.
func drainHTTPResponse(t *testing.T, cmd tea.Cmd) HTTPResponseMsg {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case HTTPResponseMsg:
			return msg
		case tea.BatchMsg:
			queue = append(queue, msg...)
		}
	}
	t.Fatal("no response message produced")
	return HTTPResponseMsg{}
}

// TestHTTPDuplicateDispatchRejected: the same request may not fire twice in
// parallel (#1272).
func TestHTTPDuplicateDispatchRejected(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	srv := slowServer(t, release)
	m := httpFileApp(t, srv.URL)

	out, first := m.Update(HTTPRunMsg{})
	m = out.(Model)
	if first == nil {
		t.Fatal("the first dispatch must run")
	}
	out, second := m.Update(HTTPRunMsg{})
	m = out.(Model)
	if second != nil {
		if _, ok := second().(HTTPResponseMsg); ok {
			t.Fatal("a duplicate dispatch must not fire a second request")
		}
	}
	if len(m.httpFlight) != 1 {
		t.Errorf("in-flight entries after the duplicate: %d, want 1", len(m.httpFlight))
	}
}

// TestHTTPCancelAbortsDispatch: http.cancel aborts through the dispatch
// context and the abort reads as a confirmation, not an error (#1272).
func TestHTTPCancelAbortsDispatch(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	srv := slowServer(t, release)
	m := httpFileApp(t, srv.URL)

	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	if len(m.httpFlight) != 1 {
		t.Fatalf("dispatch must be tracked, got %d", len(m.httpFlight))
	}

	// Cancel while the server still holds the request.
	out, _ = m.Update(HTTPCancelMsg{})
	m = out.(Model)

	msg := drainHTTPResponse(t, cmd)
	if msg.Err == nil {
		t.Fatal("a canceled dispatch must report an error")
	}
	out, _ = m.Update(msg)
	m = out.(Model)
	if len(m.httpFlight) != 0 {
		t.Errorf("cancel must clear the in-flight entry: %+v", m.httpFlight)
	}
	if m.activeWS().Panes.Has(pane.HTTPKey) {
		t.Error("a canceled request must not open a response pane")
	}
}

// TestHTTPCancelWithoutFlightIsQuiet: the palette command is always
// reachable; with nothing running it just says so.
func TestHTTPCancelWithoutFlightIsQuiet(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPCancelMsg{})
	m = out.(Model)
	if len(m.httpFlight) != 0 {
		t.Error("nothing to cancel must stay nothing")
	}
}

// TestHTTPPaneCancelKeyRoutes: "x" in the focused pane reaches the host's
// cancel path (#1272).
func TestHTTPPaneCancelKeyRoutes(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	srv := slowServer(t, release)
	m := httpFileApp(t, srv.URL)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)

	out, runCmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	if runCmd == nil {
		t.Fatal("dispatch expected")
	}
	out, cmd := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("x must emit a command")
	}
	if _, ok := cmd().(httppane.CancelMsg); !ok {
		t.Fatalf("message type: %T", cmd())
	}
	out, _ = m.Update(httppane.CancelMsg{})
	m = out.(Model)
	for _, e := range m.httpFlight {
		if !e.canceled {
			t.Error("the entry must be marked canceled")
		}
	}
}
