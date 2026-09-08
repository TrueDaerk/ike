package app

import (
	"testing"

	"ike/internal/pane"
)

// termrerun_test.go covers #2543: terminal.rerunLast — which shell the re-run
// lands in, that the keyboard stays put, and the prompt gate.

func TestRerunLastCommandRegisteredAndBound(t *testing.T) {
	m := newSized()
	if _, ok := m.reg.Command("terminal.rerunLast"); !ok {
		t.Fatal("terminal.rerunLast must be registered")
	}
	if _, ok := m.playChordFor("terminal.rerunLast"); !ok {
		t.Fatal("terminal.rerunLast should ship with a default keybind")
	}
}

// TestRerunLastTargetsOpenPopup: with the popup open (blurred — the state the
// command is used from) its focused tab is the target, and the re-run leaves
// the keyboard in the editor.
func TestRerunLastTargetsOpenPopup(t *testing.T) {
	m := openTestPopupWith(t, sendApp(t, "echo hello\n"))
	m.blurPopupLayer()

	want := m.popup.inst.ActiveTerminal()
	got, hidden := m.terminalRerunTarget()
	if got != want {
		t.Fatalf("target = %p, want the popup's focused tab %p", got, want)
	}
	if hidden {
		t.Fatal("an open popup is not hidden")
	}
	if !want.Running() {
		t.Skip("popup shell did not spawn (no PTY available); target resolution above is what this test guards")
	}
	if res := m.rerunLastInTerminal(); res != termRerunOK {
		t.Fatalf("result = %v, want termRerunOK", res)
	}
	if !m.popup.blurred {
		t.Fatal("re-running in an already-open popup must not steal the keyboard")
	}
}

// TestRerunLastTargetsFocusedTerminalPane: no popup, a focused terminal pane
// — the re-run goes there instead of opening an overlay over it.
func TestRerunLastTargetsFocusedTerminalPane(t *testing.T) {
	m := sendApp(t, "echo hello\n")
	m = dispatch(t, m, TerminalToggleMsg{})
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatal("setup: terminal.toggle should focus a fresh terminal pane")
	}
	t.Cleanup(func() { inst.Terminal().Close() })

	got, hidden := m.terminalRerunTarget()
	if got != inst.ActiveTerminal() || hidden {
		t.Fatal("target should be the focused terminal pane's shell, not hidden")
	}
	if m.popup.open {
		t.Fatal("a live terminal pane must not cause the popup to open")
	}
}

// TestRerunLastUsesHiddenPopupAndArmsIndicator is the headline case: the
// popup was used and hidden again — the re-run goes into that shell without
// showing the layer, and the statusbar activity indicator (#2309) is armed so
// the command leaves a trace.
func TestRerunLastUsesHiddenPopupAndArmsIndicator(t *testing.T) {
	m := openTestPopupWith(t, sendApp(t, "echo hello\n"))
	want := m.popup.inst.ActiveTerminal()
	m = dispatch(t, m, TerminalPopupMsg{}) // hide, shells retained
	if m.popup.open {
		t.Fatal("setup: the popup should be hidden")
	}
	m.popupUnseen = false

	got, hidden := m.terminalRerunTarget()
	if got != want || !hidden {
		t.Fatalf("target = %p hidden=%v, want the retained popup shell %p hidden=true", got, hidden, want)
	}
	if !want.Running() {
		t.Skip("popup shell did not spawn (no PTY available); target resolution above is what this test guards")
	}
	if res := m.rerunLastInTerminal(); res != termRerunOK {
		t.Fatalf("result = %v, want termRerunOK", res)
	}
	if m.popup.open {
		t.Fatal("re-running in the hidden popup must not show it")
	}
	if !m.popupUnseen {
		t.Fatal("a re-run in the hidden popup should arm the activity indicator")
	}
}

// TestRerunLastOpensPopupAsLastResort: no shell anywhere — the popup is
// opened for the purpose.
func TestRerunLastOpensPopupAsLastResort(t *testing.T) {
	m := sendApp(t, "echo hello\n")
	if m.popup.open || m.popup.inst != nil {
		t.Fatal("setup: the popup should start absent")
	}
	m = dispatch(t, m, TerminalRerunLastMsg{})
	if !m.popup.open || m.popup.inst == nil {
		t.Fatal("with no shell around, the re-run should open the popup terminal")
	}
	t.Cleanup(func() { m.popup.inst.CloseTerminalTabs() })
	if m.popup.inst.ActiveTerminal() == nil {
		t.Fatal("the opened popup should carry a live shell")
	}
}

// TestRerunLastRefusesBusyShell is the prompt gate (#1340 reused): a terminal
// running a program is not typed into — Up + Enter would land in the
// program — and the caller is told the shell is busy.
func TestRerunLastRefusesBusyShell(t *testing.T) {
	m := sendApp(t, "echo hello\n")
	m = dispatch(t, m, TerminalToggleMsg{})
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatal("setup: terminal.toggle should focus a fresh terminal pane")
	}
	term := inst.Terminal()
	term.StartCommand("busy", []string{"/bin/sh", "-c", "sleep 5"}, t.TempDir(), nil)
	t.Cleanup(term.Close)
	if !term.Running() {
		t.Skip("command session did not spawn (no PTY available)")
	}

	if res := m.rerunLastInTerminal(); res != termRerunBusy {
		t.Fatalf("result = %v, want termRerunBusy", res)
	}
	if m.popup.open {
		t.Fatal("a refused re-run must not open the popup")
	}
}

// TestRerunLastIgnoresToolPanes guards the #741/#772 rule: a custom tool pane
// is not the user's shell, so the re-run opens the popup rather than typing
// into the tool.
func TestRerunLastIgnoresToolPanes(t *testing.T) {
	m := sendApp(t, "echo hello\n")
	m = dispatch(t, m, TerminalToggleMsg{})
	inst := m.activeWS().Panes.FocusedInstance()
	t.Cleanup(func() { inst.Terminal().Close() })
	inst.Terminal().SetTool("lazygit")

	target, _ := m.terminalRerunTarget()
	t.Cleanup(func() {
		if m.popup.inst != nil {
			m.popup.inst.CloseTerminalTabs()
		}
	})
	if !m.popup.open {
		t.Fatal("a tool pane is not a re-run target — the popup should have opened")
	}
	if inst.ActiveTerminal() == target {
		t.Fatal("the re-run must not go to a tool pane's shell")
	}
}
