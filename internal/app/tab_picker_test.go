package app

import (
	"strings"
	"testing"

	"ike/internal/palette"
)

// tab_picker_test.go covers the MRU tab picker (#2151): the entry ordering,
// the speed-search filtering of the palette mode and the activation a picked
// row performs.

// TestTabPickerEntriesAreMRUOrdered: the rows follow the pane's activation
// recency, with the tab shown right now moved to the end — the preselected
// first row is the tab to flip back to.
func TestTabPickerEntriesAreMRUOrdered(t *testing.T) {
	dir := t.TempDir()
	withTabLimit(t, 0) // no eviction: all four tabs stay open
	a := writeTemp(t, dir, "a.txt", "a\n")
	b := writeTemp(t, dir, "b.txt", "b\n")
	c := writeTemp(t, dir, "c.txt", "c\n")
	d := writeTemp(t, dir, "d.txt", "d\n")
	m := openApp(t, a, b, c, d) // tabs 0..3, tab 3 (d) active
	inst := m.activeWS().Panes.FocusedInstance()
	m.switchTab(inst, 1) // b
	m.switchTab(inst, 0) // a — now active; recency: a, b, d, c

	entries := m.tabPickerEntries()
	var got []string
	for _, e := range entries {
		got = append(got, e.label)
	}
	want := []string{"b.txt", "d.txt", "c.txt", "a.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("picker order %v, want %v", got, want)
	}
	if !entries[len(entries)-1].current {
		t.Fatal("the active tab must be the marked, last row")
	}
	if entries[0].index != 1 || entries[0].pane != m.activeEditorKey() {
		t.Fatalf("first row must address tab 1 of the focused pane, got %+v", entries[0])
	}
	if !strings.Contains(entries[0].detail, "b.txt") {
		t.Fatalf("row detail must carry the path, got %q", entries[0].detail)
	}
}

// TestTabPickerModeSpeedSearchFilters: typing narrows the rows by fuzzy match
// over the tab label while the MRU order of the survivors is kept.
func TestTabPickerModeSpeedSearchFilters(t *testing.T) {
	mode := newTabPickerMode()
	mode.entries = []tabPickerEntry{
		{label: "server.go", detail: "cmd/server.go", pane: "editor1", index: 2},
		{label: "client.go", detail: "cmd/client.go", pane: "editor1", index: 0},
		{label: "serve_test.go", detail: "cmd/serve_test.go", pane: "editor1", index: 1, current: true},
	}
	if items := mode.Results("", palette.Context{}); len(items) != 3 || items[0].Title != "server.go" {
		t.Fatalf("an empty query lists every tab in entry order, got %+v", items)
	}
	items := mode.Results("serv", palette.Context{})
	if len(items) != 2 {
		t.Fatalf("speed search must filter, got %d rows", len(items))
	}
	if items[0].Title != "server.go" || items[1].Title != "serve_test.go" {
		t.Fatalf("filtering must keep the MRU order, got %q,%q", items[0].Title, items[1].Title)
	}
	if items[1].Badge == "" {
		t.Fatal("the current tab's row must carry a badge")
	}
	picked, ok := items[0].Msg.(TabPickedMsg)
	if !ok || picked.Pane != "editor1" || picked.Index != 2 {
		t.Fatalf("activation msg = %+v", items[0].Msg)
	}
	if len(mode.Results("zzz", palette.Context{})) != 0 {
		t.Fatal("a non-matching query must list nothing")
	}
}

// TestTabPickerActivatesPickedTab: the picked row's tab becomes the pane's
// active one, and a stale index (the tab closed while the palette was open)
// is ignored instead of activating a neighbour.
func TestTabPickerActivatesPickedTab(t *testing.T) {
	dir := t.TempDir()
	withTabLimit(t, 0)
	a := writeTemp(t, dir, "a.txt", "a\n")
	b := writeTemp(t, dir, "b.txt", "b\n")
	c := writeTemp(t, dir, "c.txt", "c\n")
	m := openApp(t, a, b, c)
	key := m.activeEditorKey()

	tm, _ := m.Update(TabPickedMsg{Pane: key, Index: 0})
	m = tm.(Model)
	inst := m.activeWS().Panes.Get(key)
	if inst.ActiveTab() != 0 || inst.Editor().Path() != a {
		t.Fatalf("picking row 0 must activate tab 0, got %d (%q)", inst.ActiveTab(), inst.Editor().Path())
	}
	if m.activeWS().Panes.Focused() != key {
		t.Fatal("activating a picked tab must focus its pane")
	}
	tm, _ = m.Update(TabPickedMsg{Pane: key, Index: 42})
	m = tm.(Model)
	if inst := m.activeWS().Panes.Get(key); inst.ActiveTab() != 0 {
		t.Fatalf("an out-of-range pick must be a no-op, active = %d", inst.ActiveTab())
	}
}

// TestTabPickerOpensLockedPalette: editor.tab.picker opens the palette locked
// to the tab mode with the pane's tabs loaded; a single-tab pane has nothing
// to switch to and does not open it.
func TestTabPickerOpensLockedPalette(t *testing.T) {
	dir := t.TempDir()
	withTabLimit(t, 0)
	a := writeTemp(t, dir, "a.txt", "a\n")
	m := openApp(t, a)
	tm, _ := m.Update(TabPickerMsg{})
	m = tm.(Model)
	if m.palette.IsOpen() {
		t.Fatal("a single-tab pane must not open the picker")
	}
	b := writeTemp(t, dir, "b.txt", "b\n")
	tm, _ = m.openPath(b, false)
	m = tm.(Model)
	tm, _ = m.Update(TabPickerMsg{})
	m = tm.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("editor.tab.picker must open the palette")
	}
	if len(m.tabPicker.entries) != 2 {
		t.Fatalf("the mode must carry both tabs, got %d", len(m.tabPicker.entries))
	}
}
