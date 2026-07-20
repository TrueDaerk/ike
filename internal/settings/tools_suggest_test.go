package settings

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/toolcatalog"
)

// runBatch executes a command that may be a tea.Batch, committing any config
// reload it produces and running every other branch (installs) for its side
// effects.
func runBatch(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	msgs := []tea.Msg{cmd()}
	for len(msgs) > 0 {
		msg := msgs[0]
		msgs = msgs[1:]
		switch m := msg.(type) {
		case tea.BatchMsg:
			for _, c := range m {
				msgs = append(msgs, c())
			}
		case config.ConfigReloadedMsg:
			config.Set(m.Config)
		}
	}
}

// suggestStub pins the suggestion catalog and fakes PATH resolution: names in
// present resolve, everything else is missing.
func suggestStub(t *testing.T, present []string, entries ...toolcatalog.Entry) {
	t.Helper()
	origCat, origLook := suggestionCatalog, toolcatalog.LookPath
	suggestionCatalog = func() []toolcatalog.Entry { return entries }
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
	t.Cleanup(func() { suggestionCatalog, toolcatalog.LookPath = origCat, origLook })
}

func suggestEntry(name string) toolcatalog.Entry {
	return toolcatalog.Entry{
		Name:        name,
		Command:     name + "-bin",
		Placement:   "bottom",
		Description: "Suggested tool",
		Recipes:     [][]string{{"fakebrew", "install", name}},
	}
}

func TestToolsPageSuggestionsListAndAdd(t *testing.T) {
	suggestStub(t, []string{"sugtool-bin"}, suggestEntry("sugtool"))
	p := toolsPage(t)
	p.Update(key("s"))
	if !p.Capturing() {
		t.Fatal("s must open the suggestion picker and capture keys")
	}
	if v := p.View(100, 20); !strings.Contains(v, "sugtool") || !strings.Contains(v, "installed") {
		t.Fatalf("suggestion view = %q", v)
	}
	apply(t, p.Update(key("enter")))
	got := config.Get().Tools.Custom
	if len(got) != 1 || got[0].Name != "sugtool" || got[0].Command != "sugtool-bin" ||
		got[0].Placement != "bottom" {
		t.Fatalf("entries after add = %+v", got)
	}
	if p.Capturing() {
		t.Fatal("add must close the picker")
	}
}

func TestToolsPageSuggestionsExcludeConfigured(t *testing.T) {
	suggestStub(t, nil, suggestEntry("donetool"))
	p := toolsPage(t)
	addTool(t, p, "donetool", "donetool-bin")
	p.Update(key("s"))
	if p.Capturing() {
		t.Fatal("picker must not open when everything is configured")
	}
	if p.note == "" {
		t.Fatal("a note must explain why nothing opened")
	}
}

func TestToolsPageSuggestionsEscBack(t *testing.T) {
	suggestStub(t, nil, suggestEntry("backtool"))
	p := toolsPage(t)
	p.Update(key("s"))
	p.Update(key("esc"))
	if p.Capturing() {
		t.Fatal("esc must close the picker")
	}
	if got := config.Get().Tools.Custom; len(got) != 0 {
		t.Fatalf("esc must not write entries, got %+v", got)
	}
}

func TestToolsPageSuggestionAddInstallsMissing(t *testing.T) {
	// Binary missing, installer present: the add command must carry the
	// install alongside the config write.
	suggestStub(t, []string{"fakebrew"}, suggestEntry("misstool"))
	var ran [][]string
	origRun := toolcatalog.RunInstall
	toolcatalog.RunInstall = func(argv []string) ([]byte, error) {
		ran = append(ran, argv)
		return nil, nil
	}
	t.Cleanup(func() { toolcatalog.RunInstall = origRun })

	p := toolsPage(t)
	p.Update(key("s"))
	cmd := p.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter must return the write+install command")
	}
	runBatch(t, cmd)
	if len(ran) != 1 || strings.Join(ran[0], " ") != "fakebrew install misstool" {
		t.Fatalf("expected one install run, got %v", ran)
	}
	if got := config.Get().Tools.Custom; len(got) != 1 || got[0].Name != "misstool" {
		t.Fatalf("entries after add = %+v", got)
	}
}
