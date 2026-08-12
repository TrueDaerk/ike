package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/help"
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

// HTTPCopyBodyMsg runs http.copyBody: the shown response body to the
// clipboard (#1266).
type HTTPCopyBodyMsg struct{}

// HTTPCopyHeadersMsg runs http.copyHeaders: status line plus headers.
type HTTPCopyHeadersMsg struct{}

// HTTPCopyFoldMsg runs http.copyFold: the fold at the top of the response
// viewer, hidden rows included, to the clipboard (#1787).
type HTTPCopyFoldMsg struct{}

// HTTPResponseHistoryMsg runs http.responseHistory: focus the viewer and
// report how many stored responses the current request has (#1267).
type HTTPResponseHistoryMsg struct{}

// HTTPShowResponseMsg runs http.showResponse: show the stored responses of
// the request under the cursor without dispatching it (#1492).
type HTTPShowResponseMsg struct{}

// HTTPResendMsg runs http.resend: send the shown response's stored request
// again, exactly as it went out (#1832).
type HTTPResendMsg struct{}

// HTTPResponseMsg delivers one finished dispatch back into the update loop.
type HTTPResponseMsg struct {
	Source  string // .http file the request came from (history keying, #1251)
	Request string // httpfile request key, labels the viewer
	Resp    *httpclient.Response
	Err     error
}

// HTTPStreamStartMsg announces a recognized streaming response (#1776): the
// headers arrived, the body will follow chunk by chunk. events is the
// dispatch's event channel the update loop keeps reading from.
type HTTPStreamStartMsg struct {
	Source  string
	Request string
	Status  string
	Proto   string
	Headers http.Header
	events  chan tea.Msg
}

// HTTPStreamChunkMsg carries one received body chunk of a live stream
// (#1776); the final HTTPResponseMsg still follows and finalizes the viewer.
type HTTPStreamChunkMsg struct {
	Source  string
	Request string
	Chunk   []byte
	events  chan tea.Msg
}

// nextHTTPEvent reads the next event of a streaming dispatch off-loop — the
// usual bubbletea channel pump: each stream message re-arms it until the
// final HTTPResponseMsg ends the chain.
func nextHTTPEvent(events chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-events }
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
	return m.dispatchHTTP(ed.Path(), req.Key(), requestLabel(req),
		func(ctx context.Context, source, key string, cb httpclient.StreamCallbacks) (*httpclient.Response, error) {
			// The .http file's directory anchors relative external-body paths
			// (#1305): `< ./payload.json` is relative to the request file, not
			// to wherever IKE was started.
			return httpclient.DispatchStream(ctx, req,
				httpclient.Options{BaseDir: filepath.Dir(source)}, cb)
		})
}

// resendHTTPRequest runs http.resend (ctrl+r in the pane, its header button,
// the palette — #1832): the response on show carries the request as it was
// sent, and that snapshot goes out again unchanged. Nothing is parsed,
// nothing is substituted, so an edited .http file, a changed variable or a
// switched environment cannot alter what is repeated. The answer lands in the
// pane and in the history like any other dispatch, snapshot included.
func (m *Model) resendHTTPRequest() tea.Cmd {
	p := m.httpPanel()
	if p == nil {
		m.host.Notify(host.Info, "http: no response pane open")
		return nil
	}
	snap := p.CurrentRequest()
	if snap == nil {
		// Either a legacy history entry (written before the capture existed)
		// or a live stream — say which, never fail silently.
		m.host.Notify(host.Info, "http: this response has no stored request — re-run it from the .http file with http.run")
		return nil
	}
	key := p.Request()
	return m.dispatchHTTP(m.httpPaneSource, key, snap.Label(),
		func(ctx context.Context, _, key string, cb httpclient.StreamCallbacks) (*httpclient.Response, error) {
			return httpclient.Resend(ctx, key, snap, httpclient.Options{}, cb)
		})
}

