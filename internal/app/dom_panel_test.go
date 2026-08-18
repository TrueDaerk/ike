package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/domview"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/registry"
)

// dom_panel_test.go covers the DOM inspector tool window wiring (#1929): the
// toggle state machine, the async parse funnel, cursor follow, selector
// highlight routing, navigation through the open funnel, the non-HTML notice
// and layout persistence.

const domFixture = `<div id="root">
  <ul class="list">
    <li class="item">one</li>
    <li class="item">two</li>
  </ul>
  <p>tail</p>
</div>
`

// domProject builds a project with one HTML fixture and one Go file.
func domProject(t *testing.T) (html, goFile string) {
	t.Helper()
	root := t.TempDir()
	html = filepath.Join(root, "page.html")
	if err := os.WriteFile(html, []byte(domFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	goFile = filepath.Join(root, "main.go")
	if err := os.WriteFile(goFile, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return html, goFile
}

// domSeed opens the HTML fixture and the inspector, feeding the async parse
// result back in — the state every interaction test starts from.
func domSeed(t *testing.T) (Model, string) {
	t.Helper()
	htmlPath, _ := domProject(t)
	m := newSized()
	tm, _ := m.openPath(htmlPath, false)
	m = tm.(Model)
	tm, cmd := m.Update(DOMToggleMsg{})
	m = tm.(Model)
	m = domDeliver(t, m, cmd)
	return m, htmlPath
}

// domDeliver finds the domParsedMsg in a settled pass's command tree and
// feeds it into the model.
func domDeliver(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for _, msg := range cmdMsgs(cmd) {
		if parsed, ok := msg.(domParsedMsg); ok {
			tm, _ := m.Update(parsed)
			return tm.(Model)
		}
	}
	t.Fatal("no domParsedMsg in the command tree — the async parse never spawned")
	return m
}

func TestDOMToggleStateMachine(t *testing.T) {
	m, _ := domSeed(t)
	editorKey := m.activeEditorKey()
	if !m.activeWS().Panes.Has(pane.DOMKey) {
		t.Fatal("toggle must open the DOM pane")
	}
	if m.activeWS().Panes.Focused() != pane.DOMKey {
		t.Fatal("the fresh pane must hold focus")
	}

	// Focused → toggle returns focus to where it came from.
	tm, _ := m.Update(DOMToggleMsg{})
	m = tm.(Model)
	if m.activeWS().Panes.Focused() != editorKey {
		t.Fatalf("focus = %q, want %q", m.activeWS().Panes.Focused(), editorKey)
	}
	if !m.activeWS().Panes.Has(pane.DOMKey) {
		t.Fatal("returning focus must not close the pane")
	}

	// Unfocused → toggle focuses the open pane.
	tm, _ = m.Update(DOMToggleMsg{})
	m = tm.(Model)
	if m.activeWS().Panes.Focused() != pane.DOMKey {
		t.Fatal("toggle must re-focus the open pane")
	}
}

func TestDOMParseDeliveryAndFollow(t *testing.T) {
	m, htmlPath := domSeed(t)
	sp := m.domPanel()
	if sp == nil || sp.Path() != htmlPath || len(sp.Rows()) != 8 {
		t.Fatalf("delivery did not reach the panel: path=%q rows=%d", sp.Path(), len(sp.Rows()))
	}
	// The request dedup holds the delivered state; the next settled pass
	// must not reparse the unchanged buffer.
	tm, cmd := m.Update(DOMToggleMsg{}) // focus back to the editor
	m = tm.(Model)
	for _, msg := range cmdMsgs(cmd) {
		if _, ok := msg.(domParsedMsg); ok {
			t.Fatal("an unchanged buffer must not reparse every pass")
		}
	}

	// Cursor follow: move the caret into the text of the first <li>; the
	// settled pass highlights the enclosing node.
	ed := m.activeWS().Panes.Get(m.activeEditorKey()).Editor()
	ed.SetCursor(2, 22)
	tm, _ = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m = tm.(Model)
	if got := m.domPanel().Current(); got != 3 {
		t.Fatalf("current = %d, want 3 (the “one” text node)", got)
	}
}

func TestDOMEditRetriggersParse(t *testing.T) {
	m, _ := domSeed(t)
	tm, _ := m.Update(DOMToggleMsg{}) // focus the editor
	m = tm.(Model)
	before := m.domPanel().DocVersion()
	// An insert bumps the document version; the settled pass reparses.
	tm, _ = m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	m = tm.(Model)
	tm, cmd := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m = domDeliver(t, tm.(Model), cmd)
	if m.domPanel().DocVersion() == before {
		t.Fatal("the reparse must carry the new document version")
	}
}

func TestDOMSelectorHighlightsSyncToEditors(t *testing.T) {
	m, htmlPath := domSeed(t)
	// Type "/li" into the focused pane: each key runs a settled pass which
	// routes the match ranges to the editors.
	for _, k := range []tea.KeyPressMsg{
		{Code: '/', Text: "/"}, {Code: 'l', Text: "l"}, {Code: 'i', Text: "i"},
	} {
		tm, _ := m.Update(k)
		m = tm.(Model)
	}
	sp := m.domPanel()
	if len(sp.Matches()) != 2 {
		t.Fatalf("matches = %d, want 2", len(sp.Matches()))
	}
	if m.domHLPath != htmlPath || m.domHLRev != sp.MatchesRev() {
		t.Fatalf("highlight sync lags: path=%q rev=%d, pane rev=%d", m.domHLPath, m.domHLRev, sp.MatchesRev())
	}
}

func TestDOMNavigateThroughFunnel(t *testing.T) {
	m, htmlPath := domSeed(t)
	tm, _ := m.Update(domview.NavigateMsg{Path: htmlPath, Line: 2, Col: 4})
	m = tm.(Model)
	m = m.atPosition(t, htmlPath, 2)

	// The jump went through the open funnel, so nav.back returns to the origin.
	tm, _ = m.Update(NavBackMsg{})
	tm.(Model).atPosition(t, htmlPath, 0)
}

func TestDOMNonHTMLBufferShowsNotice(t *testing.T) {
	htmlPath, goFile := domProject(t)
	m := newSized()
	tm, _ := m.openPath(goFile, false)
	m = tm.(Model)
	tm, cmd := m.Update(DOMToggleMsg{})
	m = tm.(Model)
	for _, msg := range cmdMsgs(cmd) {
		if _, ok := msg.(domParsedMsg); ok {
			t.Fatal("a non-HTML buffer must not parse")
		}
	}
	sp := m.domPanel()
	if sp == nil || len(sp.Rows()) != 0 || sp.Path() != goFile {
		t.Fatalf("panel should show the non-HTML notice for %q", goFile)
	}
	// Switching to the HTML file re-arms the parse on the next settled pass.
	tm, _ = m.openPath(htmlPath, false)
	m = tm.(Model)
	tm, cmd = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m = domDeliver(t, tm.(Model), cmd)
	if m.domPanel().Path() != htmlPath {
		t.Fatalf("panel path = %q, want %q", m.domPanel().Path(), htmlPath)
	}
}

func TestDOMPanePersists(t *testing.T) {
	store := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", store)

	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	out, _ = m.Update(DOMToggleMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.DOMKey) {
		t.Fatal("setup: pane not open")
	}
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)

	m2 := NewWith(registry.New(), host.MapConfig{})
	inst := m2.activeWS().Panes.Get(pane.DOMKey)
	if inst == nil || inst.Kind() != pane.KindDOM {
		t.Fatal("panel did not restore")
	}
	if inst.DOM().Path() != "" {
		t.Fatal("the panel restores empty; the first sync refills it")
	}
}

func TestIsHTMLPath(t *testing.T) {
	for _, p := range []string{"a.html", "B.HTM", "c.xhtml"} {
		if !isHTMLPath(p) {
			t.Fatalf("%q should count as HTML", p)
		}
	}
	for _, p := range []string{"a.go", "b.xml", "c.md", "html"} {
		if isHTMLPath(p) {
			t.Fatalf("%q should not count as HTML", p)
		}
	}
}
