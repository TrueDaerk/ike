package app

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/domview"
	"ike/internal/editor"
	"ike/internal/editor/buffer"
	"ike/internal/htmldom"
	"ike/internal/layout"
	"ike/internal/pane"
)

// dom_panel.go wires the DOM inspector tool window (#1929): a singleton
// right-split pane showing the focused HTML buffer's parsed DOM tree with a
// CSS selector tester, mirroring the Structure panel's toggle state machine.
// The buffer parses off the UI loop (domSyncCmd spawns a command goroutine
// keyed by path + document version), the pane's selector matches route to
// every editor showing the file as a DOMMatchesMsg overlay, and node jumps go
// through the standard open funnel.

// DOMToggleMsg runs dom.toggle.
type DOMToggleMsg struct{}

// domParsedMsg delivers one async parse result; path and version tag it
// against the request so a stale delivery is recognizable.
type domParsedMsg struct {
	path    string
	version int
	doc     *htmldom.Document
}

// toggleDOMPanel is the dom.toggle state machine, mirroring
// toggleStructurePanel: no panel → open at the right; unfocused → focus it;
// focused → return focus to the remembered pane.
func (m *Model) toggleDOMPanel() {
	m.togglePanel(pane.DOMKey, func() tea.Cmd { m.openDOMPanel(); return nil })
}

// domPanel returns the singleton panel model, or nil when it is not open.
func (m Model) domPanel() *domview.Model {
	if !m.activeWS().Panes.Has(pane.DOMKey) {
		return nil
	}
	return m.activeWS().Panes.Get(pane.DOMKey).DOM()
}

// openDOMPanel splits the active editor (fallback: focused leaf) at the right
// with the singleton panel; the first sync pass parses the buffer.
func (m *Model) openDOMPanel() {
	m.openToolPane(m.activeWS().Panes.AddDOM, fixedZone(layout.ZoneRight), func(string) {
		m.domReqPath = "" // a fresh open always parses
	})
}

// isHTMLPath reports whether the file is one the DOM inspector parses,
// matching the html language registration's extensions.
func isHTMLPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm", ".xhtml":
		return true
	}
	return false
}

// domSyncCmd runs once per settled Update pass (the Update wrapper): while
// the panel is open it follows the active editor's cursor (enclosing node
// highlight) and spawns an async reparse when the shown tree belongs to
// another file or an older document version. The request dedup (domReqPath +
// domReqVersion) keeps one buffer state from parsing twice. The selector
// match highlights ride the same pass: whenever the pane's match revision
// moved, the ranges re-route to every editor showing the file.
func (m *Model) domSyncCmd() tea.Cmd {
	sp := m.domPanel()
	// The common case is no inspector and no highlighted file (#2187): with
	// both absent there is nothing to follow and nothing to clear, so the
	// pass costs one map lookup and a string compare instead of walking to
	// the active editor.
	if sp == nil && m.domHLPath == "" {
		return nil
	}
	var cmds []tea.Cmd
	if sp != nil {
		if key := m.activeEditorKey(); key != "" {
			if ed := m.activeWS().Panes.Get(key).Editor(); ed != nil && ed.HasFile() {
				if cmd := m.domBufferSync(sp, ed); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
	}
	if hl := m.domHighlightSync(sp); hl != nil {
		cmds = append(cmds, hl)
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// domBufferSync follows the cursor when the shown tree is current, and
// spawns the parse goroutine when it is not.
func (m *Model) domBufferSync(sp *domview.Model, ed *editor.Model) tea.Cmd {
	path := ed.Path()
	if !isHTMLPath(path) {
		sp.SetNotHTML(path)
		return nil
	}
	version := ed.DocVersion()
	if sp.Path() == path && sp.DocVersion() == version {
		line, col := ed.Cursor() // 1-based
		sp.FollowCursor(line-1, col-1)
		return nil
	}
	if m.domReqPath == path && m.domReqVersion == version {
		return nil // this buffer state is already parsing
	}
	m.domReqPath, m.domReqVersion = path, version
	text := ed.Text()
	return func() tea.Msg {
		// Off the UI loop: multi-MB fixtures parse without a stall.
		return domParsedMsg{path: path, version: version, doc: htmldom.Parse(text)}
	}
}

// domHighlightSync re-routes the pane's selector-match ranges to the editors
// whenever the match revision moved, clearing a previously highlighted file
// when the pane switched buffers or closed.
func (m *Model) domHighlightSync(sp *domview.Model) tea.Cmd {
	if sp == nil {
		if m.domHLPath == "" {
			return nil
		}
		path := m.domHLPath
		m.domHLPath, m.domHLRev = "", 0
		return m.routeToEditor(path, editor.DOMMatchesMsg{Path: path, Current: -1})
	}
	if sp.Path() == m.domHLPath && sp.MatchesRev() == m.domHLRev {
		return nil
	}
	var cmds []tea.Cmd
	if m.domHLPath != "" && m.domHLPath != sp.Path() {
		if cmd := m.routeToEditor(m.domHLPath, editor.DOMMatchesMsg{Path: m.domHLPath, Current: -1}); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	m.domHLPath, m.domHLRev = sp.Path(), sp.MatchesRev()
	if sp.Path() != "" {
		ranges, cur := sp.MatchRanges()
		out := make([]buffer.Range, len(ranges))
		for i, r := range ranges {
			out[i] = buffer.Range{
				Start: buffer.Position{Line: r.StartLine, Col: r.StartCol},
				End:   buffer.Position{Line: r.EndLine, Col: r.EndCol},
			}
		}
		if cmd := m.routeToEditor(sp.Path(), editor.DOMMatchesMsg{Path: sp.Path(), Ranges: out, Current: cur}); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// applyDOMParsed feeds an async parse result to the open panel; a delivery
// older than the newest request is dropped (its successor is in flight).
func (m *Model) applyDOMParsed(msg domParsedMsg) {
	sp := m.domPanel()
	if sp == nil {
		return
	}
	if msg.path != m.domReqPath || msg.version != m.domReqVersion {
		return
	}
	sp.SetDoc(msg.path, msg.version, msg.doc)
}
