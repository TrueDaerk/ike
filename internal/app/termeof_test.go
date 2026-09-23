package app

// termeof_test.go covers #2701: ctrl+d at an idle prompt is the shell's EOF,
// not a missing keybind. The chord forwards to the pty, the shell exits, the
// tab hosting it closes like a dedicated shell pane does, and nothing about
// the press reaches the usage log as an "unbound" chord.

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/keymap"
	"ike/internal/pane"
	"ike/internal/telemetry"
	"ike/internal/terminal"
)

// ctrlD is the EOF chord the shell owns on every platform.
var ctrlD = tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}

// TestTerminalUnboundChordReachesPty: a terminal-context key press with no
// binding is written to the pty. ctrl+d is the case from the telemetry — it
// ends the idle shell, which is only observable if the byte got there.
func TestTerminalUnboundChordReachesPty(t *testing.T) {
	m, key := openTestTerminal(t)
	term := m.activeWS().Panes.Get(key).Terminal()
	waitIdle(t, term)
	if _, ok := m.bindings.Table().Lookup(keymap.MustParseChord("ctrl+d"), keymap.Terminal); ok {
		t.Fatal("precondition: ctrl+d must have no terminal-context binding")
	}

	m = dismissOnboarding(m)
	out, _ := m.Update(ctrlD)
	m = out.(Model)
	// The first EOF can race a shell that has just reached its prompt under
	// a loaded full-suite run (#1008): redeliver while waiting.
	deadline := time.Now().Add(15 * time.Second)
	for term.Running() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		if term.Running() && !term.Busy() {
			out, _ = m.Update(ctrlD)
			m = out.(Model)
		}
	}
	if term.Running() {
		t.Fatal("ctrl+d must reach the pty and end the idle shell")
	}
}

// TestTerminalUnboundChordNotRecorded: the exited read-only view (#1951) is
// still the terminal context, and a chord pressed there is late shell input,
// not an expected-but-missing keybind. The telemetry this issue came from
// recorded exactly that.
func TestTerminalUnboundChordNotRecorded(t *testing.T) {
	m, key := openTestTerminal(t)
	term := m.activeWS().Panes.Get(key).Terminal()
	waitIdle(t, term)
	term.SendEOF()
	waitExited(t, term)
	if m.focusedDeadTerminal() == nil {
		t.Fatal("precondition: the focused pane must hold the exited session")
	}
	if got := m.focusContext(); got != string(keymap.Terminal) {
		t.Fatalf("precondition: focus context = %q, want terminal", got)
	}

	m = drainKey(m, ctrlD)
	if m.pendUnbound != nil {
		t.Fatalf("ctrl+d left a pending unbound event: %+v", *m.pendUnbound)
	}
	for _, ev := range usageEvents(t, m) {
		if ev.Type != telemetry.TypeKey {
			continue
		}
		if ev.Data["status"] == telemetry.KeyStatusUnbound && ev.Data["context"] == string(keymap.Terminal) {
			t.Fatalf("a terminal chord recorded unbound: %+v", ev.Data)
		}
	}
	_ = key
}

// TestShellTabClosesOnExit: a shell hosted as a terminal tab (#573, #729)
// closes when its session ends, the way a dedicated shell pane does — before
// #2701 the dead tab lingered and had to be closed by hand.
func TestShellTabClosesOnExit(t *testing.T) {
	m, _ := openTestTerminal(t)
	// cmd+t converts the terminal pane into a tab host and adds a sibling
	// shell (#729, #983); the new tab is the focused one.
	handled, out, _ := m.terminalReservedKey("cmd+t")
	if !handled {
		t.Fatal("setup: cmd+t must be reserved while a terminal is focused")
	}
	m = out.(Model)
	inst := m.activeWS().Panes.Get(m.activeWS().Panes.Focused())
	if inst == nil || inst.Kind() != pane.KindEditor || inst.TabCount() != 2 {
		t.Fatalf("setup: expected a two-tab terminal host, got %+v", inst)
	}
	t.Cleanup(inst.CloseTerminalTabs)
	sess := inst.ActiveTerminal().SessionKey()

	out, _ = m.Update(terminal.ExitedMsg{Key: sess})
	m = out.(Model)
	if inst.TabCount() != 1 {
		t.Fatalf("the finished shell tab should have closed, tabs = %d", inst.TabCount())
	}
	if rest := inst.TabTerminal(0); rest == nil || rest.SessionKey() == sess {
		t.Fatal("the surviving tab must be the other shell")
	}
}

// TestCommandTabSurvivesExit: a command session (#576) keeps its output when
// it ends — the close on EOF is for plain shells only.
func TestCommandTabSurvivesExit(t *testing.T) {
	m := newSized()
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindEditor {
		t.Skip("no editor pane to host the run")
	}
	term := deadTermTab(t, m, inst, "tabrun")
	t.Cleanup(inst.CloseTerminalTabs)
	tabs := inst.TabCount()

	out, _ := m.Update(terminal.ExitedMsg{Key: term.SessionKey()})
	m = out.(Model)
	if inst.TabCount() != tabs {
		t.Fatalf("a finished command tab must stay open, tabs = %d, want %d", inst.TabCount(), tabs)
	}
}
