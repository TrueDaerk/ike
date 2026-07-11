package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	ilsp "ike/internal/lsp"
	"ike/internal/ui"
)

// symbolprompt.go is the workspace-symbol query prompt (0250 phase 1, #294):
// project.goToClass (cmd+o / leader S) asks for a symbol name here, Enter
// hands the query to the LSP bridge continuation, and the hits come back as
// a SymbolResultsMsg rendered through the references palette mode.

// symbolPromptState is the open prompt: the bridge continuation plus the
// input line. nil when no symbol search is in flight.
type symbolPromptState struct {
	apply func(string) tea.Cmd
	input string
}

var symbolPromptCursor = lipgloss.NewStyle().Reverse(true)

// openSymbolPrompt shows the empty query prompt.
func (m *Model) openSymbolPrompt(msg ilsp.SymbolPromptMsg) {
	m.symbolPrompt = &symbolPromptState{apply: msg.Apply}
	m.renderSymbolPrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// symbolPromptOpen reports whether the shell currently shows the prompt.
func (m Model) symbolPromptOpen() bool { return m.symbolPrompt != nil && m.shell.IsOpen() }

// renderSymbolPrompt (re)fills the shell for the current input.
func (m *Model) renderSymbolPrompt() {
	line := "> " + m.symbolPrompt.input + symbolPromptCursor.Render(" ")
	m.shell.SetContent(ui.ModelContent{
		Heading: "Go to symbol",
		Body: func() string {
			return line + "\n\nenter search the workspace · esc cancel"
		},
	})
}

// updateSymbolPrompt consumes every key while the prompt is open: enter runs
// the bridge continuation with the typed query, esc cancels.
func (m Model) updateSymbolPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.symbolPrompt
	closePrompt := func() {
		m.symbolPrompt = nil
		m.shell.Close()
	}
	switch {
	case msg.Code == tea.KeyEscape:
		closePrompt()
		return m, nil
	case msg.Code == tea.KeyEnter:
		query := strings.TrimSpace(s.input)
		apply := s.apply
		closePrompt()
		if query == "" || apply == nil {
			return m, nil
		}
		return m, apply(query)
	case msg.Code == tea.KeyBackspace:
		if r := []rune(s.input); len(r) > 0 {
			s.input = string(r[:len(r)-1])
		}
	case msg.Code == 'u' && msg.Mod == tea.ModCtrl:
		s.input = ""
	case msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
		s.input += msg.Text
	default:
		return m, nil
	}
	m.renderSymbolPrompt()
	return m, nil
}
