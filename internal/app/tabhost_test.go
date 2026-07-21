package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/registry"
)

// tabhost_test.go covers #836: terminal and tool panes as center-merge
// targets — a drop in their interior converts them into tab hosts, the live
// session becoming the first tab.

// closeAllSessions ends every terminal session the model still runs.
func closeAllSessions(m Model) {
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil {
			continue
		}
		switch inst.Kind() {
		case pane.KindTerminal:
			inst.Terminal().Close()
		case pane.KindEditor:
			inst.CloseTerminalTabs()
		}
	}
}

// TestToolPaneDropOnTerminalCenterStacksSessions (#836): dragging a whole
// tool pane onto a terminal pane's center converts the terminal into a tab
// host — the shell stays as the first tab, the tool session joins beside it,
// the vacated tool pane closes, and the chrome shows the tool glyph.
func TestToolPaneDropOnTerminalCenterStacksSessions(t *testing.T) {
	withTools(t, sleepTool("watcher"))
	m := sized(t, 100, 40)
	t.Cleanup(func() { closeAllSessions(m) })

	out, _ := m.Update(TerminalNewMsg{})
	m = out.(Model)
	termKey := m.activeWS().Panes.Focused()
	out, _ = m.Update(ToolOpenMsg{Name: "watcher"})
	m = out.(Model)
	toolKey := m.activeWS().Panes.Focused()
	if toolKey == termKey {
		t.Fatal("setup: tool must open its own pane")
	}
	panesBefore := len(m.lay.Panes)

	// Drag the tool pane's title bar onto the terminal pane's center (the
	// top border is the resize band, #761 — grab the title text row).
	tr := m.lay.Panes[toolKey]
	m = step(m, press(tr.X+2, tr.Y+1))
	dst := m.lay.Panes[termKey]
	m = step(m, release(dst.X+dst.W/2, dst.Y+dst.H/2))

	if m.activeWS().Panes.Has(toolKey) {
		t.Fatal("the vacated tool pane must close after the merge")
	}
	if len(m.lay.Panes) != panesBefore-1 {
		t.Fatalf("panes = %d, want %d", len(m.lay.Panes), panesBefore-1)
	}
	host := m.activeWS().Panes.Get(termKey)
	if host.Kind() != pane.KindEditor || host.TabCount() != 2 {
		t.Fatalf("terminal must convert to a tab host with 2 tabs, kind=%v tabs=%d", host.Kind(), host.TabCount())
	}
	shell, tool := host.TabTerminal(0), host.TabTerminal(1)
	if shell == nil || !shell.Running() || shell.Tool() != "" {
		t.Fatal("the shell session must survive as the first tab")
	}
	if tool == nil || !tool.Running() || tool.Tool() != "watcher" {
		t.Fatal("the tool session must join as the second tab")
	}
	// The single-instance toggle (#835) still finds the tab-hosted tool.
	if n := len(m.toolLocations("watcher")); n != 1 {
		t.Fatalf("tool instances = %d, want 1", n)
	}
	// The tab bar labels the tool tab with its glyph; the pane title keeps
	// the tool chrome while its tab is active.
	if v := m.render(); !strings.Contains(v, "⚙ watcher") {
		t.Fatal("the tab bar must label the tool tab with ⚙")
	}
}

// TestFileTabDropOnToolCenterMerges (#836): a dragged file tab lands in a
// tool pane's center — the pane converts, the file opens beside the running
// tool session.
func TestFileTabDropOnToolCenterMerges(t *testing.T) {
	// withTools after tabApp: New() inside reloads the config and would wipe
	// an earlier injected entry.
	m, paths := tabApp(t)
	withTools(t, sleepTool("watcher"))
	t.Cleanup(func() { closeAllSessions(m) })
	src := m.activeWS().Panes.Focused()
	out, _ := m.Update(ToolOpenMsg{Name: "watcher"})
	m = out.(Model)
	toolKey := m.activeWS().Panes.Focused()
	m.setFocus(src)

	x, y := barCell(t, m, 1)
	m = step(m, press(x, y))
	tr := m.lay.Panes[toolKey]
	m = step(m, release(tr.X+tr.W/2, tr.Y+tr.H/2))

	host := m.activeWS().Panes.Get(toolKey)
	if host.Kind() != pane.KindEditor || host.TabCount() != 2 {
		t.Fatalf("tool pane must convert to a tab host with 2 tabs, kind=%v tabs=%d", host.Kind(), host.TabCount())
	}
	if tt := host.TabTerminal(0); tt == nil || tt.Tool() != "watcher" || !tt.Running() {
		t.Fatal("the tool session must survive as the first tab")
	}
	if ed := host.TabEditor(host.ActiveTab()); ed == nil || ed.Path() != paths[0] {
		t.Fatal("the dragged file must be the active tab")
	}
}

// TestExplorerStaysEdgeOnlyTarget (#836): panes without tab capability keep
// the plain relocate/edge zones — no center merge on the explorer.
func TestExplorerStaysEdgeOnlyTarget(t *testing.T) {
	m, _ := tabApp(t)
	x, y := barCell(t, m, 1)
	m = step(m, press(x, y))
	er := m.lay.Panes[pane.ExplorerKey]
	m = step(m, motion(er.X+er.W/2, er.Y+er.H/2))
	if _, _, _, ok := m.moveGhost(); ok {
		t.Fatal("the explorer's interior must stay a non-target for tab drags")
	}
	m = step(m, release(x, y))
}

// TestTabHostPersistsToolTabAndRestores (#836): a converted pane saves an
// editor identity carrying its tool tabs; a fresh model under the same store
// restores the slot with the tool restarted (and no placeholder editor tab).
func TestTabHostPersistsToolTabAndRestores(t *testing.T) {
	withTools(t, sleepTool("watcher"))
	store := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", store)
	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)
	t.Cleanup(func() { closeAllSessions(m) })

	out, _ = m.Update(ToolOpenMsg{Name: "watcher"})
	m = out.(Model)
	toolKey := m.activeWS().Panes.Focused()
	if !m.ensureTabHost(toolKey) {
		t.Fatal("setup: tool pane must convert to a tab host")
	}
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	closeAllSessions(m)

	m2 := NewWith(registry.New(), host.MapConfig{})
	t.Cleanup(func() { closeAllSessions(m2) })
	inst := m2.activeWS().Panes.Get(toolKey)
	if inst == nil || inst.Kind() != pane.KindEditor {
		t.Fatalf("converted pane must restore as a tab host under %q", toolKey)
	}
	if inst.TabCount() != 1 {
		t.Fatalf("restored tabs = %d, want just the tool (no placeholder editor tab)", inst.TabCount())
	}
	tt := inst.TabTerminal(0)
	if tt == nil || tt.Tool() != "watcher" || !tt.Running() {
		t.Fatal("the tool tab must restore restarted")
	}
}
