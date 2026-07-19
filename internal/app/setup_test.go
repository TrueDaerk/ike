package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/lang"
)

// finishTourFully pages past the last tour page, which starts the setup flow.
func finishTourFully(t *testing.T, m Model) Model {
	t.Helper()
	for i := 0; i < 5; i++ {
		tm, _ := m.Update(key("l"))
		m = tm.(Model)
	}
	if m.tour != nil {
		t.Fatal("tour must be closed after paging past the last page")
	}
	return m
}

// toolchainStub registers a language with a toolchain capability for the
// duration of the test.
type toolchainStub struct{ path string }

func (s toolchainStub) Detect(string) (map[string]any, bool) { return nil, false }
func (s toolchainStub) Interpreter(string) (string, bool)    { return s.path, s.path != "" }

func toolchainLang(t *testing.T, id, path string) {
	t.Helper()
	lang.Register(lang.Language{ID: id, Extensions: []string{id}, Toolchain: toolchainStub{path: path}})
	t.Cleanup(func() { lang.Register(lang.Language{ID: id}) })
}

func TestTourFinishOpensThemePicker(t *testing.T) {
	m := tourSeed(t)
	m = finishTourFully(t, m)
	if !m.themePickOpen() {
		t.Fatal("finishing the tour must open the theme picker")
	}
	if v := ansi.Strip(m.shell.View()); !strings.Contains(v, "Choose a theme") {
		t.Fatalf("view missing the theme picker: %q", v)
	}
}

func TestTourEscSkipsSetupFlow(t *testing.T) {
	m := tourSeed(t)
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.themePickOpen() || len(m.setupQueue) != 0 {
		t.Fatal("escaping the tour must not start the setup flow")
	}
}

func TestThemePickPreviewAndPersist(t *testing.T) {
	m := tourSeed(t)
	m = finishTourFully(t, m)
	before := m.themePal
	tm, _ := m.Update(key("j"))
	m = tm.(Model)
	if m.themePal == before {
		t.Fatal("moving the cursor must preview the highlighted theme")
	}
	picked := m.themePick.names[m.themePick.cursor]
	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	if m.themePick != nil {
		t.Fatal("enter must close the theme picker")
	}
	if cmd == nil {
		t.Fatal("enter must return the persist command")
	}
	m = drainCmd(m, cmd)
	if got := userSettings(t); !strings.Contains(got, picked) {
		t.Fatalf("enter must persist theme.name=%q, settings:\n%s", picked, got)
	}
}

func TestThemePickEscRestores(t *testing.T) {
	m := tourSeed(t)
	m = finishTourFully(t, m)
	before := m.themePal
	tm, _ := m.Update(key("j"))
	m = tm.(Model)
	if m.themePal == before {
		t.Fatal("moving the cursor must preview the highlighted theme")
	}
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.themePick != nil {
		t.Fatal("esc must close the theme picker")
	}
	// Esc restores the pre-dialog palette and persists nothing.
	if s, err := os.ReadFile(filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "settings.toml")); err == nil &&
		strings.Contains(string(s), "[theme]") {
		t.Fatalf("esc must not persist a theme, settings:\n%s", s)
	}
}

func TestSetupFlowChainsLSPAndToolchain(t *testing.T) {
	onboardLang(t, "setuplang")
	toolchainLang(t, "setuptool", "/usr/bin/setuptool")
	// Not a first run: lsp.onboarded is already set, so the flow's LSP step
	// must force-open the dialog past the gate.
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "settings.toml"), []byte("[lsp]\nonboarded = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm, _ = tm.(Model).Update(ShowWelcomeTourMsg{})
	m = tm.(Model)
	m = finishTourFully(t, m)
	if !m.themePickOpen() {
		t.Fatal("step 1 must be the theme picker")
	}
	// esc keeps the theme and advances to the LSP dialog — forced open even
	// though this is not a first run.
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if !m.onboardingOpen() {
		t.Fatal("step 2 must be the LSP server picker")
	}
	// esc skips installs and advances to the toolchain summary.
	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("onboarding esc must persist lsp.onboarded")
	}
	if !m.toolchainInfoOpen() {
		t.Fatal("step 3 must be the toolchain summary")
	}
	if v := ansi.Strip(m.shell.View()); !strings.Contains(v, "setuptool — /usr/bin/setuptool") {
		t.Fatalf("summary missing the detected interpreter: %q", v)
	}
	tm2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm2.(Model)
	if m.toolchainInfoOpen() || m.shell.IsOpen() || len(m.setupQueue) != 0 {
		t.Fatal("enter must end the setup flow")
	}
}

func TestToolchainSummaryMarksMissing(t *testing.T) {
	toolchainLang(t, "misstool", "")
	m := tourSeed(t)
	m = finishTourFully(t, m)
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // theme: keep
	m = tm.(Model)
	if !m.toolchainInfoOpen() {
		t.Fatal("without LSP recipes the flow must land on the toolchain summary")
	}
	if v := ansi.Strip(m.shell.View()); !strings.Contains(v, "✗ misstool — not found") {
		t.Fatalf("summary must mark the missing toolchain: %q", v)
	}
}
