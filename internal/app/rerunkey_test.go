package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/pane"
)

// rerunkey_test.go covers the JetBrains Rerun chord (#2314): ctrl+r in the two
// panes that own a "do that again" action — the HTTP response pane, where it
// re-sends the request the shown response came from, and the archive viewer,
// where it re-reads the listing. Both used to be pane-local keys the keymap
// layer never knew about, which is why they were logged as unbound presses.

// TestCtrlRResendsInHTTPResponsePane is the acceptance case: with the response
// pane focused, ctrl+r resolves through the keymap to http.resend and the
// stored request goes out again — the file may have changed meanwhile, the
// re-send repeats the bytes that produced the shown response.
func TestCtrlRResendsInHTTPResponsePane(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	m := newSized()
	path := filepath.Join(t.TempDir(), "req.http")
	if err := os.WriteFile(path, []byte("### one\nGET "+srv.URL+"/a\n"), 0o644); err != nil {
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
	if m.httpPanel() == nil {
		t.Fatal("the response pane must be open")
	}

	m.setFocus(pane.HTTPKey)
	m.layout()
	if got := m.keyContext(); got != "http" {
		t.Fatalf("key context = %q, want http", got)
	}
	out, cmd = m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m = out.(Model)
	var found bool
	for _, msg := range cmdMsgs(cmd) {
		if _, ok := msg.(HTTPResendMsg); ok {
			found = true
		}
	}
	if !found {
		t.Fatalf("ctrl+r must dispatch http.resend, got %#v", cmdMsgs(cmd))
	}

	// …and the command actually re-sends: the server sees the request twice.
	out, cmd = m.Update(HTTPResendMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("http.resend must dispatch")
	}
	again := drainHTTPResponse(t, cmd)
	if again.Err != nil {
		t.Fatal(again.Err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 2 || paths[1] != "/a" {
		t.Fatalf("server saw %v, want two GETs of /a", paths)
	}
}

// TestCtrlRReloadsArchiveListing: the same chord in the archive viewer re-reads
// the file, so members added to a re-packed archive appear without closing the
// pane.
func TestCtrlRReloadsArchiveListing(t *testing.T) {
	m := newSized()
	dir := t.TempDir()
	p := filepath.Join(dir, "src.tar")
	if err := os.WriteFile(p, tarBytes(t, map[string]string{"a.txt": "a\n"}), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(OpenArchiveMsg{Path: p})
	m = out.(Model)
	keys := archiveKeys(m)
	if len(keys) != 1 {
		t.Fatalf("expected one archive pane, got %v", keys)
	}
	if got := m.activeWS().Panes.Get(keys[0]).Archive().Entries(); got != 1 {
		t.Fatalf("entries = %d, want 1", got)
	}

	// The archive is re-packed with a second member while the pane is open.
	if err := os.WriteFile(p, tarBytes(t, map[string]string{"a.txt": "a\n", "b.txt": "b\n"}), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := m.keyContext(); got != "archive" {
		t.Fatalf("key context = %q, want archive", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	av := m.activeWS().Panes.Get(keys[0]).Archive()
	if got := av.Entries(); got != 2 {
		t.Fatalf("after ctrl+r entries = %d, want 2", got)
	}
	var rows []string
	for i := 0; i < av.Rows(); i++ {
		rows = append(rows, av.RowName(i))
	}
	if strings.Join(rows, ",") != "a.txt,b.txt" {
		t.Fatalf("rows = %v, want the reloaded listing", rows)
	}
}

// TestArchiveReloadWithoutArchiveFocusExplains: the palette entry needs an
// archive viewer; without one it says so instead of doing nothing.
func TestArchiveReloadWithoutArchiveFocusExplains(t *testing.T) {
	m := newSized()
	out, _ := m.Update(ArchiveReloadMsg{})
	m = out.(Model)
	if got := extractNotice(m); !strings.Contains(got, "focus an archive viewer") {
		t.Fatalf("notification = %q", got)
	}
}
