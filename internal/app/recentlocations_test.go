package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
	"ike/internal/nav"
)

// recentlocations_test.go covers Last Edit Location and Recent Locations
// (#2545): buffer edits record into the ring through the editor emitter,
// nav.lastEdit walks it back, the picker lists edits and jumps once each,
// and a foreign-project row parks the open and starts the switch.

// typeAt jumps to line and inserts text in insert mode, returning to normal.
func typeAt(m Model, line int, text string) Model {
	m = dismissOnboarding(m)                               // the first-start LSP dialog eats scripted keys on some machines
	m = drainKey(m, tea.KeyPressMsg{Code: 'G', Text: "G"}) // large motion: records a jump
	for i := 0; i < 9-line; i++ {
		m = drainKey(m, tea.KeyPressMsg{Code: 'k', Text: "k"})
	}
	m = drainKey(m, tea.KeyPressMsg{Code: 'i', Text: "i"})
	for _, r := range text {
		m = drainKey(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
}

func TestEditRingRecordsBufferChanges(t *testing.T) {
	root, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)
	m = typeAt(m, 2, "x")
	m = typeAt(m, 8, "y")

	rec := m.editRing.Recent()
	if len(rec) != 2 || rec[0].Path != files[0] || rec[0].Line != 8 || rec[1].Line != 2 {
		t.Fatalf("ring = %+v", rec)
	}
	if rec[0].Root != root && rec[0].Root != m.activeWS().Root {
		t.Fatalf("root = %q, want the workspace root", rec[0].Root)
	}
}

func TestNavLastEditWalksBack(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)
	m = typeAt(m, 2, "x")
	tm, _ = m.openPath(files[1], false)
	m = typeAt(tm.(Model), 7, "y")
	tm, _ = m.openPath(files[2], false)
	m = tm.(Model).atPosition(t, files[2], 0)

	tm, _ = m.Update(NavLastEditMsg{})
	m = tm.(Model).atPosition(t, files[1], 7)
	tm, _ = m.Update(NavLastEditMsg{})
	m = tm.(Model).atPosition(t, files[0], 2)
	tm, _ = m.Update(NavLastEditMsg{})
	m = tm.(Model).atPosition(t, files[0], 2)
	if len(m.toasts) != 1 || !strings.Contains(m.toasts[0].text, "no earlier edit location") {
		t.Fatalf("toasts = %+v", m.toasts)
	}
	// The jump recorded a departure like any other: back returns to c.
	tm, _ = m.Update(NavBackMsg{})
	tm.(Model).atPosition(t, files[1], 7)
}

func TestNavLastEditEmptyToasts(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	tm, _ = tm.(Model).Update(NavLastEditMsg{})
	m = tm.(Model)
	if len(m.toasts) != 1 || !strings.Contains(m.toasts[0].text, "no earlier edit location") {
		t.Fatalf("toasts = %+v", m.toasts)
	}
}

func TestRecentLocationsListsEditsThenJumps(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = typeAt(tm.(Model), 2, "x")
	tm, _ = m.Update(ilsp.DefinitionMsg{Path: files[1], Line: 5, Col: 0})
	m = tm.(Model)

	tm, _ = m.Update(ShowRecentLocationsMsg{})
	m = tm.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("picker must open")
	}
	items := m.recentLocs.items
	if len(items) < 2 {
		t.Fatalf("items = %+v", items)
	}
	if !strings.HasPrefix(items[0].Title, "✎") || !strings.HasSuffix(items[0].Title, "a.go:3") {
		t.Fatalf("first row = %q, want the edit at a.go:3", items[0].Title)
	}
	if items[0].Detail != "xl2" {
		t.Fatalf("preview = %q, want the buffer's edited line", items[0].Detail)
	}
	var jumpRows int
	for _, it := range items {
		if strings.HasPrefix(it.Title, "↷") {
			jumpRows++
		}
		if it.Preview.Path == "" {
			t.Fatalf("row %q has no code preview target", it.Title)
		}
	}
	if jumpRows == 0 {
		t.Fatalf("no jump rows among %+v", items)
	}
	// Activating a row jumps through the open funnel.
	tm, _ = m.Update(items[0].Msg)
	tm.(Model).atPosition(t, files[0], 2)
}

func TestRecentLocationsDedupesEditAndJump(t *testing.T) {
	var mode recentLocationsMode
	edits := []nav.Location{{Position: nav.Position{Path: "/p/a.go", Line: 4}, Root: "/p"}}
	jumps := []nav.Position{{Path: "/p/a.go", Line: 4, Col: 3}, {Path: "/p/b.go", Line: 1}}
	mode.Set(edits, jumps, func(string, int) string { return "" })
	if len(mode.items) != 2 {
		t.Fatalf("items = %+v, want the edit row and b.go only", mode.items)
	}
	if !strings.HasPrefix(mode.items[0].Title, "✎") || !strings.HasPrefix(mode.items[1].Title, "↷") {
		t.Fatalf("order = %q, %q", mode.items[0].Title, mode.items[1].Title)
	}
	if got := mode.items[0].Msg.(RecentLocationJumpMsg).Root; got != "/p" {
		t.Fatalf("root = %q", got)
	}
}

func TestRecentLocationsEmptyToasts(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(ShowRecentLocationsMsg{})
	m = tm.(Model)
	if m.palette.IsOpen() || len(m.toasts) != 1 || !strings.Contains(m.toasts[0].text, "no recent locations") {
		t.Fatalf("open=%v toasts=%+v", m.palette.IsOpen(), m.toasts)
	}
}

func TestRecentLocationForeignRootParksSwitch(t *testing.T) {
	other := t.TempDir()
	m := newSized()
	tm, cmd := m.Update(RecentLocationJumpMsg{Root: other, Path: other + "/x.go", Line: 3, Col: 1})
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("a foreign root must start the switch transaction")
	}
	po := m.allPendingOpen
	if po == nil || po.Root != other || po.Path != other+"/x.go" || po.Line != 4 || po.Col != 1 {
		t.Fatalf("pending open = %+v", po)
	}
}

func TestRecentLocationCommandsRegistered(t *testing.T) {
	m := newSized()
	for _, id := range []string{"nav.lastEdit", "nav.recentLocations"} {
		if _, ok := m.reg.Command(id); !ok {
			t.Errorf("command %s not registered", id)
		}
	}
}
