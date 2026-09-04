package app

import (
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/httpfile"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
	"ike/internal/pane"
)

// http_vars.go warns about `{{name}}` references no rung of the variable
// chain defines (#2158). A typo'd `{{hsot}}` used to surface only at request
// time as a failed dispatch — late, and one pane away from the line that
// caused it. The warning rides the ordinary diagnostic path
// (http_diag.go → applyDiagnostics), so it marks the reference inline, reads
// in the diagnostic popup and lands in the Problems tool window like any
// other, and it names exactly what completion offers: the file's own
// `@name=value` definitions, the selected environment, and the values earlier
// responses captured.
//
// It runs where the author can act on it: after an edit goes quiet, after a
// dispatch (a capture may just have defined the name), and when the
// environment changes under the file.

// httpVarsSource labels the diagnostics in the Problems window and in the
// popup, the way a server name would.
const httpVarsSource = "http variables"

// httpVarsQuiet is how long an .http buffer stays quiet before it is linted.
// Long enough that typing a placeholder does not flash a warning at every
// keystroke, short enough to read as immediate once the hands stop.
var httpVarsQuiet = 400 * time.Millisecond

// httpVarsTickMsg wakes the model to lint the buffers whose quiet period
// expired. gen names the model that armed it (#2194); another model's tick is
// dropped.
type httpVarsTickMsg struct{ gen int64 }

// httpVarsOnSync is the change-seam hook (editor.SyncMsg, the autosave-idle
// pattern of #731): an edited .http buffer (re)arms its lint deadline, so a
// burst of keystrokes collapses into one pass after the edits stop.
func (m *Model) httpVarsOnSync(path string) tea.Cmd {
	if m.httpVarsDeb == nil || !isHTTPPath(path) {
		return nil
	}
	m.httpVarsDeb.Mark(path, time.Now())
	return m.armHTTPVarsTick()
}

// armHTTPVarsTick schedules one wake at the earliest pending deadline; the
// tick handler re-arms while marks remain (the backup.go pattern).
func (m *Model) armHTTPVarsTick() tea.Cmd {
	return m.armTick(&m.httpVarsTickArmed, m.httpVarsDeb, func(gen int64) tea.Msg {
		return httpVarsTickMsg{gen: gen}
	})
}

// lintDueHTTPVars lints every buffer whose quiet period expired and re-arms
// while marks remain.
func (m *Model) lintDueHTTPVars(now time.Time) tea.Cmd {
	m.httpVarsTickArmed = false
	if m.httpVarsDeb == nil {
		return nil
	}
	var cmds []tea.Cmd
	for _, path := range m.httpVarsDeb.Due(now) {
		if cmd := m.lintHTTPVars(path); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if cmd := m.armHTTPVarsTick(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// lintHTTPVars republishes the unknown-variable warnings of one request file.
// The buffer's *current* text is linted, not the file on disk — the warning
// belongs to what is being written. A file open nowhere is not linted: there
// is no editor to mark and no author reading it.
func (m *Model) lintHTTPVars(path string) tea.Cmd {
	if !isHTTPPath(path) {
		return nil
	}
	ed := m.editorForPath(path)
	if ed == nil {
		return nil
	}
	text := ed.Text()
	f := httpfile.Parse(text)
	vars, hint, err := m.httpVars(path, f)
	if err != nil || hint != "" {
		// A broken environment file, or a choice among several environments
		// the user has not made yet (the hint): either way every
		// environment-defined name would read as unknown, and a file full of
		// warnings about a JSON typo somewhere else is worse than none. Both
		// cases are already loud where they belong — the dispatch error names
		// the file and the unmade choice.
		return m.setHTTPDiags(path, httpVarsSource, nil)
	}
	// Close the chain the way a dispatch does (httpclient.variables): the
	// process environment is its last rung, so `{{HOME}}` is a defined name
	// and must not be warned about.
	vars.Lookup = os.LookupEnv
	return m.setHTTPDiags(path, httpVarsSource, httpVarDiagnostics(text, f, vars))
}

// relintHTTPVarsIn re-lints every open request file of dir — the environment
// they resolve against just changed, so a name that was unknown may now be
// defined and the other way round.
func (m *Model) relintHTTPVarsIn(dir string) tea.Cmd {
	want := canonicalPath(dir)
	var cmds []tea.Cmd
	seen := map[string]bool{}
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			path := ed.Path()
			if !ed.HasFile() || seen[path] || canonicalPath(filepath.Dir(path)) != want {
				continue
			}
			seen[path] = true
			if cmd := m.lintHTTPVars(path); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	return tea.Batch(cmds...)
}

// httpVarDiagnostics warns once per `{{name}}` reference the chain does not
// define. Every occurrence is marked rather than only the first: the fix goes
// wherever the name is written, the way a compiler reports each use.
//
// A name a `# @capture` directive declares counts as defined even before any
// response produced it — the polling request of a chain is written before the
// chain has ever run, and warning about it would flag correct files.
func httpVarDiagnostics(text string, f *httpfile.File, vars *httpfile.Vars) []ilsp.Diagnostic {
	declared := map[string]bool{}
	for _, name := range f.CaptureNames() {
		declared[name] = true
	}
	var out []ilsp.Diagnostic
	for _, ref := range httpfile.References(text) {
		if declared[ref.Name] || vars.Defines(ref.Name) {
			continue
		}
		line := ref.Line - 1 // diagnostics are 0-based
		out = append(out, ilsp.Diagnostic{
			Range: buffer.Range{
				Start: buffer.Position{Line: line, Col: ref.StartCol},
				End:   buffer.Position{Line: line, Col: ref.EndCol},
			},
			Severity: protocol.SeverityWarning,
			Message: "unknown variable " + ref.Name + ": no @" + ref.Name +
				" definition in this file, no entry in the selected environment, and no response captured it",
			Source: httpVarsSource,
			Code:   "unknown-variable",
		})
	}
	return out
}
