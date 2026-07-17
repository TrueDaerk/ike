package debugpanel

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/terminal"
)

// startTerm spawns a real command session for the panel to embed; argv holds
// the PTY open (or exits) as the test needs.
func startTerm(t *testing.T, argv ...string) *terminal.Model {
	t.Helper()
	tm := terminal.NewCommand("dbg-term-test", argv, t.TempDir(), 80, 24, nil, func(tea.Msg) {})
	t.Cleanup(tm.Close)
	if tm.Pid() == 0 {
		t.Fatalf("spawn failed for %v", argv)
	}
	return &tm
}

// waitRunning polls the process state.
func waitRunning(t *testing.T, tm *terminal.Model, want bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for tm.Running() != want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if tm.Running() != want {
		t.Fatalf("Running() never became %v", want)
	}
}

// TestSetTerminalSizesToOutputColumn verifies the embedded terminal is fitted
// to the Output column (its width, the rows under the title) on SetTerminal
// and re-fitted on SetSize.
func TestSetTerminalSizesToOutputColumn(t *testing.T) {
	m := New(nil)
	m.SetSize(90, 12)
	tm := startTerm(t, "/bin/cat")
	m.SetTerminal(tm)
	_, _, ow := m.colWidths()
	if w, h := tm.Size(); w != ow || h != 11 {
		t.Fatalf("terminal size = %dx%d, want %dx%d", w, h, ow, 11)
	}
	m.SetSize(60, 8)
	_, _, ow = m.colWidths()
	if w, h := tm.Size(); w != ow || h != 7 {
		t.Fatalf("after resize terminal size = %dx%d, want %dx%d", w, h, ow, 7)
	}
}

// TestTerminalViewWinsOverOutputRows verifies the PTY view replaces the DAP
// output rows while a terminal is embedded, and the rows return once it is
// detached.
func TestTerminalViewWinsOverOutputRows(t *testing.T) {
	m := New(nil)
	m.SetSize(90, 10)
	m.AppendOutput(false, "dap-event-line\n")
	if !strings.Contains(m.View(), "dap-event-line") {
		t.Fatal("DAP output must render without a terminal")
	}
	tm := startTerm(t, "/bin/sh", "-c", "echo pty-line; sleep 30")
	m.SetTerminal(tm)
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(m.View(), "pty-line") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	v := m.View()
	if !strings.Contains(v, "pty-line") {
		t.Fatal("the embedded terminal's grid must render in the Output column")
	}
	if strings.Contains(v, "dap-event-line") {
		t.Fatal("DAP output rows must not render while a terminal is embedded")
	}
	m.CloseTerminal()
	if !strings.Contains(m.View(), "dap-event-line") {
		t.Fatal("DAP output must return after the terminal detaches")
	}
}

// TestOutputColumnKeysReachRunningTerminal verifies key routing (#676): with
// the Output column focused and the debuggee running, plain keys go raw to
// the PTY; shift+tab is the reserved escape back to the variables column.
func TestOutputColumnKeysReachRunningTerminal(t *testing.T) {
	m := New(nil)
	m.SetSize(90, 10)
	m.SetFocused(true)
	tm := startTerm(t, "/bin/cat")
	m.SetTerminal(tm)
	m.Update(key("tab")) // frames -> vars
	m.Update(key("tab")) // vars -> output
	if !m.OutputTermCapturing() {
		t.Fatal("a focused Output column with a running terminal must capture")
	}
	m.Update(key("h")) // would leave the column without a terminal
	if !tm.Occupied() {
		t.Fatal("plain keys must reach the debuggee's PTY")
	}
	if m.col != colOutput {
		t.Fatal("h must not leave the column while the debuggee runs")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.col != colVars {
		t.Fatal("shift+tab must return to the variables column")
	}
	if m.OutputTermCapturing() {
		t.Fatal("capture ends when the column loses focus")
	}
}

// TestOutputColumnKeysAfterExitRestoreNavigation verifies the panel's own
// navigation returns once the debuggee exited: h leaves the column and j/k
// page the dead terminal's scrollback instead of feeding a closed PTY.
func TestOutputColumnKeysAfterExitRestoreNavigation(t *testing.T) {
	m := New(nil)
	m.SetSize(90, 10)
	m.SetFocused(true)
	tm := startTerm(t, "/bin/sh", "-c", "exit 0")
	m.SetTerminal(tm)
	waitRunning(t, tm, false)
	m.Update(key("tab"))
	m.Update(key("tab"))
	if m.OutputTermCapturing() {
		t.Fatal("an exited terminal must not capture the keyboard")
	}
	m.Update(key("h"))
	if m.col != colVars {
		t.Fatal("h must leave the Output column once the debuggee exited")
	}
}

// TestSetTerminalReplacesAndClosesOld verifies the reuse-across-sessions path:
// embedding a fresh terminal closes the previous one.
func TestSetTerminalReplacesAndClosesOld(t *testing.T) {
	m := New(nil)
	m.SetSize(90, 10)
	old := startTerm(t, "/bin/cat")
	m.SetTerminal(old)
	fresh := startTerm(t, "/bin/cat")
	m.SetTerminal(fresh)
	waitRunning(t, old, false)
	if m.Terminal() != fresh {
		t.Fatal("the fresh terminal must be the embedded one")
	}
	if !fresh.Running() {
		t.Fatal("replacing must not touch the fresh terminal")
	}
}

// TestOutputTermHitAndClickFocus verifies the mouse seams: OutputTermHit maps
// only the Output column's interior, and a click there focuses the column.
func TestOutputTermHitAndClickFocus(t *testing.T) {
	m := New(nil)
	m.SetSize(90, 10)
	tm := startTerm(t, "/bin/cat")
	m.SetTerminal(tm)
	fw, vw, _ := m.colWidths()
	ox := fw + 1 + vw + 1
	if m.OutputTermHit(ox-2, 3) {
		t.Fatal("the variables column is not a terminal hit")
	}
	if m.OutputTermHit(ox+1, 0) {
		t.Fatal("the title row is not a terminal hit")
	}
	if !m.OutputTermHit(ox+1, 3) {
		t.Fatal("the Output column interior must be a terminal hit")
	}
	m.Click(ox+1, 3)
	if m.col != colOutput {
		t.Fatal("a click on the terminal must focus the Output column")
	}
}
