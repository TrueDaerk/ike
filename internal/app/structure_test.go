package app

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/pane"
	"ike/internal/registry"
	"ike/internal/structpanel"
)

// structure_test.go covers the Structure tool window wiring (#1025): the
// toggle state machine, the refresh triggers, symbol delivery, navigation
// through the open funnel, and layout persistence.

func structureSeed(t *testing.T) (Model, []string) {
	t.Helper()
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	return tm.(Model), files
}

func sampleNodes() []ilsp.SymbolNode {
	return []ilsp.SymbolNode{
		{Name: "Top", Kind: 12, Line: 1, Col: 0, EndLine: 5},
		{Name: "Other", Kind: 12, Line: 7, Col: 0, EndLine: 9},
	}
}

func TestStructureToggleStateMachine(t *testing.T) {
	m, _ := structureSeed(t)
	editorKey := m.activeEditorKey()

	// Open: the pane appears and takes focus.
	tm, _ := m.Update(StructureToggleMsg{})
	m = tm.(Model)
	if !m.activeWS().Panes.Has(pane.StructureKey) {
		t.Fatal("toggle must open the structure pane")
	}
	if m.activeWS().Panes.Focused() != pane.StructureKey {
		t.Fatal("the fresh pane must hold focus")
	}

	// Focused → toggle returns focus to where it came from.
	tm, _ = m.Update(StructureToggleMsg{})
	m = tm.(Model)
	if m.activeWS().Panes.Focused() != editorKey {
		t.Fatalf("focus = %q, want %q", m.activeWS().Panes.Focused(), editorKey)
	}
	if !m.activeWS().Panes.Has(pane.StructureKey) {
		t.Fatal("returning focus must not close the pane")
	}

	// Unfocused → toggle focuses the open pane.
	tm, _ = m.Update(StructureToggleMsg{})
	m = tm.(Model)
	if m.activeWS().Panes.Focused() != pane.StructureKey {
		t.Fatal("toggle must re-focus the open pane")
	}
}

func TestStructureOpenRequestsSymbolsOnce(t *testing.T) {
	m, files := structureSeed(t)
	tm, _ := m.Update(StructureToggleMsg{})
	m = tm.(Model)
	if m.structReqPath != files[0] {
		t.Fatalf("opening must issue a refresh for %q, requested %q", files[0], m.structReqPath)
	}
	// While the request is outstanding (or the server has no provider),
	// further settled passes must not re-request or re-arm the debounce.
	if cmd := m.structureSyncCmd(); cmd != nil {
		t.Fatal("an unanswered path must not re-request every pass")
	}
	// A save of a path the cache cannot answer for still re-requests, once
	// the burst window of the opening dispatch has passed (#2401).
	m.structLastAt = time.Now().Add(-structBurstWindow)
	m.structForce = true
	if cmd := m.structureSyncCmd(); cmd == nil {
		t.Fatal("a forced refresh must re-request the same path")
	}
	if m.structForce {
		t.Fatal("the forced refresh must consume the flag")
	}
}

func TestStructureSymbolsDeliveryAndFollow(t *testing.T) {
	m, files := structureSeed(t)
	tm, _ := m.Update(StructureToggleMsg{})
	m = tm.(Model)
	tm, _ = m.Update(ilsp.DocumentSymbolsMsg{Path: files[0], Symbols: sampleNodes()})
	m = tm.(Model)
	sp := m.structPanel()
	if sp == nil || len(sp.Rows()) != 2 || sp.Path() != files[0] {
		t.Fatalf("delivery did not reach the panel: %+v", sp)
	}

	// Cursor follow: move the editor caret into "Other"; the settled pass
	// highlights the enclosing symbol.
	tm, _ = m.Update(StructureToggleMsg{}) // focus back to the editor
	m = tm.(Model)
	ed := m.activeWS().Panes.Get(m.activeEditorKey()).Editor()
	ed.SetCursor(8, 0)
	tm, _ = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m = tm.(Model)
	if got := m.structPanel().Current(); got != 1 {
		t.Fatalf("current = %d, want 1 (enclosing symbol)", got)
	}
}

