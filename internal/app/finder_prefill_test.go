package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/httpclient"
)

// prefillApp opens body in an editor and returns the app, ready for a visual
// selection.
func prefillApp(t *testing.T, body string) Model {
	t.Helper()
	m := newSized()
	path := filepath.Join(t.TempDir(), "hit.go")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	return out.(Model)
}

// selectWord enters visual mode and extends the selection to the end of the
// word under the cursor ("ve").
func selectWord(m Model) Model {
	m = drainKey(m, tea.KeyPressMsg{Code: 'v', Text: "v"})
	return drainKey(m, tea.KeyPressMsg{Code: 'e', Text: "e"})
}

// TestFindInPathPrefillsEditorSelection is the headline acceptance criterion
// of #2165: an editor visual selection prefills Find in Path, and typing
// replaces it without any extra keys.
func TestFindInPathPrefillsEditorSelection(t *testing.T) {
	m := prefillApp(t, "needle one\ntwo\n")
	m = selectWord(m)
	if sel := m.activeSelectionText(); sel != "needle" {
		t.Fatalf("visual selection = %q, want %q", sel, "needle")
	}

	out, _ := m.Update(OpenFindInPathMsg{})
	m = out.(Model)
	if !m.finder.IsOpen() {
		t.Fatal("project.findInPath must open the overlay")
	}
	if got := m.finder.Query(); got != "needle" {
		t.Fatalf("prefilled query = %q, want %q", got, "needle")
	}

	// The prefill is selected: the first typed character replaces it.
	m = drainKey(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := m.finder.Query(); got != "x" {
		t.Fatalf("typing over the prefill should replace it, got %q", got)
	}
}

// TestReplaceInPathPrefillsEditorSelection: replace-in-path shares the seam.
func TestReplaceInPathPrefillsEditorSelection(t *testing.T) {
	m := prefillApp(t, "needle one\n")
	m = selectWord(m)
	out, _ := m.Update(OpenReplaceInPathMsg{})
	m = out.(Model)
	if got := m.finder.Query(); got != "needle" {
		t.Fatalf("prefilled replace query = %q, want %q", got, "needle")
	}
}

// TestFindInPathWithoutSelectionKeepsQuery: with nothing selected the overlay
// opens exactly as before — the remembered query survives.
func TestFindInPathWithoutSelectionKeepsQuery(t *testing.T) {
	m := prefillApp(t, "needle one\n")
	out, _ := m.Update(OpenFindInPathMsg{})
	m = out.(Model)
	for _, r := range "remembered" {
		m = drainKey(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	if sel := m.activeSelectionText(); sel != "" {
		t.Fatalf("no selection expected outside visual mode, got %q", sel)
	}
	out, _ = m.Update(OpenFindInPathMsg{})
	m = out.(Model)
	if got := m.finder.Query(); got != "remembered" {
		t.Fatalf("without a selection the remembered query must survive, got %q", got)
	}
}

// TestFindInPathMultiLineSelectionKeepsQuery: a line-spanning selection
// prefills nothing (the documented rule, matching the editor's "/" search).
func TestFindInPathMultiLineSelectionKeepsQuery(t *testing.T) {
	m := prefillApp(t, "needle one\ntwo three\n")
	out, _ := m.Update(OpenFindInPathMsg{})
	m = out.(Model)
	for _, r := range "remembered" {
		m = drainKey(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	// Visual-line over two lines.
	m = drainKey(m, tea.KeyPressMsg{Code: 'V', Text: "V", Mod: tea.ModShift})
	m = drainKey(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if sel := m.activeSelectionText(); sel != "needle one\ntwo three" {
		t.Fatalf("two-line selection = %q", sel)
	}

	out, _ = m.Update(OpenFindInPathMsg{})
	m = out.(Model)
	if got := m.finder.Query(); got != "remembered" {
		t.Fatalf("multi-line selection must not prefill, got %q", got)
	}
}

// TestSelectionSourceHTTPResponseViewer covers a non-editor selection source
// from the audit: the HTTP response viewer's mouse selection reaches the same
// seam, so Find in Path prefills from it too.
func TestSelectionSourceHTTPResponseViewer(t *testing.T) {
	m := newSized()
	key := m.activeWS().Panes.AddHTTP()
	hp := m.activeWS().Panes.Get(key).HTTP()
	hp.SetSize(120, 20)
	hp.Set("r", &httpclient.Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
		Body: []byte("alpha beta"), Duration: time.Millisecond,
	})
	m.activeWS().Panes.SetFocused(key)

	// The title bar takes y == 0 and every row renders with one leading
	// space, so (1, 1) is the first cell of the first composed row.
	hp.MousePress(1, 1)
	hp.MouseDrag(4, 1)
	hp.MouseRelease()
	want := hp.SelectionText()
	if want == "" {
		t.Fatal("mouse drag left no selection in the response viewer")
	}
	if got := m.activeSelectionText(); got != want {
		t.Fatalf("HTTP viewer selection = %q, want %q", got, want)
	}

	out, _ := m.Update(OpenFindInPathMsg{})
	m = out.(Model)
	if got := m.finder.Query(); got != want {
		t.Fatalf("prefilled query = %q, want the viewer selection %q", got, want)
	}
}
