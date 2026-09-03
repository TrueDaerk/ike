package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// structure_dispatch_test.go counts what actually leaves the funnel (#2401):
// the telemetry showed lsp.documentSymbols as the loudest internal command —
// re-requested after every focus change, every content-free save and every
// project switch. These tests drive those triggers against a registry whose
// lsp.documentSymbols command counts its runs, so a dispatch that never
// happens is provable (a suppressed dispatch also records no internal
// telemetry event, since the recording lives inside the dispatch funnel).

// symbolDispatchModel builds a model whose lsp.documentSymbols command counts
// every run into calls.
func symbolDispatchModel(t *testing.T, calls *int) Model {
	t.Helper()
	// Empty on purpose: with IKE_CONFIG_DIR set, every project shares one
	// state directory, so a switch would restore the departing project's
	// session — each root must keep its own .ike state here.
	t.Setenv("IKE_CONFIG_DIR", "")
	reg := registry.New()
	reg.Add(fakePlugin{id: "lsp", caps: plugin.Capabilities{Commands: []plugin.Command{{
		ID: "lsp.documentSymbols", Title: "Document symbols", Scope: plugin.GlobalScope(),
		Run: func(host.API) tea.Cmd { *calls++; return nil },
	}}}})
	m := NewWith(reg, host.MapConfig{"project.auto_save_on_switch": "false"})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return tm.(Model)
}

// ageStructBurst backdates the burst-dedup stamp, so a request suppressed
// after it can only have been suppressed by the cache.
func ageStructBurst(m *Model) { m.structLastAt = time.Now().Add(-2 * structBurstWindow) }

