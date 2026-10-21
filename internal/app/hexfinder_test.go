package app

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/palette"
	"ike/internal/pane"
)

// hexfinder_test.go covers the "@" file finder from the hex view (#3026):
// in NORMAL navigation it floats over the viewer and the pick lands as a tab
// in the viewer's pane; in EDIT mode (text insertion) "@" is a byte, and in
// the open search line it is query text.

func TestAtInHexViewNormalModeOpensFinderIntoItsPane(t *testing.T) {
	m, binPath := hexEditOpen(t, false)
	key := m.activeWS().Panes.Focused()
	m = pfAt(m)
	if !m.palette.IsOpen() || !m.palette.Anchored() {
		t.Fatalf("@ in the hex view: open=%v anchored=%v, want the anchored finder", m.palette.IsOpen(), m.palette.Anchored())
	}
	x, y, _, _ := m.paneAnchor()
	if r := m.lay.Panes[key]; x != r.X+1 || y != r.Y+1 {
		t.Fatalf("finder anchored at %d,%d, want the hex pane's interior %d,%d", x, y, r.X+1, r.Y+1)
	}
	if m.panelFinderHost != key {
		t.Fatalf("panelFinderHost = %q, want %q", m.panelFinderHost, key)
	}

	path := filepath.Join(t.TempDir(), "pick.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(palette.OpenFileMsg{Path: path})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != key {
		t.Fatalf("focus moved to %q, want the hex view's pane %q", m.activeWS().Panes.Focused(), key)
	}
	host := m.activeWS().Panes.FocusedInstance()
	if host == nil || host.Kind() != pane.KindEditor || host.TabCount() != 2 {
		t.Fatalf("the hex pane should be a tab host with two tabs, got %v", host)
	}
	if c := host.TabContent(0); c == nil || c.Kind() != pane.KindHex || c.Hex().Path() != binPath {
		t.Fatal("the first tab should keep the hex view")
	}
	if ed := host.Editor(); ed == nil || ed.Path() != canonicalPath(path) {
		t.Fatal("the active tab should be the picked file")
	}
}

func TestAtInHexViewEditModeWritesByte(t *testing.T) {
	m, _ := hexEditOpen(t, false)
	m = hexEditType(m, "i")
	hv := m.focusedContent().Hex()
	if !hv.Capturing() {
		t.Fatal("i should enter text insertion")
	}
	m = pfAt(m)
	if m.palette.IsOpen() {
		t.Fatal("@ in EDIT mode must not open the finder")
	}
	hv = m.focusedContent().Hex()
	if hv.Modified() != 1 {
		t.Fatalf("modified bytes = %d, want the typed @", hv.Modified())
	}
	if _, err := hv.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(hv.Path())
	if err != nil {
		t.Fatal(err)
	}
	if data[0] != '@' {
		t.Fatalf("first byte = %q, want '@'", data[0])
	}
}

func TestAtInHexViewSearchLineIsQueryText(t *testing.T) {
	m, _ := hexEditOpen(t, true)
	m = hexEditType(m, "/")
	if !m.focusedContent().Hex().Searching() {
		t.Fatal("/ should open the search line")
	}
	m = pfAt(m)
	if m.palette.IsOpen() {
		t.Fatal("@ in the search line must not open the finder")
	}
	if got := m.focusedContent().Hex().SearchQuery(); got != "@" {
		t.Fatalf("search query = %q, want \"@\"", got)
	}
}
