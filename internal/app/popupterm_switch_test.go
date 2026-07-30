package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/project"
)

// popupterm_switch_test.go — the popup terminal is per-project state (#1407):
// it parks with the workspace on a seamless switch (#777), resumes unchanged,
// counts as activity in the close/evict guards, and its sessions end when the
// workspace is torn down or the app quits.

// twoRoots builds two project directories and chdirs into the first.
func twoRoots(t *testing.T) (a, b string) {
	t.Helper()
	base := t.TempDir()
	a, b = filepath.Join(base, "a"), filepath.Join(base, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(a)
	return a, b
}

func TestPopupParksWithWorkspaceAndResumes(t *testing.T) {
	_, b := twoRoots(t)
	m := openTestPopupWith(t, switchModel(t))
	sess := m.popup.inst.ActiveTerminal().SessionKey()

	// Switch away with the popup open: the new project starts with its own
	// zero popup — nothing renders, keys route normally.
	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	if m.popup.inst != nil || m.popup.open {
		t.Fatal("the new project must start with an empty popup")
	}
	if v := m.render(); strings.Contains(v, "POPUP TERMINAL") {
		t.Fatal("the parked popup must not render in the new project")
	}
	// The parked workspace carries the popup in Aux.
	parked := m.ws.Peek(m.ws.Background()[0])
	extras, ok := parked.Aux.(wsExtras)
	if !ok || extras.popup.inst == nil || !extras.popup.open {
		t.Fatal("the popup must park in the workspace's Aux, open state preserved")
	}
	if !extras.popup.inst.ActiveTerminal().Running() {
		t.Fatal("parked popup sessions must keep running")
	}

	// The new project's popup is its own instance, isolated from the parked one.
	m = openTestPopupWith(t, m)
	if m.popup.inst.ActiveTerminal().SessionKey() == sess {
		t.Fatal("the new project's popup must not reuse the parked instance")
	}
	out, _ = m.Update(TerminalPopupMsg{})
	m = out.(Model) // hide b's popup again

	// Switch back: same instance, same session, still open.
	out, _ = m.Update(project.SwitchProjectMsg{Root: m.ws.Background()[0]})
	m = out.(Model)
	if m.popup.inst == nil || !m.popup.open {
		t.Fatal("switching back must restore the popup as left (open)")
	}
	if got := m.popup.inst.ActiveTerminal().SessionKey(); got != sess {
		t.Fatalf("restored popup session = %q, want %q", got, sess)
	}
	if v := m.render(); !strings.Contains(v, "POPUP TERMINAL") {
		t.Fatal("the restored open popup must render again")
	}
}

func TestQuitClosesParkedPopupSessions(t *testing.T) {
	_, b := twoRoots(t)
	m := openTestPopupWith(t, switchModel(t))
	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	parked := m.ws.Peek(m.ws.Background()[0])
	term := parked.Aux.(wsExtras).popup.inst.ActiveTerminal()
	if !term.Running() {
		t.Fatal("test setup: parked popup shell must be running")
	}
	if _, cmd := m.quit(); cmd == nil {
		t.Fatal("quit must return the exit command")
	}
	if term.Running() {
		t.Fatal("quit must end parked workspaces' popup sessions too")
	}
}

func TestWorkspaceTeardownEndsPopupSessions(t *testing.T) {
	_, b := twoRoots(t)
	m := openTestPopupWith(t, switchModel(t))
	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	root := m.ws.Background()[0]
	parked := m.ws.Peek(root)
	term := parked.Aux.(wsExtras).popup.inst.ActiveTerminal()

	// Closing the parked workspace from the list (#820) counts the popup
	// shell as activity, so the guard asks first; d tears it down.
	out, _ = m.Update(project.CloseWorkspaceMsg{Path: root})
	m = out.(Model)
	if !m.wsClosePromptOpen() {
		t.Fatal("a parked workspace with a running popup shell must open the close guard")
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	m = out.(Model)
	if m.ws.Peek(root) != nil {
		t.Fatal("d must drop the workspace")
	}
	if term.Running() {
		t.Fatal("workspace teardown must end its popup sessions")
	}
}

func TestProjectCloseGuardCountsPopup(t *testing.T) {
	_, b := twoRoots(t)
	m := switchModel(t)
	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	m = openTestPopupWith(t, m) // active project's popup, shell running

	out, _ = m.Update(project.CloseProjectMsg{})
	m = out.(Model)
	if m.projectClosePending == nil {
		t.Fatal("project.close with a running popup shell must ask first")
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.projectClosePending != nil {
		t.Fatal("esc must cancel the close")
	}
}
