package app

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/httpclient"
	"ike/internal/httphistory"
	"ike/internal/httppane"
	"ike/internal/palette"
	"ike/internal/pane"
)

// seedHTTPBodies stores one response per body for a request, oldest first —
// the store keeps them newest first, so the last body ends up at index 0.
func seedHTTPBodies(t *testing.T, source, key string, bodies ...string) {
	t.Helper()
	store := httphistory.New(httpHistoryDir())
	for i, body := range bodies {
		resp := &httpclient.Response{
			Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
			Headers:    http.Header{"Content-Type": {"application/json"}},
			Body:       []byte(body),
			RequestKey: key,
		}
		store.Append(source, key, httphistory.FromResponse(resp, time.Now().Add(time.Duration(i)*time.Second)))
	}
}

// showStored puts the request's stored responses into the viewer, the way
// http.showResponse does.
func showStored(t *testing.T, m Model, source, key string) Model {
	t.Helper()
	out, _ := m.Update(ShowStoredHTTPResponseMsg{Source: source, Request: key})
	return out.(Model)
}

// TestHTTPDiffKeyRoutes: "D" in the focused viewer asks the host for the
// entry picker (#1992) — the pane reaches neither the history store nor the
// diff viewer.
func TestHTTPDiffKeyRoutes(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Source: "/p/req.http", Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'D', Text: "D", Mod: tea.ModShift})
	if cmd == nil {
		t.Fatal("D must emit a command")
	}
	if _, ok := cmd().(httppane.DiffHistoryMsg); !ok {
		t.Fatalf("message type: %T", cmd())
	}
}

// TestHTTPDiffPickerListsOtherEntries: the picker offers every stored
// response of the request except the one on show, and choosing a row opens
// the pair in the diff pane, older on the left (#1992).
func TestHTTPDiffPickerListsOtherEntries(t *testing.T) {
	m := httpApp(t)
	path := httpPickerFile(t)
	seedHTTPBodies(t, path, "first", `{"n":1}`, `{"n":2}`, `{"n":3}`)
	m = showStored(t, m, path, "first")

	out, _ := m.Update(httppane.DiffHistoryMsg{})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("D must open the entry picker")
	}
	items := m.httpEntries.Results("", palette.Context{})
	if len(items) != 2 {
		t.Fatalf("picker rows: %d, want 2 (3 stored minus the one on show)", len(items))
	}
	for _, it := range items {
		if strings.HasPrefix(it.Title, "1/3") {
			t.Fatalf("the shown entry must not be offered: %q", it.Title)
		}
		if !strings.Contains(it.Detail, "HTTP/1.1") {
			t.Errorf("row detail must describe the response: %q", it.Detail)
		}
	}

	msg, ok := items[0].Msg.(DiffHTTPEntriesMsg)
	if !ok {
		t.Fatalf("row message type: %T", items[0].Msg)
	}
	if msg.Shown != 0 || msg.Other != 1 || msg.Request != "first" || msg.Source != path {
		t.Fatalf("row message: %+v", msg)
	}

	out, _ = m.Update(msg)
	m = out.(Model)
	inst, _, _, ok := m.diffSlot()
	if !ok {
		t.Fatal("choosing an entry must open the diff viewer")
	}
	left, right := inst.Diff().Titles()
	// Index 1 is the older response ({"n":2}), index 0 the newer ({"n":3}).
	if !strings.HasPrefix(left, "first 2/3") || !strings.HasPrefix(right, "first 1/3") {
		t.Fatalf("titles: %q vs %q, want the older response on the left", left, right)
	}
	if inst.Diff().HunkCount() == 0 {
		t.Fatal("two different bodies must diff to at least one hunk")
	}
}