func TestStructureNavigateMovesEditorAndRecordsHistory(t *testing.T) {
	m, files := structureSeed(t)
	tm, _ := m.Update(StructureToggleMsg{})
	m = tm.(Model)
	tm, _ = m.Update(structpanel.NavigateMsg{Path: files[0], Line: 7, Col: 2})
	m = tm.(Model)
	m = m.atPosition(t, files[0], 7)

	// The jump went through the open funnel, so nav.back returns to the origin.
	tm, _ = m.Update(NavBackMsg{})
	m = tm.(Model).atPosition(t, files[0], 0)
}

func TestStructureBufferSwitchRefreshes(t *testing.T) {
	m, files := structureSeed(t)
	tm, _ := m.Update(StructureToggleMsg{})
	m = tm.(Model)
	tm, _ = m.Update(ilsp.DocumentSymbolsMsg{Path: files[0], Symbols: sampleNodes()})
	m = tm.(Model)

	// Opening an uncached file arms the debounce on the next settled pass;
	// the request itself only goes out once the timer fires (#2319).
	tm, _ = m.openPath(files[1], false)
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m = tm.(Model)
	if m.structReqPath == files[1] {
		t.Fatal("the switch must debounce, not request immediately")
	}
	if m.structDebPath != files[1] {
		t.Fatalf("debounce armed for %q, want %q", m.structDebPath, files[1])
	}
	tm, _ = m.Update(structDebounceMsg{seq: m.structDebSeq})
	m = tm.(Model)
	if m.structReqPath != files[1] {
		t.Fatalf("switching buffers must request %q, requested %q", files[1], m.structReqPath)
	}
}

func TestStructureTabSwitchCacheHit(t *testing.T) {
	m, files := structureSeed(t)
	tm, _ := m.Update(StructureToggleMsg{})
	m = tm.(Model)
	tm, _ = m.Update(ilsp.DocumentSymbolsMsg{Path: files[0], Symbols: sampleNodes()})
	m = tm.(Model)
	tm, _ = m.openPath(files[1], false)
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"}) // settled pass arms files[1]
	m = tm.(Model)
	tm, _ = m.Update(structDebounceMsg{seq: m.structDebSeq})
	m = tm.(Model)
	tm, _ = m.Update(ilsp.DocumentSymbolsMsg{Path: files[1], Symbols: sampleNodes()[:1]})
	m = tm.(Model)

	// Switching back to the unchanged first buffer must refeed the panel
	// from the cache — no debounce armed, no request dispatched (#2319).
	debSeq := m.structDebSeq
	tm, _ = m.openPath(files[0], false)
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m = tm.(Model)
	if m.structReqPath != files[1] {
		t.Fatalf("the cached switch must not request, requested %q", m.structReqPath)
	}
	if m.structDebSeq != debSeq {
		t.Fatal("the cached switch must not arm the debounce")
	}
	sp := m.structPanel()
	if sp == nil || sp.Path() != files[0] || len(sp.Rows()) != 2 {
		t.Fatalf("the panel must refill from the cache: %+v", sp)
	}
}

func TestStructureEditInvalidatesCache(t *testing.T) {
	m, files := structureSeed(t)
	tm, _ := m.Update(StructureToggleMsg{})
	m = tm.(Model)
	tm, _ = m.Update(ilsp.DocumentSymbolsMsg{Path: files[0], Symbols: sampleNodes()})
	m = tm.(Model)
	tm, _ = m.Update(StructureToggleMsg{}) // focus back to the editor
	m = tm.(Model)

	// An edit bumps the buffer DocVersion past the cached tree: the settled
	// pass arms the debounced refresh, and its tick re-requests the path.
	for _, k := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"}, {Code: 'x', Text: "x"}, {Code: tea.KeyEscape},
	} {
		tm, _ = m.Update(k)
		m = tm.(Model)
	}
	if m.structDebPath != files[0] {
		t.Fatalf("the edit must arm the refresh for %q, armed %q", files[0], m.structDebPath)
	}
	tm, _ = m.Update(structDebounceMsg{seq: m.structDebSeq})
	m = tm.(Model)
	ed := m.activeWS().Panes.Get(m.activeEditorKey()).Editor()
	if m.structReqPath != files[0] || m.structReqVersion != ed.DocVersion() {
		t.Fatalf("the edit must re-request %q at version %d, got %q at %d",
			files[0], ed.DocVersion(), m.structReqPath, m.structReqVersion)
	}
	// The reply caches at the requested version, so the next settled pass is
	// quiet again and the panel shows the refreshed tree.
	tm, _ = m.Update(ilsp.DocumentSymbolsMsg{Path: files[0], Symbols: sampleNodes()[:1]})
	m = tm.(Model)
	if cmd := m.structureSyncCmd(); cmd != nil {
		t.Fatal("the refreshed cache must be quiet on the next pass")
	}
	if got := len(m.structPanel().Rows()); got != 1 {
		t.Fatalf("panel rows = %d, want 1 (refreshed tree)", got)
	}
}

