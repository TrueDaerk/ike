package app

import (
	"sort"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
)

// http_diag.go merges the diagnostics of the two producers an .http file has
// (#2158): failed `# @capture` directives (http_capture.go, #1993) and
// unknown `{{name}}` references (http_vars.go). Both publish through
// applyDiagnostics, which replaces a path's whole set — so whichever
// published last would erase the other. Each producer's set is therefore kept
// per file and the union is what reaches the editor: a producer that has
// nothing to say clears its own share and leaves the other's markers alone.
//
// `.http` files have no language server, so nothing else publishes for these
// paths and the set is ours alone.

// setHTTPDiags records owner's diagnostics for path and republishes the
// file's merged set. An empty set clears that owner's share.
func (m *Model) setHTTPDiags(path, owner string, diags []ilsp.Diagnostic) tea.Cmd {
	if path == "" {
		return nil
	}
	if m.httpDiags == nil {
		m.httpDiags = map[string]map[string][]ilsp.Diagnostic{}
	}
	byOwner, ok := m.httpDiags[path]
	if !ok {
		byOwner = map[string][]ilsp.Diagnostic{}
		m.httpDiags[path] = byOwner
	}
	if len(diags) == 0 {
		delete(byOwner, owner)
	} else {
		byOwner[owner] = diags
	}
	merged := m.httpDiagsFor(path)
	if len(byOwner) == 0 {
		delete(m.httpDiags, path)
	}
	cmd := m.applyDiagnostics(path, merged)
	m.refreshProblemsPanel()
	return cmd
}

// httpDiagsFor is one file's merged set, ordered by position so the Problems
// window and the diagnostic popup read top-down like every other set.
func (m *Model) httpDiagsFor(path string) []ilsp.Diagnostic {
	var out []ilsp.Diagnostic
	for _, diags := range m.httpDiags[path] {
		out = append(out, diags...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Range.Start, out[j].Range.Start
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Col < b.Col
	})
	return out
}
