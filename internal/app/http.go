package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/httpclient"
	"ike/internal/httpfile"
	"ike/internal/httphistory"
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
	Source  string // .http file the request came from (history keying, #1251)
	Request string // httpfile request key, labels the viewer
	Resp    *httpclient.Response
	Err     error
}

// httpHistoryDir is the project-local response-history location (#1251),
// following the .ike/ convention of localHistoryDir.
func httpHistoryDir() string {
	base := ".ike"
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		base = d
	}
	return filepath.Join(base, "http")
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
	source := ed.Path()
	return func() tea.Msg {
		resp, err := httpclient.Dispatch(context.Background(), req, httpclient.Options{})
		return HTTPResponseMsg{Source: source, Request: key, Resp: resp, Err: err}
	}
}

// httpPanel returns the singleton viewer model, or nil when it is not open
// as a *visible* layout leaf. The registry entry alone is not enough (#1271):
// window.hideAllTools removes the leaf but keeps the instance registered, and
// filling an invisible pane looks like a dispatch that did nothing.
func (m Model) httpPanel() *httppane.Model {
	if !m.activeWS().Panes.Has(pane.HTTPKey) || !m.leafVisible(pane.HTTPKey) {
		return nil
	}
	return m.activeWS().Panes.Get(pane.HTTPKey).HTTP()
}

// leafVisible reports whether key is a leaf of the active layout tree.
func (m Model) leafVisible(key string) bool {
	for _, k := range layout.Leaves(m.activeWS().Tree) {
		if k == key {
			return true
		}
	}
	return false
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
	existed := m.activeWS().Panes.Has(pane.HTTPKey)
	key := m.activeWS().Panes.AddHTTP()
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, layout.ZoneBottom)
	if !ok {
		if !existed {
			// Only a freshly created instance is discarded — a hidden one
			// (hideAllTools) keeps its registration for the restore (#1271).
			m.activeWS().Panes.Close(key)
		}
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
		// Reopening failed (no target leaf, empty tree) — never swallow a
		// finished dispatch silently (#1271).
		m.host.Notify(host.Error, "http: response received but the viewer cannot open — "+msg.Request)
		return
	}
	p.Set(msg.Request, msg.Resp)
	if msg.Source != "" {
		// Persist under .ike/http/ and hand the stored predecessors to the
		// viewer for h/l browsing (#1251); best effort like local history.
		store := httphistory.New(httpHistoryDir())
		store.Append(msg.Source, msg.Request, httphistory.FromResponse(msg.Resp, time.Now()))
		entries := store.List(msg.Source, msg.Request)
		items := make([]httppane.HistoryItem, 0, len(entries))
		for _, e := range entries {
			items = append(items, httppane.HistoryItem{Resp: e.Response(msg.Request), At: e.Time})
		}
		p.SetHistory(items)
	}
	m.layout()
}
