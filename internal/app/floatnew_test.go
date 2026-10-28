package app

import (
	"runtime"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// floatnew_test.go — terminal.newFloatPanel (#3076): cmd+n inside the popup
// layer opens a fresh shell in a new floating panel, cascaded from the
// keyboard owner; cmd+n everywhere else keeps creating a file.

// cmdN is the macOS cmd+n press; the binding ships on macOS only (darwinRows).
var cmdN = tea.KeyPressMsg{Code: 'n', Mod: tea.ModSuper}

func skipOffDarwin(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("the cmd+n popup-layer binding ships on macOS only")
	}
}

// closeFloats ends every panel's shells when the test finishes.
func closeFloats(t *testing.T, m Model) {
	t.Helper()
	fs := append([]*floatTerm(nil), m.floatTerms...)
	t.Cleanup(func() {
		for _, f := range fs {
			f.inst.CloseTerminalTabs()
		}
	})
}

// checkNewPanel asserts the topmost panel is a fresh, focused, project-owned
// shell panel, clamped on screen.
func checkNewPanel(t *testing.T, m Model, wantPanels int) *floatTerm {
	t.Helper()
	if len(m.floatTerms) != wantPanels {
		t.Fatalf("want %d floating panels, got %d", wantPanels, len(m.floatTerms))
	}
	f := m.floatTerms[len(m.floatTerms)-1]
	if m.floatFocused() != f || m.popupFocused() != f.inst {
		t.Fatal("the new panel must take the keyboard")
	}
	if !m.popupLayerFocused() {
		t.Fatal("the popup layer must be shown and focused")
	}
	if f.inst.TabCount() != 1 || f.inst.ActiveTerminal() == nil {
		t.Fatal("the new panel must host one fresh shell tab")
	}
	if f.global {
		t.Fatal("the new panel must be project-owned like a torn-out tab")
	}
	if f.x < 0 || f.y < 0 || f.x+f.w > m.width || f.y+f.h > m.height {
		t.Fatalf("panel must be clamped on screen, got %d,%d %dx%d", f.x, f.y, f.w, f.h)
	}
	return f
}

// checkCascade asserts f sits +2/+1 from (ax, ay), clamped on screen.
func checkCascade(t *testing.T, m Model, f *floatTerm, ax, ay int) {
	t.Helper()
	wantX := min(ax+floatCascadeDX, max(m.width-f.w, 0))
	wantY := min(ay+floatCascadeDY, max(m.height-f.h, 0))
	if f.x != wantX || f.y != wantY {
		t.Fatalf("panel origin = %d,%d, want cascade %d,%d", f.x, f.y, wantX, wantY)
	}
}

func TestCmdNInPopupBoxOpensFloatPanel(t *testing.T) {
	skipOffDarwin(t)
	m := openTestPopup(t)
	bx, by, bw, bh := m.popupTermRect()
	sess := m.popup.inst.ActiveTerminal().SessionKey()
	m = drainKey(m, cmdN)
	closeFloats(t, m)
	f := checkNewPanel(t, m, 1)
	checkCascade(t, m, f, bx, by)
	if dw, dh := m.floatPanelDefaultSize(); f.w != dw || f.h != dh || f.w != bw || f.h != bh {
		t.Fatalf("panel size = %dx%d, want the default box size %dx%d", f.w, f.h, bw, bh)
	}
	if m.popup.inst.TabCount() != 1 || m.popup.inst.ActiveTerminal().SessionKey() != sess {
		t.Fatal("the popup box must keep its own session untouched")
	}
	if f.inst.ActiveTerminal().SessionKey() == sess {
		t.Fatal("the panel must run a new shell, not the box's")
	}
}

func TestCmdNInFloatPanelOpensSecondPanel(t *testing.T) {
	skipOffDarwin(t)
	m := openTestPopup(t)
	m = drainKey(m, cmdN)
	closeFloats(t, m)
	first := m.floatTerms[0]
	m = drainKey(m, cmdN)
	closeFloats(t, m)
	second := checkNewPanel(t, m, 2)
	checkCascade(t, m, second, first.x, first.y)
	if second == first {
		t.Fatal("cmd+n in a panel must open another panel")
	}
}

func TestCmdNCascadeClampsAtScreenEdge(t *testing.T) {
	skipOffDarwin(t)
	m := openTestPopup(t)
	m = drainKey(m, cmdN)
	closeFloats(t, m)
	f := m.floatTerms[0]
	f.x, f.y = m.width-f.w, m.height-f.h // parked in the bottom-right corner
	m = drainKey(m, cmdN)
	closeFloats(t, m)
	g := checkNewPanel(t, m, 2)
	checkCascade(t, m, g, f.x, f.y)
	if g.x != f.x || g.y != f.y {
		t.Fatal("a cascade past the corner must clamp onto the screen edge")
	}
}

func TestNewFloatPanelFromPaletteWithoutPopup(t *testing.T) {
	m := sized(t, 100, 40)
	m = dispatch(t, m, TerminalNewFloatPanelMsg{})
	closeFloats(t, m)
	f := checkNewPanel(t, m, 1)
	// Default spot and size: the popup box's own (#1793 offset), uncascaded.
	if x0, y0, w, h := m.popupTermRect(); f.x != x0 || f.y != y0 || f.w != w || f.h != h {
		t.Fatalf("panel = %d,%d %dx%d, want the box's default %d,%d %dx%d", f.x, f.y, f.w, f.h, x0, y0, w, h)
	}
	if m.popup.inst != nil {
		t.Fatal("no popup box must be spawned for a palette-opened panel")
	}
}

func TestCmdNInEditorStillCreatesFile(t *testing.T) {
	skipOffDarwin(t)
	root, file := projectDir(t)
	chdir(t, root)
	m := newSized()
	tm, _ := m.openPath(file, false)
	m = tm.(Model)
	m = drainKey(m, cmdN)
	if len(m.floatTerms) != 0 {
		t.Fatal("cmd+n in an editor must not open a floating panel")
	}
	if !m.explorer().Prompting() {
		t.Fatal("cmd+n in an editor must open the new-file prompt")
	}
}

func TestCmdNInPaneTerminalStaysWithShell(t *testing.T) {
	skipOffDarwin(t)
	m := newSized()
	tm, _ := m.Update(TerminalNewTabMsg{})
	m = tm.(Model)
	if m.focusContext() != "terminal" {
		t.Skip("setup: no focused pane terminal")
	}
	if handled, _ := m.terminalContextChord(cmdN); handled {
		t.Fatal("a pane terminal must not run the popup-only cmd+n")
	}
}
