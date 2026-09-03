package app

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/project"
	"ike/internal/terminal"
)

// popuptermscope_test.go covers terminal.popup_scope (#2406): "global" keeps a
// single popup terminal for the whole app — one shell, one scrollback, riding
// across project switches and asked to cd into the new root whenever it is
// idle — while "project" keeps the per-project parking of #1407.

// globalScopeConfig points the user settings layer at a global-scope popup,
// with the auto-save gate off like the other switch tests.
func globalScopeConfig(t *testing.T) {
	t.Helper()
	userConfig(t, "[terminal]\npopup_scope = \"global\"\n\n[project]\nauto_save_on_switch = false\n")
}

// waitIdle blocks until the shell has finished starting up and sits at its
// prompt, the state the follow-along cd is gated on.
func waitIdle(t *testing.T, term *terminal.Model) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if term.Running() && !term.Busy() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the popup shell never reached an idle prompt")
}

// termText is the shell's visible grid, unstyled.
func termText(term *terminal.Model) string { return ansi.Strip(term.View()) }

func TestPopupScopeGlobalSurvivesProjectSwitch(t *testing.T) {
	_, b := twoRoots(t)
	m := openTestPopupWith(t, switchModel(t))
	inst, sess := m.popup.inst, m.popup.inst.ActiveTerminal().SessionKey()
	globalScopeConfig(t)

	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	if m.popup.inst != inst {
		t.Fatal("a global popup must ride across the switch, not park with the project")
	}
	if got := m.popup.inst.ActiveTerminal().SessionKey(); got != sess {
		t.Fatalf("popup session after the switch = %q, want the carried %q", got, sess)
	}
	if !m.popup.open {
		t.Fatal("a global popup left open must still be open in the new project")
	}
	if !m.popup.inst.ActiveTerminal().Running() {
		t.Fatal("the carried popup's shell must keep running")
	}
	// The parked workspace holds no popup at all, so its teardown can never
	// end the shell the whole app shares.
	parked := m.ws.Peek(m.ws.Background()[0])
	if extras, ok := parked.Aux.(wsExtras); ok && extras.popup.inst != nil {
		t.Fatal("a global popup must not park in the departing workspace's Aux")
	}
}

// TestPopupScopeGlobalCdsIdleShell: the carried shell is asked to cd into the
// new project root when it sits idle at its prompt.
func TestPopupScopeGlobalCdsIdleShell(t *testing.T) {
	_, b := twoRoots(t)
	m := openTestPopupWith(t, switchModel(t))
	term := m.popup.inst.ActiveTerminal()
	waitIdle(t, term)
	globalScopeConfig(t)

	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(termText(term), "cd '") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the idle popup shell must be sent a cd line; grid was:\n%s", termText(term))
}

// TestPopupScopeGlobalKeepsBusyShell: a shell with a foreground job owns its
// stdin, so the cd line is withheld — it would land in the job, not the shell.
func TestPopupScopeGlobalKeepsBusyShell(t *testing.T) {
	_, b := twoRoots(t)
	m := openTestPopupWith(t, switchModel(t))
	term := m.popup.inst.ActiveTerminal()
	waitIdle(t, term)
	term.SendLine("sleep 5")
	busy := time.Now().Add(10 * time.Second)
	for time.Now().Before(busy) && !term.Busy() {
		time.Sleep(20 * time.Millisecond)
	}
	if !term.Busy() {
		t.Fatal("test setup: the foreground job never took the shell")
	}
	globalScopeConfig(t)

	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	// Give a wrongly-sent line time to be echoed before concluding.
	time.Sleep(300 * time.Millisecond)
	if strings.Contains(termText(term), "cd '") {
		t.Fatalf("a busy shell must not be sent the cd line; grid was:\n%s", termText(term))
	}
}

// TestPopupScopeProjectStillParks: the default scope is untouched by #2406 —
// the popup parks with its project and the new one starts empty (#1407).
func TestPopupScopeProjectStillParks(t *testing.T) {
	_, b := twoRoots(t)
	m := openTestPopupWith(t, switchModel(t))
	userConfig(t, "[terminal]\npopup_scope = \"project\"\n\n[project]\nauto_save_on_switch = false\n")

	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	if m.popup.inst != nil || m.popup.open {
		t.Fatal("with the project scope the incoming project must start with an empty popup")
	}
	parked := m.ws.Peek(m.ws.Background()[0])
	if extras, ok := parked.Aux.(wsExtras); !ok || extras.popup.inst == nil {
		t.Fatal("with the project scope the popup must park in the workspace's Aux")
	}
}
