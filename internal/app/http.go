package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/editor"
	"ike/internal/help"
	"ike/internal/host"
	"ike/internal/httpclient"
	"ike/internal/httpfile"
	"ike/internal/httphistory"
	"ike/internal/httppane"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/telemetry"
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

// HTTPCopyResponseMsg runs http.copyResponse: the response pane's own copy
// key as a bound command (#2315) — the selection when the pane has one, else
// the whole body.
type HTTPCopyResponseMsg struct{}

// HTTPCopyHeadersMsg runs http.copyHeaders: status line plus headers.
type HTTPCopyHeadersMsg struct{}

// HTTPCopyFoldMsg runs http.copyFold: the fold at the top of the response
// viewer, hidden rows included, to the clipboard (#1787).
type HTTPCopyFoldMsg struct{}

// HTTPToggleRawBodyMsg runs http.toggleRawBody: the shown response body
// switches between pretty-printed and as-received (#2157).
type HTTPToggleRawBodyMsg struct{}

// HTTPJQPlaygroundMsg runs http.jqPlayground: the jq playground opens over the
// shown response body (#2157) — the palette pendant of the viewer's "q".
type HTTPJQPlaygroundMsg struct{}

// HTTPLoadMoreBodyMsg runs http.loadMoreBody: one more window of a spooled
// response body is pulled into the viewer (#2157).
type HTTPLoadMoreBodyMsg struct{}

// HTTPOpenBodyFileMsg runs http.openBodyFile: the complete body of a spooled
// response opens as a file (#2157).
type HTTPOpenBodyFileMsg struct{}

// HTTPResponseHistoryMsg runs http.responseHistory: focus the viewer and
// report how many stored responses the current request has (#1267).
type HTTPResponseHistoryMsg struct{}

// HTTPShowResponseMsg runs http.showResponse: show the stored responses of
// the request under the cursor without dispatching it (#1492).
type HTTPShowResponseMsg struct{}

// HTTPResendMsg runs http.resend: send the shown response's stored request
// again, exactly as it went out (#1832).
type HTTPResendMsg struct{}

// HTTPRerunMsg runs http.rerun: dispatch the shown history entry's request
// again from its .http file, with the current variables and environment
// (#2247).
type HTTPRerunMsg struct{}

// HTTPDiffResponsesMsg runs http.diffResponses: compare the shown stored
// response with another one of the same request, side by side (#1992).
type HTTPDiffResponsesMsg struct{}

// HTTPDiffPreviousRunMsg runs http.diffPreviousRun: compare the shown stored
// response directly with the run before it, no picker (#2060).
type HTTPDiffPreviousRunMsg struct{}

// HTTPSelectEnvMsg runs http.selectEnvironment: pick the http-client.env.json
// environment the file's {{name}} placeholders resolve against (#1867).
type HTTPSelectEnvMsg struct{}

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

// httpStreamQuiet is the chunk coalescing window (#2176), the terminal's
// notifyQuiet idea: an SSE endpoint delivering thousands of tiny chunks used
// to cost one full Update+render — plus a viewer resync — per chunk, which is
// quadratic over the stream. One window's bytes arrive as one message; short
// enough that a live stream still reads as live.
const httpStreamQuiet = 12 * time.Millisecond

// httpChunkCoalescer buffers a stream's received body chunks and emits one
// HTTPStreamChunkMsg per quiet window (#2176). The mutex is held across the
// channel send — like the debugEventCoalescer — so the flush timer and the
// dispatch goroutine's finish can never reorder; finish marks the coalescer
// done, which keeps a late timer from sending after the channel closed.
type httpChunkCoalescer struct {
	mu    sync.Mutex
	emit  func([]byte) // sends one chunk message into the events channel
	buf   []byte
	armed bool
	done  bool
}

// add is the httpclient.StreamCallbacks OnChunk seam: the chunk folds into
// the current window's buffer.
func (c *httpChunkCoalescer) add(chunk []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done {
		return
	}
	c.buf = append(c.buf, chunk...)
	if !c.armed {
		c.armed = true
		time.AfterFunc(httpStreamQuiet, c.flush)
	}
}

// flush delivers the window's bytes.
func (c *httpChunkCoalescer) flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.armed = false
	c.flushLocked()
}

// flushLocked emits the buffered bytes; the caller holds c.mu. The channel
// send may block on a slow UI — that is the stream's backpressure, unchanged.
func (c *httpChunkCoalescer) flushLocked() {
	if c.done || len(c.buf) == 0 {
		return
	}
	buf := c.buf
	c.buf = nil
	c.emit(buf)
}

// finish flushes the tail and stops the coalescer — called on the dispatch
// goroutine after the exchange returned, ahead of the final HTTPResponseMsg,
// so every received byte precedes the finalizing message.
func (c *httpChunkCoalescer) finish() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flushLocked()
	c.done = true
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

