package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/explorer"
	"ike/internal/httpfile"
	"ike/internal/httphistory"
	"ike/internal/httppane"
	"ike/internal/pane"
)

// http_nolog_test.go covers the `# @no-log` directive's history write path
// (#3115): the exchange stays off disk, but the session keeps it.

// historyFiles lists the per-request history files on disk (the bodies/
// directory aside).
func historyFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(httpHistoryDir())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestHTTPNoLogSkipsDiskKeepsSession(t *testing.T) {
	// serial: withStore: t.Setenv
	withStore(t)
	body := "one"
	srv := bodyServer(t, func() string { return body })

	m := httpApp(t)
	path := filepath.Join(t.TempDir(), "req.http")
	src := "### quiet\n# @no-log\n# @capture v = .v\nGET " + srv.URL + "/a\n\n### loud\nGET " + srv.URL + "/b\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)
	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	resp := drainHTTPResponse(t, cmd)
	if resp.Err != nil || resp.Request != "quiet" {
		t.Fatalf("dispatched %q, err %v", resp.Request, resp.Err)
	}
	out, _ = m.Update(resp)
	m = out.(Model)

	if files := historyFiles(t); len(files) != 0 {
		t.Fatalf("a @no-log exchange must not reach the disk history: %v", files)
	}
	if n := len(httphistory.New(httpHistoryDir()).List(path, "quiet")); n != 0 {
		t.Fatalf("stored entries: %d, want 0", n)
	}
	if n := len(m.httpHistoryEntries(path, "quiet")); n != 1 {
		t.Fatalf("session entries: %d, want 1", n)
	}
	if v := plainView(m); !strings.Contains(v, httppane.NotLoggedMarker) {
		t.Errorf("the response pane must show the %q marker", httppane.NotLoggedMarker)
	}
	// The capture of the session-only exchange still resolves.
	if got := m.httpCaptured(path, httpfile.Parse(src)); got["v"] != "one" {
		t.Errorf("captured = %v, want v=one from the session entry", got)
	}

	// A re-run still offers the previous-vs-new diff from the in-memory ring.
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
	if _, n := m.httpPanel().HistoryIndex(); n != 2 {
		t.Errorf("pane history entries: %d, want 2", n)
	}
	inst, _, _, ok := m.diffSlot()
	if !ok {
		t.Fatal("the re-run of a @no-log request must open the previous-vs-new diff")
	}
	if inst.Diff().HunkCount() == 0 {
		t.Fatal("the changed body must diff to at least one hunk")
	}
	if files := historyFiles(t); len(files) != 0 {
		t.Fatalf("the re-run must stay off disk too: %v", files)
	}
	// The history window lists what is on disk only.
	if reqs := m.storedHTTPRequests(path); len(reqs) != 0 {
		t.Errorf("history window lists %v, want nothing", reqs)
	}

	// The next block logs again.
	out, _ = m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)
	m.activeEditor().SetCursor(6, 0)
	out, cmd = m.Update(HTTPRunMsg{})
	m = out.(Model)
	resp = drainHTTPResponse(t, cmd)
	if resp.Err != nil || resp.Request != "loud" {
		t.Fatalf("dispatched %q, err %v", resp.Request, resp.Err)
	}
	out, _ = m.Update(resp)
	m = out.(Model)
	if n := len(httphistory.New(httpHistoryDir()).List(path, "loud")); n != 1 {
		t.Errorf("stored entries of the next block: %d, want 1", n)
	}
	if files := historyFiles(t); len(files) != 1 {
		t.Errorf("history files: %v, want exactly the logged block's", files)
	}
	if v := plainView(m); strings.Contains(v, httppane.NotLoggedMarker) {
		t.Errorf("a logged exchange must not carry the marker")
	}
}