// dispatchHTTP runs one exchange off-loop and pumps its events into the
// update loop — shared by http.run and http.resend. send performs the actual
// call; everything around it (duplicate guard, in-flight bookkeeping, the
// event channel) is the same for both.
//
// The exchange runs on its own goroutine and reports through events: a
// non-streaming response yields exactly one HTTPResponseMsg, a recognized
// stream (#1776) first a start message, then one chunk message per received
// piece, then the finalizing HTTPResponseMsg. The buffered channel gives
// natural backpressure — a slow UI slows the read, never drops data.
func (m *Model) dispatchHTTP(source, key, label string,
	send func(ctx context.Context, source, key string, cb httpclient.StreamCallbacks) (*httpclient.Response, error)) tea.Cmd {
	flightKey := httpFlightKey(source, key)
	if _, running := m.httpFlight[flightKey]; running {
		// Duplicate-dispatch guard (#1272): never fire the same request twice
		// in parallel behind the user's back.
		m.host.Notify(host.Info, "http: "+key+" is already running — cancel it with http.cancel (or x in the response pane)")
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	tick := m.startHTTPFlight(flightKey, &httpFlightEntry{
		label:   label,
		request: key,
		started: time.Now(),
		cancel:  cancel,
	})
	events := make(chan tea.Msg, 32)
	dispatch := func() tea.Msg {
		go func() {
			resp, err := send(ctx, source, key, httpclient.StreamCallbacks{
				OnHeaders: func(status string, _ int, proto string, headers http.Header) {
					events <- HTTPStreamStartMsg{Source: source, Request: key,
						Status: status, Proto: proto, Headers: headers, events: events}
				},
				OnChunk: func(chunk []byte) {
					events <- HTTPStreamChunkMsg{Source: source, Request: key,
						Chunk: chunk, events: events}
				},
			})
			cancel() // release the context regardless of the outcome
			events <- HTTPResponseMsg{Source: source, Request: key, Resp: resp, Err: err}
			close(events)
		}()
		return <-events
	}
	return tea.Batch(dispatch, tick)
}

// httpPanel returns the singleton viewer model, or nil when it is not open
// under a *visible* layout leaf. The registry entry alone is not enough
// (#1271): window.hideAllTools removes the leaf but keeps the instance
// registered, and filling an invisible pane looks like a dispatch that did
// nothing. The viewer may live as a content tab of a tab host (#1778) — a
// background tab still fills, like a background JetBrains tab would.
func (m Model) httpPanel() *httppane.Model {
	hostKey, _, inst, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindHTTP
	})
	if !ok || !m.leafVisible(hostKey) {
		return nil
	}
	return inst.HTTP()
}

// focusHTTPPanel focuses the HTTP viewer wherever it lives — its own pane or
// a tab of a host (#1778), activating that tab.
func (m *Model) focusHTTPPanel() {
	if hostKey, tabIdx, _, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindHTTP
	}); ok {
		m.focusContentAt(hostKey, tabIdx)
	}
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
// adaptive placement (auxZone, #1588) with the singleton response viewer.
func (m *Model) openHTTPPanel() {
	// The viewer may already live as a content tab of a tab host (#1778):
	// nothing to open then — httpPanel resolves it and the fill lands there.
	if _, tabIdx, _, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindHTTP
	}); ok && tabIdx >= 0 {
		return
	}
	target := m.activeEditorKey()
	if target == "" {
		target = m.activeWS().Panes.Focused()
	}
	if target == "" || m.activeWS().Tree == nil {
		return
	}
	existed := m.activeWS().Panes.Has(pane.HTTPKey)
	key := m.activeWS().Panes.AddHTTP()
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, m.auxZone(target))
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

// beginHTTPStream routes a stream start into the viewer (#1776), opening it
// like fillHTTPPanel does: status and headers show immediately, the body
// grows via appendHTTPStream. A viewer that cannot open is no failure — the
// finalizing HTTPResponseMsg still lands and reports.
func (m *Model) beginHTTPStream(msg HTTPStreamStartMsg) {
	if m.httpPanel() == nil {
		m.openHTTPPanel()
	}
	p := m.httpPanel()
	if p == nil {
		return
	}
	p.StartStream(msg.Request, msg.Proto, msg.Status, msg.Headers)
	m.layout()
}

// appendHTTPStream feeds one live chunk into the viewer (#1776). A closed or
// re-purposed viewer (history browsing ends the live view) drops the chunk —
// the finalizing response carries the whole body anyway.
func (m *Model) appendHTTPStream(msg HTTPStreamChunkMsg) {
	if p := m.httpPanel(); p != nil {
		p.AppendStream(msg.Chunk)
	}
}

