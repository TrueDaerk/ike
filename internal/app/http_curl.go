package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/httpfile"
	"ike/internal/ui"
)

// http_curl.go wires the two curl conversions of the HTTP client (#1994).
//
// http.importCurl opens a one-line prompt — prefilled from the clipboard when
// that already holds a curl command, which is the devtools "Copy as cURL"
// case — parses it (internal/httpfile.ParseCurl) and appends the equivalent
// request block to the .http file in the focused editor. Flags that have no
// spelling in a request block are named in a warning notice, never dropped in
// silence.
//
// http.copyAsCurl is the way back: the request block under the caret, with the
// variable chain applied exactly as a dispatch would apply it, rendered as a
// runnable curl command on the clipboard.

// ImportCurlMsg asks the root model to open the curl import prompt.
// Dispatched by http.importCurl (palette).
type ImportCurlMsg struct{}

// HTTPCopyAsCurlMsg runs http.copyAsCurl: the request under the caret to the
// clipboard as a curl command.
type HTTPCopyAsCurlMsg struct{}

// httpEditor returns the focused editor when it holds a request file, and
// says why it does not otherwise — both conversions need one.
func (m *Model) httpEditor(what string) (*editor.Model, bool) {
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, what+": focus a file tab first")
		return nil, false
	}
	if !isHTTPPath(ed.Path()) {
		m.host.Notify(host.Info, what+": not an .http file")
		return nil, false
	}
	return ed, true
}

// curlImportEditor returns the editor an import writes into: a request file
// in normal mode. The mode matters because the block is spliced through the
// editor's paste path — mid-insert it would join the open insert, and in
// visual mode it would replace the selection instead of appending.
func (m *Model) curlImportEditor() (*editor.Model, bool) {
	ed, ok := m.httpEditor("curl import")
	if !ok {
		return nil, false
	}
	if ed.ModeName() != editor.Normal {
		m.host.Notify(host.Info, "curl import: leave insert/visual mode first")
		return nil, false
	}
	return ed, true
}

// startCurlImport opens the prompt. It refuses early when there is nowhere to
// put the block, so the command is never typed for nothing.
func (m *Model) startCurlImport() {
	if _, ok := m.curlImportEditor(); !ok {
		return
	}
	m.curlImportOpen = true
	m.curlImportInput = ""
	if text := clipboardRead(); httpfile.IsCurlCommand(text) {
		// The overwhelmingly common source is a "Copy as cURL" still sitting
		// on the clipboard; offering it saves the paste and stays editable.
		m.curlImportInput = flattenCurlCommand(text)
	}
	m.curlImportPos = len([]rune(m.curlImportInput))
	m.renderCurlImportPrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// flattenCurlCommand folds a multi-line command (the backslash-wrapped
// spelling devtools and documentation produce) onto the prompt's single line.
// Only the line breaks and their continuation backslashes go; quoted values
// keep every space they carry.
func flattenCurlCommand(text string) string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimSuffix(line, "\\"))
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, " ")
}

// curlImportPromptOpen reports whether the shell shows the import prompt.
func (m Model) curlImportPromptOpen() bool { return m.curlImportOpen && m.shell.IsOpen() }

// renderCurlImportPrompt (re)fills the shell for the current input.
func (m *Model) renderCurlImportPrompt() {
	// A curl command is long; the value scrolls inside a window around the
	// cursor so the box never grows wider than the terminal.
	avail := m.width - 10
	if avail < 20 {
		avail = 20
	}
	line := "> " + windowedInput(m.curlImportInput, m.curlImportPos, avail)
	m.shell.SetContent(ui.ModelContent{
		Heading: "Import curl command",
		Body: func() string {
			return line + "\n\npaste a curl command · enter import · esc cancel"
		},
	})
}

// updateCurlImportPrompt consumes every key while the prompt is open, like
// the other single-field shell prompts.
func (m Model) updateCurlImportPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	closePrompt := func() {
		m.curlImportOpen = false
		m.curlImportInput = ""
		m.curlImportPos = 0
		m.shell.Close()
	}
	switch {
	case msg.Code == tea.KeyEscape:
		closePrompt()
		return m, nil
	case msg.Code == tea.KeyEnter:
		cmd := strings.TrimSpace(m.curlImportInput)
		closePrompt()
		if cmd == "" {
			return m, nil
		}
		return m, m.importCurlCommand(cmd)
	default:
		if out, pos, handled, _ := ui.EditKey(msg, m.curlImportInput, m.curlImportPos); handled {
			m.curlImportInput, m.curlImportPos = out, pos
		}
	}
	m.renderCurlImportPrompt()
	return m, nil
}

