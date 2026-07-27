package app

import (
	"context"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/httpclient"
	"ike/internal/httpfile"
	"ike/internal/httppane"
	"ike/internal/layout"
	"ike/internal/pane"
)

// http.go wires the HTTP client UX (#1250, epic #1247): http.run dispatches
// the .http request block under the focused editor's cursor via
// internal/httpclient and shows the result in the singleton read-only
// response viewer (internal/httppane), which is reused across dispatches
// like the Usages tool window (#1155).

// HTTPRunMsg runs http.run: dispatch the request under the cursor.
type HTTPRunMsg struct{}

// HTTPResponseMsg delivers one finished dispatch back into the update loop.
type HTTPResponseMsg struct {
	Request string // httpfile request key, labels the viewer
	Resp    *httpclient.Response
	Err     error
}

// isHTTPPath reports whether path is a request file the HTTP client runs.
func isHTTPPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == httpfile.Ext || ext == ".rest"
}

// runHTTPRequestAtCursor resolves the request block under the focused
// editor's cursor and dispatches it off-loop; the response (or error)
// returns as an HTTPResponseMsg.
func (m *Model) runHTTPRequestAtCursor() tea.Cmd {
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "http: focus a file tab first")
		return nil
	}
	if !isHTTPPath(ed.Path()) {
		m.host.Notify(host.Info, "http: not an .http file")
		return nil
	}
	f := httpfile.Parse(ed.Text())
	line, _ := ed.CursorPos()
	req, ok := f.RequestAt(line + 1)
	if !ok {
		for _, e := range f.Errors {
			if line+1 >= e.Line {
				m.host.Notify(host.Error, "http: "+e.Error())
				return nil
			}
		}
		m.host.Notify(host.Info, "http: no request under the cursor")
		return nil
	}
	key := req.Key()
	return func() tea.Msg {
		resp, err := httpclient.Dispatch(context.Background(), req, httpclient.Options{})
		return HTTPResponseMsg{Request: key, Resp: resp, Err: err}
	}
}

// httpPanel returns the singleton viewer model, or nil when it is not open.
func (m Model) httpPanel() *httppane.Model {
	if !m.activeWS().Panes.Has(pane.HTTPKey) {
		return nil
	}
	return m.activeWS().Panes.Get(pane.HTTPKey).HTTP()
}

// openHTTPPanel splits the active editor (fallback: focused leaf) at the
// bottom with the singleton response viewer.
func (m *Model) openHTTPPanel() {
	target := m.activeEditorKey()
	if target == "" {
		target = m.activeWS().Panes.Focused()
	}
	if target == "" || m.activeWS().Tree == nil {
		return
	}
	key := m.activeWS().Panes.AddHTTP()
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, layout.ZoneBottom)
	if !ok {
		m.activeWS().Panes.Close(key)
		return
	}
	m.activeWS().Tree = tree
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// fillHTTPPanel routes one dispatch result into the viewer, opening it first
// when it is not part of the layout — the reuse path: a later dispatch
// replaces the content of the existing pane.
func (m *Model) fillHTTPPanel(msg HTTPResponseMsg) {
	if msg.Err != nil {
		m.host.Notify(host.Error, "http: "+msg.Err.Error())
		return
	}
	if m.httpPanel() == nil {
		m.openHTTPPanel()
	}
	p := m.httpPanel()
	if p == nil {
		return
	}
	p.Set(msg.Request, msg.Resp)
	m.layout()
}