// fillHTTPPanel routes one dispatch result into the viewer, opening it first
// when it is not part of the layout — the reuse path: a later dispatch
// replaces the content of the existing pane.
func (m *Model) fillHTTPPanel(msg HTTPResponseMsg) {
	canceled := m.finishHTTPFlight(httpFlightKey(msg.Source, msg.Request))
	if msg.Err != nil {
		if canceled || errors.Is(msg.Err, context.Canceled) {
			// The user aborted it (#1272): a confirmation, not a failure.
			m.host.Notify(host.Info, "http: "+msg.Request+" canceled")
			return
		}
		m.host.Notify(host.Error, "http: "+msg.Err.Error())
		return
	}
	if canceled {
		// A canceled *stream* still returns its partial response (#1776): what
		// arrived stays visible and reaches the history, the notice says why
		// the body ends where it does.
		m.host.Notify(host.Info, "http: "+msg.Request+" canceled — keeping the partial response")
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
	m.httpPaneSource = msg.Source // where a re-send's answer is stored (#1832)
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

// copyHTTPResponse copies the shown response to the system clipboard
// (#1266): the headers block when headers is true, the body otherwise. It
// goes through the pane's CopyMsg like the pane-local "y"/"Y" keys, so both
// entry points share one confirmation path.
func (m *Model) copyHTTPResponse(headers bool) tea.Cmd {
	p := m.httpPanel()
	if p == nil {
		m.host.Notify(host.Info, "http: no response pane open")
		return nil
	}
	text, what := p.BodyText(), "response body"
	if headers {
		text, what = p.HeadersText(), "response headers"
	}
	if text == "" {
		m.host.Notify(host.Info, "http: nothing to copy")
		return nil
	}
	return func() tea.Msg { return httppane.CopyMsg{Text: text, What: what} }
}

// copyHTTPFold runs http.copyFold (#1787): the response viewer's target fold
// goes to the clipboard whole, through the same CopyMsg path as "y"/"Y".
func (m *Model) copyHTTPFold() tea.Cmd {
	p := m.httpPanel()
	if p == nil {
		m.host.Notify(host.Info, "http: no response pane open")
		return nil
	}
	cmd := p.CopyTargetFold()
	if cmd == nil {
		m.host.Notify(host.Info, "http: no fold to copy")
	}
	return cmd
}

// httpPaneKeys documents the response viewer's pane-local keys for the help
// overlay (#1267): they belong to no command in the registry, so the
// cheatsheet would otherwise never mention that responses can be browsed,
// searched or copied at all.
var httpPaneKeys = []struct{ Key, Title string }{
	{"h / l  ← / →", "Browse older / newer stored response"},
	{"s", "Keep scroll position while browsing history (per request)"},
	{"j / k", "Scroll"},
	{"shift+← / shift+→", "Pan sideways in wide lines"},
	{"0 / $", "Left edge / right edge"},
	{"g / G", "Top / bottom"},
	{"/", "Search in the response"},
	{"n / N", "Next / previous match"},
	{"za / zc / zo / zM / zR", "Toggle / close / open folds"},
	{"zy", "Copy the target fold whole (or click its ⧉)"},
	{"y", "Copy selection (or the whole body)"},
	{"Y", "Copy status line and headers"},
	{"ctrl+r", "Re-send this response's request unchanged (or click ⟳ re-send)"},
	{"x", "Cancel the running request"},
	{"esc", "Clear search and selection"},
}

// showHTTPHistory runs http.responseHistory (#1267): it focuses the response
// viewer and says how many stored responses the current request has, making
// the h/l browsing discoverable from the palette.
func (m *Model) showHTTPHistory() {
	p := m.httpPanel()
	if p == nil {
		m.host.Notify(host.Info, "http: no response yet — run an .http request first")
		return
	}
	m.focusHTTPPanel() // the viewer may live in a tab (#1778)
	m.layout()
	idx, n := p.HistoryIndex()
	if n <= 1 {
		m.host.Notify(host.Info, "http: 1 stored response — ←/→ browse older/newer ones as they arrive")
		return
	}
	m.host.Notify(host.Info, fmt.Sprintf("http: showing %d/%d stored responses — ←/→ browse", idx+1, n))
}

// showStoredHTTPResponse runs http.showResponse (#1492): it loads the stored
// responses of the request block under the cursor from .ike/http/ and shows
// them in the viewer without dispatching anything — the way to look at what
// request A answered while the pane still shows request B. The h/l browsing
// then works exactly as after a dispatch (#1251).
func (m *Model) showStoredHTTPResponse() {
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "http: focus a file tab first")
		return
	}
	if !isHTTPPath(ed.Path()) {
		m.host.Notify(host.Info, "http: not an .http file")
		return
	}
	f := httpfile.Parse(ed.Text())
	line, _ := ed.CursorPos()
	req, ok := f.RequestAt(line + 1)
	if !ok {
		m.host.Notify(host.Info, "http: no request under the cursor")
		return
	}
	key := req.Key()
	entries := httphistory.New(httpHistoryDir()).List(ed.Path(), key)
	if len(entries) == 0 {
		m.host.Notify(host.Info, "http: no stored responses for "+key+" — dispatch it once with http.run")
		return
	}
	if m.httpPanel() == nil {
		m.openHTTPPanel()
	}
	p := m.httpPanel()
	if p == nil {
		// Same failure mode as fillHTTPPanel (#1271): never pretend silently.
		m.host.Notify(host.Error, "http: stored responses exist but the viewer cannot open — "+key)
		return
	}
	items := make([]httppane.HistoryItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, httppane.HistoryItem{Resp: e.Response(key), At: e.Time})
	}
	p.Set(key, items[0].Resp)
	p.SetHistory(items)
	m.httpPaneSource = ed.Path() // a re-send from here stores under the same key (#1832)
	m.focusHTTPPanel()           // the viewer may live in a tab (#1778)
	m.layout()
	m.host.Notify(host.Info, fmt.Sprintf("http: %d stored response(s) for %s — ←/→ browse", len(items), key))
}

