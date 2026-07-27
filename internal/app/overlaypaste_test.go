package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/explorer"
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
	before := m.renameInput

	tm, _ := m.Update(tea.PasteMsg{Content: "-copy"})
	m = tm.(Model)

	if m.renameInput != before+"-copy" {
		t.Fatalf("rename input = %q, want %q", m.renameInput, before+"-copy")
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
