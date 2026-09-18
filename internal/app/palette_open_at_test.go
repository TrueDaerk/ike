package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/palette"
	"ike/internal/registry"
)

// TestPaletteOpenFileAtLine guards #2636's root-model half: a palette row that
// names a position as well as a file — the '@' finder's pasted
// "path:line[:col]" row — opens the file *and* places the cursor there, in the
// focused pane like every other palette pick.
func TestPaletteOpenFileAtLine(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "pasted.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {}\n\n// tail\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)

	// Line and Col are 1-based as pasted; the model converts them to the
	// editor's 0-based jump, which reports 1-based positions back.
	out, _ = m.Update(palette.OpenFileMsg{Path: path, Line: 3, Col: 6})
	m = out.(Model)

	ed := m.editorForPath(canonicalPath(path))
	if ed == nil {
		t.Fatal("the pasted path did not open")
	}
	if line, col := ed.Cursor(); line != 3 || col != 6 {
		t.Fatalf("cursor = %d:%d, want the pasted 3:6", line, col)
	}

	// A row without a position keeps the plain open: no jump, no clamping.
	out, _ = m.Update(palette.OpenFileMsg{Path: path})
	m = out.(Model)
	if ed := m.editorForPath(canonicalPath(path)); ed == nil {
		t.Fatal("a positionless row must still open the file")
	}
}