// paneKeysHelpGroup lists the focused pane's local keys for the cheatsheet
// (#1267). Only the HTTP response viewer contributes so far; other panes can
// join the same way.
func (m Model) paneKeysHelpGroup() help.Group {
	g := help.Group{}
	if m.focusContext() != "http" {
		return g
	}
	g.Label = "http response pane"
	for _, k := range httpPaneKeys {
		g.Entries = append(g.Entries, help.Entry{ID: "http.pane." + k.Key, Title: k.Title, Shortcut: k.Key})
	}
	return g
}

// --- in-flight tracking (#1272) ---

// httpFlightEntry is one dispatch currently running: what it is, since when,
// and how to abort it.
type httpFlightEntry struct {
	label   string // "GET /_cat/indices", for the indicator
	request string // request key, for the pane's pending marker
	started time.Time
	cancel  context.CancelFunc
	// canceled marks an abort the user asked for, so the resulting
	// context.Canceled reads as a confirmation instead of a transport error.
	canceled bool
}

// httpTickMsg repaints the in-flight indicator while requests run.
type httpTickMsg struct{}

// HTTPCancelMsg runs http.cancel: abort every in-flight dispatch (#1272).
type HTTPCancelMsg struct{}

// httpFlightTick is the indicator's repaint interval — fast enough that the
// elapsed time visibly moves, slow enough to stay free.
const httpFlightTick = 250 * time.Millisecond

// httpFlightKey identifies one request across dispatches.
func httpFlightKey(source, request string) string { return source + "\x00" + request }

// httpFlightSegment renders the statusline indicator: the running request
// with its elapsed time, or a count when several run at once.
func (m Model) httpFlightSegment() string {
	if len(m.httpFlight) == 0 {
		return ""
	}
	if len(m.httpFlight) > 1 {
		oldest := time.Now()
		for _, e := range m.httpFlight {
			if e.started.Before(oldest) {
				oldest = e.started
			}
		}
		return fmt.Sprintf("⟳ http: %d requests (%s)", len(m.httpFlight), elapsed(oldest))
	}
	for _, e := range m.httpFlight {
		return fmt.Sprintf("⟳ http: %s (%s)", e.label, elapsed(e.started))
	}
	return ""
}

