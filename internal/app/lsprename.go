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
	input string
	pos   int
}

// openLSPRenamePrompt shows the prompt prefilled with the symbol placeholder,
// fully selected in spirit: the cursor sits at the end so typing extends and
// ctrl+u (via backspaces) clears.
func (m *Model) openLSPRenamePrompt(msg ilsp.RenamePromptMsg) {
	m.lspRename = &lspRenameState{
		path:  msg.Path,
		apply: msg.Apply,
		input: msg.Placeholder,
		pos:   len([]rune(msg.Placeholder)),
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
	r := []rune(s.input)
	before, after := string(r[:s.pos]), ""
	cur := " "
	if s.pos < len(r) {
		cur = string(r[s.pos])
		after = string(r[s.pos+1:])
	}
	line := "> " + before + renamePromptCursor.Render(cur) + after
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
	r := []rune(s.input)
	switch {
	case msg.Code == tea.KeyEscape:
		closePrompt()
		return m, nil
	case msg.Code == tea.KeyEnter:
		name := strings.TrimSpace(s.input)
		apply := s.apply
		closePrompt()
		if name == "" || apply == nil {
			return m, nil
		}
		return m, apply(name)
	case msg.Code == tea.KeyLeft:
		if s.pos > 0 {
			s.pos--
		}
	case msg.Code == tea.KeyRight:
		if s.pos < len(r) {
			s.pos++
		}
	case msg.Code == tea.KeyHome:
		s.pos = 0
	case msg.Code == tea.KeyEnd:
		s.pos = len(r)
	case msg.Code == tea.KeyBackspace:
		if s.pos > 0 {
			s.input = string(append(r[:s.pos-1:s.pos-1], r[s.pos:]...))
			s.pos--
		}
	case msg.Code == 'u' && msg.Mod == tea.ModCtrl:
		s.input = ""
		s.pos = 0
	case msg.Code == tea.KeyDelete:
		if s.pos < len(r) {
			s.input = string(append(r[:s.pos:s.pos], r[s.pos+1:]...))
		}
	case msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
		ins := []rune(msg.Text)
		s.input = string(append(append(append([]rune{}, r[:s.pos]...), ins...), r[s.pos:]...))
		s.pos += len(ins)
	}
	m.renderLSPRenamePrompt()
	return m, nil
}
