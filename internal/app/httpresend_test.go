package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/httphistory"
	"ike/internal/httppane"
	"ike/internal/pane"
)

// findMsg runs cmd (possibly a nested batch) and returns the first message of
// type T it produces, discarding everything else.
func findMsg[T tea.Msg](cmd tea.Cmd) (T, bool) {
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case T:
			return msg, true
		case tea.BatchMsg:
			queue = append(queue, msg...)
		}
	}
	var zero T
	return zero, false
}

// recorded is one request a re-send test server saw.
type recorded struct {
	method string
	path   string
	token  string
}

// resendServer answers every request with JSON and records what it received.
func resendServer(t *testing.T, log *[]recorded) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*log = append(*log, recorded{method: r.Method, path: r.URL.Path, token: r.Header.Get("X-Token")})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"path":"` + r.URL.Path + `"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestHTTPResendRepeatsStoredRequestAfterEdit is the point of #1832: the
// request that goes out again is the one that produced the shown response,
// even after the .http file was rewritten in between.
func TestHTTPResendRepeatsStoredRequestAfterEdit(t *testing.T) {
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
	if m.httpPanel().CurrentRequest() == nil {
		t.Fatal("the shown response must carry its as-sent request")
	}

	// The file changes completely — buffer and disk.
	second := "### one\nGET " + srv.URL + "/b\nX-Token: second\n"
	if err := os.WriteFile(path, []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}
	m.activeEditor().RestoreText(second)

	// ctrl+r in the focused response pane.
	m.setFocus(pane.HTTPKey)
	m.layout()
	out, cmd = m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("ctrl+r in the response pane must produce a command")
	}
	// The settled pass may batch side commands (e.g. the symbol-refresh
	// debounce, #2319) alongside the resend; find it in the batch.
	msg, ok := findMsg[httppane.ResendMsg](cmd)
	if !ok {
		t.Fatalf("no ResendMsg produced by %T", cmd())
	}
	out, cmd = m.Update(msg)
	m = out.(Model)
	if cmd == nil {
		t.Fatal("the re-send must dispatch")
	}
	again := drainHTTPResponse(t, cmd)
	if again.Err != nil {
		t.Fatal(again.Err)
	}
	if again.Request != "one" || again.Source != path {
		t.Errorf("re-send routed as %q from %q", again.Request, again.Source)
	}
	if len(log) != 2 {
		t.Fatalf("server saw %d requests, want 2", len(log))
	}
	if log[1].path != "/a" || log[1].token != "first" {
		t.Errorf("re-sent %s %s (X-Token %q), want the stored GET /a with first",
			log[1].method, log[1].path, log[1].token)
	}

	// The answer lands in the pane and in the history like any dispatch.
	out, _ = m.Update(again)
	m = out.(Model)
	if _, n := m.httpPanel().HistoryIndex(); n != 2 {
		t.Errorf("history entries in the pane: %d, want 2", n)
	}
	entries := httphistory.New(httpHistoryDir()).List(path, "one")
	if len(entries) != 2 {
		t.Fatalf("stored entries: %d, want 2", len(entries))
	}
	for i, e := range entries {
		if e.Request == nil {
			t.Fatalf("entry %d lost its request snapshot", i)
		}
		if e.Request.URL != srv.URL+"/a" {
			t.Errorf("entry %d snapshot url %q, want %s/a", i, e.Request.URL, srv.URL)
		}
	}
}

// TestHTTPResendLegacyEntryNotifies: a history entry written before the
// snapshot existed cannot be re-sent — the pane says so instead of firing
// something else (#1832).
func TestHTTPResendLegacyEntryNotifies(t *testing.T) {
	m := httpApp(t)
	path := filepath.Join(t.TempDir(), "req.http")
	if err := os.WriteFile(path, []byte("### one\nGET http://localhost/a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// sampleResponse carries no snapshot — exactly a pre-#1832 entry.
	store := httphistory.New(httpHistoryDir())
	store.Append(path, "one", httphistory.FromResponse(sampleResponse("one"), time.Now()))

	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)
	out, _ = m.Update(HTTPShowResponseMsg{})
	m = out.(Model)
	if m.httpPanel() == nil {
		t.Fatal("the stored response must be on show")
	}
	if m.httpPanel().CanResend() {
		t.Error("a legacy entry must not advertise re-send")
	}

	out, _ = m.Update(HTTPResendMsg{})
	m = out.(Model)
	if len(m.httpFlight) != 0 {
		t.Errorf("re-sending a legacy entry must dispatch nothing: %v", m.httpFlight)
	}
}

// TestHTTPResendWithoutPaneNotifies: the palette entry is reachable with no
// viewer open (#1832).
func TestHTTPResendWithoutPaneNotifies(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResendMsg{})
	m = out.(Model)
	if len(m.httpFlight) != 0 {
		t.Errorf("no pane means no dispatch: %v", m.httpFlight)
	}
	if m.activeWS().Panes.Has(pane.HTTPKey) {
		t.Error("re-send must not open a pane by itself")
	}
}

// TestHTTPResendClickOnAffordance: the header button dispatches the same way
// the key does (#1832).
func TestHTTPResendClickOnAffordance(t *testing.T) {
	var log []recorded
	srv := resendServer(t, &log)

	m := httpApp(t)
	path := filepath.Join(t.TempDir(), "req.http")
	src := "### one\nGET " + srv.URL + "/a\nX-Token: first\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
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
	m.setFocus(pane.HTTPKey)
	m.layout()

	r, ok := m.lay.Panes[pane.HTTPKey]
	if !ok {
		t.Fatal("the response pane must be laid out")
	}
	p := m.httpPanel()
	col := -1
	for x := 0; x < r.W; x++ {
		if p.ResendHit(x, 0) {
			col = x
			break
		}
	}
	if col < 0 {
		t.Fatal("the header must carry a re-send affordance")
	}
	out, cmd = m.Update(press(r.X+paneContentX+col, r.Y+paneContentY))
	m = out.(Model)
	if cmd == nil {
		t.Fatal("clicking the affordance must dispatch")
	}
	again := drainHTTPResponse(t, cmd)
	if again.Err != nil {
		t.Fatal(again.Err)
	}
	if len(log) != 2 || log[1].path != "/a" {
		t.Errorf("server log after the click: %+v", log)
	}
	if p.HasSelection() {
		t.Error("the affordance must not start a text selection")
	}
}
