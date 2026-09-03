package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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
//
// The prompt also takes a **URL** (#2009), which changes the shape of the
// interaction: a path is imported the moment enter is pressed, while a URL is
// first *resolved and validated* off the update loop (fetch, well-known probe
// run, parse) and only then confirmed by a second enter. Validating before
// confirming is what makes the failure modes legible — a dead host, a 404
// everywhere, a document that is not OpenAPI each stop at the dialog with
// their own message and turn the popup red — instead of the import blowing up
// after the prompt is already gone. The confirmed import generates from the
// bytes the check fetched, so a dynamically served spec cannot change between
// the two enters.

// ImportOpenAPIMsg asks the root model to open the OpenAPI import prompt.
// Dispatched by http.importOpenAPI (palette).
type ImportOpenAPIMsg struct{}

// openAPIImportDoneMsg carries a finished import back into Update.
type openAPIImportDoneMsg struct {
	res *openapi.ImportResult
	err error
}

// openAPICheckDoneMsg carries a finished URL discovery back into the open
// prompt. seq identifies the check the result belongs to: the input may have
// been edited (or the prompt closed) while the network was busy, and a stale
// answer must not unblock a confirm for a URL nobody typed anymore.
type openAPICheckDoneMsg struct {
	seq   int
	input string
	disc  *openapi.Discovery
	err   error
}

// startOpenAPIImport opens the shell prompt asking for the spec's path or URL.
func (m *Model) startOpenAPIImport() {
	m.oapiImportOpen = true
	m.oapiImportInput.Set("." + string(os.PathSeparator))
	m.resetOpenAPICheck()
	m.renderOpenAPIImportPrompt(nil)
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// openAPIImportPromptOpen reports whether the shell shows the import prompt.
func (m Model) openAPIImportPromptOpen() bool { return m.oapiImportOpen && m.shell.IsOpen() }

// resetOpenAPICheck drops a finished or in-flight URL check — every edit of
// the input invalidates what was verified, so a confirm always refers to the
// URL currently on screen. Bumping the sequence makes any answer still in
// flight stale.
func (m *Model) resetOpenAPICheck() {
	m.oapiCheckSeq++
	m.oapiChecking = false
	m.oapiCheckErr = ""
	m.oapiCheckDisc = nil
	m.shell.SetAccent(nil)
}

// renderOpenAPIImportPrompt (re)fills the shell with the prompt for the
// current input; completion candidates render underneath, as does the state
// of the URL check (checking / resolved URL / error).
func (m *Model) renderOpenAPIImportPrompt(candidates []string) {
	line := "> " + m.oapiImportInput.View()
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
	status, hint := m.openAPICheckStatus()
	m.shell.SetContent(ui.ModelContent{
		Heading: "Import OpenAPI 3.x spec (JSON or YAML) — path or URL",
		Body: func() string {
			return line + sug + status + "\n\n" + hint
		},
	})
}

// openAPICheckStatus renders the URL check's state as the status block below
// the input plus the key hint line, which differs per state: a path imports
// on enter, a URL is checked by the first enter and imported by the second.
func (m Model) openAPICheckStatus() (status, hint string) {
	pal := m.pal()
	switch {
	case m.oapiChecking:
		return "\n\n  checking " + strings.TrimSpace(m.oapiImportInput.Text) + " …",
			"esc cancel"
	case m.oapiCheckErr != "":
		bad := lipgloss.NewStyle().Foreground(pal.Error)
		return "\n\n" + bad.Render("  ✗ "+m.oapiCheckErr),
			"enter retry · esc cancel"
	case m.oapiCheckDisc != nil:
		ok := lipgloss.NewStyle().Foreground(pal.Success)
		d := m.oapiCheckDisc
		found := fmt.Sprintf("  ✓ %d operations at %s", len(d.Spec.Operations), d.URL)
		if len(d.Probed) > 0 {
			found += fmt.Sprintf("\n  (probed %d well-known path(s) first)", len(d.Probed))
		}
		return "\n\n" + ok.Render(found), "enter import · esc cancel"
	case openapi.IsURL(m.oapiImportInput.Text):
		return "", "enter check URL · esc cancel"
	}
	return "", "tab complete · enter import · esc cancel"
}

// updateOpenAPIImportPrompt consumes every key while the prompt is open,
// mirroring the JetBrains keymap import prompt's line editing and tab
// completion.
func (m Model) updateOpenAPIImportPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	closePrompt := func() {
		m.oapiImportOpen = false
		m.oapiImportInput.Clear()
		m.resetOpenAPICheck()
		m.shell.Close()
	}
	var candidates []string
	switch {
	case msg.Code == tea.KeyEscape:
		closePrompt()
		return m, nil
	case msg.Code == tea.KeyEnter:
		input := strings.TrimSpace(m.oapiImportInput.Text)
		if input == "" {
			closePrompt()
			return m, nil
		}
		if openapi.IsURL(input) {
			if d := m.oapiCheckDisc; d != nil {
				closePrompt()
				return m, runOpenAPIURLImport(d)
			}
			if m.oapiChecking {
				return m, nil // the answer is on its way
			}
			cmd := m.startOpenAPICheck(input)
			m.renderOpenAPIImportPrompt(nil)
			return m, cmd
		}
		closePrompt()
		return m, runOpenAPIImport(expandHome(input))
	case msg.Code == tea.KeyTab:
		if openapi.IsURL(m.oapiImportInput.Text) {
			break // nothing on the filesystem completes a URL
		}
		res := pathcomplete.Complete(m.oapiImportInput.Text)
		m.oapiImportInput.Set(res.Completed)
		candidates = res.Candidates
	default:
		if handled, changed := m.oapiImportInput.Key(msg); handled {
			if changed {
				m.resetOpenAPICheck()
			}
		}
	}
	m.renderOpenAPIImportPrompt(candidates)
	return m, nil
}

