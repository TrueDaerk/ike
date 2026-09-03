package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
	"ike/internal/ui"
)

// lsprename.go is the symbol-rename prompt (Roadmap 0100, #6). The LSP bridge
// validates the position (prepareRename) and sends a RenamePromptMsg carrying
// the placeholder and an Apply continuation; this file owns only the input UI
// — line editing mirrors the file-rename prompt — and hands the typed name
// back to the continuation on enter.

// lspRenameState is the open prompt: the bridge continuation plus the input
// line. nil when no symbol rename is in flight.
type lspRenameState struct {
	path  string
	apply func(string) tea.Cmd
	input ui.Field
}

// openLSPRenamePrompt shows the prompt prefilled with the symbol placeholder,
// fully selected in spirit: the cursor sits at the end so typing extends and
// ctrl+u (via backspaces) clears.
func (m *Model) openLSPRenamePrompt(msg ilsp.RenamePromptMsg) {
	m.lspRename = &lspRenameState{
		path:  msg.Path,
		apply: msg.Apply,
		input: ui.NewField(msg.Placeholder),
	}
	m.renderLSPRenamePrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// lspRenameOpen reports whether the shell currently shows the symbol prompt.
func (m Model) lspRenameOpen() bool { return m.lspRename != nil && m.shell.IsOpen() }

// renderLSPRenamePrompt (re)fills the shell for the current input.
func (m *Model) renderLSPRenamePrompt() {
	s := m.lspRename
	line := "> " + s.input.View()
	m.shell.SetContent(ui.ModelContent{
		Heading: "Rename symbol",
		Body: func() string {
			return line + "\n\nenter rename · esc cancel"
		},
	})
}

// updateLSPRenamePrompt consumes every key while the prompt is open. Enter
// runs the bridge continuation with the typed name; esc cancels — nothing has
// been sent to the server yet, so cancel is free.
func (m Model) updateLSPRenamePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.lspRename
	closePrompt := func() {
		m.lspRename = nil
		m.shell.Close()
	}
	switch {
	case msg.Code == tea.KeyEscape:
		closePrompt()
		return m, nil
	case msg.Code == tea.KeyEnter:
		name := strings.TrimSpace(s.input.Text)
		apply := s.apply
		closePrompt()
		if name == "" || apply == nil {
			return m, nil
		}
		return m, apply(name)
	case msg.Code == 'u' && msg.Mod == tea.ModCtrl:
		// ctrl+u clears the whole line — the prompt's own chord, kept ahead
		// of ui.EditKey (caller chords win, #2459).
		s.input.Clear()
	default:
		s.input.Key(msg)
	}
	m.renderLSPRenamePrompt()
	return m, nil
}
