package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/lang"
)

// onboardLang registers a language carrying an install recipe for the duration
// of the test; the cleanup re-registers the ID bare (no server), which removes
// it from the onboarding offer for later tests in the package.
func onboardLang(t *testing.T, id string) {
	t.Helper()
	lang.Register(lang.Language{ID: id, Extensions: []string{id}, Server: &lang.ServerSpec{
		Language: id,
		Command:  "definitely-missing-" + id + "-server",
		Install:  []string{"true"},
	}})
	t.Cleanup(func() { lang.Register(lang.Language{ID: id}) })
}

// onboardSeed builds a sized app on a fresh (empty) config dir — a first start.
func onboardSeed(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := New()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return tm.(Model)
}

// runOnboardCmd executes the command a dialog answer returned and routes the
// resulting reload back through Update, mirroring the program loop.
func runOnboardCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("answer must return a command")
	}
	msg := cmd()
	if _, ok := msg.(config.ConfigReloadedMsg); !ok {
		t.Fatalf("command must reload the config, got %T", msg)
	}
	tm, _ := m.Update(msg)
	return tm.(Model)
}

func userSettings(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "settings.toml"))
	if err != nil {
		t.Fatalf("user settings file must exist after the dialog: %v", err)
	}
	return string(data)
}

func TestOnboardingShownOnFirstStart(t *testing.T) {
	onboardLang(t, "obgo")
	m := onboardSeed(t)
	if !m.onboardingOpen() {
		t.Fatal("first start must open the onboarding dialog")
	}
	v := m.shell.View()
	if !strings.Contains(v, "obgo") || !strings.Contains(v, "[x]") || !strings.Contains(v, "definitely-missing-obgo-server") {
		t.Fatalf("dialog must list the server pre-checked: %q", v)
	}
}

func TestOnboardingNotShownWhenConfigExists(t *testing.T) {
	onboardLang(t, "obgo")
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "settings.toml"), []byte("[theme]\nname = \"default\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if tm.(Model).onboardingOpen() {
		t.Fatal("an existing user config is not a first start — no dialog")
	}
}

func TestOnboardingNotShownWithoutRecipes(t *testing.T) {
	m := onboardSeed(t)
	if m.onboardingOpen() {
		t.Fatal("no server with an install recipe ⇒ no dialog")
	}
}

func TestOnboardingRespectsAutoInstallOff(t *testing.T) {
	onboardLang(t, "obgo")
	// auto_install=false can only come from the project layer on a first
	// start; that must mean "ask me nothing, install nothing".
	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, ".ike"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".ike", "settings.toml"), []byte("[lsp]\nauto_install = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(proj)
	m := onboardSeed(t)
	if m.onboardingOpen() {
		t.Fatal("lsp.auto_install=false must suppress the dialog")
	}
}

func TestOnboardingEscSkipsAndPersists(t *testing.T) {
	onboardLang(t, "obgo")
	m := onboardSeed(t)
	tm, cmd := m.updateOnboarding(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.onboardingOpen() {
		t.Fatal("esc closes the dialog")
	}
	m = runOnboardCmd(t, m, cmd)
	if s := userSettings(t); !strings.Contains(s, "onboarded = true") {
		t.Fatalf("skip must persist lsp.onboarded: %q", s)
	}
	if strings.Contains(userSettings(t), "enabled = false") {
		t.Fatal("skip must not disable any server")
	}
	// Never shown again: a second startup on the same config dir stays quiet.
	m2 := New()
	tm2, _ := m2.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if tm2.(Model).onboardingOpen() {
		t.Fatal("dialog must not return after being skipped")
	}
}

func TestOnboardingUncheckedServersDisabled(t *testing.T) {
	onboardLang(t, "obgo")
	onboardLang(t, "obpy")
	m := onboardSeed(t)
	// Uncheck everything, then confirm: nothing installs, both servers are
	// written off, and the flag persists.
	m = answer(m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	tm, cmd := m.updateOnboarding(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	if m.onboardingOpen() {
		t.Fatal("enter closes the dialog")
	}
	m = runOnboardCmd(t, m, cmd)
	c, _ := config.Load(config.Discover("."))
	for _, id := range []string{"obgo", "obpy"} {
		if on, ok := c.LSP.Servers[id]["enabled"].(bool); !ok || on {
			t.Fatalf("unchecked server %s must persist enabled=false, got %v", id, c.LSP.Servers[id])
		}
	}
	if !c.LSP.Onboarded {
		t.Fatal("confirm must persist lsp.onboarded")
	}
}

func TestOnboardingToggleAndCursor(t *testing.T) {
	onboardLang(t, "obgo")
	onboardLang(t, "obpy")
	m := onboardSeed(t)
	// Uncheck the first row, move down, keep the second checked.
	m = answer(m, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	v := m.shell.View()
	if !strings.Contains(v, "[ ]") || !strings.Contains(v, "[x]") {
		t.Fatalf("space must uncheck only the highlighted row: %q", v)
	}
	m = answer(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.onboarding.cursor != 1 {
		t.Fatalf("j must move the cursor, at %d", m.onboarding.cursor)
	}
	// Other keys are swallowed by the modal.
	m = answer(m, tea.KeyPressMsg{Code: 'q', Text: "q"})
	if !m.onboardingOpen() {
		t.Fatal("unrelated keys must not close the dialog")
	}
}
