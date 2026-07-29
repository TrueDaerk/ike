package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/breakpanel"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/registry"
)

func breakpointsApp(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return out.(Model)
}

func TestBreakpointsToggleLifecycle(t *testing.T) {
	m := breakpointsApp(t)
	before := m.activeWS().Panes.Focused()

	out, _ := m.Update(BreakpointsToggleMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.BreakpointsKey) || m.activeWS().Panes.Focused() != pane.BreakpointsKey {
		t.Fatalf("first toggle must open + focus the panel (focus=%q)", m.activeWS().Panes.Focused())
	}

	out, _ = m.Update(BreakpointsToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != before {
		t.Fatalf("focus = %q, want %q", m.activeWS().Panes.Focused(), before)
	}

	out, _ = m.Update(BreakpointsToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != pane.BreakpointsKey {
		t.Fatal("third toggle must re-focus the panel")
	}
}

// TestBreakpointsPanelSyncsWithGutter guards the live-sync acceptance of
// #1377: a ctrl+f8 toggle in the editor lands in the open list, and a
// list-side removal clears the store the gutter renders from.
func TestBreakpointsPanelSyncsWithGutter(t *testing.T) {
	m := breakpointsApp(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "a.go")
	if err := os.WriteFile(target, []byte("package a\n\nvar x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(BreakpointsToggleMsg{})
	m = out.(Model)
	p := m.breakpointsPanel()
	if p == nil || p.Rows() != 0 {
		t.Fatalf("panel must open empty, got %v", p)
	}

	// Open the file, toggle a breakpoint on line 2 via the command path.
	out, _ = m.openPathAt(target, 2, 0)
	m = out.(Model)
	out, _ = m.Update(DebugToggleBreakpointMsg{})
	m = out.(Model)
	if p := m.breakpointsPanel(); p.Rows() != 2 {
		t.Fatalf("gutter toggle must reach the list: rows = %d, want 2 (header+bp)", p.Rows())
	}

	// A list-side removal empties the store.
	key := bpKey(target)
	out, cmd := m.Update(breakpanel.RemoveMsg{Path: key, Line: 2})
	m = out.(Model)
	_ = cmd
	if m.bpts.Count() != 0 || m.breakpointsPanel().Rows() != 0 {
		t.Fatalf("list removal must clear store (%d) and rows (%d)", m.bpts.Count(), m.breakpointsPanel().Rows())
	}
}

// TestBreakpointsPanelActions covers enable/disable, delete-all and the jump
// routed through the standard open funnel.
func TestBreakpointsPanelActions(t *testing.T) {
	m := breakpointsApp(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "a.go")
	if err := os.WriteFile(target, []byte("package a\n\nvar x = 1\nvar y = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	key := bpKey(target)
	m.bpts.Toggle(key, 2)
	m.bpts.Toggle(key, 3)

	// Disable one from the list; the store and the persisted file follow.
	out, _ := m.Update(breakpanel.ToggleEnabledMsg{Path: key, Line: 2})
	m = out.(Model)
	if m.bpts.Enabled(key, 2) || !m.bpts.Enabled(key, 3) {
		t.Fatal("toggle-enabled must disable exactly the addressed breakpoint")
	}
	// Re-enable.
	out, _ = m.Update(breakpanel.ToggleEnabledMsg{Path: key, Line: 2})
	m = out.(Model)
	if !m.bpts.Enabled(key, 2) {
		t.Fatal("second toggle must re-enable")
	}

	// Jump routes through the open funnel: file opens, cursor on the line.
	out, cmd := m.Update(breakpanel.OpenLocationMsg{Path: key, Line: 3})
	m = out.(Model)
	_ = cmd
	ed := m.activeEditor()
	if ed == nil || ed.Path() != target {
		t.Fatalf("jump must open %s", target)
	}
	if line, _ := ed.CursorPos(); line != 3 {
		t.Fatalf("cursor line = %d, want 3", line)
	}

	// Delete all empties the store.
	out, _ = m.Update(breakpanel.RemoveAllMsg{})
	m = out.(Model)
	if m.bpts.Count() != 0 {
		t.Fatalf("remove-all left %d breakpoints", m.bpts.Count())
	}
}

func TestBreakpointsPanePersists(t *testing.T) {
	m := breakpointsApp(t)
	out, _ := m.Update(BreakpointsToggleMsg{})
	m = out.(Model)

	tree, ids, ok := loadLayout()
	if !ok || tree == nil {
		t.Fatal("layout must have been saved")
	}
	if ids[pane.BreakpointsKey].Kind != "breakpoints" {
		t.Fatalf("identity = %+v", ids[pane.BreakpointsKey])
	}

	// A fresh model over the same config dir restores the leaf seeded from
	// the persisted store.
	m.bpts.Toggle("a.py", 1)
	_ = m.bpts.Save()
	m2 := NewWith(registry.New(), host.MapConfig{})
	out2, _ := m2.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 = out2.(Model)
	if !m2.activeWS().Panes.Has(pane.BreakpointsKey) {
		t.Fatal("restored layout must keep the breakpoints pane leaf")
	}
	p := m2.breakpointsPanel()
	if p == nil || p.Rows() != 2 {
		t.Fatalf("restored panel must seed from the persisted store, rows = %d", p.Rows())
	}
	if !strings.Contains(stripped(m2), "BREAKPOINTS") {
		t.Fatal("pane chrome must title the panel")
	}
}
