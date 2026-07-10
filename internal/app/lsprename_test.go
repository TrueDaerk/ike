package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
)

func openRenamePrompt(t *testing.T, placeholder string) (Model, *string) {
	t.Helper()
	m := sized(t, 100, 40)
	var applied string
	out, _ := m.Update(ilsp.RenamePromptMsg{
		Path:        "/proj/a.go",
		Placeholder: placeholder,
		Apply: func(name string) tea.Cmd {
			applied = name
			return nil
		},
	})
	m = out.(Model)
	if !m.lspRenameOpen() {
		t.Fatal("RenamePromptMsg should open the prompt")
	}
	return m, &applied
}

func TestLSPRenamePromptApplies(t *testing.T) {
	m, applied := openRenamePrompt(t, "Greet")
	// Extend the placeholder and confirm.
	for _, r := range "ing" {
		out, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = out.(Model)
	}
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if *applied != "Greeting" {
		t.Fatalf("apply should receive the typed name, got %q", *applied)
	}
	if m.lspRenameOpen() {
		t.Fatal("enter should close the prompt")
	}
}

func TestLSPRenamePromptCancelAndEmpty(t *testing.T) {
	m, applied := openRenamePrompt(t, "Greet")
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if *applied != "" || m.lspRenameOpen() {
		t.Fatal("esc must cancel without applying")
	}

	// An emptied input on enter is a no-op too.
	m, applied = openRenamePrompt(t, "x")
	out, _ = m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m = out.(Model)
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if *applied != "" {
		t.Fatalf("empty name must not apply, got %q", *applied)
	}
}

func TestLSPRenamePromptSwallowsKeys(t *testing.T) {
	m, applied := openRenamePrompt(t, "sym")
	out, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = out.(Model)
	if cmd != nil || !m.lspRenameOpen() {
		t.Fatal("prompt must consume keys without side effects")
	}
	_ = applied
}
