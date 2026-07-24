package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// TestSaveChainDoneCompletesDeferredWrite guards the app's side of format/
// organize-imports on save (#1148): a manual save parked behind the chain is
// completed — written to disk — when the bridge's SaveChainDoneMsg arrives.
func TestSaveChainDoneCompletesDeferredWrite(t *testing.T) {
	m := newSized()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	for _, k := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"},
		{Code: 'X', Text: "X"},
		{Code: tea.KeyEscape},
	} {
		m = drainKey(m, k)
	}

	// Register the fake provider after openPath: the app test binary links
	// plugins/lsp (keymap coverage), whose fileOpened hook installs the real
	// bridge provider and would overwrite an earlier registration.
	ilsp.SetSaveChain(func(path string, organize, format bool) tea.Cmd {
		return func() tea.Msg { return nil }
	})
	t.Cleanup(func() { ilsp.SetSaveChain(nil) })

	views := m.editorViewsForPath(path)
	if len(views) == 0 {
		t.Fatal("no editor view for the opened path")
	}
	ed := views[0]
	ed.Configure(host.MapConfig{"editor.format_on_save": "true"})
	nm, _ := ed.Update(editor.ActionMsg{Action: "write"})
	*ed = nm
	if !ed.SavePending() {
		t.Fatal("manual save must park behind the chain")
	}
	if data, _ := os.ReadFile(path); string(data) != "one\n" {
		t.Fatalf("file must stay untouched while the chain runs, got %q", data)
	}

	tm, _ = m.Update(ilsp.SaveChainDoneMsg{Path: path})
	m = tm.(Model)
	if ed.SavePending() {
		t.Fatal("SaveChainDoneMsg must complete the parked save")
	}
	if data, _ := os.ReadFile(path); !strings.HasPrefix(string(data), "Xone") {
		t.Fatalf("deferred write must land on chain completion, got %q", data)
	}
}
