package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
)

// floatcmddrag_test.go — cmd+drag anywhere in a floating terminal panel moves
// it (#3035), while a cmd+click without motion still opens the file reference
// under the pointer (#1168).

// floatPanelWithRef tears out a wide floating panel, prints a reference to an
// existing file into its session, and returns the screen cell of the
// reference plus its path.
func floatPanelWithRef(t *testing.T) (Model, *floatTerm, int, int, string) {
	t.Helper()
	m := openTestPopupWith(t, sized(t, 400, 60))
	m, f := tearOutFirstTab(t, m)
	// A short dir keeps the reference on one row of the panel.
	dir, err := os.MkdirTemp("/tmp", "ike3035-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package x\n\nfunc y() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := path + ":3:6"
	term := f.inst.ActiveTerminal()
	term.InjectOutput([]byte("\r\n" + ref + "\r\n"))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for y, line := range strings.Split(ansi.Strip(term.View()), "\n") {
			if x := strings.Index(line, ref); x >= 0 {
				return m, f, f.x + paneContentX + x + 2, f.y + paneContentY + y, path
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("reference %q never rendered in the panel", ref)
	return m, f, 0, 0, ""
}

func cmdPress(x, y int) tea.MouseMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft, Mod: tea.ModSuper}
}

func TestFloatPanelCmdDragMovesPanel(t *testing.T) {
	m, f, x, y, path := floatPanelWithRef(t)
	x0, y0 := f.x, f.y
	m = step(m, cmdPress(x, y))
	if m.floatMove == nil || m.floatMove.target != f {
		t.Fatal("a cmd+press in the panel content must arm the move drag")
	}
	m = step(m, motion(x-4, y-2))
	m = step(m, motion(x-6, y-3))
	m = step(m, release(x-6, y-3))
	if m.floatMove != nil {
		t.Fatal("the release must end the move drag")
	}
	if f.x != x0-6 || f.y != y0-3 {
		t.Fatalf("the panel must follow the drag: want %d,%d got %d,%d", x0-6, y0-3, f.x, f.y)
	}
	if m.editorForPath(path) != nil {
		t.Fatal("a cmd+drag must not open the reference under the press")
	}
	if f.inst.ActiveTerminal().HasSelection() {
		t.Fatal("a cmd+drag must not start a selection")
	}
}

func TestFloatPanelCmdDragClampsToScreen(t *testing.T) {
	m, f, x, y, _ := floatPanelWithRef(t)
	m = step(m, cmdPress(x, y))
	m = step(m, motion(x+10_000, y+10_000))
	m = step(m, release(x+10_000, y+10_000))
	if f.x != max(m.width-f.w, 0) || f.y != max(m.height-f.h, 0) {
		t.Fatalf("the panel must clamp like a title-row drag, got %d,%d", f.x, f.y)
	}
}

func TestFloatPanelCmdClickWithoutMotionOpensReference(t *testing.T) {
	for _, mod := range []tea.KeyMod{tea.ModSuper, tea.ModMeta} {
		m, f, x, y, path := floatPanelWithRef(t)
		x0, y0 := f.x, f.y
		m = step(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft, Mod: mod})
		if m.editorForPath(path) != nil {
			t.Fatalf("mod %v: the press alone must not decide — wait for motion or release", mod)
		}
		// A motion report without a cell change is not a move.
		m = step(m, motion(x, y))
		m = step(m, release(x, y))
		ed := m.editorForPath(path)
		if ed == nil {
			t.Fatalf("mod %v: cmd+click without motion must open the reference", mod)
		}
		if line, col := ed.CursorPos(); line != 2 || col != 5 {
			t.Fatalf("mod %v: cursor = %d:%d, want 2:5 (0-based)", mod, line, col)
		}
		if f.x != x0 || f.y != y0 {
			t.Fatalf("mod %v: a click must not move the panel", mod)
		}
	}
}

func TestFloatPanelPlainDragStillSelects(t *testing.T) {
	m, f, x, y, _ := floatPanelWithRef(t)
	x0, y0 := f.x, f.y
	m = step(m, press(x, y))
	if m.floatMove != nil {
		t.Fatal("a plain press in the content must not arm a move")
	}
	if m.drag == nil || m.drag.kind != dragTermSelect {
		t.Fatal("a plain press must still anchor a selection")
	}
	m = step(m, motion(x+5, y))
	m = step(m, release(x+5, y))
	if f.x != x0 || f.y != y0 {
		t.Fatal("a plain drag must not move the panel")
	}
}
