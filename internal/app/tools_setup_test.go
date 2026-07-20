package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/toolcatalog"
)

// writeUserSettings seeds the user settings file in the test config dir.
func writeUserSettings(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "settings.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readUserSettings returns the user settings file, "" when absent.
func readUserSettings(t *testing.T) string {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "settings.toml"))
	return string(data)
}

// catalogStub pins the setup-dialog catalog to fixed entries and fakes PATH
// resolution: names in present resolve, everything else is missing.
func catalogStub(t *testing.T, present []string, entries ...toolcatalog.Entry) {
	t.Helper()
	origCat, origLook := setupCatalog, toolcatalog.LookPath
	setupCatalog = func() []toolcatalog.Entry { return entries }
	set := make(map[string]bool, len(present))
	for _, p := range present {
		set[p] = true
	}
	toolcatalog.LookPath = func(name string) (string, error) {
		if set[name] {
			return "/fake/bin/" + name, nil
		}
		return "", errors.New(name + " not found")
	}
	t.Cleanup(func() { setupCatalog, toolcatalog.LookPath = origCat, origLook })
}

func stubEntry(name string) toolcatalog.Entry {
	return toolcatalog.Entry{
		Name:        name,
		Command:     name + "-bin",
		Placement:   "bottom",
		Description: "Stub tool",
		Recipes:     [][]string{{"fakebrew", "install", name}},
	}
}

// toolSetupSeed builds a sized app on a fresh config dir and opens the dialog
// via its palette message.
func toolSetupSeed(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := New()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm, _ = tm.(Model).Update(ShowToolSetupMsg{})
	return tm.(Model)
}

func TestToolSetupListsOfferedWithInstallState(t *testing.T) {
	installed, missing := stubEntry("insttool"), stubEntry("misstool")
	catalogStub(t, []string{"insttool-bin", "fakebrew"}, installed, missing)
	m := toolSetupSeed(t)
	if !m.toolSetupOpen() {
		t.Fatal("tools.setup must open the dialog")
	}
	v := ansi.Strip(m.shell.View())
	if !strings.Contains(v, "[x] insttool — Stub tool (installed)") {
		t.Fatalf("installed tool must start checked and marked: %q", v)
	}
	if !strings.Contains(v, "[ ] misstool — Stub tool (installs via fakebrew install misstool)") {
		t.Fatalf("missing tool must start unchecked with its recipe: %q", v)
	}
}

func TestToolSetupGateAndConfiguredHidden(t *testing.T) {
	gated := stubEntry("gatedtool")
	gated.Requires = "docker"
	// The gate is applied by toolcatalog.Offered; the dialog seam uses it
	// verbatim, so pin the real function with a catalog of one gated entry.
	origLook := toolcatalog.LookPath
	toolcatalog.LookPath = func(name string) (string, error) { return "", errors.New("nope") }
	t.Cleanup(func() { toolcatalog.LookPath = origLook })
	for _, e := range toolcatalog.Offered() {
		if e.Requires != "" {
			t.Fatalf("gated entry %s offered without its gate", e.Name)
		}
	}

	// Already-configured tools are not offered again: with every catalog
	// entry configured the dialog refuses to open.
	catalogStub(t, nil, stubEntry("donetool"))
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	writeUserSettings(t, "[[tools.custom]]\nname = \"donetool\"\ncommand = \"donetool-bin\"\n")
	m := New()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm, _ = tm.(Model).Update(ShowToolSetupMsg{})
	m = tm.(Model)
	if m.toolSetupOpen() {
		t.Fatal("dialog must not open when every catalog tool is configured")
	}
}

func TestToolSetupConfirmWritesConfigAndInstalls(t *testing.T) {
	installed, missing := stubEntry("insttool"), stubEntry("misstool")
	catalogStub(t, []string{"insttool-bin", "fakebrew"}, installed, missing)
	var ran [][]string
	origRun := toolcatalog.RunInstall
	toolcatalog.RunInstall = func(argv []string) ([]byte, error) {
		ran = append(ran, argv)
		return nil, nil
	}
	t.Cleanup(func() { toolcatalog.RunInstall = origRun })

	m := toolSetupSeed(t)
	// Check the missing tool too (cursor starts on row 0, j moves to it).
	tm, _ := m.Update(key("j"))
	tm, _ = tm.(Model).Update(tea.KeyPressMsg{Text: " ", Code: tea.KeySpace})
	m = tm.(Model)
	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	if m.toolSetupOpen() {
		t.Fatal("enter must close the dialog")
	}
	if cmd == nil {
		t.Fatal("enter with a selection must return the write+install command")
	}
	m = drainCmd(m, cmd)

	s := userSettings(t)
	for _, want := range []string{"insttool", "misstool", "insttool-bin", "placement = \"bottom\""} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings missing %q:\n%s", want, s)
		}
	}
	// The reload re-shapes the palette: both tool commands exist immediately.
	for _, id := range []string{"tool.insttool", "tool.misstool"} {
		if _, ok := m.reg.Command(id); !ok {
			t.Fatalf("command %s must exist after the reload", id)
		}
	}
	// Only the missing tool installs; the failed-lookup afterwards is fine
	// (the toast reports it), but the recipe must have run exactly once.
	if len(ran) != 1 || strings.Join(ran[0], " ") != "fakebrew install misstool" {
		t.Fatalf("expected one install of misstool, ran %v", ran)
	}
}

func TestToolSetupEscWritesNothing(t *testing.T) {
	catalogStub(t, nil, stubEntry("skiptool"))
	m := toolSetupSeed(t)
	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.toolSetupOpen() || cmd != nil {
		t.Fatal("esc must close the dialog without any command")
	}
	if s := readUserSettings(t); strings.Contains(s, "tools") {
		t.Fatalf("esc must not write tools config:\n%s", s)
	}
}