// isHTTPBuffer reports whether an editor holds requests the client runs: an
// .http/.rest file, or a file-less buffer treated as HTTP (#2033). Every gate
// that only reads the buffer's text asks this instead of isHTTPPath(Path()),
// which is what makes "Run Request" work on a pasted request block.
func isHTTPBuffer(ed *editor.Model) bool {
	return ed != nil && isHTTPPath(ed.LangPath())
}

// httpSource names the request file a dispatch is attributed to: the buffer's
// path, or — for a file-less HTTP buffer (#2033) — its synthetic language name
// ("buffer.http"). The name keys the response history and anchors relative
// external bodies (`< ./payload.json`), which for a buffer with no file means
// the working directory.
func httpSource(ed *editor.Model) string {
	if ed.HasFile() {
		return ed.Path()
	}
	return ed.LangPath()
}

// runHTTPRequestAtCursor resolves the request block under the focused
// editor's cursor and dispatches it off-loop; the response (or error)
// returns as an HTTPResponseMsg.
func (m *Model) runHTTPRequestAtCursor() tea.Cmd {
	ed := m.activeEditor()
	if ed == nil {
		m.host.Notify(host.Info, "http: focus a file tab first")
		return nil
	}
	if !isHTTPBuffer(ed) {
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
	return m.dispatchHTTPRequest(httpSource(ed), f, req)
}

// dispatchHTTPRequest sends one parsed request block of source, resolving the
// current variables and environment first — the path http.run takes once it
// has found the block under the cursor, and the one a history re-run (#2247)
// takes once it has found the block by request key.
func (m *Model) dispatchHTTPRequest(source string, f *httpfile.File, req *httpfile.Request) tea.Cmd {
	// User-defined variables (#1867): the file's own @name=value definitions
	// plus the selected http-client.env.json environment. A broken
	// environment file aborts before anything is sent.
	vars, envHint, err := m.httpVars(source, f)
	if err != nil {
		m.host.Notify(host.Error, "http: "+err.Error())
		return nil
	}
	return m.dispatchHTTP(source, req.Key(), requestLabel(req), req.WebSocket != nil,
		func(ctx context.Context, source, key string, cb httpclient.WSCallbacks) (*httpclient.Response, error) {
			// The .http file's directory anchors relative external-body paths
			// (#1305): `< ./payload.json` is relative to the request file, not
			// to wherever IKE was started.
			opts := httpclient.Options{BaseDir: filepath.Dir(source), Vars: vars}
			var resp *httpclient.Response
			var err error
			if req.WebSocket != nil {
				// A WEBSOCKET block (#2422) opens a session instead of an
				// exchange; frames stream through the same callbacks.
				resp, err = httpclient.DispatchWS(ctx, req, opts, cb)
			} else {
				resp, err = httpclient.DispatchStream(ctx, req, opts, cb.StreamCallbacks)
			}
			if err != nil && envHint != "" && strings.Contains(err.Error(), "unresolved placeholders") {
				// The likely cause is the unmade choice, so name it where the
				// failure is read instead of leaving the variable a mystery.
				err = fmt.Errorf("%v — %s", err, envHint)
			}
			return resp, err
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
	// A re-send is a re-run too (#2247): what its answer is worth comparing
	// with is the run before it, so it arms the same auto-diff.
	m.armHTTPRerunDiff(p.Source(), key)
	ws := snap.Method == httpfile.WebSocketMethod
	return m.dispatchHTTP(p.Source(), key, snap.Label(), ws,
		func(ctx context.Context, _, key string, cb httpclient.WSCallbacks) (*httpclient.Response, error) {
			if ws {
				// A stored websocket session (#2422) re-opens and replays its
				// initial messages instead of repeating one exchange.
				return httpclient.ResendWS(ctx, key, snap, httpclient.Options{}, cb)
			}
			return httpclient.Resend(ctx, key, snap, httpclient.Options{}, cb.StreamCallbacks)
		})
}

// rerunHTTPRequest runs http.rerun ("R" in the pane, the palette — #2247):
// the request the shown history entry belongs to goes out again, re-read from
// its .http file and resolved against the *current* variables and
// environment. That is the difference to http.resend (#1832), which repeats
// the stored bytes: a re-run asks "does this request still answer the same
// today?", so an edited block, a changed variable and a switched environment
// all count. The answer lands in the pane and in the history like any
// dispatch, and — with http.diff_after_rerun on — the previous-vs-new diff
// opens by itself once it is stored.
func (m *Model) rerunHTTPRequest() tea.Cmd {
	p := m.httpPanel()
	if p == nil {
		m.host.Notify(host.Info, "http: no response pane open — run an .http request, or show a stored one with http.showResponse")
		return nil
	}
	source, key := p.Source(), p.Request()
	if source == "" || key == "" {
		m.host.Notify(host.Info, "http: no stored response to re-run — dispatch a request first")
		return nil
	}
	text := m.httpSourceText(source)
	if text == "" {
		m.host.Notify(host.Info, "http: "+filepath.Base(source)+" cannot be read — re-send the stored request with http.resend instead")
		return nil
	}
	f := httpfile.Parse(text)
	var req *httpfile.Request
	for _, r := range f.Requests {
		if r.Key() == key {
			req = r
			break
		}
	}
	if req == nil {
		// The block was renamed, moved or deleted since the response was
		// stored; the verbatim re-send still works and is named here rather
		// than guessed at.
		m.host.Notify(host.Info, "http: "+key+" is no longer in "+filepath.Base(source)+" — re-send the stored request with http.resend (ctrl+r)")
		return nil
	}
	m.armHTTPRerunDiff(source, key)
	return m.dispatchHTTPRequest(source, f, req)
}

// armHTTPRerunDiff marks one dispatch as a re-run whose answer should be
// compared with the run before it as soon as it is stored (#2247). The mark is
// keyed like the in-flight set, so two re-runs of different requests never
// take each other's diff, and a disabled setting simply never arms.
func (m *Model) armHTTPRerunDiff(source, key string) {
	if !config.Get().HTTP.DiffAfterRerun {
		return
	}
	if m.httpRerunDiff == nil {
		m.httpRerunDiff = map[string]bool{}
	}
	m.httpRerunDiff[httpFlightKey(source, key)] = true
}

// takeHTTPRerunDiff reports whether this finished dispatch was an armed re-run
// and clears the mark — a diff is offered once, for the run that asked for it.
func (m *Model) takeHTTPRerunDiff(source, key string) bool {
	flightKey := httpFlightKey(source, key)
	armed := m.httpRerunDiff[flightKey]
	delete(m.httpRerunDiff, flightKey)
	return armed
}

// dispatchHTTP runs one exchange off-loop and pumps its events into the
// update loop — shared by http.run, http.resend and http.rerun. send performs
// the actual call; everything around it (duplicate guard, in-flight
// bookkeeping, the event channel) is the same for all three.
//
// The exchange runs on its own goroutine and reports through events: a
// non-streaming response yields exactly one HTTPResponseMsg, a recognized
// stream (#1776) first a start message, then one chunk message per received
// piece, then the finalizing HTTPResponseMsg. The buffered channel gives
// natural backpressure — a slow UI slows the read, never drops data.
func (m *Model) dispatchHTTP(source, key, label string, ws bool,
	send func(ctx context.Context, source, key string, cb httpclient.WSCallbacks) (*httpclient.Response, error)) tea.Cmd {
	flightKey := httpFlightKey(source, key)
	if _, running := m.httpFlight[flightKey]; running {
		// Duplicate-dispatch guard (#1272): never fire the same request twice
		// in parallel behind the user's back.
		m.host.Notify(host.Info, "http: "+key+" is already running — cancel it with http.cancel (or x in the response pane)")
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	// Flight lifecycle (#2348, timed through the shared helper since #2403):
	// the start event — plus everything enqueued before it, the dispatching
	// command event included — is flushed to disk before the exchange leaves,
	// so a dispatch that never comes back is still attributable ("start
	// without end") instead of invisible. Structural only: no URL, no request
	// key, no label.
	endOp := m.usage.OpTimer(telemetry.OpHTTPFlight)
	tick := m.startHTTPFlight(flightKey, &httpFlightEntry{
		label:   label,
		request: key,
		started: time.Now(),
		cancel:  cancel,
		ws:      ws,
		endOp:   endOp,
	})
	m.usage.FlushSoon()
	events := make(chan tea.Msg, 32)
	// Chunks coalesce per quiet window (#2176): one message — one Update pass,
	// one render, one viewer resync — per window instead of per received chunk.
	coal := &httpChunkCoalescer{emit: func(buf []byte) {
		events <- HTTPStreamChunkMsg{Source: source, Request: key,
			Chunk: buf, events: events}
	}}
	dispatch := func() tea.Msg {
		go func() {
			resp, err := send(ctx, source, key, httpclient.WSCallbacks{
				StreamCallbacks: httpclient.StreamCallbacks{
					OnHeaders: func(status string, _ int, proto string, headers http.Header) {
						events <- HTTPStreamStartMsg{Source: source, Request: key,
							Status: status, Proto: proto, Headers: headers, events: events}
					},
					OnChunk: coal.add,
				},
				// The open session's handle (#2422) travels the event channel
				// like every other stream fact, so the update loop stores it
				// on the flight entry without a cross-goroutine write.
				OnSession: func(s *httpclient.WSSession) {
					events <- HTTPWSSessionMsg{Source: source, Request: key,
						Session: s, events: events}
				},
			})
			cancel()      // release the context regardless of the outcome
			coal.finish() // the buffered tail lands ahead of the final response
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

// httpResponseVisible reports whether the response viewer is on screen right
// now (#2364) — a registered viewer under a visible layout leaf, which is
// exactly what httpPanel resolves. window.hideAllTools (#1271) keeps the
// instance registered but drops its leaf, and that is the case the completion
// notice exists for: a viewer nobody can see reports nothing by itself.
func (m Model) httpResponseVisible() bool { return m.httpPanel() != nil }

// notifyHTTPCompletion announces a finished dispatch nobody was watching
// (#2364). The response pane carries status and duration in its status row,
// but dispatches are asynchronous and the answer usually lands after the focus
// has moved on — a 404 or a five-second flight then reported itself only to
// whoever happened to open the pane afterwards. Two triggers speak up through
// the ordinary notification channel (and therefore into notifications.history
// as well): a non-2xx status, always, and a wall clock past
// http.notify_slow_ms, which 0 switches off. A visible pane stays quiet: the
// status row already said it, and repeating that as a toast is noise.
func (m *Model) notifyHTTPCompletion(e *httpFlightEntry, msg HTTPResponseMsg, visible bool) {
	if visible || msg.Resp == nil || msg.Resp.StatusCode <= 0 {
		return
	}
	// A GraphQL operation that failed answers with HTTP 200 and an `errors`
	// array (#2423), so the status code alone would report it as a success.
	gqlErrors := msg.Resp.GraphQLErrors()
	failed := msg.Resp.StatusCode < 200 || msg.Resp.StatusCode >= 300 || len(gqlErrors) > 0
	if msg.Resp.StatusCode == http.StatusSwitchingProtocols {
		// A finished websocket session (#2422) ends on its 101 handshake
		// status — a success, not a 1xx failure.
		failed = false
	}
	limit := config.Get().HTTP.NotifySlowMs
	slow := limit > 0 && msg.Resp.Duration >= time.Duration(limit)*time.Millisecond
	if !failed && !slow {
		return
	}
	// The flight's label is the "METHOD /path" the indicator showed; without
	// an entry (a response outliving the model that sent it, see
	// finishHTTPFlight) the request key is the best name left.
	what := msg.Request
	if e != nil && e.label != "" {
		what = e.label
	}
	sev, tail := host.Info, ""
	if failed {
		sev = host.Warn
	}
	if slow {
		tail = fmt.Sprintf(", slower than %s", formatElapsed(time.Duration(limit)*time.Millisecond))
	}
	if n := len(gqlErrors); n > 0 {
		// The status is 200: without naming the GraphQL errors the notice
		// would read as a success that merely took a while.
		tail += fmt.Sprintf(", %d GraphQL error(s)", n)
	}
	m.host.Notify(sev, fmt.Sprintf("http: %s → %s (%s%s)",
		what, msg.Resp.Status, formatElapsed(msg.Resp.Duration), tail))
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
	if !m.insertToolPane(key, target, m.auxZone(target)) {
		if !existed {
			// Only a freshly created instance is discarded — a hidden one
			// (hideAllTools) keeps its registration for the restore (#1271).
			m.activeWS().Panes.Close(key)
		}
		return
	}
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// beginHTTPStream routes a stream start into the viewer (#1776), opening it
// like fillHTTPPanel does: status and headers show immediately, the body
// grows via appendHTTPStream. A viewer that cannot open is no failure — the
// finalizing HTTPResponseMsg still lands and reports.
func (m *Model) beginHTTPStream(msg HTTPStreamStartMsg) {
	e, flying := m.httpFlight[httpFlightKey(msg.Source, msg.Request)]
	if flying {
		e.streamed = true // the flight-end event says streaming happened (#2348)
	}
	if m.httpPanel() == nil {
		m.openHTTPPanel()
	}
	p := m.httpPanel()
	if p == nil {
		return
	}
	p.StartStream(msg.Request, msg.Proto, msg.Status, msg.Headers)
	// The source is known at stream start (#2422): the websocket input line
	// resolves its flight — and therefore its session — through it.
	p.SetSource(msg.Source)
	if flying && e.ws {
		p.SetWSLive() // unlock the interactive input line (#2422)
	}
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
// replaces the content of the existing pane. The returned command carries the
// capture report (#1993) into the .http buffer.
func (m *Model) fillHTTPPanel(msg HTTPResponseMsg) tea.Cmd {
	// Read before the fill opens the viewer (#2364): what matters is whether
	// the user was looking at the response pane when the answer arrived, not
	// whether routing the answer put one on screen.
	visible := m.httpResponseVisible()
	flight := m.finishHTTPFlight(httpFlightKey(msg.Source, msg.Request))
	m.recordHTTPFlightEnd(flight, msg)
	canceled := flight != nil && flight.canceled
	// Taken up front (#2247), so a failed or canceled re-run disarms too
	// instead of leaving its diff waiting for the next unrelated dispatch.
	rerun := m.takeHTTPRerunDiff(msg.Source, msg.Request)
	if msg.Err != nil {
		if canceled || errors.Is(msg.Err, context.Canceled) {
			// The user aborted it (#1272): a confirmation, not a failure.
			m.host.Notify(host.Info, "http: "+msg.Request+" canceled")
			return nil
		}
		m.host.Notify(host.Error, "http: "+msg.Err.Error())
		return nil
	}
	if canceled {
		// A canceled *stream* still returns its partial response (#1776): what
		// arrived stays visible and reaches the history, the notice says why
		// the body ends where it does.
		m.host.Notify(host.Info, "http: "+msg.Request+" canceled — keeping the partial response")
	} else {
		// An abort already reported itself above; everything else that failed
		// or took too long while the pane was away reports here (#2364).
		m.notifyHTTPCompletion(flight, msg, visible)
	}
	// A failed capture (#1993) is reported on its own directive line, whether
	// or not the viewer opens — the next request depends on the value.
	report := m.reportHTTPCaptures(msg.Source, msg.Resp)
	if m.httpPanel() == nil {
		m.openHTTPPanel()
	}
	p := m.httpPanel()
	if p == nil {
		// Reopening failed (no target leaf, empty tree) — never swallow a
		// finished dispatch silently (#1271).
		m.host.Notify(host.Error, "http: response received but the viewer cannot open — "+msg.Request)
		return report
	}
	p.Set(msg.Request, msg.Resp)
	// The source enables the pane's request picker (#1829) and tells a
	// re-send (#1832) where to store its answer.
	p.SetSource(msg.Source)
	stored := 0
	if msg.Source != "" {
		// Persist under .ike/http/ and hand the stored predecessors to the
		// viewer for h/l browsing (#1251); best effort like local history.
		store := httphistory.New(httpHistoryDir())
		store.Append(msg.Source, msg.Request, httphistory.FromResponse(msg.Resp, time.Now()))
		entries := store.List(msg.Source, msg.Request)
		stored = len(entries)
		items := make([]httppane.HistoryItem, 0, len(entries))
		for _, e := range entries {
			items = append(items, httppane.HistoryItem{Resp: e.Response(msg.Request), At: e.Time})
		}
		p.SetHistory(items)
	}
	// The body composed plain; the syntax pass runs off-loop (#2353) and
	// paints the rows via httppane.HighlightedMsg — the update pass never
	// waits on a parse again.
	if cmd := p.HighlightCmd(); cmd != nil {
		report = tea.Batch(report, cmd)
	}
	// The response may just have captured a value (#1993), which defines a
	// name that read as unknown until now — re-lint after the entry is
	// stored, since that store is where the captured values are read from
	// (#2158).
	if cmd := m.lintHTTPVars(msg.Source); cmd != nil {
		report = tea.Batch(report, cmd)
	}
	m.layout()
	// A re-run's answer is only half the point (#2247): the comparison with
	// the run before it is the other half, and it opens here — after the
	// entry is stored, which is what makes "the previous run" the entry the
	// re-run replaced.
	// A first-ever run has nothing to compare with; that is not worth a
	// notice, so the auto-diff simply stays away.
	if rerun && stored > 1 {
		m.openHTTPPreviousRunDiff()
	}
	return report
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

// copyHTTPResponseOrSelection runs http.copyResponse (#2315): cmd+c in the
// response pane means what the pane's own copy key means — the live selection
// when there is one, the whole body otherwise. The pane owns that choice, so
// the host only forwards, and the copy travels the same CopyMsg path every
// other response copy uses.
func (m *Model) copyHTTPResponseOrSelection() tea.Cmd {
	p := m.httpPanel()
	if p == nil {
		m.host.Notify(host.Info, "http: no response pane open")
		return nil
	}
	cmd := p.CopyKeyCmd()
	if cmd == nil {
		m.host.Notify(host.Info, "http: nothing to copy")
	}
	return cmd
}

// searchHTTPResponse runs http.search (#2400): the response viewer's own
// search key ("/" , cmd+f, ctrl+f) as a bound command, so the chord resolves
// in the keymap table instead of being logged unbound while the pane quietly
// handled it. Focus follows the search — typing belongs in the prompt.
func (m *Model) searchHTTPResponse() tea.Cmd {
	p := m.httpPanel()
	if p == nil {
		m.host.Notify(host.Info, "http: no response pane open")
		return nil
	}
	p.BeginSearch()
	return nil
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

// toggleHTTPRawBody runs http.toggleRawBody (#2157): the palette pendant of
// the viewer's "t". It reports the new state, so the command is usable
// without looking at the footer, and carries the recompose's off-loop syntax
// pass (#2353).
func (m *Model) toggleHTTPRawBody() tea.Cmd {
	p := m.httpPanel()
	if p == nil {
		m.host.Notify(host.Info, "http: no response pane open")
		return nil
	}
	if p.ToggleRaw() {
		m.host.Notify(host.Info, "http: showing the raw response body")
	} else {
		m.host.Notify(host.Info, "http: showing the pretty-printed response body")
	}
	return p.HighlightCmd()
}

// loadMoreHTTPBody runs http.loadMoreBody (#2157): the palette pendant of the
// viewer's "m". A body that is fully on screen says so rather than doing
// nothing; a grown one carries the recompose's off-loop syntax pass (#2353).
func (m *Model) loadMoreHTTPBody() tea.Cmd {
	p := m.httpPanel()
	if p == nil {
		m.host.Notify(host.Info, "http: no response pane open")
		return nil
	}
	if !p.LoadMore() {
		if p.BodyFileGone() {
			// The rest of the body lived in a file that no longer exists
			// (#2385) — pruned history, a cleaned temp dir. Say that instead
			// of pretending everything is on screen.
			m.host.Notify(host.Info, "http: the rest of this body is gone — its body file no longer exists")
			return nil
		}
		m.host.Notify(host.Info, "http: the whole response body is already shown")
		return nil
	}
	m.host.Notify(host.Info, fmt.Sprintf("http: showing %d of %d body bytes", p.ShownBodyBytes(), p.TotalBodyBytes()))
	return p.HighlightCmd()
}

// openHTTPBodyFile runs http.openBodyFile (#2157): the palette pendant of the
// viewer's "o". Only a spooled body has a file behind it — a small one lives
// in memory and is saved through http.saveResponse instead.
func (m *Model) openHTTPBodyFile() tea.Cmd {
	p := m.httpPanel()
	if p == nil {
		m.host.Notify(host.Info, "http: no response pane open")
		return nil
	}
	cmd := p.OpenBodyFileCmd()
	if cmd == nil {
		if p.BodyFileGone() {
			// There was a file once (#2385): explain its absence rather than
			// let a raw "no such file" surface downstream.
			m.host.Notify(host.Info, "http: this body's file no longer exists — only the stored head remains")
			return nil
		}
		m.host.Notify(host.Info, "http: this response body is held in memory — save it with http.saveResponse")
	}
	return cmd
}

// httpPaneKeys documents the response viewer's pane-local keys for the help
// overlay (#1267): they belong to no command in the registry, so the
// cheatsheet would otherwise never mention that responses can be browsed,
// searched or copied at all.
var httpPaneKeys = []struct{ Key, Title string }{
	{"h / l  ← / →", "Browse older / newer stored response"},
	{"r", "Switch to another request's stored responses"},
	{"s", "Keep scroll position while browsing history (per request)"},
	{"D", "Compare this stored response with another one (diff)"},
	{"P", "Diff this stored response against the previous run"},
	{"j / k", "Scroll"},
	{"shift+← / shift+→", "Pan sideways in wide lines"},
	{"0 / $", "Left edge / right edge"},
	{"g / G", "Top / bottom"},
	{"/ · ctrl+f / cmd+f", "Search in the response"},
	{"n / N", "Next / previous match"},
	{"t", "Toggle raw / pretty-printed body"},
	{"q", "Open the jq playground on this body"},
	{"m", "Load the next window of a large (spooled) body"},
	{"o", "Open the whole spooled body as a file"},
	{"za / zc / zo / zM / zR", "Toggle / close / open folds"},
	{"zy", "Copy the target fold whole (or click its ⧉)"},
	{"y", "Copy selection (or the whole body)"},
	{"Y", "Copy status line and headers"},
	{"ctrl+r", "Re-send this response's request unchanged (or click ⟳ re-send)"},
	{"R", "Re-run this request from its .http file (current environment)"},
	{"C", "Copy this response's request as a curl command"},
	{"S", "Save the raw response body to a file"},
	{"x", "Cancel the running request (or close the websocket session)"},
	{"enter · i (websocket)", "Open the input line of a live websocket session"},
	{"↑ / ↓ (websocket input)", "Step through sent websocket messages"},
	{"esc", "Clear search and selection"},
}

// showHTTPHistory runs http.responseHistory (#1267): it focuses the response
// viewer and says how many stored responses the current request has, making
// the h/l browsing discoverable from the palette.
func (m *Model) showHTTPHistory() {
	p := m.httpPanel()
	if p == nil {
		// The stored history of a request survives restarts (#1251), so an
		// empty pane is no dead end — name the way in (#1829).
		m.host.Notify(host.Info, "http: no response yet — run an .http request, or show a stored one with http.showResponse")
		return
	}
	m.focusHTTPPanel() // the viewer may live in a tab (#1778)
	m.layout()
	idx, n := p.HistoryIndex()
	if n <= 1 {
		m.host.Notify(host.Info, "http: 1 stored response — ←/→ browse older/newer ones as they arrive · r switch request")
		return
	}
	m.host.Notify(host.Info, fmt.Sprintf("http: showing %d/%d stored responses — ←/→ browse", idx+1, n))
}

// showStoredHTTPResponse runs http.showResponse (#1492): it loads the stored
// responses of the request block under the cursor from .ike/http/ and shows
// them in the viewer without dispatching anything — the way to look at what
// request A answered while the pane still shows request B. The h/l browsing
// then works exactly as after a dispatch (#1251).
func (m *Model) showStoredHTTPResponse() tea.Cmd {
	ed := m.activeEditor()
	if ed == nil {
		m.host.Notify(host.Info, "http: focus a file tab first")
		return nil
	}
	if !isHTTPBuffer(ed) {
		m.host.Notify(host.Info, "http: not an .http file")
		return nil
	}
	f := httpfile.Parse(ed.Text())
	line, _ := ed.CursorPos()
	req, ok := f.RequestAt(line + 1)
	if !ok {
		m.host.Notify(host.Info, "http: no request under the cursor")
		return nil
	}
	return m.loadStoredHTTPResponse(httpSource(ed), req.Key())
}

// loadStoredHTTPResponse shows the stored responses of one request in the
// viewer — the shared loading path of http.showResponse (#1492) and the
// pane-local request picker (#1829), which differ only in how they name the
// request.
func (m *Model) loadStoredHTTPResponse(source, key string) tea.Cmd {
	entries := httphistory.New(httpHistoryDir()).List(source, key)
	if len(entries) == 0 {
		m.host.Notify(host.Info, "http: no stored responses for "+key+" — dispatch it once with http.run")
		return nil
	}
	if m.httpPanel() == nil {
		m.openHTTPPanel()
	}
	p := m.httpPanel()
	if p == nil {
		// Same failure mode as fillHTTPPanel (#1271): never pretend silently.
		m.host.Notify(host.Error, "http: stored responses exist but the viewer cannot open — "+key)
		return nil
	}
	items := make([]httppane.HistoryItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, httppane.HistoryItem{Resp: e.Response(key), At: e.Time})
	}
	p.Set(key, items[0].Resp)
	p.SetHistory(items)
	p.SetSource(source) // request picker (#1829) and re-send target (#1832)
	m.focusHTTPPanel()  // the viewer may live in a tab (#1778)
	m.layout()
	m.host.Notify(host.Info, fmt.Sprintf("http: %d stored response(s) for %s — ←/→ browse · r switch request", len(items), key))
	return p.HighlightCmd() // the off-loop syntax pass of the shown entry (#2353)
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
	// streamed marks a dispatch whose response was recognized as a stream
	// (#1776) — the flight-end telemetry event carries it (#2348).
	streamed bool
	// ws marks a websocket session (#2422): the flight-end event carries
	// kind=ws plus the frame count, and the pane unlocks its input line.
	ws bool
	// wsSession is the open session's handle (#2422), stored when the
	// HTTPWSSessionMsg arrives; the pane's input line sends through it.
	wsSession *httpclient.WSSession
	// endOp closes the flight's op lifecycle event (#2403): the closer
	// telemetry.OpTimer handed out when the dispatch left, which stamps the
	// elapsed ms into the end phase. Nil for a foreign entry.
	endOp func(phase string, detail map[string]string)
}

// httpTickMsg repaints the in-flight indicator while requests run. gen names
// the model that armed it (#2194); another model's tick is dropped.
type httpTickMsg struct{ gen int64 }

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
func elapsed(since time.Time) string { return formatElapsed(time.Since(since)) }

// formatElapsed is the indicator's duration spelling — milliseconds below a
// second, one decimal of seconds above it. The completion notice (#2364)
// speaks the same way, so a flight reads identically while it runs and once
// it is reported.
func formatElapsed(d time.Duration) string {
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
	m.httpFlight[key] = e
	m.markHTTPPending()
	m.refreshHTTPFlightMarks()
	// Armed-flag guard (#2163), not "was the map empty": the map empties the
	// moment the last flight finishes, but its final tick is still in flight
	// for up to httpFlightTick — arming on emptiness in that window built a
	// second chain, and both re-armed forever after.
	if m.httpTickArmed {
		return nil // a tick loop is already running
	}
	m.httpTickArmed = true
	gen := m.modelGen
	return tea.Tick(httpFlightTick, func(time.Time) tea.Msg { return httpTickMsg{gen: gen} })
}

// finishHTTPFlight drops a finished dispatch and returns its entry — nil when
// no flight is registered under key (a response arriving after a project
// switch rebuilt the model, #2235).
func (m *Model) finishHTTPFlight(key string) *httpFlightEntry {
	e, ok := m.httpFlight[key]
	if !ok {
		return nil
	}
	delete(m.httpFlight, key)
	m.markHTTPPending()
	m.refreshHTTPFlightMarks()
	return e
}

// recordHTTPFlightEnd emits the flight's closing telemetry event (#2348):
// how the dispatch ended ("ok" / "error" / "canceled"), how long it flew, the
// status class and whether it streamed — all structural, nothing from the
// request or response itself. A flight without an entry (foreign model, see
// finishHTTPFlight) records nothing: there is no start to pair it with.
func (m *Model) recordHTTPFlightEnd(e *httpFlightEntry, msg HTTPResponseMsg) {
	if e == nil {
		return
	}
	phase := "ok"
	switch {
	case e.canceled || errors.Is(msg.Err, context.Canceled):
		phase = "canceled"
	case msg.Err != nil:
		phase = "error"
	}
	d := map[string]string{"stream": strconv.FormatBool(e.streamed)}
	if e.ws {
		// A websocket session (#2422): the kind plus the frame count — how
		// much crossed the wire, nothing of what it said.
		d["kind"] = "ws"
		if msg.Resp != nil {
			d["frames"] = strconv.Itoa(msg.Resp.Frames)
		}
	}
	if msg.Resp != nil && msg.Resp.StatusCode > 0 {
		d["class"] = fmt.Sprintf("%dxx", msg.Resp.StatusCode/100)
	}
	// The phase breakdown (#2404) travels with the ok event: "1.4 s mean" only
	// becomes actionable once the export says whether the time went into DNS,
	// the connect, the handshake or the wait for the first byte. Structural
	// numbers, like everything else here — nothing from the request or the
	// response itself.
	if msg.Resp != nil && !msg.Resp.Timing.IsZero() {
		t := msg.Resp.Timing
		ms := func(d time.Duration) string { return strconv.FormatInt(d.Milliseconds(), 10) }
		d["dns_ms"] = ms(t.DNS)
		d["connect_ms"] = ms(t.Connect)
		d["tls_ms"] = ms(t.TLS)
		d["ttfb_ms"] = ms(t.TTFB)
		d["transfer_ms"] = ms(t.Transfer)
		d["reused"] = strconv.FormatBool(t.Reused)
	}
	// The timer carries the ms field (#2403); an entry without one — a foreign
	// model's, or a test-built stub — falls back to its own start stamp so the
	// event keeps its shape.
	if e.endOp != nil {
		e.endOp(phase, d)
		return
	}
	d["ms"] = strconv.FormatInt(time.Since(e.started).Milliseconds(), 10)
	m.usage.Op(telemetry.OpHTTPFlight, phase, d)
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
			if idle || !isHTTPBuffer(ed) {
				ed.SetHTTPFlight(nil)
				continue
			}
			ed.SetHTTPFlight(m.httpFlightMarks(httpSource(ed), ed.Text()))
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
	// The pane spells out how to abort a slow flight (#2404) and needs the
	// user's *own* chord for it, not the shipped default: it is resolved from
	// the live table on every state change rather than cached, so a rebind
	// shows up on the next dispatch.
	p.SetCancelChord(m.httpCancelChord())
	for _, e := range m.httpFlight {
		p.SetPending(e.request, e.started)
		return
	}
	p.ClearPending()
}

// httpCancelChord names the chord bound to http.cancel in the live table, for
// the response pane's in-flight hint (#2404). A delivered chord wins over a
// fragile one: the hint exists to be pressed, and naming a chord this
// terminal swallows would be advice that does not work. "" when the command
// carries no binding at all — the hint then names the pane key alone.
func (m Model) httpCancelChord() string {
	if m.bindings == nil || m.bindings.Table() == nil {
		return ""
	}
	best := ""
	for _, b := range m.bindings.Table().Bindings() {
		if b.Command != "http.cancel" {
			continue
		}
		if !b.Fragile {
			return b.Chord.String()
		}
		if best == "" {
			best = b.Chord.String()
		}
	}
	return best
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
