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
//
// The message may also carry a note and a validator (#2672): the note is a
// line under the input saying what the rename touches beyond the server's
// edits (the occurrences inside consumed PHP traits) or that the index
// performs it; the validator rejects a name before Apply runs, and the prompt
// shows its message and stays open instead of closing.

// lspRenameState is the open prompt: the bridge continuation plus the input
// line, the note and the validator, and the last rejection shown.
type lspRenameState struct {
	path     string
	apply    func(string) tea.Cmd
	validate func(string) string
	note     string
	problem  string
	input    ui.Field
}

// openLSPRenamePrompt shows the prompt prefilled with the symbol placeholder,
// fully selected in spirit: the cursor sits at the end so typing extends and
// ctrl+u (via backspaces) clears.
func (m *Model) openLSPRenamePrompt(msg ilsp.RenamePromptMsg) {
	m.lspRename = &lspRenameState{
		path:     msg.Path,
		apply:    msg.Apply,
		validate: msg.Validate,
		note:     msg.Note,
		input:    ui.NewField(msg.Placeholder),
	}
	m.renderLSPRenamePrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// lspRenameOpen reports whether the shell currently shows the symbol prompt.
func (m Model) lspRenameOpen() bool { return m.lspRename != nil && m.shell.IsOpen() }

// renderLSPRenamePrompt (re)fills the shell for the current input: the input
// line, the note when the bridge sent one, the rejection of the last enter
// while it stands, and the key legend.
func (m *Model) renderLSPRenamePrompt() {
	s := m.lspRename
	var sb strings.Builder
	sb.WriteString("> " + s.input.View())
	if s.note != "" {
		sb.WriteString("\n" + s.note)
	}
	if s.problem != "" {
		sb.WriteString("\n" + s.problem)
	}
	sb.WriteString("\n\nenter rename · esc cancel")
	body := sb.String()
	m.shell.SetContent(ui.ModelContent{
		Heading: "Rename symbol",
		Body: func() string {
			return body
		},
	})
}

// updateLSPRenamePrompt consumes every key while the prompt is open. Enter
// runs the bridge continuation with the typed name — unless the validator
// rejects it, in which case the rejection shows and the prompt stays; esc
// cancels — nothing has been sent to the server yet, so cancel is free.
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
		if name != "" && s.validate != nil {
			if problem := s.validate(name); problem != "" {
				s.problem = problem
				m.renderLSPRenamePrompt()
				return m, nil
			}
		}
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
		s.problem = ""
	default:
		s.input.Key(msg)
		s.problem = ""
	}
	m.renderLSPRenamePrompt()
	return m, nil
}
