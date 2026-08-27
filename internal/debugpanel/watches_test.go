package debugpanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/dap"
)

func esc() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEscape} }

// typeText feeds a string into the open inline editor rune by rune.
func typeText(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// TestWatchesRenderAheadOfScopes verifies the section leads the variables
// tree with expression, value and error rows.
func TestWatchesRenderAheadOfScopes(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 12)
	m.SetFrames(frames())
	m.SetScopes([]dap.Scope{{Name: "Locals", VariablesReference: 100}})
	m.SetWatches([]WatchResult{
		{Expr: "x+1", Value: "42", Type: "int"},
		{Expr: "boom()", Err: "not defined"},
	})
	v := m.View()
	if !strings.Contains(v, "Watches") {
		t.Fatalf("view must show the watches section:\n%s", v)
	}
	if !strings.Contains(v, "x+1 = 42") {
		t.Fatalf("view must show the evaluated watch:\n%s", v)
	}
	if !strings.Contains(v, "boom() = ⚠ not defined") {
		t.Fatalf("view must show the failed watch:\n%s", v)
	}
	if strings.Index(v, "Watches") > strings.Index(v, "Locals") {
		t.Fatalf("watches must lead the tree:\n%s", v)
	}
}

// TestWatchAddFlow drives `a` + typing + enter into an AddWatchMsg.
func TestWatchAddFlow(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 12)
	m.SetFrames(frames())
	m.Update(key("tab")) // into the variables column
	m.Update(key("a"))
	if !m.Editing() {
		t.Fatal("a must open the watch editor")
	}
	typeText(&m, "len(s)")
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter must commit the watch")
	}
	msg, ok := cmd().(AddWatchMsg)
	if !ok || msg.Expr != "len(s)" {
		t.Fatalf("commit = %+v", msg)
	}
	if m.watchRoot != nil {
		t.Fatal("the placeholder row must leave with the commit; the app pushes the real list")
	}
}

// TestWatchAddCancelDropsPlaceholder verifies esc removes the pending row.
func TestWatchAddCancelDropsPlaceholder(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 12)
	m.SetFrames(frames())
	m.Update(key("tab"))
	m.Update(key("a"))
	typeText(&m, "x")
	m.Update(esc())
	if m.Editing() || m.watchRoot != nil {
		t.Fatal("esc must cancel the add and drop the placeholder")
	}
}

// TestWatchEditAndRemove drives `e` into EditWatchMsg and `d` into
// RemoveWatchMsg on a watch row, and checks emptied text removes.
func TestWatchEditAndRemove(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 12)
	m.SetFrames(frames())
	m.SetWatches([]WatchResult{{Expr: "x", Value: "1"}})
	m.Update(key("tab"))
	m.Update(key("j")) // from the section root onto the watch row
	m.Update(key("e"))
	if !m.Editing() {
		t.Fatal("e on a watch row must open the expression editor")
	}
	typeText(&m, "*2")
	msg := m.Update(key("enter"))()
	if got, ok := msg.(EditWatchMsg); !ok || got.Index != 0 || got.Expr != "x*2" {
		t.Fatalf("edit commit = %+v", msg)
	}

	m.SetWatches([]WatchResult{{Expr: "x*2", Value: "2"}})
	m.varSel = 1
	msg = m.Update(key("d"))()
	if got, ok := msg.(RemoveWatchMsg); !ok || got.Index != 0 {
		t.Fatalf("remove = %+v", msg)
	}

	// Emptying the expression in the editor removes the watch too.
	m.SetWatches([]WatchResult{{Expr: "y", Value: "3"}})
	m.varSel = 1
	m.Update(key("e"))
	m.editBuf = nil
	m.editCur = 0
	msg = m.Update(key("enter"))()
	if got, ok := msg.(RemoveWatchMsg); !ok || got.Index != 0 {
		t.Fatalf("empty-edit commit = %+v", msg)
	}
}

// TestWatchStructuredResultExpands verifies a watch result with a
// variablesReference expands through the ordinary ExpandVarMsg/SetChildren
// round trip.
func TestWatchStructuredResultExpands(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 12)
	m.SetFrames(frames())
	m.SetWatches([]WatchResult{{Expr: "obj", Value: "Point{…}", Ref: 900}})
	m.Update(key("tab"))
	m.Update(key("j"))
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter on an unloaded structured watch must request expansion")
	}
	if msg, ok := cmd().(ExpandVarMsg); !ok || msg.Ref != 900 {
		t.Fatalf("expand = %+v", cmd())
	}
	m.SetChildren(900, []dap.Variable{{Name: "x", Value: "1"}, {Name: "y", Value: "2"}})
	v := m.View()
	if !strings.Contains(v, "x = 1") || !strings.Contains(v, "y = 2") {
		t.Fatalf("children must render under the watch:\n%s", v)
	}
}

// TestWatchesSurviveScopeRefresh verifies SetScopes (a new stop's tree) does
// not clobber the watches section.
func TestWatchesSurviveScopeRefresh(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 12)
	m.SetFrames(frames())
	m.SetWatches([]WatchResult{{Expr: "x", Value: "1"}})
	m.SetScopes([]dap.Scope{{Name: "Locals", VariablesReference: 100}})
	if !strings.Contains(m.View(), "x = 1") {
		t.Fatal("SetScopes must not drop the watches section")
	}
}

// TestWatchesEvaluateUnsupportedNotice: an adapter without evaluate keeps the
// expressions listed — the list is per project, not per adapter — but the
// section header says why no values follow (#2174).
func TestWatchesEvaluateUnsupportedNotice(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 12)
	m.SetFrames(frames())
	if !m.EvaluateSupported() {
		t.Fatal("a fresh panel must assume evaluate works")
	}
	m.SetEvaluateSupported(false)
	m.SetWatches([]WatchResult{{Expr: "x+1"}, {Expr: "obj"}})
	v := m.View()
	if !strings.Contains(v, watchesUnsupportedNote) {
		t.Fatalf("view must carry the capability notice:\n%s", v)
	}
	if !strings.Contains(v, "x+1") || !strings.Contains(v, "obj") {
		t.Fatalf("expressions must stay listed:\n%s", v)
	}
	if m.EvaluateSupported() {
		t.Fatal("EvaluateSupported must mirror the setter")
	}
	// Adding a watch still works — the list outlives this adapter.
	m.Update(key("tab"))
	m.Update(key("a"))
	if !m.Editing() {
		t.Fatal("adding a watch must stay possible without evaluate")
	}
	m.Update(esc())

	// A capable adapter drops the notice again.
	m.SetEvaluateSupported(true)
	m.SetWatches([]WatchResult{{Expr: "x+1", Value: "42"}})
	if v := m.View(); strings.Contains(v, watchesUnsupportedNote) {
		t.Fatalf("notice must clear once evaluate works:\n%s", v)
	}
}
