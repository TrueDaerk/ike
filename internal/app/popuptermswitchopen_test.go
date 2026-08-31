package app

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/project"
)

// popuptermswitchopen_test.go covers terminal.popup_on_switch (#2362): the
// "always-open" mode opens the incoming project's popup terminal after every
// project switch — resuming the parked instance when there is one, spawning a
// fresh shell otherwise — while the "restore" default leaves the open/closed
// state exactly as #1407 restored it.

// userConfig points the user settings layer at a temp file holding content,
// so the config the switch reloads for the incoming root carries it. The
// switch re-runs config.Load through Discover, which honours IKE_CONFIG_DIR.
func userConfig(t *testing.T, content string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IKE_CONFIG_DIR", dir)
}

// alwaysOpenConfig is the user layer both always-open tests run with; the
// auto-save gate stays off so the switch runs unguarded like switchModel's.
func alwaysOpenConfig(t *testing.T) {
	t.Helper()
	userConfig(t, "[terminal]\npopup_on_switch = \"always-open\"\n\n[project]\nauto_save_on_switch = false\n")
}

func TestPopupOnSwitchAlwaysOpenSpawnsFreshPopup(t *testing.T) {
	_, b := twoRoots(t)
	m := switchModel(t)
	if m.popup.inst != nil || m.popup.open {
		t.Fatal("test setup: the model must start without a popup")
	}
	alwaysOpenConfig(t)

	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	if m.popup.inst == nil || !m.popup.open {
		t.Fatal("always-open must open a fresh popup in a project that has none")
	}
	t.Cleanup(func() { m.popup.inst.CloseTerminalTabs() })
	if m.popup.blurred {
		t.Fatal("the auto-opened popup must own the keyboard, like the toggle chord")
	}
	if m.popup.inst.TabCount() != 1 || m.popup.inst.ActiveTerminal() == nil {
		t.Fatalf("the fresh popup must carry one live shell tab, got %d tabs", m.popup.inst.TabCount())
	}
	if !m.popup.inst.ActiveTerminal().Running() {
		t.Fatal("the auto-opened popup's shell must be running")
	}
}

func TestPopupOnSwitchAlwaysOpenResumesParkedPopup(t *testing.T) {
	a, b := twoRoots(t)
	m := openTestPopupWith(t, switchModel(t))
	sess := m.popup.inst.ActiveTerminal().SessionKey()
	// Leave a with the popup closed: under "restore" it would come back closed.
	out, _ := m.Update(TerminalPopupMsg{})
	m = out.(Model)
	if m.popup.open {
		t.Fatal("test setup: the popup must be hidden before switching away")
	}
	alwaysOpenConfig(t)

	out, _ = m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	out, _ = m.Update(project.SwitchProjectMsg{Root: a})
	m = out.(Model)
	if !m.popup.open || m.popup.inst == nil {
		t.Fatal("always-open must open the popup a project was left with closed")
	}
	if got := m.popup.inst.ActiveTerminal().SessionKey(); got != sess {
		t.Fatalf("resumed popup session = %q, want the parked %q — a fresh shell was spawned instead", got, sess)
	}
	if !m.popup.inst.ActiveTerminal().Running() {
		t.Fatal("the resumed popup's parked shell must still be running")
	}
}

func TestPopupOnSwitchRestoreKeepsClosedPopupClosed(t *testing.T) {
	a, b := twoRoots(t)
	m := openTestPopupWith(t, switchModel(t))
	out, _ := m.Update(TerminalPopupMsg{})
	m = out.(Model) // hide a's popup
	// The default mode, spelled out: switching back restores it as left.
	userConfig(t, "[terminal]\npopup_on_switch = \"restore\"\n\n[project]\nauto_save_on_switch = false\n")

	out, _ = m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	if m.popup.open {
		t.Fatal("restore must not open a popup in a project that has none")
	}
	out, _ = m.Update(project.SwitchProjectMsg{Root: a})
	m = out.(Model)
	if m.popup.open {
		t.Fatal("restore must bring the popup back closed, as it was left")
	}
	if m.popup.inst == nil {
		t.Fatal("the parked popup instance must still resume, just hidden")
	}
}

func TestPopupOnSwitchAlwaysOpenRefocusesBlurredPopup(t *testing.T) {
	a, b := twoRoots(t)
	m := openTestPopupWith(t, switchModel(t))
	m.blurPopupLayer()
	alwaysOpenConfig(t)

	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	out, _ = m.Update(project.SwitchProjectMsg{Root: a})
	m = out.(Model)
	if !m.popup.open || m.popup.blurred {
		t.Fatal("always-open must hand the keyboard back to a popup parked blurred")
	}
	t.Cleanup(func() { m.popup.inst.CloseTerminalTabs() })
}