// TestHTTPDiffNormalizesJSON is the acceptance criterion inside the app flow
// (#1992): two responses differing only in key order and formatting produce
// an empty diff, while a changed value still shows up.
func TestHTTPDiffNormalizesJSON(t *testing.T) {
	m := httpApp(t)
	path := httpPickerFile(t)
	seedHTTPBodies(t, path, "first",
		`{"a":1,"b":{"x":true,"y":2}}`,
		"{\n  \"b\": {\n    \"y\": 2,\n    \"x\": true\n  },\n  \"a\": 1\n}",
	)
	m = showStored(t, m, path, "first")

	out, _ := m.Update(DiffHTTPEntriesMsg{Source: path, Request: "first", Shown: 0, Other: 1})
	m = out.(Model)
	inst, _, _, ok := m.diffSlot()
	if !ok {
		t.Fatal("the diff viewer must open")
	}
	if n := inst.Diff().HunkCount(); n != 0 {
		t.Fatalf("a key-order-only difference must diff to nothing, got %d hunks", n)
	}

	// A real change still differs.
	seedHTTPBodies(t, path, "second", `{"a":1}`, `{"a":2}`)
	out, _ = m.Update(DiffHTTPEntriesMsg{Source: path, Request: "second", Shown: 0, Other: 1})
	m = out.(Model)
	inst, _, _, _ = m.diffSlot()
	if inst.Diff().HunkCount() == 0 {
		t.Fatal("a changed value must still diff")
	}
}

// TestHTTPDiffComparesHeaders: the diff text carries the status line and the
// headers as well as the body (#1992).
func TestHTTPDiffComparesHeaders(t *testing.T) {
	m := httpApp(t)
	path := httpPickerFile(t)
	store := httphistory.New(httpHistoryDir())
	for _, ct := range []string{"application/json", "application/json; charset=utf-8"} {
		store.Append(path, "first", httphistory.Entry{
			Time: time.Now(), Proto: "HTTP/1.1", Status: "200 OK", StatusCode: 200,
			Headers: http.Header{"Content-Type": {ct}},
			Body:    []byte(`{"a":1}`),
		})
	}
	m = showStored(t, m, path, "first")

	out, _ := m.Update(DiffHTTPEntriesMsg{Source: path, Request: "first", Shown: 0, Other: 1})
	m = out.(Model)
	inst, _, _, ok := m.diffSlot()
	if !ok {
		t.Fatal("the diff viewer must open")
	}
	if inst.Diff().HunkCount() == 0 {
		t.Fatal("a header-only difference must show up in the diff")
	}
}

// TestHTTPDiffSingleEntryExplains: one stored response has nothing to compare
// against — say so instead of opening an empty picker (#1992).
func TestHTTPDiffSingleEntryExplains(t *testing.T) {
	m := httpApp(t)
	path := httpPickerFile(t)
	seedHTTPBodies(t, path, "first", `{"n":1}`)
	m = showStored(t, m, path, "first")

	out, _ := m.Update(httppane.DiffHistoryMsg{})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Error("a single stored response must not open the picker")
	}
	if _, _, _, ok := m.diffSlot(); ok {
		t.Error("nothing to compare must not open a diff pane")
	}
}

// TestHTTPDiffWithoutPaneExplains: the command is reachable from the palette
// at any time, so it has to survive being run with no response pane (#1992).
func TestHTTPDiffWithoutPaneExplains(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPDiffResponsesMsg{})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Error("no response pane must not open the picker")
	}
}

// TestHTTPDiffStalePairExplains: the history can be pruned between opening
// the picker and choosing a row — the stale pair must not diff the wrong two
// responses (#1992).
func TestHTTPDiffStalePairExplains(t *testing.T) {
	m := httpApp(t)
	path := httpPickerFile(t)
	seedHTTPBodies(t, path, "first", `{"n":1}`, `{"n":2}`)
	m = showStored(t, m, path, "first")
	if err := os.RemoveAll(httpHistoryDir()); err != nil {
		t.Fatal(err)
	}

	out, _ := m.Update(DiffHTTPEntriesMsg{Source: path, Request: "first", Shown: 0, Other: 1})
	m = out.(Model)
	if _, _, _, ok := m.diffSlot(); ok {
		t.Error("a vanished pair must not open a diff pane")
	}
}

// TestHTTPDiffKeyInHelp: the pane-local action is listed in the cheatsheet
// (#1992) — it belongs to no registry command.
func TestHTTPDiffKeyInHelp(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)
	g := m.paneKeysHelpGroup()
	var found bool
	for _, e := range g.Entries {
		if e.Shortcut == "D" {
			found = true
		}
	}
	if !found {
		t.Errorf("the diff key must appear in the pane help group: %+v", g.Entries)
	}
}

