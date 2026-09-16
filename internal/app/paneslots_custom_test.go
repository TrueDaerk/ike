package app

import (
	"testing"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/pane"
)

// paneslots_custom_test.go covers [[tools.custom]] names in layout.pane_slots
// (#2601): every custom tool pane is a terminal pane, so the reserved number
// must follow the tool *name* — otherwise the first custom pane in reading
// order would answer every custom chord — and a chord on a closed one has to
// open it through the tool's own tool.<name> route.

// customSlotApp is a sized app whose reserved-number table is slots and whose
// configured tools are entries, each running a process that outlives the test.
func customSlotApp(t *testing.T, slots string, names ...string) Model {
	t.Helper()
	entries := make([]config.ToolEntry, 0, len(names))
	for _, n := range names {
		entries = append(entries, sleepTool(n))
	}
	withTools(t, entries...)
	return numberedApp(t, host.MapConfig{
		"layout.pane_slots":             slots,
		"notifications.timeout_seconds": "1",
	})
}

// openCustomTool runs the tool.<name> route and returns the pane hosting it,
// closing the process when the test ends.
func openCustomTool(t *testing.T, m *Model, name string) string {
	t.Helper()
	m.openTool(name, false)
	m.layout()
	inst := m.toolPane(name)
	if inst == nil {
		t.Fatalf("tool %q did not open a pane", name)
	}
	t.Cleanup(func() { inst.Terminal().Close() })
	return inst.Key()
}

// TestReservedNumbersTellCustomToolsApart: two custom tools pinned to two
// numbers each answer their own chord. They share pane.KindTerminal, so a
// kind-only claim would give both numbers to whichever opened first.
func TestReservedNumbersTellCustomToolsApart(t *testing.T) {
	m := customSlotApp(t, "lazygit=6,k9s=7", "lazygit", "k9s")
	lazygit := openCustomTool(t, &m, "lazygit")
	k9s := openCustomTool(t, &m, "k9s")
	if got := m.paneNumberOf(lazygit); got != 6 {
		t.Errorf("lazygit numbered %d, want its reserved 6", got)
	}
	if got := m.paneNumberOf(k9s); got != 7 {
		t.Errorf("k9s numbered %d, want its reserved 7", got)
	}
	// The chrome carries the reserved number like a built-in window's does.
	if got := m.paneNumberBadgeText(lazygit); got != " 6 " {
		t.Errorf("lazygit badge = %q, want \" 6 \"", got)
	}
	// And each chord lands on its own tool, whatever reading order says.
	for _, tc := range []struct {
		n    int
		want string
	}{{6, lazygit}, {7, k9s}} {
		m.setFocus(m.activeEditorKey())
		tm, cmd := m.Update(PaneFocusIndexMsg{Index: tc.n})
		m = drainCmd(tm.(Model), cmd)
		if got := m.activeWS().Panes.Focused(); got != tc.want {
			t.Errorf("ctrl+%d focused %s, want %s", tc.n, got, tc.want)
		}
	}
}

// TestReservedChordOpensAClosedCustomTool: the number addresses the tool, not
// a pane that happens to exist — the chord spawns it through tool.<name> and
// focuses it, and pressing it again goes there rather than toggling it away.
func TestReservedChordOpensAClosedCustomTool(t *testing.T) {
	m := customSlotApp(t, "lazygit=6", "lazygit")
	if m.toolPane("lazygit") != nil {
		t.Fatal("precondition: the tool should start closed")
	}
	tm, cmd := m.Update(PaneFocusIndexMsg{Index: 6})
	m = drainCmd(tm.(Model), cmd)
	m.layout()
	inst := m.toolPane("lazygit")
	if inst == nil {
		t.Fatal("the reserved chord did not open the custom tool")
	}
	t.Cleanup(func() { inst.Terminal().Close() })
	if got := m.activeWS().Panes.Focused(); got != inst.Key() {
		t.Errorf("the reserved chord focused %s, want the tool pane", got)
	}
	if got := m.paneNumberOf(inst.Key()); got != 6 {
		t.Errorf("the opened tool is numbered %d, want its reserved 6", got)
	}
	m.setFocus(m.activeEditorKey())
	tm, cmd = m.Update(PaneFocusIndexMsg{Index: 6})
	m = drainCmd(tm.(Model), cmd)
	if got := m.activeWS().Panes.Focused(); got != inst.Key() {
		t.Errorf("the chord on the open tool focused %s, want the tool pane", got)
	}
}

// TestCustomToolHonoursItsHomePosition: the chord opens the tool the way its
// own command does, so the configured placement (#1889) still decides where
// the pane lands.
func TestCustomToolHonoursItsHomePosition(t *testing.T) {
	prev := config.Get()
	c := *prev
	entry := sleepTool("lazygit")
	entry.Placement = "right"
	c.Tools.Custom = []config.ToolEntry{entry}
	config.Set(&c)
	t.Cleanup(func() { config.Set(prev) })

	m := numberedApp(t, host.MapConfig{
		"layout.pane_slots":             "lazygit=6",
		"notifications.timeout_seconds": "1",
	})
	tm, cmd := m.Update(PaneFocusIndexMsg{Index: 6})
	m = drainCmd(tm.(Model), cmd)
	m.layout()
	inst := m.toolPane("lazygit")
	if inst == nil {
		t.Fatal("the reserved chord did not open the custom tool")
	}
	t.Cleanup(func() { inst.Terminal().Close() })
	r, ok := m.lay.Panes[inst.Key()]
	if !ok {
		t.Fatal("the opened tool has no layout rectangle")
	}
	for key, other := range m.lay.Panes {
		if key != inst.Key() && other.X > r.X {
			t.Fatalf("the tool pane is not at the right edge: %s sits further right", key)
		}
	}
}

// TestUnconfiguredCustomToolNumberNotifies: a table naming a tool no
// [[tools.custom]] entry defines reserves nothing — the config layer dropped
// the entry — so the chord is the ordinary notified no-op (#275).
func TestUnconfiguredCustomToolNumberNotifies(t *testing.T) {
	m := customSlotApp(t, "gone=6", "lazygit")
	if _, ok := m.paneSlotTable()[6]; ok {
		t.Fatal("a vanished tool must reserve no number")
	}
	focused := m.activeWS().Panes.Focused()
	tm, cmd := m.Update(PaneFocusIndexMsg{Index: 6})
	m = drainCmd(tm.(Model), cmd)
	if got := m.activeWS().Panes.Focused(); got != focused {
		t.Errorf("the chord moved focus to %s, want no move", got)
	}
	if !notifiedAbout(m, "focus pane 6") {
		t.Errorf("the chord must notify, history = %v", m.history)
	}
}

// TestBuiltinWinsAPaneSlotNameCollision: a custom tool named like a built-in
// window does not take the entry — the window does, and the tool keeps no
// number at all.
func TestBuiltinWinsAPaneSlotNameCollision(t *testing.T) {
	m := customSlotApp(t, "vcs=3", "vcs")
	def, ok := m.paneSlotTable()[3]
	if !ok {
		t.Fatal("the entry must still reserve 3")
	}
	if def.tool != "" || def.kind != pane.KindVCS {
		t.Fatalf("pane 3 resolves to %+v, want the built-in VCS window", def)
	}
}
