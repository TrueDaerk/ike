package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/dap"
	"ike/internal/debugpanel"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/registry"
	"ike/internal/terminal"
)

// termcloseexit_test.go — closing a finished terminal in every placement
// (#2192). A pseudo-terminal is an output vehicle for a run/tool/debuggee:
// it outlives its process on purpose, so the close action must work on the
// dead session where it lives — no docking it into a pane first. The old
// close path sent the child an EOF, which a finished session never receives.

// cmdW is the reserved close chord (#986); ParseKey folds super onto cmd.
var cmdW = tea.KeyPressMsg{Code: 'w', Mod: tea.ModSuper}

// deadTermTab installs a command session that exits immediately as a new tab
// on inst and waits for the child to be gone.
func deadTermTab(t *testing.T, m Model, inst *pane.Instance, key string) *terminal.Model {
	t.Helper()
	term := inst.AddTerminalTab(terminal.NewCommand(key,
		[]string{"/bin/sh", "-c", "exit 3"}, t.TempDir(), 40, 10, nil, m.host.Send))
	waitExited(t, term)
	return term
}

// waitExited blocks until term's session reports itself finished.
func waitExited(t *testing.T, term *terminal.Model) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if term.Exited() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the command session never exited")
}

// TestPopupTabClosesAfterExit: a finished run parked in a popup terminal tab
// (#1398) closes with the reserved cmd+w, where the EOF path used to no-op.
func TestPopupTabClosesAfterExit(t *testing.T) {
	m := openTestPopup(t)
	inst := m.popup.inst
	deadTermTab(t, m, inst, "popuprun")
	if inst.TabCount() != 2 {
		t.Fatalf("test setup: popup should hold two tabs, got %d", inst.TabCount())
	}

	out, _ := m.Update(cmdW)
	m = out.(Model)
	if m.popup.inst == nil || m.popup.inst.TabCount() != 1 {
		t.Fatal("cmd+w on a finished popup tab must close it without docking it first")
	}
	if m.popup.inst.ActiveTerminal() == nil || m.popup.inst.ActiveTerminal().Exited() {
		t.Fatal("the surviving live shell tab must stay open")
	}
}

// TestPopupTabCloseGlyphClosesAfterExit: the active tab's ✕ takes the same
// route as cmd+w, so the mouse close works on a finished session too.
func TestPopupTabCloseGlyphClosesAfterExit(t *testing.T) {
	m := openTestPopup(t)
	inst := m.popup.inst
	deadTermTab(t, m, inst, "popuprun")
	m.requestPopupTabClose()
	if m.popup.inst == nil || m.popup.inst.TabCount() != 1 {
		t.Fatal("the ✕ of a finished popup tab must close it")
	}
}

// TestFloatPanelClosesAfterExit: the same for a torn-out floating panel
// (#1793) — the placement with no pane registry behind it at all. Its last
// tab going drops the panel.
func TestFloatPanelClosesAfterExit(t *testing.T) {
	m := openTestPopup(t)
	m, f := tearOutFirstTab(t, m)
	f.inst.CloseTerminalTabs() // the panel's shell ends, its tab stays
	m.setFloatFocus(f)
	if !f.inst.ActiveTerminal().Exited() {
		t.Fatal("test setup: the panel's session should be finished")
	}

	out, _ := m.Update(cmdW)
	m = out.(Model)
	if len(m.floatTerms) != 0 {
		t.Fatal("cmd+w on a finished floating panel must close it")
	}
	if !m.popup.open || m.popup.inst == nil {
		t.Fatal("the popup box must survive the panel's close")
	}
}

// TestDebugConsoleClosesAfterExit: the DAP console (#1370, #2190) is a pipe
// session — its emulator stays open past FinishPipe, so Running() keeps
// reporting true. The close path must read the exited state instead, or the
// finished area can never be closed from the keyboard.
func TestDebugConsoleClosesAfterExit(t *testing.T) {
	m, _, path := debugModelReg(t, registry.Global(), host.MapConfig{})
	frames := []dap.StackFrame{{ID: 1, Name: "f", Source: dap.Source{Path: path}, Line: 2, Column: 1}}
	tm, _ := m.Update(debugStoppedMsg{threadID: 1, frames: frames})
	m = tm.(Model)
	p := m.debugPanel()
	p.SetTab(debugpanel.TabConsole)
	p.Term().FinishPipe(3, true)
	m.setFocus(pane.DebugKey)
	if m.terminalFocused() {
		t.Fatal("a console past FinishPipe must not count as a live terminal")
	}

	m = drainKey(m, cmdW)
	if m.activeWS().Panes.Has(pane.DebugKey) {
		t.Fatal("cmd+w on a finished debug console must close the area")
	}
}

// TestToolPaneClosesAfterExit guards the placement that already worked
// (#741/#810) against regressing with the shared exited check.
func TestToolPaneClosesAfterExit(t *testing.T) {
	withTools(t, config.ToolEntry{Name: "shortlived", Command: "sh", Args: []string{"-c", "exit 3"}})
	m := sizedWith(t, registry.Global(), 100, 40)
	out, _ := m.Update(ToolOpenMsg{Name: "shortlived"})
	m = out.(Model)
	inst := m.toolPane("shortlived")
	if inst == nil {
		t.Fatal("test setup: the tool pane must open")
	}
	key := inst.Key()
	t.Cleanup(func() { inst.Terminal().Close() })
	m.setFocus(key)
	waitExited(t, inst.Terminal())

	m = drainKey(m, cmdW)
	if m.activeWS().Panes.Has(key) {
		t.Fatal("cmd+w on a finished tool pane must close it")
	}
}

// TestExitedTerminalChromeMarker: the finished state reads from the chrome
// (#2192) — the pane title spells it out with the exit code, the tab segment
// carries the compact glyph, which is all a popup-layer box shows.
func TestExitedTerminalChromeMarker(t *testing.T) {
	m := openTestPopup(t)
	inst := m.popup.inst
	deadTermTab(t, m, inst, "popuprun")

	labels := tabLabels(inst)
	if !strings.Contains(labels[1], termExitedGlyph) {
		t.Fatalf("the finished tab must carry the exited glyph, got %q", labels[1])
	}
	if strings.Contains(labels[0], termExitedGlyph) {
		t.Fatalf("the live tab must stay unmarked, got %q", labels[0])
	}
	if got := termExitedTitle(inst.TabTerminal(1)); !strings.Contains(got, "exited (code 3)") {
		t.Fatalf("the title suffix must name the exit code, got %q", got)
	}
	if got := termExitedTitle(inst.TabTerminal(0)); got != "" {
		t.Fatalf("a live session must add no title suffix, got %q", got)
	}
}
