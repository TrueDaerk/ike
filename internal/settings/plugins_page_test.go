package settings

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/config"
	"ike/internal/lang"
)

func init() {
	// The lang.go shim resolves its details from the lang registry; register
	// a minimal Go language directly (importing the real language package
	// would cycle: plugins -> plugin -> settings).
	lang.Register(lang.Language{
		ID:         "go",
		Extensions: []string{"go"},
		Server: &lang.ServerSpec{
			Language: "go",
			Command:  "gopls",
			Install:  []string{"go", "install", "golang.org/x/tools/gopls@latest"},
		},
	})
}

func pluginsFixture(onToggle func(string, bool) tea.Cmd) *PluginsPage {
	list := func() []PluginInfo {
		return []PluginInfo{
			{ID: "zeta", Enabled: true, Commands: []string{"zeta.run"}, Panes: 1},
			{ID: "example", Enabled: false, Hooks: 2},
			{ID: "lang-go", Enabled: true},
		}
	}
	return NewPluginsPage(config.Options{}, list, onToggle)
}

func TestPluginsPageListsSortedWithState(t *testing.T) {
	p := pluginsFixture(nil)
	v := ansi.Strip(p.View(120, 40))
	for _, want := range []string{"example", "disabled", "2 hooks", "zeta", "1 command · 1 pane", "lang-go", "language go", "server gopls"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}
	// Sorted by id: example < lang.go < zeta.
	if strings.Index(v, "example") > strings.Index(v, "lang-go") || strings.Index(v, "lang-go") > strings.Index(v, "zeta") {
		t.Fatal("rows should sort by id")
	}
}

func TestPluginsPageToggleRoundTrip(t *testing.T) {
	var gotID string
	var gotEnable bool
	p := pluginsFixture(func(id string, enable bool) tea.Cmd {
		gotID, gotEnable = id, enable
		return nil
	})
	// Row 0 is "example" (disabled) after sorting; e should enable it.
	p.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	if gotID != "example" || gotEnable != true {
		t.Fatalf("toggle = %q %v", gotID, gotEnable)
	}
	// Move to zeta (enabled) and toggle off.
	p.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	p.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	p.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	if gotID != "zeta" || gotEnable != false {
		t.Fatalf("toggle = %q %v", gotID, gotEnable)
	}
}

func TestPluginsPageInspectExpands(t *testing.T) {
	p := pluginsFixture(nil)
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	v := ansi.Strip(p.View(120, 40))
	if !strings.Contains(v, "hooks") {
		t.Fatalf("view = %s", v)
	}
	// lang-go row expands to language details.
	p.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	v = ansi.Strip(p.View(120, 40))
	for _, want := range []string{"extensions: go", "server: gopls", "install: go install"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}
}