// TestStructureDispatchesOnceAcrossFocusSaveAndSwitch walks the telemetry's
// own sequence — open → focus the panel → focus back → save without a change →
// switch project away and back — and expects exactly the one request the open
// needs. Nothing in the sequence changes the buffer, so nothing else may reach
// the language server.
func TestStructureDispatchesOnceAcrossFocusSaveAndSwitch(t *testing.T) {
	base := t.TempDir()
	rootA, rootB := filepath.Join(base, "a"), filepath.Join(base, "b")
	for _, d := range []string{rootA, rootB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(rootA, "main.go")
	if err := os.WriteFile(file, []byte("l0\nl1\nl2\nl3\nl4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(rootA)

	calls := 0
	m := symbolDispatchModel(t, &calls)
	tm, _ := m.openPath(file, false)
	m = tm.(Model)
	tm, _ = m.Update(StructureToggleMsg{}) // opening the panel is the one request
	m = tm.(Model)
	if calls != 1 {
		t.Fatalf("opening the panel must request once, ran %d times", calls)
	}
	tm, _ = m.Update(ilsp.DocumentSymbolsMsg{Path: file, Symbols: sampleNodes()})
	m = tm.(Model)

	// Focus the editor, the panel, the editor again: three settled passes on
	// an unedited buffer.
	for i := 0; i < 3; i++ {
		ageStructBurst(&m)
		tm, _ = m.Update(StructureToggleMsg{})
		m = tm.(Model)
	}
	if calls != 1 {
		t.Fatalf("focus changes without an edit must not request, ran %d times", calls)
	}

	// A save that wrote the same content the cached tree was taken from.
	ageStructBurst(&m)
	tm, _ = m.Update(todoSavedMsg{path: file})
	m = tm.(Model)
	if calls != 1 {
		t.Fatalf("a save without a content change must not request, ran %d times", calls)
	}

	// Away to the other project and back: the cache parks with the workspace,
	// so the resumed buffer is answered from memory.
	tm, _ = m.performSwitch(rootB)
	m = tm.(Model)
	tm, _ = m.performSwitch(rootA)
	m = tm.(Model)
	ageStructBurst(&m)
	tm, _ = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"}) // a settled pass in the resumed project
	m = tm.(Model)
	if calls != 1 {
		t.Fatalf("a project round trip must not re-request an unchanged buffer, ran %d times", calls)
	}
	if got := m.docSymbols[file].Version; got == 0 && len(m.docSymbols) == 0 {
		t.Fatal("the resumed project lost the symbol cache")
	}

	// An edit in the resumed project still refreshes: the funnel is quiet,
	// not dead.
	ed := m.activeWS().Panes.Get(m.activeEditorKey()).Editor()
	if ed == nil || ed.Path() != file {
		t.Fatalf("setup: the resumed project must show %q", file)
	}
	entry := m.docSymbols[file]
	entry.Version-- // stand in for an edit past the cached tree
	m.docSymbols[file] = entry
	ageStructBurst(&m)
	m.structReqPath, m.structReqVersion = "", 0
	if cmd := m.structureSyncCmd(); cmd == nil {
		t.Fatal("an edited-past buffer must still refresh")
	}
	if calls != 2 {
		t.Fatalf("the edited buffer must request once more, ran %d times", calls)
	}
}

// TestStructureBurstCollapsesRepeatDispatch: two immediate refreshes for the
// same (path, version) inside the burst window cost one request — the second
// never reaches the command, so it records no internal telemetry event either.
func TestStructureBurstCollapsesRepeatDispatch(t *testing.T) {
	root, files := navProject(t)
	t.Chdir(root)
	calls := 0
	m := symbolDispatchModel(t, &calls)
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)
	tm, _ = m.Update(StructureToggleMsg{})
	m = tm.(Model)
	if calls != 1 {
		t.Fatalf("setup: opening must request once, ran %d times", calls)
	}
	// A forced refresh of the same, still-uncached path: without the dedup it
	// would dispatch again.
	m.structForce = true
	if cmd := m.structureSyncCmd(); cmd != nil {
		t.Fatal("a repeat inside the burst window must not dispatch")
	}
	if calls != 1 {
		t.Fatalf("the burst window must collapse the repeat, ran %d times", calls)
	}
	// Past the window the same force does go out.
	ageStructBurst(&m)
	m.structForce = true
	if cmd := m.structureSyncCmd(); cmd == nil {
		t.Fatal("past the burst window the refresh must dispatch")
	}
	if calls != 2 {
		t.Fatalf("the post-window refresh must request, ran %d times", calls)
	}
}

// TestStructureCacheParksWithWorkspace: the parked workspace carries the
// documentSymbol cache in its Aux, and resuming installs it back on the
// rebuilt model.
func TestStructureCacheParksWithWorkspace(t *testing.T) {
	base := t.TempDir()
	rootA, rootB := filepath.Join(base, "a"), filepath.Join(base, "b")
	for _, d := range []string{rootA, rootB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(rootA, "main.go")
	if err := os.WriteFile(file, []byte("l0\nl1\nl2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(rootA)

	calls := 0
	m := symbolDispatchModel(t, &calls)
	tm, _ := m.openPath(file, false)
	m = tm.(Model)
	tm, _ = m.Update(StructureToggleMsg{})
	m = tm.(Model)
	tm, _ = m.Update(ilsp.DocumentSymbolsMsg{Path: file, Symbols: sampleNodes()})
	m = tm.(Model)

	tm, _ = m.performSwitch(rootB)
	m = tm.(Model)
	parked := m.ws.Peek(m.ws.Background()[0])
	extras, ok := parked.Aux.(wsExtras)
	if !ok {
		t.Fatalf("parked Aux lost its wsExtras: %T", parked.Aux)
	}
	if len(extras.docSymbols[file].Symbols) != 2 {
		t.Fatalf("the symbol cache must park with the workspace: %+v", extras.docSymbols)
	}

	tm, _ = m.performSwitch(rootA)
	m = tm.(Model)
	if len(m.docSymbols[file].Symbols) != 2 {
		t.Fatalf("the resumed model must carry the cache back: %+v", m.docSymbols)
	}
}