// pasteCurlImportPrompt inserts a paste into the command input at its cursor
// (#1873). The input is one line, so a wrapped command is flattened the way
// every other single-field prompt flattens a paste (#1936).
func (m *Model) pasteCurlImportPrompt(text string) bool {
	out, pos, changed := ui.PasteText(m.curlImportInput, m.curlImportPos, flattenCurlCommand(text))
	if !changed {
		return false
	}
	m.curlImportInput, m.curlImportPos = out, pos
	m.renderCurlImportPrompt()
	return true
}

// importCurlCommand parses the command and appends the request block to the
// focused .http file. The block lands at the end of the buffer under its own
// "###" separator — inserting at the caret could split the block the caret
// happens to sit in — and the caret follows it, so the import is visible and
// runnable at once.
func (m *Model) importCurlCommand(cmd string) tea.Cmd {
	ed, ok := m.curlImportEditor()
	if !ok {
		return nil
	}
	imp, err := httpfile.ParseCurl(cmd)
	if err != nil {
		m.host.Notify(host.Error, "curl import: "+err.Error())
		return nil
	}
	text := ed.Text()
	name := uniqueRequestName(httpfile.Parse(text), curlRequestName(imp.Request))
	block := httpfile.FormatRequest(imp.Request, name)
	lines := strings.Split(text, "\n")
	if strings.TrimSpace(lines[len(lines)-1]) != "" {
		block = "\n" + block // never glue the block onto the previous line
	}
	ed.SetCursor(len(lines)-1, 0)
	pasted := ed.PasteText(block)
	if f := httpfile.Parse(ed.Text()); len(f.Requests) > 0 {
		ed.JumpTo(f.Requests[len(f.Requests)-1].Line-1, 0)
	}

	notice := "curl import: added ### " + name
	level := host.Info
	if s := imp.IgnoredSummary(); s != "" {
		notice += " — ignored flags: " + s
		level = host.Warn
	}
	m.host.Notify(level, notice)
	return pasted
}

// curlRequestName names an imported block after what it does — "POST /things"
// — which is what a reader of the file needs and what the response history
// keys the request by.
func curlRequestName(r *httpfile.Request) string {
	target := r.Target
	if _, rest, ok := strings.Cut(target, "://"); ok {
		if slash := strings.Index(rest, "/"); slash >= 0 {
			target = rest[slash:]
		} else {
			target = "/"
		}
	}
	if q := strings.IndexAny(target, "?#"); q >= 0 {
		target = target[:q]
	}
	if target == "" {
		target = "/"
	}
	return r.Method + " " + target
}

// uniqueRequestName appends a counter while the file already holds a request
// of that name — two blocks sharing a name would share a history key.
func uniqueRequestName(f *httpfile.File, name string) string {
	taken := map[string]bool{}
	for _, r := range f.Requests {
		taken[r.Name] = true
	}
	for i := 2; taken[name]; i++ {
		if candidate := name + " " + strconv.Itoa(i); !taken[candidate] {
			return candidate
		}
	}
	return name
}

// copyHTTPRequestAsCurl runs http.copyAsCurl (#1994): the request block under
// the caret, resolved through the same variable chain a dispatch uses, on the
// clipboard as a runnable curl command.
func (m *Model) copyHTTPRequestAsCurl() tea.Cmd {
	ed, ok := m.httpEditor("curl")
	if !ok {
		return nil
	}
	f := httpfile.Parse(ed.Text())
	line, _ := ed.CursorPos()
	req, found := f.RequestAt(line + 1)
	if !found {
		m.host.Notify(host.Info, "curl: no request under the cursor")
		return nil
	}
	vars, hint, err := m.httpVars(ed.Path(), f)
	if err != nil {
		m.host.Notify(host.Error, "curl: "+err.Error())
		return nil
	}
	vars.Lookup = os.LookupEnv // closes the chain, exactly as dispatch does
	resolved, err := req.ResolveVars(vars)
	if err != nil {
		notice := "curl: " + err.Error()
		if hint != "" {
			notice += " — " + hint
		}
		m.host.Notify(host.Error, notice)
		return nil
	}
	// An external body is relative to the .http file (#1305) while curl
	// resolves "@path" against the working directory, so the exported command
	// carries the path from the file's directory.
	resolved.BodyFile = curlBodyPath(ed.Path(), resolved.BodyFile)
	clipboardWrite(httpfile.ExportCurl(resolved))
	m.host.Notify(host.Info, "copied "+requestLabel(req)+" as curl")
	return nil
}

// curlBodyPath rebases a request's external-body path onto the directory the
// exported command runs in.
func curlBodyPath(source, body string) string {
	if body == "" || filepath.IsAbs(body) || strings.HasPrefix(body, "~") {
		return body
	}
	return filepath.Join(filepath.Dir(source), body)
}
