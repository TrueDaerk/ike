package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/host"
	"ike/internal/layout"
	ilsp "ike/internal/lsp"
	"ike/internal/pane"
	"ike/internal/problems"
	"ike/internal/registry"
)

func problemsApp(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return out.(Model)
}

func testDiag(line int, sev int, msg string) ilsp.Diagnostic {
	return ilsp.Diagnostic{Range: buffer.Range{Start: buffer.Position{Line: line}}, Severity: sev, Message: msg}
}

func TestProblemsToggleLifecycle(t *testing.T) {
	m := problemsApp(t)
	before := m.activeWS().Panes.Focused()

	out, _ := m.Update(ProblemsToggleMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.ProblemsKey) || m.activeWS().Panes.Focused() != pane.ProblemsKey {
		t.Fatalf("first toggle must open + focus the panel (focus=%q)", m.activeWS().Panes.Focused())
	}

	// Second toggle returns focus whence it came.
	out, _ = m.Update(ProblemsToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != before {
		t.Fatalf("focus = %q, want %q", m.activeWS().Panes.Focused(), before)
	}

	// Third re-focuses the existing pane without duplicating it.
	out, _ = m.Update(ProblemsToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != pane.ProblemsKey {
		t.Fatal("third toggle must re-focus the panel")
	}
}

func TestProblemsStoreAggregatesUnopenedFiles(t *testing.T) {
	m := problemsApp(t)

	// A publish for a file no editor has open still lands in the store.
	out, _ := m.Update(ilsp.DiagnosticsMsg{Path: "/never/opened.go", Diagnostics: []ilsp.Diagnostic{testDiag(3, 1, "boom")}})
	m = out.(Model)
	if m.probStore.Len() != 1 {
		t.Fatalf("store len = %d, want 1", m.probStore.Len())
	}

	// A batch adds more files and an empty set clears one.
	out, _ = m.Update(ilsp.DiagnosticsBatchMsg{Items: []ilsp.DiagnosticsMsg{
		{Path: "/other.go", Diagnostics: []ilsp.Diagnostic{testDiag(1, 2, "warn")}},
		{Path: "/never/opened.go", Diagnostics: nil},
	}})
	m = out.(Model)
	if m.probStore.Len() != 1 || m.probStore.Get("/other.go") == nil {
		t.Fatalf("batch: store = %v", m.probStore.Paths())
	}
}

func TestProblemsPanelLiveUpdatesAndNavigates(t *testing.T) {
	m := problemsApp(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "a.go")
	if err := os.WriteFile(target, []byte("package a\n\nvar x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := m.Update(ProblemsToggleMsg{})
	m = out.(Model)
	out, _ = m.Update(ilsp.DiagnosticsMsg{Path: target, Diagnostics: []ilsp.Diagnostic{testDiag(2, 1, "unused: x")}})
	m = out.(Model)

	p := m.problemsPanel()
	if p == nil || p.Rows() != 2 {
		t.Fatalf("panel must show header+diag rows, got %v", p)
	}
	if !strings.Contains(stripped(m), "unused: x") {
		t.Fatal("live update must reach the rendered panel")
	}

	// Enter on the diagnostic opens the file at its location.
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = out.(Model)
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

func TestProblemsPanePersists(t *testing.T) {
	m := problemsApp(t)
	out, _ := m.Update(ProblemsToggleMsg{})
	m = out.(Model)

	// The toggle saved the layout; the pane's identity round-trips as
	// "problems" so a restart restores the slot (empty, re-fed live).
	tree, ids, ok := loadLayout()
	if !ok || tree == nil {
		t.Fatal("layout must have been saved")
	}
	if ids[pane.ProblemsKey].Kind != "problems" {
		t.Fatalf("identity = %+v", ids[pane.ProblemsKey])
	}
}

// Compile-time guard: the panel message the app routes stays in sync.
var _ = problems.OpenLocationMsg{}

// TestRestoreLayoutAcceptsProblemsLeaf guards #1157: a saved layout holding
// the Problems pane restores it (empty) instead of silently falling back to
// the default layout — the restoreLayout pre-filter was missing "problems".
func TestRestoreLayoutAcceptsProblemsLeaf(t *testing.T) {
	m := problemsApp(t)
	out, _ := m.Update(ProblemsToggleMsg{})
	m = out.(Model)
	if _, _, ok := loadLayout(); !ok {
		t.Fatal("layout must have been saved")
	}
	// A fresh model over the SAME config dir restores the saved tree — the
	// problems leaf must survive the pre-filter (not fall back to default).
	// Built like problemsApp but without re-pointing IKE_CONFIG_DIR.
	m2 := NewWith(registry.New(), host.MapConfig{})
	out2, _ := m2.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 = out2.(Model)
	found := false
	for _, key := range layout.Leaves(m2.activeWS().Tree) {
		if key == pane.ProblemsKey {
			found = true
		}
	}
	if !found {
		t.Fatal("restored layout must keep the problems pane leaf (#1157)")
	}
}
