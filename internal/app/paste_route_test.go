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

// TestPasteMsgInsertsBlockIntoEditor verifies a bracketed paste routes to the
// focused editor as one block (#603), not per character.
func TestPasteMsgInsertsBlockIntoEditor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	// Enter insert mode at the start, then paste a multi-line block.
	m = drainKey(m, tea.KeyPressMsg{Text: "i", Code: 'i'})

	tm, _ = m.Update(tea.PasteMsg{Content: "one\ntwo"})
	m = tm.(Model)

	got := ansi.Strip(m.activeEditor().View())
	if !strings.Contains(got, "one") || !strings.Contains(got, "twoabc") {
		t.Fatalf("pasted block not inserted as one edit: %q", got)
	}
}

// TestPasteMsgIgnoredWhenOverlayOpen verifies a paste does not leak into the
// hidden editor while a modal overlay (the palette) owns the keyboard.
func TestPasteMsgIgnoredWhenOverlayOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	m.openPalette()
	if !m.overlayCapturesKeyboard() {
		t.Fatal("palette should capture the keyboard")
	}

	tm, _ = m.Update(tea.PasteMsg{Content: "LEAK"})
	m = tm.(Model)

	if strings.Contains(m.activeEditor().View(), "LEAK") {
		t.Fatal("paste leaked into the editor while the palette was open")
	}
}

// TestPasteMsgRoutesIntoEditorSearchLine is the #1380 regression guard: with
// the in-editor search bar open ("/" or cmd+f), a bracketed paste lands in the
// search input — before the fix it fell through into the buffer at the cursor.
// The search line is editor-internal state, not an overlay, so the app-level
// router (#1273) cannot see it; the editor routes it itself.
func TestPasteMsgRoutesIntoEditorSearchLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	m = drainKey(m, tea.KeyPressMsg{Text: "/", Code: '/'})

	tm, _ = m.Update(tea.PasteMsg{Content: "needle"})
	m = tm.(Model)

	got := ansi.Strip(m.activeEditor().View())
	if !strings.Contains(got, "/needle") {
		t.Fatalf("paste did not reach the search line: %q", got)
	}
	if strings.Contains(got, "needleabc") || strings.Contains(got, "aneedle") {
		t.Fatalf("paste leaked into the buffer: %q", got)
	}
}