// TestHTTPDiffPreviousRunKeyRoutes: "P" in the focused viewer asks the host to
// diff directly against the previous run (#2060) — no picker in between.
func TestHTTPDiffPreviousRunKeyRoutes(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Source: "/p/req.http", Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'P', Text: "P", Mod: tea.ModShift})
	if cmd == nil {
		t.Fatal("P must emit a command")
	}
	if _, ok := cmd().(httppane.DiffPreviousRunMsg); !ok {
		t.Fatalf("message type: %T", cmd())
	}
}

// TestHTTPDiffPreviousRunOpensDirectly: with several stored responses, the
// shortcut opens the shown one against the run right before it, skipping the
// picker entirely (#2060).
func TestHTTPDiffPreviousRunOpensDirectly(t *testing.T) {
	m := httpApp(t)
	path := httpPickerFile(t)
	seedHTTPBodies(t, path, "first", `{"n":1}`, `{"n":2}`, `{"n":3}`)
	m = showStored(t, m, path, "first")

	out, _ := m.Update(httppane.DiffPreviousRunMsg{})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Error("the direct shortcut must not open the picker")
	}
	inst, _, _, ok := m.diffSlot()
	if !ok {
		t.Fatal("P must open the diff viewer directly")
	}
	left, right := inst.Diff().Titles()
	// Shown is index 0 ({"n":3}); the previous run is index 1 ({"n":2}).
	if !strings.HasPrefix(left, "first 2/3") || !strings.HasPrefix(right, "first 1/3") {
		t.Fatalf("titles: %q vs %q, want the previous run on the left", left, right)
	}
	if inst.Diff().HunkCount() == 0 {
		t.Fatal("two different bodies must diff to at least one hunk")
	}
}

// TestHTTPDiffPreviousRunAtOldestExplains: the oldest stored response has no
// earlier run to compare against — say so instead of diffing nothing (#2060).
func TestHTTPDiffPreviousRunAtOldestExplains(t *testing.T) {
	m := httpApp(t)
	path := httpPickerFile(t)
	seedHTTPBodies(t, path, "first", `{"n":1}`, `{"n":2}`)
	m = showStored(t, m, path, "first")

	m.setFocus(pane.HTTPKey)
	out, _ := m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	m = out.(Model)
	if idx, n := m.httpPanel().HistoryIndex(); idx != n-1 {
		t.Fatalf("history index: %d/%d, want the oldest entry on show", idx, n)
	}

	out, _ = m.Update(httppane.DiffPreviousRunMsg{})
	m = out.(Model)
	if _, _, _, ok := m.diffSlot(); ok {
		t.Error("the oldest run must not open a diff pane")
	}
}

// TestHTTPDiffPreviousRunSingleEntryExplains: one stored response has no
// previous run to compare against (#2060).
func TestHTTPDiffPreviousRunSingleEntryExplains(t *testing.T) {
	m := httpApp(t)
	path := httpPickerFile(t)
	seedHTTPBodies(t, path, "first", `{"n":1}`)
	m = showStored(t, m, path, "first")

	out, _ := m.Update(httppane.DiffPreviousRunMsg{})
	m = out.(Model)
	if _, _, _, ok := m.diffSlot(); ok {
		t.Error("a single stored response must not open a diff pane")
	}
}

// TestHTTPDiffPreviousRunWithoutPaneExplains: the command is reachable from
// the palette at any time, so it has to survive being run with no response
// pane open (#2060).
func TestHTTPDiffPreviousRunWithoutPaneExplains(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPDiffPreviousRunMsg{})
	m = out.(Model)
	if _, _, _, ok := m.diffSlot(); ok {
		t.Error("no response pane must not open a diff pane")
	}
}

// TestHTTPDiffPreviousRunKeyInHelp: the pane-local action is listed in the
// cheatsheet (#2060) — it belongs to no registry command.
func TestHTTPDiffPreviousRunKeyInHelp(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)
	g := m.paneKeysHelpGroup()
	var found bool
	for _, e := range g.Entries {
		if e.Shortcut == "P" {
			found = true
		}
	}
	if !found {
		t.Errorf("the diff-previous-run key must appear in the pane help group: %+v", g.Entries)
	}
}
