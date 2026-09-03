package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor"
	"ike/internal/explorer"
	"ike/internal/project"
)

// openedFile returns a sized app with one file open in an editor.
func openedFile(t *testing.T, content string) (Model, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	return tm.(Model), path
}

// TestPasteReachesPaletteQuery is the #1273 regression guard: a bracketed
// paste while the palette owns the keyboard lands in its query. It used to be
// dropped on the floor by handlePaste's overlay bail, so search-everywhere
// could not be pasted into at all.
func TestPasteReachesPaletteQuery(t *testing.T) {
	m, _ := openedFile(t, "abc\n")
	m.openPalette()

	tm, _ := m.Update(tea.PasteMsg{Content: "needle"})
	m = tm.(Model)

	if got := ansi.Strip(m.palette.View()); !strings.Contains(got, "needle") {
		t.Fatalf("paste did not reach the palette query: %q", got)
	}
}

// TestPasteReachesFinderQuery: the same for the find-in-path overlay.
func TestPasteReachesFinderQuery(t *testing.T) {
	m, _ := openedFile(t, "abc\n")
	m.finder.SetSize(m.width, m.height)
	m.finder.Open(t.TempDir())
	if !m.overlayCapturesKeyboard() {
		t.Fatal("finder should capture the keyboard")
	}

	tm, _ := m.Update(tea.PasteMsg{Content: "haystack"})
	m = tm.(Model)

	if got := m.finder.Query(); got != "haystack" {
		t.Fatalf("finder query = %q, want the pasted text", got)
	}
}

// TestPasteReachesRenamePrompt: the file-rename prompt is a floating-shell
// input, and takes a paste at its cursor.
func TestPasteReachesRenamePrompt(t *testing.T) {
	m, _ := openedFile(t, "abc\n")
	if cmd := m.startRenameFile(); cmd != nil {
		m = drainCmd(m, cmd)
	}
	if !m.renameOpen() {
		t.Fatal("rename prompt should be open")
	}
	before := m.renameInput.Text

	tm, _ := m.Update(tea.PasteMsg{Content: "-copy"})
	m = tm.(Model)

	if m.renameInput.Text != before+"-copy" {
		t.Fatalf("rename input = %q, want %q", m.renameInput.Text, before+"-copy")
	}
}

// TestPasteReachesSaveAsPrompt: the untitled-buffer save-as path (#730) is a
// floating-shell input too, and shares the #1273 gap the clone dialog had
// (#1873).
func TestPasteReachesSaveAsPrompt(t *testing.T) {
	m := newSized()
	m = dirtyUntitled(t, m)
	tm, cmd := m.Update(editor.ActionMsg{Action: "write"})
	m = drainCmd(tm.(Model), cmd)
	if !m.saveAsOpen() {
		t.Fatal("write on an untitled buffer must open the save-as prompt")
	}

	tm, _ = m.Update(tea.PasteMsg{Content: "new.txt"})
	m = tm.(Model)

	if m.saveAsInput.Text != "new.txt" {
		t.Fatalf("saveAsInput = %q, want the pasted text", m.saveAsInput.Text)
	}
}

// TestPasteReachesNewProjectNameStep: the new-project wizard's name step
// (#1718) takes a paste at its cursor (#1873).
func TestPasteReachesNewProjectNameStep(t *testing.T) {
	m := newSized()
	cloneProjectsDir(t)
	tm, cmd := m.Update(project.OpenNewProjectMsg{})
	m = drainCmd(tm.(Model), cmd)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter}) // Plain: straight to the name step
	if m.newProj.step != newProjStepName {
		t.Fatalf("step = %d, want name", m.newProj.step)
	}

	tm, _ = m.Update(tea.PasteMsg{Content: "pasted-proj"})
	m = tm.(Model)

	if m.newProj.name.Text != "pasted-proj" {
		t.Fatalf("newProj.name = %q, want the pasted text", m.newProj.name.Text)
	}
}

// TestPasteReachesLayoutSavePrompt: the save-layout name prompt (#1175)
// takes a paste at its cursor (#1873).
func TestPasteReachesLayoutSavePrompt(t *testing.T) {
	m := sized(t, 100, 40)
	m = step(m, SaveLayoutPromptMsg{})
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter}) // keep-all-panes selection
	if !m.layoutSavePromptOpen() {
		t.Fatal("prompt must be open")
	}

	tm, _ := m.Update(tea.PasteMsg{Content: "dev"})
	m = tm.(Model)

	if m.layoutSaveInput.Text != "dev" {
		t.Fatalf("layoutSaveInput = %q, want the pasted text", m.layoutSaveInput.Text)
	}
}

// TestPasteReachesJBImportPrompt: the JetBrains keymap import prompt (#677)
// takes a paste at its cursor (#1873).
func TestPasteReachesJBImportPrompt(t *testing.T) {
	m, _ := openedFile(t, "abc\n")
	tm, _ := m.Update(ImportJetBrainsKeymapMsg{})
	m = tm.(Model)
	if !m.jbImportPromptOpen() {
		t.Fatal("ImportJetBrainsKeymapMsg must open the import prompt")
	}
	before := m.jbImportInput.Text

	tm, _ = m.Update(tea.PasteMsg{Content: "keymap.xml"})
	m = tm.(Model)

	if m.jbImportInput.Text != before+"keymap.xml" {
		t.Fatalf("jbImportInput = %q, want %q", m.jbImportInput.Text, before+"keymap.xml")
	}
}

// TestPasteDroppedForNonInputOverlay: an overlay with no text input swallows
// the paste — it must not fall through into the editor hidden underneath.
func TestPasteDroppedForNonInputOverlay(t *testing.T) {
	m, _ := openedFile(t, "abc\n")
	m = drainKey(m, tea.KeyPressMsg{Text: "i", Code: 'i'}) // insert mode
	m.undoTree.Open(nil)
	if !m.overlayCapturesKeyboard() {
		t.Fatal("undo tree should capture the keyboard")
	}

	tm, _ := m.Update(tea.PasteMsg{Content: "LEAK"})
	m = tm.(Model)

	if strings.Contains(ansi.Strip(m.activeEditor().View()), "LEAK") {
		t.Fatal("paste leaked into the editor under a non-input overlay")
	}
}

// TestCmdVPastesIntoOverlay: Cmd+V maps to editor.paste, which no overlay
// handles, and overlays own the keyboard before the keymap layer runs — so
// the chord is routed explicitly (#1273).
func TestCmdVPastesIntoOverlay(t *testing.T) {
	restore := clipboardRead
	clipboardRead = func() string { return "from-clipboard" }
	t.Cleanup(func() { clipboardRead = restore })

	m, _ := openedFile(t, "abc\n")
	m.openPalette()

	tm, _ := m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModSuper})
	m = tm.(Model)

	if got := ansi.Strip(m.palette.View()); !strings.Contains(got, "from-clipboard") {
		t.Fatalf("cmd+v did not paste into the palette: %q", got)
	}
}