// elapsed formats a running duration for the indicator.
func elapsed(since time.Time) string {
	d := time.Since(since)
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// requestLabel is the short "METHOD /path" form the indicator shows.
func requestLabel(req *httpfile.Request) string {
	target := req.Target
	if u, err := url.Parse(target); err == nil && u.Path != "" {
		target = u.Path
		if u.RawQuery != "" {
			target += "?" + u.RawQuery
		}
	}
	if len(target) > 40 {
		target = target[:39] + "…"
	}
	return req.Method + " " + target
}

// startHTTPFlight registers a dispatch and returns its context plus the tick
// command that keeps the indicator moving.
func (m *Model) startHTTPFlight(key string, e *httpFlightEntry) tea.Cmd {
	if m.httpFlight == nil {
		m.httpFlight = map[string]*httpFlightEntry{}
	}
	first := len(m.httpFlight) == 0
	m.httpFlight[key] = e
	m.markHTTPPending()
	m.refreshHTTPFlightMarks()
	if !first {
		return nil // a tick loop is already running
	}
	return tea.Tick(httpFlightTick, func(time.Time) tea.Msg { return httpTickMsg{} })
}

// finishHTTPFlight drops a finished dispatch and reports whether the user had
// canceled it.
func (m *Model) finishHTTPFlight(key string) (canceled bool) {
	e, ok := m.httpFlight[key]
	if !ok {
		return false
	}
	delete(m.httpFlight, key)
	m.markHTTPPending()
	m.refreshHTTPFlightMarks()
	return e.canceled
}

// httpFlightMarks builds the inline indicators for one .http buffer (#1746):
// 0-based request line to "⟳ <elapsed>", for the requests of that file that
// are running right now. The file's text is re-parsed rather than the dispatch
// line remembered, so the indicator follows the request when the user edits
// above it while the dispatch is out.
func (m Model) httpFlightMarks(path, text string) map[int]string {
	if path == "" || len(m.httpFlight) == 0 {
		return nil
	}
	running := make(map[string]*httpFlightEntry, len(m.httpFlight))
	for key, e := range m.httpFlight {
		if key == httpFlightKey(path, e.request) {
			running[e.request] = e
		}
	}
	if len(running) == 0 {
		return nil
	}
	var marks map[int]string
	for _, req := range httpfile.Parse(text).Requests {
		e, ok := running[req.Key()]
		if !ok || req.Line <= 0 {
			continue
		}
		if marks == nil {
			marks = map[int]string{}
		}
		marks[req.Line-1] = "⟳ " + elapsed(e.started)
	}
	return marks
}

// refreshHTTPFlightMarks pushes the indicators into every open editor of the
// active workspace. It runs on every flight tick — that is what keeps the
// durations moving — and whenever a dispatch starts or finishes, which is what
// makes the indicator appear and disappear. Editors of other files (and every
// editor once nothing runs) are cleared, so no marker can outlive its request.
func (m *Model) refreshHTTPFlightMarks() {
	idle := len(m.httpFlight) == 0
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if idle || !ed.HasFile() || !isHTTPPath(ed.Path()) {
				ed.SetHTTPFlight(nil)
				continue
			}
			ed.SetHTTPFlight(m.httpFlightMarks(ed.Path(), ed.Text()))
		}
	}
}

// markHTTPPending mirrors the in-flight state into the response pane, so the
// viewer says "running" instead of presenting a stale response as current.
func (m *Model) markHTTPPending() {
	p := m.httpPanel()
	if p == nil {
		return
	}
	for _, e := range m.httpFlight {
		p.SetPending(e.request, e.started)
		return
	}
	p.ClearPending()
}

// cancelHTTPRequests aborts every in-flight dispatch (http.cancel, palette,
// and "x" in the response pane).
func (m *Model) cancelHTTPRequests() {
	if len(m.httpFlight) == 0 {
		m.host.Notify(host.Info, "http: no request is running")
		return
	}
	n := 0
	for _, e := range m.httpFlight {
		e.canceled = true
		if e.cancel != nil {
			e.cancel()
			n++
		}
	}
	m.host.Notify(host.Info, fmt.Sprintf("http: canceling %d request(s)", n))
}
