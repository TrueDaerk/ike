package app

import (
	"ike/internal/config"
	"ike/internal/httpfile"
	"ike/internal/pane"
	"ike/internal/secret"
)

// http_capture_hints.go shows what a `# @capture name = <jq>` directive
// (#1993) took out of its response (#3120): the value hangs as a dimmed hint
// at the end of the directive's line —
//
//	# @capture task = .task        = node-1:42
//
// A successful capture used to show nothing at all, so checking it meant
// sending the next request or reading the history JSON. The value shown is
// the one the variable chain would resolve for `{{name}}` right now
// (httpCaptured → httphistory.CapturedIn: newest response wins), so it is
// there again after a reopen and follows a later request that captured the
// same name. A directive whose capture failed keeps its warning
// (http_capture.go) and shows no value; one that never ran shows nothing.
//
// The hints ride the editor's line-note channel (editor/linenotes.go) and
// refresh wherever the unknown-variable lint does — after a dispatch has
// stored its entry, after an edit goes quiet, on an environment change — plus
// when the buffer opens and when http.capture_hints is toggled.

// captureHintMax is how many runes of a captured value the hint shows before
// it is cut with an ellipsis.
const captureHintMax = 60

// captureHintValue is the display form of one captured value: masked under
// a secret-suspect name exactly like the editor's secret masking draws it,
// line breaks folded to ⏎ (a hint is one row), cut to captureHintMax runes.
func captureHintValue(name, value string) string {
	if secret.Suspect(name) {
		return secret.Mask
	}
	return foldHintValue(value, captureHintMax)
}

// httpCaptureNotes maps the 0-based line of every capture directive of f that
// currently resolves to a value onto its hint text. Directives whose capture
// failed on the last dispatch (they carry a capture diagnostic) are left out.
func (m *Model) httpCaptureNotes(path string, f *httpfile.File) map[int]string {
	values := m.httpCaptured(path, f)
	if len(values) == 0 {
		return nil
	}
	failed := map[int]bool{}
	for _, d := range m.httpDiags[path][httpCaptureSource] {
		failed[d.Range.Start.Line] = true
	}
	notes := map[int]string{}
	for _, r := range f.Requests {
		for _, c := range r.Captures {
			line := c.Line - 1
			v, ok := values[c.Name]
			if !ok || line < 0 || failed[line] {
				continue
			}
			notes[line] = "= " + captureHintValue(c.Name, v)
		}
	}
	return notes
}

// refreshHTTPCaptureHints recomputes the hints of every open view of path
// from the buffer's current text; with http.capture_hints off it clears them.
func (m *Model) refreshHTTPCaptureHints(path string) {
	if !isHTTPPath(path) {
		return
	}
	views := m.editorViewsForPath(path)
	if len(views) == 0 {
		return
	}
	var notes map[int]string
	if config.Get().HTTP.CaptureHints {
		notes = m.httpCaptureNotes(path, httpfile.Parse(views[0].Text()))
	}
	for _, ed := range views {
		if len(notes) == 0 && !ed.HasLineNotes() {
			continue
		}
		ed.SetLineNotes(notes, ed.DocVersion())
	}
}

// refreshAllHTTPCaptureHints refreshes every open request file — the
// http.capture_hints toggle just changed.
func (m *Model) refreshAllHTTPCaptureHints() {
	seen := map[string]bool{}
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			path := ed.Path()
			if !ed.HasFile() || seen[path] || !isHTTPPath(path) {
				continue
			}
			seen[path] = true
			m.refreshHTTPCaptureHints(path)
		}
	}
}
