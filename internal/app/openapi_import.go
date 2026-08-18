package app

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/openapi"
	"ike/internal/pathcomplete"
	"ike/internal/ui"
)

// openapi_import.go drives the OpenAPI import of the HTTP client (#1939): the
// http.importOpenAPI command opens a shell prompt asking for an OpenAPI 3.x
// document's path (tab = filesystem completion, like the JetBrains keymap
// import, #677); enter reads it, writes `<spec>.http` plus the
// http-client.env.json / http-client.private.env.json skeletons next to the
// spec, opens the generated file and reports a summary — including what the
// generator had to leave out.

// ImportOpenAPIMsg asks the root model to open the OpenAPI import prompt.
// Dispatched by http.importOpenAPI (palette).
type ImportOpenAPIMsg struct{}

// openAPIImportDoneMsg carries a finished import back into Update.
type openAPIImportDoneMsg struct {
	res *openapi.ImportResult
	err error
}

// startOpenAPIImport opens the shell prompt asking for the spec's path.
func (m *Model) startOpenAPIImport() {
	m.oapiImportOpen = true
	m.oapiImportInput = "." + string(os.PathSeparator)
	m.oapiImportPos = len([]rune(m.oapiImportInput))
	m.renderOpenAPIImportPrompt(nil)
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// openAPIImportPromptOpen reports whether the shell shows the import prompt.
func (m Model) openAPIImportPromptOpen() bool { return m.oapiImportOpen && m.shell.IsOpen() }

// renderOpenAPIImportPrompt (re)fills the shell with the prompt for the
// current input; completion candidates render underneath.
func (m *Model) renderOpenAPIImportPrompt(candidates []string) {
	r := []rune(m.oapiImportInput)
	pos := m.oapiImportPos
	before, after := string(r[:pos]), ""
	cur := " "
	if pos < len(r) {
		cur = string(r[pos])
		after = string(r[pos+1:])
	}
	line := "> " + before + renamePromptCursor.Render(cur) + after
	const maxLines = 8
	var sug string
	if n := len(candidates); n > 0 {
		shown := candidates
		if n > maxLines {
			shown = candidates[:maxLines]
		}
		sug = "\n\n  " + strings.Join(shown, "\n  ")
		if n > maxLines {
			sug += fmt.Sprintf("\n  … +%d more", n-maxLines)
		}
	}
	m.shell.SetContent(ui.ModelContent{
		Heading: "Import OpenAPI 3.x spec (JSON or YAML)",
		Body: func() string {
			return line + sug + "\n\ntab complete · enter import · esc cancel"
		},
	})
}

// updateOpenAPIImportPrompt consumes every key while the prompt is open,
// mirroring the JetBrains keymap import prompt's line editing and tab
// completion.
func (m Model) updateOpenAPIImportPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	closePrompt := func() {
		m.oapiImportOpen = false
		m.oapiImportInput = ""
		m.oapiImportPos = 0
		m.shell.Close()
	}
	r := []rune(m.oapiImportInput)
	var candidates []string
	switch {
	case msg.Code == tea.KeyEscape:
		closePrompt()
		return m, nil
	case msg.Code == tea.KeyEnter:
		path := strings.TrimSpace(m.oapiImportInput)
		closePrompt()
		if path == "" {
			return m, nil
		}
		return m, runOpenAPIImport(expandHome(path))
	case msg.Code == tea.KeyTab:
		res := pathcomplete.Complete(m.oapiImportInput)
		m.oapiImportInput = res.Completed
		m.oapiImportPos = len([]rune(m.oapiImportInput))
		candidates = res.Candidates
	case msg.Code == tea.KeyLeft:
		if m.oapiImportPos > 0 {
			m.oapiImportPos--
		}
	case msg.Code == tea.KeyRight:
		if m.oapiImportPos < len(r) {
			m.oapiImportPos++
		}
	case msg.Code == tea.KeyHome:
		m.oapiImportPos = 0
	case msg.Code == tea.KeyEnd:
		m.oapiImportPos = len(r)
	case msg.Code == tea.KeyBackspace:
		if m.oapiImportPos > 0 {
			m.oapiImportInput = string(append(r[:m.oapiImportPos-1:m.oapiImportPos-1], r[m.oapiImportPos:]...))
			m.oapiImportPos--
		}
	case msg.Code == tea.KeyDelete:
		if m.oapiImportPos < len(r) {
			m.oapiImportInput = string(append(r[:m.oapiImportPos:m.oapiImportPos], r[m.oapiImportPos+1:]...))
		}
	case msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
		ins := []rune(msg.Text)
		m.oapiImportInput = string(append(append(append([]rune{}, r[:m.oapiImportPos]...), ins...), r[m.oapiImportPos:]...))
		m.oapiImportPos += len(ins)
	}
	m.renderOpenAPIImportPrompt(candidates)
	return m, nil
}

// pasteOpenAPIImportPrompt inserts a paste into the path input at its cursor
// (#1873).
func (m *Model) pasteOpenAPIImportPrompt(text string) bool {
	out, pos, changed := ui.PasteText(m.oapiImportInput, m.oapiImportPos, text)
	if !changed {
		return false
	}
	m.oapiImportInput, m.oapiImportPos = out, pos
	m.renderOpenAPIImportPrompt(nil)
	return true
}

// runOpenAPIImport reads and generates off the update loop — a large spec is
// parsed and rendered whole, which has no business blocking the UI.
func runOpenAPIImport(path string) tea.Cmd {
	return func() tea.Msg {
		res, err := openapi.ImportFile(path)
		return openAPIImportDoneMsg{res: res, err: err}
	}
}

// finishOpenAPIImport reports the outcome and opens the generated file, so the
// user lands in the requests instead of having to find them.
func (m Model) finishOpenAPIImport(msg openAPIImportDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.host.Notify(host.Error, "openapi import: "+msg.err.Error())
		return m, nil
	}
	level := host.Info
	if len(msg.res.Skipped) > 0 || len(msg.res.MissingVars) > 0 {
		level = host.Warn
	}
	m.host.Notify(level, "openapi import: "+msg.res.Summary())
	return m.openPathInEditor(msg.res.HTTPFile)
}
