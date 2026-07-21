package settings

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
)

func toolsPage(t *testing.T) *ToolsPage {
	t.Helper()
	restoreConfig(t)
	return NewToolsPage(testOpts(t))
}

// typeText feeds a string into the page rune by rune.
func typeText(p *ToolsPage, s string) {
	for _, r := range s {
		p.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
}

// addTool drives the full add flow: open form, fill name/command, save.
func addTool(t *testing.T, p *ToolsPage, name, command string) {
	t.Helper()
	p.Update(key("a"))
	if !p.Capturing() {
		t.Fatal("a must open the form and capture keys")
	}
	typeText(p, name)
	p.Update(key("tab"))
	typeText(p, command)
	apply(t, p.Update(key("enter")))
}

func TestToolsPageAddWritesAndReloads(t *testing.T) {
	p := toolsPage(t)
	addTool(t, p, "lazygit", "lazygit")
	got := config.Get().Tools.Custom
	if len(got) != 1 || got[0].Name != "lazygit" || got[0].Command != "lazygit" {
		t.Fatalf("entries after add = %+v", got)
	}
	if p.Capturing() {
		t.Fatal("save must close the form")
	}
}

func TestToolsPageAddAllFields(t *testing.T) {
	p := toolsPage(t)
	p.Update(key("a"))
	typeText(p, "sq")
	p.Update(key("tab"))
	typeText(p, "sqlit")
	p.Update(key("tab"))
	typeText(p, "--db dev.sqlite")
	p.Update(key("tab"))
	typeText(p, "./data")
	p.Update(key("tab"))
	typeText(p, "right")
	apply(t, p.Update(key("enter")))
	got := config.Get().Tools.Custom
	if len(got) != 1 {
		t.Fatalf("entries = %+v", got)
	}
	e := got[0]
	if e.Command != "sqlit" || len(e.Args) != 2 || e.Args[0] != "--db" ||
		e.Cwd != "./data" || e.Placement != "right" {
		t.Fatalf("entry = %+v", e)
	}
}

// TestToolsPageMultipleField (#835): the sixth form field sets Multiple,
// round-trips through edit, and rejects non-boolean input.
func TestToolsPageMultipleField(t *testing.T) {
	p := toolsPage(t)
	p.Update(key("a"))
	typeText(p, "claude")
	p.Update(key("tab"))
	typeText(p, "claude")
	for i := 0; i < 4; i++ { // args, cwd, placement → multiple
		p.Update(key("tab"))
	}
	typeText(p, "maybe")
	p.Update(key("enter"))
	if !p.Capturing() || !strings.Contains(p.note, "multiple must be true or false") {
		t.Fatalf("invalid multiple must fail validation, note=%q", p.note)
	}
	for range "maybe" {
		p.Update(key("backspace"))
	}
	typeText(p, "true")
	apply(t, p.Update(key("enter")))
	got := config.Get().Tools.Custom
	if len(got) != 1 || !got[0].Multiple {
		t.Fatalf("entries = %+v, want Multiple=true", got)
	}
	// Edit seeds "true" back into the form.
	p.sel = 0
	p.Update(key("enter"))
	if p.form[5] != "true" {
		t.Fatalf("edit must seed multiple, form=%v", p.form)
	}
}

func TestToolsPageEditRoundTrip(t *testing.T) {
	p := toolsPage(t)
	addTool(t, p, "htop", "htop")
	p.sel = 0
	p.Update(key("enter")) // edit
	if !p.Capturing() || p.form[0] != "htop" {
		t.Fatalf("edit must seed the form, form=%v", p.form)
	}
	p.Update(key("tab")) // to command
	p.Update(key("backspace"))
	p.Update(key("backspace"))
	p.Update(key("backspace"))
	p.Update(key("backspace"))
	typeText(p, "btop")
	apply(t, p.Update(key("enter")))
	got := config.Get().Tools.Custom
	if len(got) != 1 || got[0].Command != "btop" {
		t.Fatalf("entries after edit = %+v", got)
	}
}

func TestToolsPageDelete(t *testing.T) {
	p := toolsPage(t)
	addTool(t, p, "one", "one")
	addTool(t, p, "two", "two")
	p.sel = 0
	apply(t, p.Update(key("d")))
	got := config.Get().Tools.Custom
	if len(got) != 1 || got[0].Name != "two" {
		t.Fatalf("entries after delete = %+v", got)
	}
}

func TestToolsPageValidation(t *testing.T) {
	p := toolsPage(t)
	addTool(t, p, "taken", "cmd")

	// Missing name.
	p.Update(key("a"))
	if cmd := p.Update(key("enter")); cmd != nil {
		t.Fatal("empty name must not save")
	}
	if !strings.Contains(p.note, "name") {
		t.Fatalf("note = %q", p.note)
	}

	// Missing command.
	typeText(p, "fresh")
	if cmd := p.Update(key("enter")); cmd != nil {
		t.Fatal("empty command must not save")
	}
	if !strings.Contains(p.note, "command") {
		t.Fatalf("note = %q", p.note)
	}

	// Duplicate name.
	p.Update(key("esc"))
	p.Update(key("a"))
	typeText(p, "taken")
	p.Update(key("tab"))
	typeText(p, "cmd")
	if cmd := p.Update(key("enter")); cmd != nil {
		t.Fatal("duplicate name must not save")
	}
	if !strings.Contains(p.note, "exists") {
		t.Fatalf("note = %q", p.note)
	}

	// Bad placement.
	p.Update(key("esc"))
	p.Update(key("a"))
	typeText(p, "fresh")
	p.Update(key("tab"))
	typeText(p, "cmd")
	for i := 0; i < 3; i++ {
		p.Update(key("tab"))
	}
	typeText(p, "top")
	if cmd := p.Update(key("enter")); cmd != nil {
		t.Fatal("bad placement must not save")
	}
	if !strings.Contains(p.note, "placement") {
		t.Fatalf("note = %q", p.note)
	}
}

func TestToolsPageEditingOwnNameIsNotADuplicate(t *testing.T) {
	p := toolsPage(t)
	addTool(t, p, "solo", "cmd")
	p.sel = 0
	p.Update(key("enter"))
	apply(t, p.Update(key("enter"))) // save unchanged: own name must pass
	if got := config.Get().Tools.Custom; len(got) != 1 || got[0].Name != "solo" {
		t.Fatalf("entries = %+v", got)
	}
}

func TestToolsPageEscCancelsWithoutWriting(t *testing.T) {
	p := toolsPage(t)
	p.Update(key("a"))
	typeText(p, "ghost")
	p.Update(key("esc"))
	if p.Capturing() {
		t.Fatal("esc must close the form")
	}
	if got := config.Get().Tools.Custom; len(got) != 0 {
		t.Fatalf("cancel must not write, got %+v", got)
	}
}

func TestToolsPageViewListsEntriesAndHints(t *testing.T) {
	p := toolsPage(t)
	v := p.View(100, 20)
	if !strings.Contains(v, "no tools configured") {
		t.Fatalf("empty view = %q", v)
	}
	addTool(t, p, "lazygit", "lazygit")
	v = p.View(100, 20)
	if !strings.Contains(v, "lazygit") || !strings.Contains(v, "bottom") {
		t.Fatalf("view must list the entry with its placement:\n%s", v)
	}
}
