package app

import (
	"strings"
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

// TestLSPRenamePromptRejectsInvalidName (#2672): a validator's rejection
// keeps the prompt open with its message shown and nothing applied; fixing
// the name clears the message and applies.
func TestLSPRenamePromptRejectsInvalidName(t *testing.T) {
	m := dismissOnboarding(sized(t, 100, 40))
	var applied string
	out, _ := m.Update(ilsp.RenamePromptMsg{
		Path:        "/proj/a.php",
		Placeholder: "abc",
		Note:        "+ 2 occurrences in traits A, C",
		Validate: func(name string) string {
			if name[0] >= '0' && name[0] <= '9' {
				return "not a PHP identifier: " + name
			}
			return ""
		},
		Apply: func(name string) tea.Cmd {
			applied = name
			return nil
		},
	})
	m = out.(Model)
	if !strings.Contains(m.shell.View(), "+ 2 occurrences in traits A, C") {
		t.Fatalf("the prompt must show the note, view:\n%s", m.shell.View())
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m = out.(Model)
	for _, r := range "1abc" {
		out, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = out.(Model)
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if applied != "" {
		t.Fatalf("a rejected name must not apply, got %q", applied)
	}
	if !m.lspRenameOpen() {
		t.Fatal("a rejected name must keep the prompt open")
	}
	if !strings.Contains(m.shell.View(), "not a PHP identifier: 1abc") {
		t.Fatalf("the rejection must be shown, view:\n%s", m.shell.View())
	}
	// Editing clears the rejection; a valid name applies.
	out, _ = m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m = out.(Model)
	if strings.Contains(m.shell.View(), "not a PHP identifier") {
		t.Fatal("editing must clear the rejection")
	}
	for _, r := range "xyz" {
		out, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = out.(Model)
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if applied != "xyz" || m.lspRenameOpen() {
		t.Fatalf("a valid name must apply and close, applied=%q open=%v", applied, m.lspRenameOpen())
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
