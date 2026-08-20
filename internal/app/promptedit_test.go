package app

// promptedit_test.go guards the #2002 sweep for the app's shell prompts: the
// file rename, symbol rename, JetBrains-import and OpenAPI-import inputs each
// hand-rolled a subset of line editing (arrows, home/end, backspace, delete)
// and now route through ui.EditKey, so word motions, the word/line kills and
// the macOS opt/cmd chords work in all of them.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// promptKeys feeds keys to a Model and returns it.
func promptKeys(m Model, keys ...tea.KeyPressMsg) Model {
	for _, k := range keys {
		out, _ := m.Update(k)
		m = out.(Model)
	}
	return m
}

func promptType(m Model, s string) Model {
	for _, r := range s {
		m = promptKeys(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

var (
	keyAltBack   = tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt}
	keyCmdBack   = tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper}
	keyAltLeft   = tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt}
	keyAltRight  = tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt}
	keyCmdLeft   = tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModSuper}
	keyCmdRight  = tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModSuper}
	keyLeftPlain = tea.KeyPressMsg{Code: tea.KeyLeft}
)

// --- symbol rename prompt ------------------------------------------------

func TestLSPRenamePromptSharedEditing(t *testing.T) {
	m, _ := openRenamePrompt(t, "old name here")
	m = promptKeys(m, keyAltBack)
	if m.lspRename.input != "old name " {
		t.Fatalf("alt+backspace: %q", m.lspRename.input)
	}
	m = promptKeys(m, keyAltLeft)
	if m.lspRename.pos != 4 {
		t.Fatalf("alt+left: cursor = %d, want 4", m.lspRename.pos)
	}
	m = promptKeys(m, keyAltRight)
	if m.lspRename.pos != 8 {
		t.Fatalf("alt+right: cursor = %d, want 8", m.lspRename.pos)
	}
	m = promptKeys(m, keyCmdRight)
	if m.lspRename.pos != 9 {
		t.Fatalf("cmd+right: cursor = %d, want 9", m.lspRename.pos)
	}
	m = promptKeys(m, keyCmdBack)
	if m.lspRename.input != "" || m.lspRename.pos != 0 {
		t.Fatalf("cmd+backspace: %q/%d", m.lspRename.input, m.lspRename.pos)
	}
}

func TestLSPRenamePromptCtrlUStillClears(t *testing.T) {
	m, _ := openRenamePrompt(t, "Greet")
	m = promptKeys(m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if m.lspRename.input != "" || m.lspRename.pos != 0 {
		t.Fatalf("ctrl+u: %q/%d", m.lspRename.input, m.lspRename.pos)
	}
}

func TestLSPRenamePromptEditsMidWord(t *testing.T) {
	m, applied := openRenamePrompt(t, "Greet")
	m = promptKeys(m, tea.KeyPressMsg{Code: tea.KeyHome})
	m = promptType(m, "My")
	m = promptKeys(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if *applied != "MyGreet" {
		t.Fatalf("applied = %q, want MyGreet", *applied)
	}
}

// --- file rename prompt --------------------------------------------------

func openFileRename(t *testing.T) Model {
	t.Helper()
	_, file := projectDir(t)
	m := newSized()
	tm, _ := m.openPath(file, false)
	m = tm.(Model)
	m = dispatch(t, m, RenameFileMsg{})
	if !m.renameOpen() {
		t.Fatal("file.rename must open the rename prompt")
	}
	return m
}

func TestRenamePromptSharedEditing(t *testing.T) {
	m := openFileRename(t) // prefilled "a.txt", cursor at the end
	m = promptKeys(m, keyAltBack)
	if m.renameInput != "a." {
		t.Fatalf("alt+backspace: %q", m.renameInput)
	}
	m = promptType(m, "md")
	if m.renameInput != "a.md" {
		t.Fatalf("typing after the kill: %q", m.renameInput)
	}
	m = promptKeys(m, keyCmdLeft)
	if m.renamePos != 0 {
		t.Fatalf("cmd+left: cursor = %d", m.renamePos)
	}
	m = promptType(m, "b")
	if m.renameInput != "ba.md" {
		t.Fatalf("insert at line start: %q", m.renameInput)
	}
	m = promptKeys(m, keyCmdBack)
	if m.renameInput != "a.md" {
		t.Fatalf("cmd+backspace: %q", m.renameInput)
	}
}

func TestRenamePromptPasteStillLandsAtCursor(t *testing.T) {
	m := openFileRename(t)
	m = promptKeys(m, keyCmdLeft)
	if !m.pasteRenamePrompt("x-") {
		t.Fatal("paste must be consumed")
	}
	if m.renameInput != "x-a.txt" || m.renamePos != 2 {
		t.Fatalf("paste: %q/%d", m.renameInput, m.renamePos)
	}
}

// --- JetBrains keymap import prompt --------------------------------------

func openJBImport(t *testing.T) Model {
	t.Helper()
	m := sized(t, 100, 40)
	m = dispatch(t, m, ImportJetBrainsKeymapMsg{})
	if !m.jbImportPromptOpen() {
		t.Fatal("the JetBrains import prompt must open")
	}
	return m
}

func TestJBImportPromptSharedEditing(t *testing.T) {
	m := openJBImport(t)
	m = promptKeys(m, keyCmdBack) // drop the "~/" prefill
	m = promptType(m, "/tmp/one two")
	m = promptKeys(m, keyAltBack)
	if m.jbImportInput != "/tmp/one " {
		t.Fatalf("alt+backspace: %q", m.jbImportInput)
	}
	m = promptKeys(m, keyCmdBack)
	if m.jbImportInput != "" {
		t.Fatalf("cmd+backspace: %q", m.jbImportInput)
	}
	m = promptType(m, "abc")
	m = promptKeys(m, keyLeftPlain, keyLeftPlain)
	m = promptType(m, "X")
	if m.jbImportInput != "aXbc" {
		t.Fatalf("mid-line insert: %q", m.jbImportInput)
	}
}

// --- OpenAPI import prompt ------------------------------------------------

func openAPIImport(t *testing.T) Model {
	t.Helper()
	m := sized(t, 100, 40)
	m = dispatch(t, m, ImportOpenAPIMsg{})
	if !m.openAPIImportPromptOpen() {
		t.Fatal("the OpenAPI import prompt must open")
	}
	return m
}

func TestOpenAPIImportPromptSharedEditing(t *testing.T) {
	m := openAPIImport(t)
	m = promptKeys(m, keyCmdBack) // drop the "~/" prefill
	m = promptType(m, "/tmp/spec one.yaml")
	m = promptKeys(m, keyAltLeft)
	if m.oapiImportPos != 14 {
		t.Fatalf("alt+left: cursor = %d, want 14", m.oapiImportPos)
	}
	m = promptKeys(m, keyAltBack)
	if m.oapiImportInput != "/tmp/spec yaml" {
		t.Fatalf("alt+backspace: %q", m.oapiImportInput)
	}
	m = promptKeys(m, keyCmdBack)
	if m.oapiImportInput != "yaml" {
		t.Fatalf("cmd+backspace kills to line start: %q", m.oapiImportInput)
	}
}