func TestStructureDebounceDropsSupersededTick(t *testing.T) {
	m, files := structureSeed(t)
	tm, _ := m.Update(StructureToggleMsg{})
	m = tm.(Model)
	tm, _ = m.Update(ilsp.DocumentSymbolsMsg{Path: files[0], Symbols: sampleNodes()})
	m = tm.(Model)

	// Cycle files[0] → files[1] → files[0]: the tick armed for files[1] is
	// stale by the time it fires, so no request for the passed-over tab.
	tm, _ = m.openPath(files[1], false)
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"}) // settled pass arms files[1]
	m = tm.(Model)
	staleSeq := m.structDebSeq
	if m.structDebPath != files[1] {
		t.Fatalf("setup: debounce armed for %q, want %q", m.structDebPath, files[1])
	}
	tm, _ = m.openPath(files[0], false)
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m = tm.(Model)
	before := m.structReqPath
	tm, _ = m.Update(structDebounceMsg{seq: staleSeq})
	m = tm.(Model)
	if m.structReqPath != before {
		t.Fatalf("a superseded tick must not request; requested %q", m.structReqPath)
	}
	if m.structReqPath == files[1] {
		t.Fatal("the passed-over tab must never be requested")
	}
}

// TestStructureSaveOfUnchangedBufferAsksNothing: a write that changed no
// content leaves the cached tree valid — the server sees the very document it
// answered for, so the forced refresh is dropped (#2401). A save of a buffer
// the cache no longer covers still re-requests.
func TestStructureSaveOfUnchangedBufferAsksNothing(t *testing.T) {
	m, files := structureSeed(t)
	tm, _ := m.Update(StructureToggleMsg{})
	m = tm.(Model)
	tm, _ = m.Update(ilsp.DocumentSymbolsMsg{Path: files[0], Symbols: sampleNodes()})
	m = tm.(Model)
	if m.structForce {
		t.Fatal("setup: no forced refresh pending")
	}
	// Clear the dispatch bookkeeping, so only the cache can explain a silent
	// pass — never the burst dedup.
	m.structLastPath, m.structLastAt = "", time.Time{}
	m.structReqPath, m.structReqVersion = "", 0
	tm, _ = m.Update(todoSavedMsg{path: files[0]})
	m = tm.(Model)
	if m.structForce {
		t.Fatal("the forced refresh must be consumed by the settled pass")
	}
	if m.structReqPath != "" {
		t.Fatalf("an unchanged save must not re-request, requested %q", m.structReqPath)
	}

	// Age the cached tree past the buffer (as an edit would): now the save
	// does re-request.
	entry := m.docSymbols[files[0]]
	entry.Version--
	m.docSymbols[files[0]] = entry
	tm, _ = m.Update(todoSavedMsg{path: files[0]})
	m = tm.(Model)
	if m.structReqPath != files[0] {
		t.Fatalf("a save past the cached version must re-request %q, requested %q", files[0], m.structReqPath)
	}
}

func TestStructurePanePersists(t *testing.T) {
	store := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", store)

	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	out, _ = m.Update(StructureToggleMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.StructureKey) {
		t.Fatal("setup: pane not open")
	}
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)

	m2 := NewWith(registry.New(), host.MapConfig{})
	inst := m2.activeWS().Panes.Get(pane.StructureKey)
	if inst == nil || inst.Kind() != pane.KindStructure {
		t.Fatal("panel did not restore")
	}
	if inst.Structure().Path() != "" {
		t.Fatal("the panel restores empty; the first sync refills it")
	}
}
