package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/backup"
	"ike/internal/config"
)

// firstRunSeed re-enables the first-run tour scan (TestMain turns it off for
// the rest of the package) and builds a sized app on a fresh config dir,
// returning the model and the startup command from the size message.
func firstRunSeed(t *testing.T) (Model, tea.Cmd) {
	t.Helper()
	tourAutoOpen = true
	t.Cleanup(func() { tourAutoOpen = false })
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := New()
	tm, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return tm.(Model), cmd
}

// runReload executes a returned command and feeds the config reload back
// through Update, mirroring the program loop.
func runReload(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a config-write command")
	}
	msg := cmd()
	if _, ok := msg.(config.ConfigReloadedMsg); !ok {
		t.Fatalf("command must reload the config, got %T", msg)
	}
	tm, _ := m.Update(msg)
	return tm.(Model)
}

func TestFirstRunTourThenLSPDialogThenNeverAgain(t *testing.T) {
	onboardLang(t, "frgo")
	m, cmd := firstRunSeed(t)
	if !m.tourOpen() {
		t.Fatal("a first start must open the welcome tour")
	}
	if m.onboardingOpen() {
		t.Fatal("the LSP dialog must wait behind the tour")
	}
	// The flag persists on OPEN — a mid-tour quit must not re-trigger the tour.
	m = runReload(t, m, cmd)
	if s := userSettings(t); !strings.Contains(s, "onboarded = true") {
		t.Fatalf("opening the tour must persist ui.onboarded: %q", s)
	}

	// Closing the tour hands the shell to the queued LSP dialog.
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.tourOpen() {
		t.Fatal("esc must close the tour")
	}
	if !m.onboardingOpen() {
		t.Fatal("the LSP dialog must open right after the tour closes")
	}

	// A second launch on the same config dir: no tour (flag set), but the LSP
	// dialog is still due — the mid-tour-quit path must not suppress it.
	m2 := New()
	tm2, _ := m2.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if tm2.(Model).tourOpen() {
		t.Fatal("the tour must not return once ui.onboarded is set")
	}
	if !tm2.(Model).onboardingOpen() {
		t.Fatal("an unanswered LSP dialog must survive the tour's config write")
	}
}

func TestNoTourWhenConfigExists(t *testing.T) {
	tourAutoOpen = true
	t.Cleanup(func() { tourAutoOpen = false })
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "settings.toml"), []byte("[theme]\nname = \"default\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if tm.(Model).tourOpen() {
		t.Fatal("an existing user config is not a first start — no tour")
	}
}

func TestRecoveryPromptWinsOverTour(t *testing.T) {
	m, cmd := firstRunSeed(t)
	if !m.tourOpen() {
		t.Fatal("setup: tour open")
	}
	// Simulate the contested startup: recovery snapshots pending. (The seed
	// has none, so re-stage the race on a fresh model state.)
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // clear the shell
	m = tm.(Model)
	_ = cmd
	m.tourPending = true
	m.recoveryPending = []backup.Snapshot{{Key: "k", Path: "f.txt"}}
	m.maybeOpenRecovery()
	if cmd := m.maybeOpenTour(); cmd != nil || m.tourOpen() {
		t.Fatal("the tour must wait while the recovery prompt holds the shell")
	}
	// Resolving recovery frees the shell and the tour follows.
	tm, tourCmd := m.updateRecovery(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if !m.tourOpen() {
		t.Fatal("closing recovery must open the waiting tour")
	}
	if tourCmd == nil {
		t.Fatal("the tour open must carry the ui.onboarded write")
	}
}
