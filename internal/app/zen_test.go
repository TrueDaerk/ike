package app

import (
	"strings"
	"testing"

	"ike/internal/layout"
	"ike/internal/pane"
)

// zen_test.go covers view.zenMode (#359): maximize + chrome-free rendering.

func TestZenHidesChromeAndRestores(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	m := openApp(t, a, b) // two tabs → tab bar visible normally
	normal := m.render()
	if !strings.Contains(normal, "NORMAL") {
		t.Fatal("setup: status line expected in normal render")
	}

	m = dispatch(t, m, ZenModeMsg{})
	if !m.zen || m.zoomed == "" {
		t.Fatal("zen must set the zoom")
	}
	// Only the editor is laid out, over the full body (no status row).
	if len(m.lay.Panes) != 1 {
		t.Fatalf("zen must zoom the editor, got %v", m.lay.Panes)
	}
	if r := m.lay.Panes[m.zoomed]; r.H != m.height-m.menuHeight() {
		t.Fatalf("zen body must reclaim the status row, H=%d", r.H)
	}
	zen := m.render()
	if strings.Contains(zen, "NORMAL") {
		t.Fatal("zen render must hide the status line")
	}
	if bar, ok := m.tabBar(m.activeWS().Panes.Get(m.zoomed), 80); ok || bar != "" {
		t.Fatal("zen must hide the tab bar")
	}

	// Toggling back restores chrome and the full layout.
	m = dispatch(t, m, ZenModeMsg{})
	if m.zen || m.zoomed != "" {
		t.Fatal("leaving zen must clear zoom (no prior manual zoom)")
	}
	if !strings.Contains(m.render(), "NORMAL") {
		t.Fatal("status line must return after zen")
	}
}

func TestZenKeepsPriorManualZoom(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	m := openApp(t, a)
	m = dispatch(t, m, MaximizePaneMsg{}) // manual zoom first
	key := m.zoomed
	m = dispatch(t, m, ZenModeMsg{})
	m = dispatch(t, m, ZenModeMsg{})
	if m.zen {
		t.Fatal("zen must be off")
	}
	if m.zoomed != key {
		t.Fatalf("prior manual zoom must survive zen, zoomed=%q", m.zoomed)
	}
}

func TestZenDropsOnTreeMutation(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	m := openApp(t, a)
	m = dispatch(t, m, ZenModeMsg{})
	m = dispatch(t, m, SplitFocusedMsg{Zone: layout.ZoneRight})
	if m.zen || m.zoomed != "" {
		t.Fatal("a tree mutation must drop zen and zoom")
	}
	if !strings.Contains(m.render(), "NORMAL") {
		t.Fatal("chrome must return when zen drops")
	}
}

func TestZenCommandRegistered(t *testing.T) {
	m := newSized()
	if _, ok := m.reg.Command("view.zenMode"); !ok {
		t.Fatal("view.zenMode must be registered")
	}
}

// Zen works for any focused pane kind (#934), not only editors.

func TestZenOnTerminalPane(t *testing.T) {
	m, key := openTestTerminal(t)
	m = dispatch(t, m, ZenModeMsg{})
	if !m.zen || m.zoomed != key {
		t.Fatalf("zen must zoom the focused terminal, zen=%v zoomed=%q", m.zen, m.zoomed)
	}
	if len(m.lay.Panes) != 1 {
		t.Fatalf("zen must lay out only the terminal, got %v", m.lay.Panes)
	}
	// The pane's own header also says TERMINAL, so check the status row —
	// the render's last line — specifically.
	if lastLine(m.render()) != "" && strings.Contains(lastLine(m.render()), "TERMINAL") {
		t.Fatal("zen render must hide the status line")
	}
	m = dispatch(t, m, ZenModeMsg{})
	if m.zen || m.zoomed != "" {
		t.Fatal("leaving zen must restore the layout")
	}
	if !strings.Contains(lastLine(m.render()), "TERMINAL") {
		t.Fatal("status line must return after zen")
	}
}

// lastLine returns the final line of a render, where the status line sits.
func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	return lines[len(lines)-1]
}

func TestZenOnToolPane(t *testing.T) {
	m := newSized()
	m.setFocus(pane.ExplorerKey)
	m.layout()
	m = dispatch(t, m, ZenModeMsg{})
	if !m.zen || m.zoomed != pane.ExplorerKey {
		t.Fatalf("zen must zoom the focused tool pane, zen=%v zoomed=%q", m.zen, m.zoomed)
	}
	if len(m.lay.Panes) != 1 {
		t.Fatalf("zen must lay out only the tool pane, got %v", m.lay.Panes)
	}
	m = dispatch(t, m, ZenModeMsg{})
	if m.zen || m.zoomed != "" {
		t.Fatal("leaving zen must restore the layout")
	}
}

func TestZenOnTerminalDropsOnTreeMutation(t *testing.T) {
	m, _ := openTestTerminal(t)
	m = dispatch(t, m, ZenModeMsg{})
	m = dispatch(t, m, SplitFocusedMsg{Zone: layout.ZoneRight})
	if m.zen || m.zoomed != "" {
		t.Fatal("a tree mutation must drop zen and zoom")
	}
	if !strings.Contains(m.render(), "NORMAL") {
		t.Fatal("chrome must return when zen drops")
	}
}
