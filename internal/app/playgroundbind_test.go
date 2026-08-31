package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/layout"
	"ike/internal/pane"
)

// playgroundbind_test.go covers the playground's binding to its *document*
// (#2355): the inline mode renders only while its pane still shows the file
// (or response) it queries, stays mounted while the pane shows something else,
// and closes when the queried document leaves the workspace.

// openOther opens a second JSON file as another tab of the focused editor
// pane — the "open a file while the playground is up" move of the issue.
func openOther(t *testing.T, m Model, body string) Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "other.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPath(path, false)
	return drainCmd(tm.(Model), cmd)
}

// TestPlaygroundHidesWhenPaneShowsAnotherFile: opening a file into the hosting
// pane makes the file visible at once; the playground keeps its query and
// result but renders nothing.
func TestPlaygroundHidesWhenPaneShowsAnotherFile(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":[1,2,3]}`))
	m = setProgram(m, ".foo[0]")
	key := m.play.paneKey
	if !m.playInlineActive(key) {
		t.Fatal("the playground must render over its own document")
	}

	m = openOther(t, m, `{"marker":"other-file"}`)
	if !m.playOpen() {
		t.Fatal("the playground must stay mounted (#1980 semantics)")
	}
	if m.playInlineActive(key) {
		t.Error("the playground must not render over another document")
	}
	if got := m.playHeaderRowsFor(key); got != 0 {
		t.Errorf("the hidden playground reserves %d header rows, want 0", got)
	}
	if m.playFocused() {
		t.Error("keys must reach the pane's own editor while the playground is hidden")
	}
	inst := m.activeWS().Panes.Get(key)
	if inst == nil {
		t.Fatal("the hosting pane vanished")
	}
	if got := inst.Editor().Text(); !strings.Contains(got, "other-file") {
		t.Errorf("the pane shows %q, want the freshly opened file", got)
	}
	if got := m.play.result.Text(); !strings.Contains(got, "1") {
		t.Errorf("the mounted playground lost its result: %q", got)
	}
}

// TestPlaygroundReturnsWithSourceTab: switching back to the queried document's
// tab brings the playground back exactly as it was — no half-rendered state in
// between.
func TestPlaygroundReturnsWithSourceTab(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":[1,2,3]}`))
	m = setProgram(m, ".foo[1]")
	key := m.play.paneKey
	srcTab := m.activeWS().Panes.Get(key).ActiveTab()
	want := m.play.result.Text()

	m = openOther(t, m, `{"marker":"other-file"}`)
	if m.playInlineActive(key) {
		t.Fatal("the playground must be hidden while another tab is active")
	}

	m.switchTab(m.activeWS().Panes.Get(key), srcTab)
	if !m.playInlineActive(key) {
		t.Fatal("returning to the source document must show the playground again")
	}
	if got := m.play.program; got != ".foo[1]" {
		t.Errorf("program = %q, want the query it was left with", got)
	}
	if got := m.play.result.Text(); got != want {
		t.Errorf("result = %q, want %q", got, want)
	}
	if got := m.playHeaderRowsFor(key); got != playHeaderRows {
		t.Errorf("header rows = %d, want the resting %d", got, playHeaderRows)
	}
}

// TestPlaygroundClosesWithSourceDocument: closing the queried document's tab
// leaves no playground pointing at a document that is gone.
func TestPlaygroundClosesWithSourceDocument(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":[1,2,3]}`))
	key := m.play.paneKey
	srcTab := m.activeWS().Panes.Get(key).ActiveTab()
	m = openOther(t, m, `{"marker":"other-file"}`)

	inst := m.activeWS().Panes.Get(key)
	if inst.TabCount() < 2 {
		t.Fatalf("expected two tabs in the hosting pane, got %d", inst.TabCount())
	}
	m.closeTab(inst, srcTab)
	out, _ := m.Update(nil)
	m = out.(Model)
	if m.playOpen() {
		t.Error("closing the queried document must close the playground")
	}
}

// TestPlaygroundSurvivesFocusChange guards the #1980 semantics the document
// binding must not disturb: moving the focus to another pane leaves the mode
// mounted *and* rendered, since its pane still shows its document.
func TestPlaygroundSurvivesFocusChange(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":[1,2,3]}`))
	key := m.play.paneKey
	m.SplitFocused(layout.ZoneRight)
	m.setFocus(m.activeWS().Panes.Focused())
	if !m.playOpen() {
		t.Fatal("the playground must survive a focus change")
	}
	if !m.playInlineActive(key) {
		t.Error("its pane still shows its document, so it must still render")
	}
}

// TestHTTPPlaygroundUnaffectedByDocumentBinding: the response playground has no
// file document (its source key is `http:<paneKey>`), and the document binding
// must not hide it.
func TestHTTPPlaygroundUnaffectedByDocumentBinding(t *testing.T) {
	noDebounce(t)
	m := httpApp(t)
	resp := sampleResponse("one")
	resp.Body = []byte(`{"items":[{"id":7},{"id":8}]}`)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: resp})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)

	m = openJQ(t, m)
	if !m.playInlineActive(pane.HTTPKey) {
		t.Fatal("the response playground must render in its pane")
	}
	m = setProgram(m, "[.items[].id]")
	if got := strings.Join(strings.Fields(m.play.result.Text()), ""); got != "[7,8]" {
		t.Errorf("result = %q, want the ids from the response body", got)
	}
	out, _ = m.Update(nil)
	m = out.(Model)
	if !m.playOpen() || !m.playInlineActive(pane.HTTPKey) {
		t.Error("the settled pass must not drop the response playground")
	}
}
