package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// stubClipboardRead replaces the read seam for the test's lifetime.
func stubClipboardRead(t *testing.T, text string) {
	t.Helper()
	orig := clipboardRead
	clipboardRead = func() string { return text }
	t.Cleanup(func() { clipboardRead = orig })
}

// TestCompareClipboardBuffer guards diff.compareWithClipboard (#1477): from
// an editor pane the whole live buffer lands on the right, the clipboard on
// the left, in a focused diff pane.
func TestCompareClipboardBuffer(t *testing.T) {
	m := newSized()
	path := writeTempFile(t, "buf.txt", "a\nold\nc\n")
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	stubClipboardRead(t, "a\nnew\nc")

	tm, _ = m.Update(CompareClipboardMsg{})
	m = tm.(Model)

	key := diffKeyOf(m)
	if key == "" {
		t.Fatal("compare with clipboard should open a diff pane")
	}
	if m.activeWS().Panes.Focused() != key {
		t.Fatalf("the diff pane should take focus, got %q", m.activeWS().Panes.Focused())
	}
	v := m.render()
	if !strings.Contains(v, "Clipboard") {
		t.Fatal("the left column should be titled Clipboard")
	}
	if !strings.Contains(v, "old") || !strings.Contains(v, "new") {
		t.Fatal("the diff should show both the buffer and the clipboard line")
	}
}

// TestCompareClipboardSelection narrows the right side to the visual
// selection: only the selected line is compared, and the title marks it.
func TestCompareClipboardSelection(t *testing.T) {
	m := newSized()
	path := writeTempFile(t, "sel.txt", "keep\nsecret\n")
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	stubClipboardRead(t, "done")

	// Visual-line select the first line only.
	m = drainKey(m, tea.KeyPressMsg{Code: 'V', Text: "V"})

	tm, _ = m.Update(CompareClipboardMsg{})
	m = tm.(Model)

	key := diffKeyOf(m)
	if key == "" {
		t.Fatal("compare with clipboard should open a diff pane")
	}
	// Check the diff pane's own view (the editor pane still shows the full
	// file); style codes split words, so compare the plain text.
	if !strings.Contains(ansi.Strip(m.render()), "(selection)") {
		t.Fatal("the right column should be marked as a selection")
	}
	v := ansi.Strip(m.activeWS().Panes.Get(key).Diff().View())
	if !strings.Contains(v, "keep") || !strings.Contains(v, "done") {
		t.Fatal("the diff should show the selection against the clipboard")
	}
	if strings.Contains(v, "secret") {
		t.Fatal("lines outside the selection must not appear in the diff")
	}
}

// TestCompareClipboardEmpty shows a notification instead of an empty diff.
func TestCompareClipboardEmpty(t *testing.T) {
	m := newSized()
	path := writeTempFile(t, "buf.txt", "a\n")
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	stubClipboardRead(t, "")

	tm, _ = m.Update(CompareClipboardMsg{})
	m = tm.(Model)

	if key := diffKeyOf(m); key != "" {
		t.Fatalf("an empty clipboard must not open a diff pane, got %q", key)
	}
	found := false
	for _, n := range m.toasts {
		if n.text == "clipboard is empty" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the empty-clipboard notification")
	}
}
