package settings

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
)

// lsp_page_test.go covers the Language Servers settings page (#130): row
// rendering from live config, override write + reload, per-server enable, the
// master switch, restart dispatch, and the missing-binary detail.

// lspPageFixture registers a test language with a server and builds the page
// bound to a throwaway project config.
func lspPageFixture(t *testing.T) (*LSPPage, config.Options, *[]string) {
	t.Helper()
	restoreConfig(t)
	lang.Register(lang.Language{ID: "lsptest", Extensions: []string{"lsptest"},
		Server: &lang.ServerSpec{Command: "fake-ls", Args: []string{"--stdio"}}})
	opts := config.Options{
		UserPath:    filepath.Join(t.TempDir(), "settings.toml"),
		ProjectRoot: t.TempDir(),
	}
	c, _ := config.Load(opts)
	config.Set(c)

	var restarts []string
	p := NewLSPPage(opts,
		func() []string { return []string{"lsptest"} },
		func() tea.Cmd { restarts = append(restarts, "*"); return nil },
		func(id string) tea.Cmd {
			restarts = append(restarts, id)
			return func() tea.Msg {
				return ilsp.ServerStatusMsg{Lang: id, Text: id + " language server restarted", Kind: ilsp.ServerEventInfo}
			}
		})
	for i, l := range p.servers() {
		if l.ID == "lsptest" {
			p.sel = i
		}
	}
	return p, opts, &restarts
}

// drainLSP executes a command tree, feeding config reloads into Set and
// status messages into the page.
func drainLSP(t *testing.T, p *LSPPage, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	switch m := cmd().(type) {
	case config.ConfigReloadedMsg:
		config.Set(m.Config)
	case ilsp.ServerStatusMsg:
		p.Receive(m)
	case tea.BatchMsg:
		for _, c := range m {
			drainLSP(t, p, c)
		}
	}
}

func TestLSPPageRendersRowFromConfig(t *testing.T) {
	p, _, _ := lspPageFixture(t)
	v := p.View(120, 40)
	if !strings.Contains(v, "lsptest") || !strings.Contains(v, "fake-ls --stdio") {
		t.Fatalf("row must show the language and its effective command line:\n%s", v)
	}
	if !strings.Contains(v, "ready") {
		t.Fatalf("a running server must render ready:\n%s", v)
	}
	if !strings.Contains(v, "@built-in") {
		t.Fatalf("no override must render the built-in source:\n%s", v)
	}
	if !strings.Contains(v, "LSP master switch: on") {
		t.Fatalf("the master switch line is missing:\n%s", v)
	}
}

func TestLSPPageCommandOverrideWritesProjectConfig(t *testing.T) {
	p, opts, _ := lspPageFixture(t)
	p.Update(key("c"))
	if !p.Capturing() {
		t.Fatal("the command editor must capture keys")
	}
	// Prefilled with the baseline; replace it wholesale.
	p.input = "custom-ls"
	drainLSP(t, p, p.Update(key("enter")))

	if got := config.Get().LSP.Servers["lsptest"]["command"]; got != "custom-ls" {
		t.Fatalf("command override = %v, want custom-ls", got)
	}
	if got := config.Origin(opts, "lsp.servers.lsptest.command"); got != "project" {
		t.Fatalf("override origin = %q, want project", got)
	}
	if v := p.View(120, 40); !strings.Contains(v, "custom-ls") || !strings.Contains(v, "@project") {
		t.Fatalf("view must show the override and its layer:\n%s", v)
	}
}

func TestLSPPageArgsAndSettingsOverrides(t *testing.T) {
	p, _, _ := lspPageFixture(t)
	p.Update(key("a"))
	p.input = "--stdio --verbose"
	drainLSP(t, p, p.Update(key("enter")))
	if v := p.View(120, 40); !strings.Contains(v, "--verbose") {
		t.Fatalf("args override must reach the effective command line:\n%s", v)
	}

	p.Update(key("s"))
	p.input = `{"telemetry":false}`
	drainLSP(t, p, p.Update(key("enter")))
	m, ok := config.Get().LSP.Servers["lsptest"]["settings"].(map[string]any)
	if !ok || m["telemetry"] != false {
		t.Fatalf("settings override = %v, want map with telemetry=false", config.Get().LSP.Servers["lsptest"]["settings"])
	}

	// Invalid JSON is rejected without leaving the editor.
	p.Update(key("s"))
	p.input = "not-json"
	p.Update(key("enter"))
	if !p.Capturing() || p.invalid == "" {
		t.Fatal("invalid JSON must be rejected in place")
	}
	p.Update(key("esc"))
}

func TestLSPPageEnableTogglesAndReset(t *testing.T) {
	p, _, _ := lspPageFixture(t)
	drainLSP(t, p, p.Update(key("e")))
	if serverOn("lsptest") {
		t.Fatal("e must disable the selected server")
	}
	if v := p.View(120, 40); !strings.Contains(v, "disabled") {
		t.Fatalf("a disabled server must render as disabled:\n%s", v)
	}

	drainLSP(t, p, p.Update(key("E")))
	if masterEnabled() {
		t.Fatal("E must flip the master switch")
	}
	if v := p.View(120, 40); !strings.Contains(v, "off (master)") {
		t.Fatalf("rows must show the master off state:\n%s", v)
	}
	drainLSP(t, p, p.Update(key("E"))) // back on

	// x resets every override; the per-server disable goes with it.
	drainLSP(t, p, p.Update(key("x")))
	if !serverOn("lsptest") {
		t.Fatal("x must clear the per-server disable")
	}
	if got := config.Get().LSP.Servers["lsptest"]["command"]; got != nil {
		t.Fatalf("x must clear overrides, command = %v", got)
	}
}

func TestLSPPageRestartDispatch(t *testing.T) {
	p, _, restarts := lspPageFixture(t)
	drainLSP(t, p, p.Update(key("r")))
	drainLSP(t, p, p.Update(tea.KeyPressMsg{Text: "R", Code: 'R', Mod: tea.ModShift}))
	if len(*restarts) != 2 || (*restarts)[0] != "lsptest" || (*restarts)[1] != "*" {
		t.Fatalf("restart dispatch = %v, want [lsptest *]", *restarts)
	}
}

func TestLSPPageMissingBinaryDetail(t *testing.T) {
	p, _, _ := lspPageFixture(t)
	p.running = func() []string { return nil }
	p.Receive(ilsp.ServerStatusMsg{Lang: "lsptest",
		Text: "fake-ls not found (LSP disabled for this language)", Kind: ilsp.ServerState})
	v := p.View(120, 40)
	if !strings.Contains(v, "missing") {
		t.Fatalf("a not-found status must render as missing:\n%s", v)
	}
	if !strings.Contains(v, "fake-ls not found") || !strings.Contains(v, "install helper: #131") {
		t.Fatalf("the failure reason and install hint must render:\n%s", v)
	}
}