// startOpenAPICheck marks the prompt as checking and returns the command that
// resolves the URL off the update loop.
func (m *Model) startOpenAPICheck(input string) tea.Cmd {
	m.resetOpenAPICheck()
	m.oapiChecking = true
	return runOpenAPICheck(m.oapiCheckSeq, input)
}

// finishOpenAPICheck folds a discovery result into the open prompt: a hit
// arms the confirm and names the resolved URL, a miss turns the popup red
// with the concrete reason. A stale or orphaned answer is dropped.
func (m Model) finishOpenAPICheck(msg openAPICheckDoneMsg) (tea.Model, tea.Cmd) {
	if !m.openAPIImportPromptOpen() || msg.seq != m.oapiCheckSeq {
		return m, nil
	}
	m.oapiChecking = false
	switch {
	case msg.err != nil:
		m.oapiCheckErr = msg.err.Error()
		m.oapiCheckDisc = nil
		m.shell.SetAccent(m.pal().Error)
	default:
		m.oapiCheckErr = ""
		m.oapiCheckDisc = msg.disc
		m.shell.SetAccent(nil)
	}
	m.renderOpenAPIImportPrompt(nil)
	return m, nil
}

// pasteOpenAPIImportPrompt inserts a paste into the path input at its cursor
// (#1873) — the usual way a spec URL arrives.
func (m *Model) pasteOpenAPIImportPrompt(text string) bool {
	if !m.oapiImportInput.Paste(text) {
		return false
	}
	m.resetOpenAPICheck()
	m.renderOpenAPIImportPrompt(nil)
	return true
}

// runOpenAPICheck resolves a URL off the update loop: discovery walks the
// well-known paths sequentially with a short timeout each, which is exactly
// the kind of wait the update loop must not take.
func runOpenAPICheck(seq int, input string) tea.Cmd {
	return func() tea.Msg {
		d, err := openapi.Discover(context.Background(), nil, input)
		return openAPICheckDoneMsg{seq: seq, input: input, disc: d, err: err}
	}
}

// runOpenAPIImport reads and generates off the update loop — a large spec is
// parsed and rendered whole, which has no business blocking the UI.
func runOpenAPIImport(path string) tea.Cmd {
	return func() tea.Msg {
		res, err := openapi.ImportFile(path)
		return openAPIImportDoneMsg{res: res, err: err}
	}
}

// runOpenAPIURLImport generates from the document the check already fetched.
// A URL has no directory of its own, so the request file and its environment
// skeletons land in the working directory — the project root, the same place
// a relative path in the prompt resolves against.
func runOpenAPIURLImport(d *openapi.Discovery) tea.Cmd {
	return func() tea.Msg {
		dir, err := cachedGetwd()
		if err != nil {
			return openAPIImportDoneMsg{err: err}
		}
		res, err := openapi.ImportDocument(d.Data, d.Name(), dir)
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
