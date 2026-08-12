package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/httphistory"
	"ike/internal/httppane"
	"ike/internal/palette"
	"ike/internal/pane"
)

// httpPickerFile writes a two-request .http file and returns its path.
func httpPickerFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "req.http")
	src := "### first\nGET http://localhost/a\n\n### second\nGET http://localhost/b\n\n### third\nGET http://localhost/c\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// seedHTTPHistory stores n responses for one request of source.
func seedHTTPHistory(t *testing.T, source, key string, n int) {
	t.Helper()
	store := httphistory.New(httpHistoryDir())
	for i := 0; i < n; i++ {
		resp := sampleResponse(key)
		resp.Body = []byte(`{"n":` + strconv.Itoa(i) + `}`)
		store.Append(source, key, httphistory.FromResponse(resp, time.Now()))
	}
}

// TestHTTPPanePickRequestKeyRoutes: "r" in the focused viewer asks the host
// for the request picker (#1829) — the pane cannot reach the history store.
func TestHTTPPanePickRequestKeyRoutes(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Source: "/p/req.http", Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)

	out, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("r must emit a command")
	}
	if _, ok := cmd().(httppane.PickRequestMsg); !ok {
		t.Fatalf("message type: %T", cmd())
	}
}

// TestHTTPRequestPickerListsStoredRequests: the picker lists exactly the
// requests of the pane's .http file that have stored responses, and choosing
// one loads its history like http.showResponse does (#1829).
func TestHTTPRequestPickerListsStoredRequests(t *testing.T) {
	m := httpApp(t)
	path := httpPickerFile(t)
	seedHTTPHistory(t, path, "first", 2)
	seedHTTPHistory(t, path, "second", 1)
	// "third" was never dispatched and must not show up.

	// The pane shows "second" — the situation the picker exists for.
	out, _ := m.Update(HTTPResponseMsg{Source: path, Request: "second", Resp: sampleResponse("second")})
	m = out.(Model)

	out, _ = m.Update(httppane.PickRequestMsg{})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("r must open the request picker")
	}
	items := m.httpRequests.Results("", palette.Context{})
	var titles []string
	for _, it := range items {
		titles = append(titles, it.Title)
	}
	if len(titles) != 2 || titles[0] != "first" || titles[1] != "second" {
		t.Fatalf("picker rows: %v, want [first second]", titles)
	}
	if !strings.Contains(items[0].Detail, "2 stored") {
		t.Errorf("row detail must state the stored count: %q", items[0].Detail)
	}

	// Activating the "first" row switches the pane to that request's history.
	msg, ok := items[0].Msg.(ShowStoredHTTPResponseMsg)
	if !ok {
		t.Fatalf("row message type: %T", items[0].Msg)
	}
	out, cmd := m.Update(msg)
	m = out.(Model)
	if cmd != nil {
		if _, ok := cmd().(HTTPResponseMsg); ok {
			t.Fatal("the picker must not dispatch")
		}
	}
	if got := m.httpPanel().Request(); got != "first" {
		t.Errorf("pane request: %q, want first", got)
	}
	if idx, n := m.httpPanel().HistoryIndex(); idx != 0 || n != 2 {
		t.Errorf("history state: %d/%d, want 0/2 (newest first, ←/→ browsable)", idx, n)
	}
	if m.activeWS().Panes.Focused() != pane.HTTPKey {
		t.Errorf("the picker must leave the response pane focused, focused=%q", m.activeWS().Panes.Focused())
	}
}

// TestHTTPRequestPickerEmptyStore: a file whose requests were never
// dispatched explains itself instead of opening an empty palette (#1829).
func TestHTTPRequestPickerEmptyStore(t *testing.T) {
	m := httpApp(t)
	path := httpPickerFile(t)
	out, _ := m.Update(HTTPResponseMsg{Source: path, Request: "first", Resp: sampleResponse("first")})
	m = out.(Model)
	// The dispatch above stored a response; drop the whole store again.
	if err := os.RemoveAll(httpHistoryDir()); err != nil {
		t.Fatal(err)
	}

	out, cmd := m.Update(httppane.PickRequestMsg{})
	m = out.(Model)
	if cmd != nil {
		cmd()
	}
	if m.palette.IsOpen() {
		t.Error("no stored responses must not open an empty picker")
	}
	if len(m.httpRequests.entries) != 0 {
		t.Errorf("stale picker rows: %+v", m.httpRequests.entries)
	}
}

// TestHTTPRequestPickerWithoutPaneUsesEditor: with no response yet the picker
// falls back to the focused .http file — stored history survives restarts
// (#1251), so the pane being empty is no dead end (#1829).
func TestHTTPRequestPickerWithoutPaneUsesEditor(t *testing.T) {
	m := httpApp(t)
	path := httpPickerFile(t)
	seedHTTPHistory(t, path, "third", 1)

	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)
	out, _ = m.Update(httppane.PickRequestMsg{})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("the picker must open for the focused .http file")
	}
	items := m.httpRequests.Results("", palette.Context{})
	if len(items) != 1 || items[0].Title != "third" {
		t.Fatalf("picker rows: %+v, want [third]", items)
	}

	out, _ = m.Update(items[0].Msg)
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.HTTPKey) {
		t.Fatal("choosing a row must open the viewer")
	}
	if got := m.httpPanel().Request(); got != "third" {
		t.Errorf("pane request: %q, want third", got)
	}
}

// TestHTTPRequestPickerReadsUnsavedBuffer: request blocks added but not saved
// yet still enumerate — the picker reads the open buffer, not the disk file.
func TestHTTPRequestPickerReadsUnsavedBuffer(t *testing.T) {
	m := httpApp(t)
	path := filepath.Join(t.TempDir(), "req.http")
	if err := os.WriteFile(path, []byte("### one\nGET http://localhost/a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedHTTPHistory(t, path, "two", 1)

	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)
	ed := m.activeEditor()
	if ed == nil {
		t.Fatal("the file must open in an editor")
	}
	ed.RestoreText("### one\nGET http://localhost/a\n\n### two\nGET http://localhost/b\n")

	out, _ = m.Update(httppane.PickRequestMsg{})
	m = out.(Model)
	items := m.httpRequests.Results("", palette.Context{})
	if len(items) != 1 || items[0].Title != "two" {
		t.Fatalf("picker rows: %+v, want [two] from the unsaved buffer", items)
	}
}

// TestHTTPPickRequestKeyInHelp: the new action is listed in the pane-local
// help (#1829) — it belongs to no registry command.
func TestHTTPPickRequestKeyInHelp(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)

	for _, e := range m.paneKeysHelpGroup().Entries {
		if e.Shortcut == "r" && strings.Contains(e.Title, "stored responses") {
			return
		}
	}
	t.Errorf("the request switch must be documented: %+v", m.paneKeysHelpGroup().Entries)
}
