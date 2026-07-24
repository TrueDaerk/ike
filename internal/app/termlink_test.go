package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/pane"
)

// openWideTerminal opens a terminal pane in an extra-wide model so a long
// absolute t.TempDir reference renders unwrapped on one row.
func openWideTerminal(t *testing.T) (Model, string) {
	t.Helper()
	m := sized(t, 220, 50)
	out, _ := m.Update(TerminalNewMsg{})
	m = out.(Model)
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatalf("terminal.new should focus a terminal pane, got %q", key)
	}
	t.Cleanup(func() { inst.Terminal().Close() })
	return m, key
}

// termRefRow types a printf into the focused terminal pane that outputs the
// reference and waits until it renders, returning the content-local row and
// the column of its first rune.
func termRefRow(t *testing.T, m Model, key, ref string) (row, col int) {
	t.Helper()
	inst := m.activeWS().Panes.Get(key)
	for _, r := range "printf '" + ref + "\\n'\r" {
		inst.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for y, line := range strings.Split(ansi.Strip(inst.Terminal().View()), "\n") {
			if strings.TrimRight(line, " ") == ref {
				return y, 0
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("reference %q never rendered", ref)
	return 0, 0
}

// TestTerminalCmdClickOpensReference guards #1168: cmd+click on a printed
// file:line reference opens the file at that position through openPathAt;
// a plain click on the same cell only anchors a selection.
func TestTerminalCmdClickOpensReference(t *testing.T) {
	m, key := openWideTerminal(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package x\n\nfunc y() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := path + ":3:6"
	row, col := termRefRow(t, m, key, ref)
	cell := func() (int, int) {
		r := m.lay.Panes[key]
		return r.X + paneContentX + col + 2, r.Y + m.contentYOff(key) + row
	}

	// Plain click: selection press, no editor opens.
	x, y := cell()
	m = step(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if ed := m.editorForPath(path); ed != nil {
		t.Fatal("plain click must not open the reference")
	}

	for _, mod := range []tea.KeyMod{tea.ModSuper, tea.ModMeta} {
		x, y = cell()
		m = step(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft, Mod: mod})
		ed := m.editorForPath(path)
		if ed == nil {
			t.Fatalf("mod %v: cmd+click must open the referenced file", mod)
		}
		line, cc := ed.CursorPos()
		if line != 2 || cc != 5 {
			t.Fatalf("mod %v: cursor = %d:%d, want 2:5 (0-based)", mod, line, cc)
		}
		// Refocus the terminal for the second round.
		m.setFocus(key)
	}
}

// TestTerminalCmdClickMissingFileInert: without an existing file the
// cmd+click neither opens anything nor anchors a selection.
func TestTerminalCmdClickMissingFileInert(t *testing.T) {
	m, key := openWideTerminal(t)
	ref := "/nowhere/ghost.go:3"
	row, col := termRefRow(t, m, key, ref)
	r := m.lay.Panes[key]
	x := r.X + paneContentX + col + 2
	y := r.Y + m.contentYOff(key) + row
	m = step(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft, Mod: tea.ModSuper})
	if ed := m.editorForPath(ref[:len(ref)-2]); ed != nil {
		t.Fatal("a non-existing reference must stay inert")
	}
	if m.activeWS().Panes.Get(key).Terminal().HasSelection() {
		t.Fatal("an inert cmd+click must not start a selection")
	}
}
