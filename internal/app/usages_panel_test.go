package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/pane"
	"ike/internal/registry"
)

func usagesApp(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return out.(Model)
}

func TestUsagesToggleLifecycle(t *testing.T) {
	m := usagesApp(t)
	before := m.activeWS().Panes.Focused()

	out, _ := m.Update(UsagesToggleMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.UsagesKey) || m.activeWS().Panes.Focused() != pane.UsagesKey {
		t.Fatalf("first toggle must open + focus the panel (focus=%q)", m.activeWS().Panes.Focused())
	}

	// Second toggle returns focus whence it came.
	out, _ = m.Update(UsagesToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != before {
		t.Fatalf("focus = %q, want %q", m.activeWS().Panes.Focused(), before)
	}

	// Third re-focuses the existing pane without duplicating it.
	out, _ = m.Update(UsagesToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != pane.UsagesKey {
		t.Fatal("third toggle must re-focus the panel")
	}
}

func TestUsagesMsgOpensFillsAndNavigates(t *testing.T) {
	m := usagesApp(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "a.go")
	if err := os.WriteFile(target, []byte("package a\n\nvar x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A panel-targeted result opens the pane without a prior toggle.
	out, _ := m.Update(ilsp.UsagesMsg{
		Symbol: "x",
		Path:   target, Line: 2, Col: 4,
		Refs: []ilsp.Reference{{Path: target, Line: 2, Col: 4, Preview: "var x = 1"}},
	})
	m = out.(Model)
	p := m.usagesPanel()
	if p == nil {
		t.Fatal("a UsagesMsg must open the pane")
	}
	if m.activeWS().Panes.Focused() != pane.UsagesKey {
		t.Fatal("the filled pane must take focus")
	}
	if p.Symbol() != "x" || p.Count() != 1 || p.Rows() != 2 {
		t.Fatalf("panel fill: symbol=%q count=%d rows=%d", p.Symbol(), p.Count(), p.Rows())
	}
	if !strings.Contains(stripped(m), "Usages: x") {
		t.Fatal("the rendered pane must carry the symbol title")
	}

	// Enter on the reference opens the file at its location.
	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("enter must dispatch the open command")
	}
	out, _ = m.Update(cmd())
	m = out.(Model)
	ed := m.activeEditor()
	if ed == nil || ed.Path() != target {
		t.Fatalf("navigation must open %s", target)
	}
	if line, _ := ed.CursorPos(); line != 2 {
		t.Fatalf("cursor line = %d, want 2", line)
	}
}

func TestUsagesRefreshKeyRunsCarriedContinuation(t *testing.T) {
	m := usagesApp(t)
	type refreshed struct{}
	out, _ := m.Update(ilsp.UsagesMsg{
		Symbol:  "Foo",
		Refs:    []ilsp.Reference{{Path: "/a.go", Line: 0, Col: 0, Preview: "Foo"}},
		Refresh: func() tea.Msg { return refreshed{} },
	})
	m = out.(Model)
	// The pane holds focus; 'r' must return the continuation the result
	// carried (the bridge re-runs the request behind it).
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("r must dispatch the refresh command")
	}
	if _, ok := cmd().(refreshed); !ok {
		t.Fatal("r must run the carried refresh continuation")
	}
}

func TestUsagesPanePersistsAndRestoresEmpty(t *testing.T) {
	m := usagesApp(t)
	out, _ := m.Update(UsagesToggleMsg{})
	m = out.(Model)

	// The toggle saved the layout; the pane's identity round-trips as
	// "usages" so a restart restores the slot (empty, re-filled on the next
	// lsp.referencesPanel run).
	tree, ids, ok := loadLayout()
	if !ok || tree == nil {
		t.Fatal("layout must have been saved")
	}
	if ids[pane.UsagesKey].Kind != "usages" {
		t.Fatalf("identity = %+v", ids[pane.UsagesKey])
	}

	// A fresh model restores the pane in its slot, empty.
	m2 := NewWith(registry.New(), host.MapConfig{})
	out, _ = m2.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 = out.(Model)
	p := m2.usagesPanel()
	if p == nil {
		t.Fatal("restore must recreate the usages pane")
	}
	if p.Rows() != 0 || p.Symbol() != "" {
		t.Fatalf("restored pane must be empty: rows=%d symbol=%q", p.Rows(), p.Symbol())
	}
}

// TestEditorContextMenuOffersUsagesPanel guards the #1020 menu list: the
// panel variant sits alongside the quick palette entry.
func TestEditorContextMenuOffersUsagesPanel(t *testing.T) {
	var palette, panel bool
	for _, it := range editorContextItems(false) {
		switch it.Command {
		case "lsp.references":
			palette = true
		case "lsp.referencesPanel":
			if it.Title != "Find Usages (Panel)" {
				t.Fatalf("panel entry title = %q", it.Title)
			}
			panel = true
		}
	}
	if !palette || !panel {
		t.Fatalf("context menu must offer both usages entries (palette=%v panel=%v)", palette, panel)
	}
}
