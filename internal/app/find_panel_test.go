package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/keymap"
	ilsp "ike/internal/lsp"
	"ike/internal/palette"
	"ike/internal/pane"
	"ike/internal/registry"
)

// findPanelApp is the usages harness on the real registry: the binding
// dispatches find.openInPanel through the registered command.
func findPanelApp(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.Global(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return out.(Model)
}

// openInFindPanelKey is the delivered half of the find.openInPanel binding
// (#2055); cmd+enter folds onto it off macOS, so the tests use it on both
// platforms.
func openInFindPanelKey() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}
}

// pressOpenInFindPanel presses the binding and applies everything the
// dispatch produced — the command's own message rides in a batch with the
// command-executed signal.
func pressOpenInFindPanel(t *testing.T, m Model) Model {
	t.Helper()
	out, cmd := m.Update(openInFindPanelKey())
	m = out.(Model)
	msgs := cmdMsgs(cmd)
	if len(msgs) == 0 {
		t.Fatal("the binding must dispatch find.openInPanel")
	}
	for _, msg := range msgs {
		out, _ = m.Update(msg)
		m = out.(Model)
	}
	return m
}

// TestUsagesPopupOpensInFindPanel is the #2055 acceptance case for the
// transient find-usages popup: the chord tips its hits into the persistent
// panel, closes the overlay, and an entry still navigates.
func TestUsagesPopupOpensInFindPanel(t *testing.T) {
	m := findPanelApp(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "a.go")
	if err := os.WriteFile(target, []byte("package a\n\nvar x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := m.Update(ilsp.ReferencesMsg{Refs: []ilsp.Reference{
		{Path: target, Line: 2, Col: 4, Preview: "var x = 1"},
		{Path: target, Line: 0, Col: 8, Preview: "package a"},
	}})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("two references must open the usages popup")
	}

	m = pressOpenInFindPanel(t, m)

	if m.palette.IsOpen() {
		t.Fatal("the hand-off must close the overlay")
	}
	p := m.usagesPanel()
	if p == nil {
		t.Fatal("the hand-off must open the panel")
	}
	if p.Count() != 2 || p.Rows() != 3 {
		t.Fatalf("panel fill: count=%d rows=%d, want 2 refs in one file", p.Count(), p.Rows())
	}
	if m.activeWS().Panes.Focused() != pane.UsagesKey {
		t.Fatal("the filled panel must take focus")
	}
	if !strings.Contains(stripped(m), "Usages") {
		t.Fatal("the panel must render the usages heading")
	}

	// The panel outlives the popup: enter still opens the location.
	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("enter in the panel must dispatch the open command")
	}
	out, _ = m.Update(cmd())
	m = out.(Model)
	if ed := m.activeEditor(); ed == nil || ed.Path() != target {
		t.Fatalf("navigation must open %s", target)
	}
}

// TestSearchEverywhereOpensInFindPanel is the same hand-off from the
// search-everywhere overlay: file hits become panel entries under a
// query-named heading, command hits (no location) are dropped.
func TestSearchEverywhereOpensInFindPanel(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "alpha.go"), []byte("package alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	m := findPanelApp(t)
	m.palette.SetSize(120, 40)
	m.palette.OpenLocked(palette.Context{Root: "."}, palette.SearchAllPrefix)
	for _, r := range "alpha" {
		out, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = out.(Model)
	}

	m = pressOpenInFindPanel(t, m)

	if m.palette.IsOpen() {
		t.Fatal("the hand-off must close the overlay")
	}
	p := m.usagesPanel()
	if p == nil {
		t.Fatal("the hand-off must open the panel")
	}
	if p.Count() == 0 {
		t.Fatal("the file hit must land in the panel")
	}
	if !strings.Contains(p.Title(), "Find: alpha") {
		t.Fatalf("panel title = %q, want the query heading", p.Title())
	}
}

// TestOpenInFindPanelIgnoresLocationlessLists: a mode without the hand-off
// (the command palette) keeps the overlay open — the chord must not close a
// list it cannot move.
func TestOpenInFindPanelIgnoresLocationlessLists(t *testing.T) {
	m := findPanelApp(t)
	m.palette.SetSize(120, 40)
	m.palette.OpenLocked(palette.Context{Root: "."}, ':')

	m = pressOpenInFindPanel(t, m)
	if !m.palette.IsOpen() {
		t.Fatal("a list without locations must leave the overlay open")
	}
	if m.usagesPanel() != nil {
		t.Fatal("no panel may open for a list without locations")
	}
}

// TestOpenInFindPanelIsDiscoverable: the binding must be visible in the
// keybinding overview, not only in the default table.
func TestOpenInFindPanelIsDiscoverable(t *testing.T) {
	m := findPanelApp(t)
	if _, ok := m.reg.Command(findPanelCommand); !ok {
		t.Fatal("the command must be registered, or the binding is inert")
	}
	// The palette's shortcut column and the cheatsheet both read the live
	// resolver, which is context-agnostic.
	if chord, ok := m.bindings.Binding(findPanelCommand); !ok || !strings.Contains(chord, "enter") {
		t.Fatalf("binding = %q, ok=%v, want the enter chord", chord, ok)
	}
	// The keybinding overview groups the table by context: the binding must
	// show up under "palette".
	var found bool
	for _, g := range m.bindings.Table().Help() {
		if g.Context != keymap.Palette {
			continue
		}
		for _, e := range g.Entries {
			found = found || e.Command == findPanelCommand
		}
	}
	if !found {
		t.Fatal("the binding is missing from the palette group of the keybinding overview")
	}
}

// TestPanelRefsKeepsLocationRowsOnly guards the row conversion: definition,
// peek and file rows carry a target, command rows do not.
func TestPanelRefsKeepsLocationRowsOnly(t *testing.T) {
	refs := panelRefs([]palette.Item{
		{Title: "run", Msg: palette.RunCommandMsg{ID: "editor.write"}},
		{Title: "a.go:3", Detail: "var x", Msg: ilsp.DefinitionMsg{Path: "/a.go", Line: 2, Col: 4}},
		{Title: "b.go:9", Msg: ilsp.PeekDefinitionMsg{Path: "/b.go", Line: 8, Col: 1}},
		{Title: "c.go", Detail: "sub/c.go", Msg: palette.OpenFileMsg{Path: "/sub/c.go"}},
	})
	if len(refs) != 3 {
		t.Fatalf("refs = %+v, want the three location rows", refs)
	}
	if refs[0].Path != "/a.go" || refs[0].Line != 2 || refs[0].Col != 4 || refs[0].Preview != "var x" {
		t.Fatalf("definition row = %+v", refs[0])
	}
	if refs[1].Path != "/b.go" || refs[1].Line != 8 || refs[1].Preview != "b.go:9" {
		t.Fatalf("peek row = %+v (title is the preview fallback)", refs[1])
	}
	if refs[2].Path != "/sub/c.go" || refs[2].Line != 0 {
		t.Fatalf("file row = %+v, want the file's top", refs[2])
	}
}
