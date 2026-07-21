package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// cmdClickModel is a sized model with a stub lsp.definition command and one
// open file, plus the editor pane's rect.
func cmdClickModel(t *testing.T) (Model, layout.Rect, *bool) {
	t.Helper()
	ran := false
	reg := registry.New()
	reg.Add(fakePlugin{id: "lsp", caps: plugin.Capabilities{Commands: []plugin.Command{{
		ID: "lsp.definition", Title: "Go to Definition",
		Run: func(h host.API) tea.Cmd { ran = true; return nil },
	}}}})
	m := sizedWith(t, reg, 100, 40)
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc target() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.openPath(path, false)
	m = out.(Model)
	var r layout.Rect
	found := false
	for key, rect := range m.lay.Panes {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
			r, found = rect, true
		}
	}
	if !found {
		t.Fatal("setup: no editor pane rect")
	}
	return m, r, &ran
}

// TestCmdClickRunsGotoDefinition guards #859: cmd+click on editor content
// moves the cursor to the clicked cell and dispatches lsp.definition — the
// same action F4 runs.
func TestCmdClickRunsGotoDefinition(t *testing.T) {
	for _, mod := range []tea.KeyMod{tea.ModSuper, tea.ModMeta} {
		m, r, ran := cmdClickModel(t)
		x := r.X + paneContentX + 10 // inside the first content line, past the gutter
		y := r.Y + paneContentY
		m = step(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft, Mod: mod})
		if !*ran {
			t.Fatalf("mod %v: cmd+click must dispatch lsp.definition", mod)
		}
	}
}

// TestPlainClickDoesNotGotoDefinition: an unmodified click only moves the
// cursor.
func TestPlainClickDoesNotGotoDefinition(t *testing.T) {
	m, r, ran := cmdClickModel(t)
	m = step(m, tea.MouseClickMsg{X: r.X + paneContentX + 10, Y: r.Y + paneContentY, Button: tea.MouseLeft})
	if *ran {
		t.Fatal("plain click must not dispatch lsp.definition")
	}
	_ = m
}

