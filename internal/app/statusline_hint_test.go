package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestEmptyEditorHintShowsAndClears guards the discovery hint (#659): a
// focused editor pane with no file points at help and search-everywhere; the
// hint disappears the moment a file opens.
func TestEmptyEditorHintShowsAndClears(t *testing.T) {
	m := newSized()
	m.setFocus(m.activeEditorKey())
	if line := ansi.Strip(m.statusLine()); !strings.Contains(line, "? help") {
		t.Fatalf("empty editor must show the discovery hint: %q", line)
	}

	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	if line := ansi.Strip(m.statusLine()); strings.Contains(line, "? help") {
		t.Fatalf("hint must clear once a file is open: %q", line)
	}
}

// TestEmptyEditorHintRendersRemap: a search-everywhere remap outside the
// curated chord list displays the live chord (resolver truth).
func TestEmptyEditorHintRendersRemap(t *testing.T) {
	m := newSized()
	hint := emptyHintSegment(m, nil)
	if !strings.Contains(hint, "shift shift") {
		t.Fatalf("default hint must carry the curated chord: %q", hint)
	}
}

// TestEmptyEditorHintSuppressedWhenNarrow: below ~70 columns the hint drops
// so it never crowds the bar.
func TestEmptyEditorHintSuppressedWhenNarrow(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m = tm.(Model)
	m.setFocus(m.activeEditorKey())
	if line := ansi.Strip(m.statusLine()); strings.Contains(line, "? help") {
		t.Fatalf("narrow terminals must drop the hint: %q", line)
	}
}

// TestStatusLineNeverWrapsAt80 guards the truncation fix (#659): with the
// hint active on an 80-column terminal the bar must never exceed the width —
// lipgloss pads but does not clip, and an over-wide bar wraps onto two rows.
func TestStatusLineNeverWrapsAt80(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = tm.(Model)
	m.setFocus(m.activeEditorKey())
	line := m.statusLine()
	if w := lipgloss.Width(line); w > 80 {
		t.Fatalf("status line %d cells wide on an 80-col terminal", w)
	}
	if strings.Contains(ansi.Strip(line), "\n") {
		t.Fatal("status line must be a single row")
	}
}
