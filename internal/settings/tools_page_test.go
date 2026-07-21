package settings

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
)

// stubHost collects pushed sub-panels for page-level tests (#883).
type stubHost struct{ stack []SubPanel }

func (s *stubHost) Push(sp SubPanel) { s.stack = append(s.stack, sp) }
func (s *stubHost) Pop() {
	if len(s.stack) > 0 {
		s.stack = s.stack[:len(s.stack)-1]
	}
}
func (s *stubHost) top() SubPanel {
	if len(s.stack) == 0 {
		return nil
	}
	return s.stack[len(s.stack)-1]
}

func toolsPage(t *testing.T) (*ToolsPage, *stubHost) {
	t.Helper()
	restoreConfig(t)
	p := NewToolsPage(testOpts(t))
	h := &stubHost{}
	p.SetSubPanelHost(h)
	return p, h
}

// form returns the open tool form, failing when none is pushed.
func form(t *testing.T, h *stubHost) *toolForm {
	t.Helper()
	f, ok := h.top().(*toolForm)
	if !ok {
		t.Fatal("expected an open tool form sub-panel")
	}
	return f
}

// typeText feeds a string into the open form rune by rune.
func typeText(f *toolForm, s string) {
	for _, r := range s {
		f.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
}

// addTool drives the full add flow: open form, fill name/command, save.
func addTool(t *testing.T, p *ToolsPage, h *stubHost, name, command string) {
	t.Helper()
	p.Update(key("a"))
	f := form(t, h)
	typeText(f, name)
	f.Update(key("tab"))
	typeText(f, command)
	apply(t, f.Update(key("enter")))
	if h.top() != nil {
		t.Fatal("save must pop the form")
	}
}

func TestToolsPageAddWritesAndReloads(t *testing.T) {
	p, h := toolsPage(t)
	addTool(t, p, h, "lazygit", "lazygit")
	got := config.Get().Tools.Custom
	if len(got) != 1 || got[0].Name != "lazygit" || got[0].Command != "lazygit" {
		t.Fatalf("entries after add = %+v", got)
	}
}

func TestToolsPageAddAllFields(t *testing.T) {
	p, h := toolsPage(t)
	p.Update(key("a"))
	f := form(t, h)
	typeText(f, "sq")
	f.Update(key("tab"))
	typeText(f, "sqlit")
	f.Update(key("tab"))
	typeText(f, "--db dev.sqlite")
	f.Update(key("tab"))
	typeText(f, "./data")
	f.Update(key("tab"))
	typeText(f, "right")
	apply(t, f.Update(key("enter")))
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
	p, h := toolsPage(t)
	p.Update(key("a"))
	f := form(t, h)
	typeText(f, "claude")
	f.Update(key("tab"))
	typeText(f, "claude")
	for i := 0; i < 4; i++ { // args, cwd, placement → multiple
		f.Update(key("tab"))
	}
	typeText(f, "maybe")
	f.Update(key("enter"))
	if h.top() == nil || !strings.Contains(f.note, "multiple must be true or false") {
		t.Fatalf("invalid multiple must fail validation, note=%q", f.note)
	}
	for range "maybe" {
		f.Update(key("backspace"))
	}
	typeText(f, "true")
	apply(t, f.Update(key("enter")))
	got := config.Get().Tools.Custom
	if len(got) != 1 || !got[0].Multiple {
		t.Fatalf("entries = %+v, want Multiple=true", got)
	}
	// Edit seeds "true" back into the form.
	p.sel = 0
	p.Update(key("enter"))
	if f2 := form(t, h); f2.form[5] != "true" {
		t.Fatalf("edit must seed multiple, form=%v", f2.form)
	}
}

func TestToolsPageEditRoundTrip(t *testing.T) {
	p, h := toolsPage(t)
	addTool(t, p, h, "htop", "htop")
	p.sel = 0
	p.Update(key("enter")) // edit
	f := form(t, h)
	if f.form[0] != "htop" {
		t.Fatalf("edit must seed the form, form=%v", f.form)
	}
	f.Update(key("tab")) // to command
	for i := 0; i < 4; i++ {
		f.Update(key("backspace"))
	}
	typeText(f, "btop")
	apply(t, f.Update(key("enter")))
	got := config.Get().Tools.Custom
	if len(got) != 1 || got[0].Command != "btop" {
		t.Fatalf("entries after edit = %+v", got)
	}
}

func TestToolsPageDelete(t *testing.T) {
	p, h := toolsPage(t)
	addTool(t, p, h, "one", "one")
	addTool(t, p, h, "two", "two")
	p.sel = 0
	p.Update(key("d"))
	apply(t, confirmVia(t, h))
	got := config.Get().Tools.Custom
	if len(got) != 1 || got[0].Name != "two" {
		t.Fatalf("entries after delete = %+v", got)
	}
}

func TestToolsPageValidation(t *testing.T) {
	p, h := toolsPage(t)
	addTool(t, p, h, "taken", "cmd")

	// Missing name.
	p.Update(key("a"))
	f := form(t, h)
	if cmd := f.Update(key("enter")); cmd != nil {
		t.Fatal("empty name must not save")
	}
	if !strings.Contains(f.note, "name") {
		t.Fatalf("note = %q", f.note)
	}

	// Missing command.
	typeText(f, "fresh")
	if cmd := f.Update(key("enter")); cmd != nil {
		t.Fatal("empty command must not save")
	}
	if !strings.Contains(f.note, "command") {
		t.Fatalf("note = %q", f.note)
	}

	// Duplicate name.
	f.Update(key("esc"))
	p.Update(key("a"))
	f = form(t, h)
	typeText(f, "taken")
	f.Update(key("tab"))
	typeText(f, "cmd")
	if cmd := f.Update(key("enter")); cmd != nil {
		t.Fatal("duplicate name must not save")
	}
	if !strings.Contains(f.note, "exists") {
		t.Fatalf("note = %q", f.note)
	}

	// Bad placement.
	f.Update(key("esc"))
	p.Update(key("a"))
	f = form(t, h)
	typeText(f, "fresh")
	f.Update(key("tab"))
	typeText(f, "cmd")
	for i := 0; i < 3; i++ {
		f.Update(key("tab"))
	}
	typeText(f, "top")
	if cmd := f.Update(key("enter")); cmd != nil {
		t.Fatal("bad placement must not save")
	}
	if !strings.Contains(f.note, "placement") {
		t.Fatalf("note = %q", f.note)
	}
}

func TestToolsPageEditingOwnNameIsNotADuplicate(t *testing.T) {
	p, h := toolsPage(t)
	addTool(t, p, h, "solo", "cmd")
	p.sel = 0
	p.Update(key("enter"))
	apply(t, form(t, h).Update(key("enter"))) // save unchanged: own name must pass
	if got := config.Get().Tools.Custom; len(got) != 1 || got[0].Name != "solo" {
		t.Fatalf("entries = %+v", got)
	}
}

func TestToolsPageEscCancelsWithoutWriting(t *testing.T) {
	p, h := toolsPage(t)
	p.Update(key("a"))
	f := form(t, h)
	typeText(f, "ghost")
	f.Update(key("esc"))
	if h.top() != nil {
		t.Fatal("esc must pop the form")
	}
	if got := config.Get().Tools.Custom; len(got) != 0 {
		t.Fatalf("cancel must not write, got %+v", got)
	}
}

// TestToolFormUmlautBackspace guards the multi-byte fix: backspace removes a
// whole rune, not one byte.
func TestToolFormUmlautBackspace(t *testing.T) {
	p, h := toolsPage(t)
	p.Update(key("a"))
	f := form(t, h)
	typeText(f, "grün")
	f.Update(key("backspace"))
	if f.form[0] != "grü" {
		t.Fatalf("after backspace = %q, want grü", f.form[0])
	}
}

// TestToolFormClickFocusesField guards #883: a press on a field row focuses
// it instead of destroying the form.
func TestToolFormClickFocusesField(t *testing.T) {
	p, h := toolsPage(t)
	p.Update(key("a"))
	f := form(t, h)
	typeText(f, "keepme")
	f.Click(0, 3) // the cwd row
	if f.field != 3 {
		t.Fatalf("field = %d, want 3", f.field)
	}
	if h.top() == nil || f.form[0] != "keepme" {
		t.Fatal("a click must never discard the form")
	}
}

func TestToolsPageViewListsEntriesAndHints(t *testing.T) {
	p, h := toolsPage(t)
	v := p.View(100, 20)
	if !strings.Contains(v, "no tools configured") {
		t.Fatalf("empty view = %q", v)
	}
	addTool(t, p, h, "lazygit", "lazygit")
	v = p.View(100, 20)
	if !strings.Contains(v, "lazygit") || !strings.Contains(v, "bottom") {
		t.Fatalf("view must list the entry with its placement:\n%s", v)
	}
}
