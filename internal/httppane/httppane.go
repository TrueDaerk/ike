// Package httppane is the read-only HTTP response viewer (#1250, epic
// #1247): a singleton bottom-split pane showing the result of the last
// dispatched .http request — status line, headers, body. The pane is reused:
// each dispatch replaces its content via Set. Bodies with a recognized
// content type are pretty-printed (JSON) and syntax-highlighted through the
// fenced-highlight path; binary bodies collapse to a notice instead of
// garbage output.
package httppane

import (
	"bytes"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/search"
	"ike/internal/highlight"
	"ike/internal/httpclient"
	"ike/internal/idcolor"
	"ike/internal/theme"
	"ike/internal/ui"
)

// rowKind classifies one display row for styling.
type rowKind int

const (
	kindStatus rowKind = iota
	kindHeader
	kindWarn
	kindBlank
	kindBody
)

// row is one pre-composed display line.
type row struct {
	kind rowKind
	text string
	body int // body line index for highlight lookup (kindBody)
}

// Model is the response viewer state. Value type with pointer-receiver
// mutators, embedded in a pane.Instance like the Usages panel (#1155).
type Model struct {
	width   int
	height  int
	focused bool
	pal     *theme.Palette
	hl      highlight.Theme

	// request labels the response ("name" or index of the dispatched
	// request); loaded marks that a response ever arrived.
	request string
	status  string
	loaded  bool

	// source is the .http file the shown request belongs to (#1829): the
	// pane-local request picker needs it to enumerate the file's other
	// requests and their stored responses. "" until a response arrives.
	source string

	// hist is the browsable response history of the current request, newest
	// first (#1251); histIdx selects the shown entry, 0 = latest.
	hist    []HistoryItem
	histIdx int

	// keepScroll marks requests whose scroll position survives history
	// stepping (#1493): comparing the same section across responses means
	// stepping without losing the spot. Keyed by request label so enabling
	// it for one request never changes another's behavior; default off.
	keepScroll map[string]bool

	rows []row
	// bodyIx indexes the highlight spans of the body lines only.
	bodyIx highlight.Index
	// Folding (#1330): folds are the body's foldable ranges in row
	// coordinates, folded the collapsed ones (header row -> end row), and
	// visible the display projection the viewport scrolls over. See fold.go.
	folds   []highlight.Fold
	folded  map[int]int
	visible []int
	// top is a *display* index into visible, not a row index: a collapsed
	// fold's body rows are not scrolled through.
	top int
	// left is the horizontal viewport offset in runes (#1290): minified JSON
	// and long header values are wider than the pane, so the view pans
	// sideways instead of clipping the rest away for good.
	left int

	// In-pane search (#1265) over the whole composed view — status line,
	// headers and formatted body alike. searching marks the open "/" prompt,
	// query is the live pattern, matches every hit in row order and cur the
	// selected one (n/N step through them). qcur is the rune cursor within
	// query while the prompt is open (#1845), edited via ui.EditKey.
	searching bool
	query     string
	qcur      int
	matches   []search.Span
	cur       int

	// pendingZ marks a typed "z" waiting for its fold command (#1330).
	pendingZ bool

	// sel is the mouse text selection (#1266), see selection.go.
	sel selection

	// sbGrab is the in-thumb grab offset of a scrollbar drag (#1367), see
	// scrollbar.go.
	sbGrab int

	// pending marks a dispatch in flight (#1272): the shown response is the
	// previous one, so the header says so rather than presenting stale
	// content as the current answer. pendingSince drives the elapsed time.
	pending      string
	pendingSince time.Time

	// raw shows the body as it arrived instead of pretty-printed (#2157), and
	// more holds the spool bytes "load more" has pulled in beyond the
	// response's in-memory head. raw belongs to the view and survives history
	// browsing; more belongs to the shown entry and is dropped on every
	// compose. See body.go.
	raw  bool
	more []byte

	// Streaming (#1776): while a recognized stream is live the viewer shows
	// status, headers and the body lines received so far — plaintext, no
	// highlight/folds until the finalizing Set. streamTail buffers the
	// incomplete last line, streamBody counts the appended body lines.
	streaming  bool
	streamTail string
	streamBody int
}

// New returns an empty viewer; responses arrive via Set.
func New(pal *theme.Palette) Model {
	m := Model{pal: pal}
	m.rebuildTheme()
	return m
}

// SetSize records the interior content size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetFocused marks the pane focused.
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the active theme.
func (m *Model) SetPalette(p *theme.Palette) {
	m.pal = p
	m.rebuildTheme()
}

// Request reports which request the shown response belongs to (tests).
func (m *Model) Request() string { return m.request }

// SetSource records the .http file the shown request comes from (#1829), so
// the pane-local picker ("r") can list that file's other stored responses.
func (m *Model) SetSource(path string) { m.source = path }

// Source reports the .http file of the shown request, "" when unknown.
func (m *Model) Source() string { return m.source }

// Rows reports the composed row count (tests).
func (m *Model) Rows() int { return len(m.rows) }

// RowText reports the text of composed row i, "" out of range (tests).
func (m *Model) RowText(i int) string {
	if i < 0 || i >= len(m.rows) {
		return ""
	}
	return m.rows[i].text
}

// Highlighted reports whether the shown body carries syntax-highlight spans
// (tests, #1270): false for an unhighlightable body *and* for a recognized
// content type whose grammar is missing from the build.
func (m *Model) Highlighted() bool { return !m.bodyIx.Empty() }

// Warnings reports the warning rows currently composed (tests).
func (m *Model) Warnings() []string {
	var out []string
	for _, r := range m.rows {
		if r.kind == kindWarn {
			out = append(out, r.text)
		}
	}
	return out
}

func (m *Model) rebuildTheme() {
	var captures map[string]string
	if m.pal != nil {
		captures = m.pal.Captures
	}
	m.hl = highlight.NewTheme(captures, nil)
}

// HistoryItem is one browsable response of the current request (#1251).
type HistoryItem struct {
	Resp *httpclient.Response
	At   time.Time // zero when unknown (the just-dispatched response)
}

// ResendMsg asks the host to send the shown entry's stored request again
// (#1832), ctrl+r or a click on the header affordance. The pane holds the
// snapshot but cannot dispatch, exactly as it cannot cancel (CancelMsg) or
// reach the clipboard (CopyMsg).
type ResendMsg struct{}

// RerunMsg asks the host to dispatch the shown history entry's request again
// from its .http file (#2247), "R" in the focused viewer. Unlike ResendMsg —
// which repeats the stored bytes — a re-run re-reads the request block and
// resolves the current variables and environment, which is the form the
// question "does it still answer the same?" usually takes. The pane knows
// neither the request file nor the environment, so the host does the work.
type RerunMsg struct{}

// CopyCurlMsg asks the host to put the shown entry's request on the clipboard
// as a runnable curl command (#2059), "C" in the focused viewer. The pane
// holds the snapshot (CurrentRequest) but neither the clipboard nor the
// notification channel, so the host does the copying — the CopyMsg
// arrangement, one step further.
type CopyCurlMsg struct{}

// SaveBodyMsg asks the host to write the shown response's raw body to a file
// (#2059), "S" in the focused viewer. The host owns the path prompt and the
// filesystem; the pane only names the moment.
type SaveBodyMsg struct{}

// DiffHistoryMsg asks the host to compare the shown stored response with
// another one of the same request (#1992), "D" in the focused viewer. The
// pane knows neither the history store nor the diff viewer, so the host opens
// the entry picker on its behalf — the PickRequestMsg arrangement.
type DiffHistoryMsg struct{}

// DiffPreviousRunMsg asks the host to compare the shown stored response with
// the one immediately before it in time (#2060), "P" in the focused viewer —
// a direct shortcut around DiffHistoryMsg's picker for the common case of
// "what changed since last time". The pane knows neither the history store
// nor the diff viewer, so the host resolves the previous run and opens the
// diff on its behalf.
type DiffPreviousRunMsg struct{}

// resendLabel is the clickable re-send affordance in the pane header (#1832).
const resendLabel = "⟳ re-send"

// CurrentRequest returns the as-sent request of the entry on show (#1832), or
// nil when there is none: a live stream, an empty pane, or a history entry
// stored before the snapshot existed.
func (m *Model) CurrentRequest() *httpclient.RequestSnapshot {
	if m.streaming || m.histIdx < 0 || m.histIdx >= len(m.hist) {
		return nil
	}
	resp := m.hist[m.histIdx].Resp
	if resp == nil {
		return nil
	}
	return resp.Request
}

// CurrentResponse returns the response on show (#2059), or nil when there is
// none — a live stream or an empty pane. Callers that need the *raw* body
// (saving it to a file) go through it rather than through BodyText, which is
// the pretty-printed, folded view of the same bytes.
func (m *Model) CurrentResponse() *httpclient.Response {
	if m.streaming || m.histIdx < 0 || m.histIdx >= len(m.hist) {
		return nil
	}
	return m.hist[m.histIdx].Resp
}

// CanResend reports whether the shown entry can be re-sent — the gate for the
// header affordance (#1832).
func (m *Model) CanResend() bool { return m.CurrentRequest() != nil }

// Set replaces the viewer content with one dispatch result — the reuse
// entry point (#1250). History browsing resets to just this response; use
// SetHistory to hand over the stored predecessors.
func (m *Model) Set(request string, resp *httpclient.Response) {
	m.hist = []HistoryItem{{Resp: resp}}
	m.histIdx = 0
	m.request = request
	m.compose(resp)
}

// SetHistory replaces the browsable history of the current request, newest
// first (#1251); the shown entry jumps to the latest. Call after Set with
// the stored responses (including the fresh one at index 0).
func (m *Model) SetHistory(items []HistoryItem) {
	if len(items) == 0 {
		return
	}
	m.hist = items
	m.histIdx = 0
	m.compose(items[0].Resp)
}

// StartStream switches the viewer to live-stream display (#1776): status
// line and headers compose immediately, body lines follow via AppendStream
// while the connection is open. History navigation clears — there is nothing
// browsable until the finalizing Set/SetHistory restore it.
func (m *Model) StartStream(request, proto, status string, headers http.Header) {
	m.loaded = true
	m.request = request
	m.hist = nil
	m.histIdx = 0
	m.top = 0
	m.left = 0
	m.rows = nil
	m.bodyIx = highlight.Index{}
	m.folds, m.folded, m.visible = nil, nil, nil
	m.streaming = true
	m.streamTail = ""
	m.streamBody = 0

	m.status = fmt.Sprintf("%s %s", proto, status)
	m.rows = append(m.rows, row{kind: kindStatus, text: m.status})
	names := make([]string, 0, len(headers))
	for n := range headers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		for _, v := range headers[n] {
			m.rows = append(m.rows, row{kind: kindHeader, text: n + ": " + v})
		}
	}
	m.rows = append(m.rows, row{kind: kindBlank})
	m.syncVisible()
	m.ClearSelection()
	m.research()
}

// AppendStream feeds one received chunk into the live view (#1776): complete
// lines become body rows, the trailing incomplete line stays buffered until
// its newline arrives. The viewport auto-follows the stream end as long as
// it *is* at the end — a user who scrolled up keeps their position, log-viewer
// style; G re-attaches. No-op unless a stream is live (a history browse or a
// finalizing Set ends the live view).
func (m *Model) AppendStream(chunk []byte) {
	if !m.streaming || len(chunk) == 0 {
		return
	}
	follow := m.top >= m.maxTop()
	first := len(m.rows)
	m.streamTail += string(chunk)
	for {
		nl := strings.IndexByte(m.streamTail, '\n')
		if nl < 0 {
			break
		}
		line := strings.TrimSuffix(m.streamTail[:nl], "\r")
		m.streamTail = m.streamTail[nl+1:]
		m.rows = append(m.rows, row{kind: kindBody, text: line, body: m.streamBody})
		m.streamBody++
	}
	if len(m.rows) == first {
		return // no completed line yet: nothing changed on screen
	}
	// A stream only appends, so the projection and the search extend at the
	// tail (#2176) instead of recomputing over every row — per-chunk O(all
	// rows) work made a long stream quadratic in total.
	m.appendVisible(first)
	if follow {
		m.top = m.maxTop()
	}
	m.researchFrom(first)
}

// appendVisible extends the display projection for rows appended at the tail
// (#2176). A live stream has no collapsed folds, so the new rows map
// one-to-one; the folded case (defensive — history browsing ends the stream)
// falls back to the full rebuild.
func (m *Model) appendVisible(from int) {
	if len(m.folded) > 0 {
		m.syncVisible()
		return
	}
	for i := from; i < len(m.rows); i++ {
		m.visible = append(m.visible, i)
	}
	m.Scroll(0) // the projection grew: clamp the viewport
}

// researchFrom appends the open query's matches over rows[from:] (#2176) —
// the streaming tail extension of research: earlier rows cannot change while
// a stream appends, so only the new rows are searched.
func (m *Model) researchFrom(from int) {
	if m.query == "" || from >= len(m.rows) {
		return
	}
	lines := make([]string, 0, len(m.rows)-from)
	for _, r := range m.rows[from:] {
		lines = append(lines, r.text)
	}
	for _, sp := range search.Compile(m.query, false, search.CaseSmart).AllMatches(buffer.New(lines)) {
		sp.Line += from
		m.matches = append(m.matches, sp)
	}
}

// Streaming reports whether a live stream is on show (tests).
func (m *Model) Streaming() bool { return m.streaming }

// SetPending marks request as in flight since at (#1272). The composed rows
// stay untouched — the previous response remains readable, it is just no
// longer presented as the current one.
func (m *Model) SetPending(request string, at time.Time) {
	m.pending, m.pendingSince = request, at
}

// ClearPending drops the in-flight marker (response, error or cancel).
func (m *Model) ClearPending() { m.pending, m.pendingSince = "", time.Time{} }

// Pending reports the in-flight request, "" when none (tests).
func (m *Model) Pending() string { return m.pending }

// HistoryIndex reports the shown entry and the history length (tests).
func (m *Model) HistoryIndex() (int, int) { return m.histIdx, len(m.hist) }

// compose rebuilds the display rows for one response — the entry point every
// content change goes through. It drops the spool bytes "load more" pulled in
// (#2157): they belong to the entry that was on show, not to this one.
func (m *Model) compose(resp *httpclient.Response) {
	m.more = nil
	m.recompose(resp)
}

// recompose rebuilds the display rows for the response already on show, with
// whatever the raw toggle and "load more" currently say (#2157). Scroll and
// selection reset like they do on a new response: the row count changed under
// them, so keeping either would point at different content.
func (m *Model) recompose(resp *httpclient.Response) {
	m.loaded = true
	m.streaming = false
	m.streamTail = ""
	m.streamBody = 0
	m.top = 0
	m.left = 0
	m.rows = nil
	m.bodyIx = highlight.Index{}
	m.folds, m.folded, m.visible = nil, nil, nil
	if resp == nil {
		m.research()
		return
	}

	m.status = fmt.Sprintf("%s %s", resp.Proto, resp.Status)
	m.rows = append(m.rows, row{kind: kindStatus, text: fmt.Sprintf("%s %s   (%s)", resp.Proto, resp.Status, roundDuration(resp.Duration))})

	names := make([]string, 0, len(resp.Headers))
	for n := range resp.Headers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		for _, v := range resp.Headers[n] {
			m.rows = append(m.rows, row{kind: kindHeader, text: n + ": " + v})
		}
	}
	for _, w := range resp.Warnings {
		m.rows = append(m.rows, row{kind: kindWarn, text: "! " + w})
	}
	m.rows = append(m.rows, row{kind: kindBlank})

	view := formatBody(resp, m.bodyBytes(resp), m.raw)
	for _, notice := range view.notices {
		m.rows = append(m.rows, row{kind: kindWarn, text: notice})
	}
	if len(view.lines) > 0 {
		if view.tag != "" && !highlight.FencedSupported(view.tag) {
			// A recognized Content-Type whose grammar is not in this build
			// (CGo-free, or the plugin not linked) renders plain — say so
			// instead of failing silently (#1270).
			m.rows = append(m.rows, row{kind: kindWarn, text: fmt.Sprintf("(no %s highlighter in this build — showing plain text)", view.tag)})
		}
		m.bodyIx = highlight.NewIndex(highlight.HighlightFenced(view.tag, view.lines))
		bodyStart := len(m.rows)
		for i, line := range view.lines {
			m.rows = append(m.rows, row{kind: kindBody, text: line, body: i})
		}
		// The body folds by its own language's rules (#1330). A body past
		// PrettyLimit carries no tag, so it is neither highlighted nor
		// scanned for folds (#2157).
		m.setFolds(highlight.FencedFolds(view.tag, view.lines), bodyStart)
	}
	m.syncVisible()
	m.ClearSelection() // the rows changed: an old selection means nothing
	m.research()       // the search survives history browsing and new responses
}

// contentTag maps a Content-Type header onto a fence tag for highlighting;
// "" means unknown → raw, unhighlighted.
func contentTag(ct string) string {
	ct = strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	switch {
	case strings.HasSuffix(ct, "/json") || strings.HasSuffix(ct, "+json"):
		return "json"
	case strings.HasSuffix(ct, "/xml") || strings.HasSuffix(ct, "+xml"):
		return "xml"
	case ct == "text/html" || ct == "application/xhtml":
		return "html"
	case ct == "text/css":
		return "css"
	case ct == "application/javascript" || ct == "text/javascript" ||
		ct == "application/x-javascript" || ct == "application/ecmascript":
		return "js"
	}
	return ""
}

// isBinary reports whether the body is not renderable text: invalid UTF-8 or
// NUL bytes.
func isBinary(b []byte) bool {
	return bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b)
}

// roundDuration trims a duration for the status row.
func roundDuration(d time.Duration) time.Duration {
	switch {
	case d > time.Second:
		return d.Round(10 * time.Millisecond)
	case d > time.Millisecond:
		return d.Round(100 * time.Microsecond)
	}
	return d
}

// Update handles one message while the pane exists; only key presses reach
// it, focus-filtered by the pane layer.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		return m.handleKey(k)
	}
	return nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.searching {
		if cmd := m.searchCopyKey(msg); cmd != nil {
			return cmd
		}
		m.searchKey(msg)
		return nil
	}
	if m.pendingZ {
		m.pendingZ = false
		// The copy chord outranks a half-typed fold sequence (#2062): "z"
		// followed by cmd+c is no fold command, so swallowing it would drop
		// a live selection for a keystroke the user never meant as a fold.
		// Bare "y" stays with foldKey — "zy" copies the target fold (#1787),
		// a copy either way.
		if copyChord(msg) {
			return m.copyKeyCmd()
		}
		return m.foldKey(msg.String())
	}
	switch msg.String() {
	case "/", "ctrl+f", "cmd+f", "super+f":
		// Open the in-pane search prompt (#1265), editor conventions. ctrl+f /
		// cmd+f alias the muscle-memory search chord used everywhere else in
		// the app — editor find, terminal scrollback search (#1504) — since
		// plain "/" alone doesn't match user expectations (#1830).
		m.searching = true
		m.query = m.selectionSearchPrefill()
		m.qcur = len([]rune(m.query))
		m.cur = 0
		m.research()
	case "n":
		m.step(1)
	case "N":
		m.step(-1)
	case "esc":
		m.clearSearch()
		m.ClearSelection()
	case "y", "ctrl+c", "cmd+c", "super+c":
		return m.copyKeyCmd()
	case "Y":
		return copyCmd(m.HeadersText(), "response headers")
	case "x":
		// Cancel the in-flight dispatch (#1272). ctrl+c is taken by copy
		// (#1266), so the abort key is its own.
		return func() tea.Msg { return CancelMsg{} }
	case "C":
		// Copy the shown entry's request as a curl command (#2059).
		// Uppercase: "c" would be one keystroke away from the copy keys and
		// the export is the rarer, deliberate action.
		return func() tea.Msg { return CopyCurlMsg{} }
	case "S":
		// Write the raw response body to a file (#2059). Uppercase because
		// "s" is the keep-scroll toggle (#1493).
		return func() tea.Msg { return SaveBodyMsg{} }
	case "ctrl+r":
		// Re-send the shown entry's stored request (#1832). The message goes
		// out even without a snapshot — the host says why nothing happened
		// instead of the key dying silently on a legacy entry.
		return func() tea.Msg { return ResendMsg{} }
	case "R":
		// Re-run the shown entry's request from the .http file, with the
		// current variables and environment (#2247). Uppercase next to the
		// verbatim ctrl+r re-send: the two are neighbours in meaning, and "r"
		// is the request picker.
		return func() tea.Msg { return RerunMsg{} }
	case "j", "down":
		m.Scroll(1)
	case "k", "up":
		m.Scroll(-1)
	case "d", "pgdown":
		m.Scroll(m.bodyHeight())
	case "u", "pgup":
		m.Scroll(-m.bodyHeight())
	case "g", "home":
		m.top = 0
		m.left = 0
	case "G", "end":
		m.top = m.maxTop()
	case "h", "left":
		// Older stored response of the same request (#1251). The arrows join
		// h/l (#1471): they are the keys a user reaches for first, and the
		// footer advertises history as the arrow-shaped action.
		m.showHistory(m.histIdx + 1)
	case "l", "right":
		// Newer stored response.
		m.showHistory(m.histIdx - 1)
	case "r":
		// Switch the pane to another request's stored history (#1829). The
		// pane knows neither the history store nor the .http file's requests,
		// so the host opens the picker on its behalf.
		return func() tea.Msg { return PickRequestMsg{} }
	case "D":
		// Compare this stored response with another one of the same request
		// (#1992). Uppercase: "d" is the page-down key.
		return func() tea.Msg { return DiffHistoryMsg{} }
	case "P":
		// Compare this stored response with the previous run of the same
		// request, no picker (#2060). Uppercase: "p" is unclaimed but the
		// pane's other diff/copy/save actions are all uppercase single keys.
		return func() tea.Msg { return DiffPreviousRunMsg{} }
	case "s":
		// Keep the scroll position while stepping through history (#1493),
		// per request; the footer anchor marks the active state.
		m.ToggleKeepScroll()
	case "t":
		// Raw ⇄ pretty-printed body (#2157). Pretty is the default — the
		// toggle is for the moments where the bytes themselves are the
		// question (a whitespace-sensitive payload, a malformed JSON answer).
		m.ToggleRaw()
	case "q":
		// Open the jq playground over this body (#2157). The pane holds the
		// body, the host owns the mode — the CopyMsg seam again.
		return func() tea.Msg { return JQPlaygroundMsg{} }
	case "m":
		// One more window of a spooled body (#2157). Nothing to load is not
		// an error: the footer only advertises the key while there is.
		m.LoadMore()
	case "o":
		// Open the whole spooled body as a file (#2157).
		return m.OpenBodyFileCmd()
	case "shift+left":
		// Sideways panning of wide bodies (#1290) moved off the bare arrows
		// when those took over history (#1471).
		m.ScrollX(-hScrollStep)
	case "shift+right":
		m.ScrollX(hScrollStep)
	case "z":
		// A fold command's second key (#1330); the viewer has no cursor, so
		// the target is the fold at the top of the view (see targetFold).
		m.pendingZ = true
		return nil
	case "0", "^":
		m.left = 0
	case "$":
		m.left = m.maxLeft()
	}
	return nil
}

// search.go territory (#1265) ------------------------------------------------

// SearchQuery reports the active pattern and whether the prompt is open
// (tests).
func (m *Model) SearchQuery() (string, bool) { return m.query, m.searching }

// MatchPosition reports the 1-based current match and the total (tests);
// 0/0 when nothing matches.
func (m *Model) MatchPosition() (int, int) {
	if len(m.matches) == 0 {
		return 0, 0
	}
	return m.cur + 1, len(m.matches)
}

// research recomputes the matches of the current query over the composed
// rows. It runs after every compose too, so browsing history or a new
// response keeps the search live instead of pointing at stale lines.
func (m *Model) research() {
	m.matches = nil
	if m.query == "" {
		m.cur = 0
		return
	}
	lines := make([]string, len(m.rows))
	for i, r := range m.rows {
		lines[i] = r.text
	}
	// Smartcase like the editor's "/" (#257): an all-lowercase pattern folds
	// case, any uppercase rune makes it exact.
	m.matches = search.Compile(m.query, false, search.CaseSmart).AllMatches(buffer.New(lines))
	if m.cur >= len(m.matches) {
		m.cur = 0
	}
}

// searchCopyKey intercepts the copy chord while the "/" prompt is open
// (#2051): the prompt otherwise swallows every key before the normal copy
// case in handleKey gets a turn, so a mouse selection made while searching
// could never reach the clipboard. ctrl+c/cmd+c/super+c are never valid
// query input, so stealing them here changes nothing about typing; "y"
// stays a plain query character since it *is* valid input. Returns nil
// (deferring to searchKey as before) unless a selection exists to copy.
func (m *Model) searchCopyKey(msg tea.KeyPressMsg) tea.Cmd {
	if !copyChord(msg) || !m.sel.on {
		return nil
	}
	return m.copyKeyCmd()
}

// copyChord reports whether msg is one of the pane's copy chords. Bare "y" is
// deliberately not one of them: it is the copy *key* at rest but valid text
// input inside a prompt, so only the modified chords can be reserved ahead of
// a capturing state (#2051, #2062).
func copyChord(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "ctrl+c", "cmd+c", "super+c":
		return true
	}
	return false
}

// copyKeyCmd is what the copy key does: the selection when there is one, else
// the whole body — the common case is "give me this response" (#1266). Every
// state that reserves the chord shares it, so the key means the same thing
// wherever it is caught.
func (m *Model) copyKeyCmd() tea.Cmd {
	if m.sel.on {
		text := m.SelectionText()
		m.ClearSelection()
		return copyCmd(text, "selection")
	}
	return copyCmd(m.BodyText(), "response body")
}

// searchKey handles one key while the "/" prompt is open. esc/enter are
// consumed here first; everything else — cursor motion, word ops, deletion,
// insertion — delegates to ui.EditKey (#763/#1845), matching the terminal
// scrollback search field.
func (m *Model) searchKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "esc":
		m.clearSearch()
		return
	case "enter":
		m.searching = false
		m.scrollToMatch()
		return
	}
	if q, cur, handled, changed := ui.EditKey(msg, m.query, m.qcur); handled {
		m.query, m.qcur = q, cur
		if changed {
			m.research()
			m.scrollToMatch()
		}
	}
}

// PasteText routes a pasted block into the open search prompt (#1955),
// mirroring the terminal scrollback search's #1882 fix: ui.PasteText
// flattens the block into the query at the rune cursor, then the same
// changed-branch searchKey runs on every edit — research() then
// scrollToMatch() — keeps matches and viewport live. A closed prompt has no
// text field to paste into, so the call is a no-op.
func (m *Model) PasteText(text string) {
	if !m.searching {
		return
	}
	q, cur, changed := ui.PasteText(m.query, m.qcur, text)
	if !changed {
		return
	}
	m.query, m.qcur = q, cur
	m.research()
	m.scrollToMatch()
}

// clearSearch drops the prompt, the pattern and every match (Esc).
func (m *Model) clearSearch() {
	m.searching = false
	m.query = ""
	m.qcur = 0
	m.matches = nil
	m.cur = 0
}

// step moves to the next (delta 1) or previous (-1) match, wrapping around.
func (m *Model) step(delta int) {
	if len(m.matches) == 0 {
		return
	}
	m.cur = (m.cur + delta + len(m.matches)) % len(m.matches)
	m.scrollToMatch()
}

// scrollToMatch brings the current match into view, keeping the viewport
// where it is when the match is already visible.
func (m *Model) scrollToMatch() {
	if len(m.matches) == 0 {
		return
	}
	sp := m.matches[m.cur]
	// A match inside a collapsed fold opens it (#1330): hiding a hit would be
	// indistinguishable from having no hit at all.
	m.reveal(sp.Line)
	d := m.displayOf(sp.Line)
	switch h := m.bodyHeight(); {
	case d < m.top:
		m.top = d
	case d >= m.top+h:
		m.top = d - h + 1
	}
	// A match past the right edge is as invisible as one below the fold
	// (#1290): pan sideways until it reads.
	switch w := m.rowWidth(); {
	case sp.Start < m.left:
		m.left = sp.Start
	case sp.End > m.left+w:
		m.left = sp.End - w
	}
	m.Scroll(0)  // clamp
	m.ScrollX(0) // clamp
}

// matchState reports whether column col of row i is inside a match (1) or
// inside the current match (2); 0 otherwise.
func (m *Model) matchState(rowIx, col int) int {
	for i, s := range m.matches {
		if s.Line == rowIx && col >= s.Start && col < s.End {
			if i == m.cur {
				return 2
			}
			return 1
		}
	}
	return 0
}

// showHistory switches the viewer to history entry i, clamped to the range.
// With keep-scroll enabled for the request (#1493) the viewport offsets
// survive the switch, clamped to the new response's extents.
func (m *Model) showHistory(i int) {
	if i < 0 || i >= len(m.hist) || i == m.histIdx {
		return
	}
	top, left := m.top, m.left
	m.histIdx = i
	m.compose(m.hist[i].Resp)
	if m.KeepScroll() {
		m.top = min(top, m.maxTop())
		m.left = min(left, m.maxLeft())
	}
}

// ToggleKeepScroll flips the keep-scroll flag of the current request (#1493)
// and reports the new state.
func (m *Model) ToggleKeepScroll() bool {
	if m.keepScroll == nil {
		m.keepScroll = map[string]bool{}
	}
	m.keepScroll[m.request] = !m.keepScroll[m.request]
	return m.keepScroll[m.request]
}

// KeepScroll reports whether the current request keeps its scroll position
// while stepping through history (#1493).
func (m *Model) KeepScroll() bool { return m.keepScroll[m.request] }

// Scroll moves the viewport by delta lines (mouse wheel + keys).
func (m *Model) Scroll(delta int) {
	m.top += delta
	if m.top > m.maxTop() {
		m.top = m.maxTop()
	}
	if m.top < 0 {
		m.top = 0
	}
}

// hScrollStep is how far one sideways key press pans. A single column would
// take forever across a minified JSON line; a screenful loses the eye's place.
const hScrollStep = 8

// ScrollX pans the viewport sideways by delta runes (keys + horizontal
// wheel), clamped to the longest composed row (#1290).
func (m *Model) ScrollX(delta int) {
	m.left += delta
	if m.left > m.maxLeft() {
		m.left = m.maxLeft()
	}
	if m.left < 0 {
		m.left = 0
	}
}

// Left reports the horizontal offset (tests).
func (m *Model) Left() int { return m.left }

func (m *Model) maxTop() int {
	return max(0, len(m.visible)-m.bodyHeight())
}

// maxLeft is the offset at which the longest row's last rune is still shown.
func (m *Model) maxLeft() int {
	longest := 0
	for _, r := range m.rows {
		if n := len([]rune(r.text)); n > longest {
			longest = n
		}
	}
	return max(0, longest-m.rowWidth())
}

// rowWidth is the room one composed row has after the leading gutter space.
func (m *Model) rowWidth() int {
	return max(1, m.width-1)
}

// bodyHeight is the room between the header and footer lines.
func (m *Model) bodyHeight() int {
	h := m.height - 2
	if h < 1 {
		h = 1
	}
	return h
}

// Title is the pane header text.
func (m *Model) Title() string {
	if m.request == "" {
		return "HTTP Response"
	}
	return "HTTP Response: " + m.request
}

// headerSeg is one piece of the pane's header line. resend marks the
// clickable re-send affordance (#1832) so the hit test can locate its columns
// from the same composition the renderer draws.
type headerSeg struct {
	text   string
	style  lipgloss.Style
	resend bool
}

// headerSegs composes the header line: title, status, the in-flight/stream
// marker, the history marker (#1473) and the re-send affordance (#1832).
func (m *Model) headerSegs(pal *theme.Palette) []headerSeg {
	head := "HTTP Response"
	if m.request != "" {
		head += ": " + m.request
	}
	segs := []headerSeg{{
		text:  " " + head,
		style: lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused),
	}}
	if m.status != "" {
		segs = append(segs, headerSeg{text: "   " + m.status, style: lipgloss.NewStyle().Faint(true)})
	}
	switch {
	case m.streaming:
		// A live stream (#1776): the rows below grow while the connection is
		// open; the marker replaces the generic pending one.
		segs = append(segs, headerSeg{
			text:  fmt.Sprintf("   ⟳ streaming… (%s)", runningFor(m.pendingSince)),
			style: lipgloss.NewStyle().Foreground(pal.Warning)})
	case m.pending != "":
		// An in-flight dispatch (#1272): the rows below are the *previous*
		// response until the new one lands.
		segs = append(segs, headerSeg{
			text:  fmt.Sprintf("   ⟳ running %s (%s)", m.pending, runningFor(m.pendingSince)),
			style: lipgloss.NewStyle().Foreground(pal.Warning)})
	}
	if m.histIdx > 0 {
		// An older history entry is on show (#1473): the footer hint alone is
		// easy to miss while reading the body, so the header carries the
		// marker where the eye actually rests.
		segs = append(segs, headerSeg{
			text:  "   " + m.historyMarker(),
			style: lipgloss.NewStyle().Foreground(pal.Warning)})
	}
	if m.CanResend() {
		// The re-send button (#1832): only where a stored request exists, so
		// its presence *is* the answer to "can this be sent again?".
		segs = append(segs,
			headerSeg{text: "   ", style: lipgloss.NewStyle()},
			headerSeg{text: resendLabel, resend: true,
				style: lipgloss.NewStyle().Foreground(pal.Accent).Underline(true)})
	}
	return segs
}

// ResendHit reports whether the pane-local cell (x, y) sits on the header's
// re-send affordance (#1832). Like the fold's copy glyph (#1787) it is tested
// before the selection press, so the label is the only re-send target.
func (m *Model) ResendHit(x, y int) bool {
	if y != 0 {
		return false
	}
	col := 0
	for _, s := range m.headerSegs(m.theme()) {
		w := lipgloss.Width(s.text)
		if s.resend {
			return x >= col && x < col+w
		}
		col += w
	}
	return false
}

// View renders the title header, the scrolled rows, and the key-hint footer.
func (m *Model) View() string {
	pal := m.theme()
	var b strings.Builder
	for _, s := range m.headerSegs(pal) {
		b.WriteString(s.style.Render(s.text))
	}
	b.WriteString("\n")

	height := m.bodyHeight()
	if len(m.rows) == 0 {
		b.WriteString(lipgloss.NewStyle().Faint(true).Render(" " + m.emptyText()))
		b.WriteString(strings.Repeat("\n", height))
	} else {
		body := make([]string, 0, height)
		for k := 0; k < height; k++ {
			line := ""
			if i := m.rowAt(m.top + k); i >= 0 {
				line = m.renderRow(pal, i)
			}
			body = append(body, line)
		}
		// The scrollbar (#1367) overlays the rightmost column when the
		// composed view overflows the viewport.
		for _, line := range m.overlayScrollbar(body) {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString(lipgloss.NewStyle().Faint(true).Render(m.clip(m.footerText())))
	return b.String()
}

// historyMarker labels an older history entry on show (#1473), e.g.
// "⧗ history 2/5 (15:04:05)". Only meaningful while histIdx > 0.
func (m *Model) historyMarker() string {
	s := fmt.Sprintf("⧗ history %d/%d", m.histIdx+1, len(m.hist))
	if at := m.hist[m.histIdx].At; !at.IsZero() {
		s += " (" + formatHistoryTime(at, time.Now()) + ")"
	}
	return s
}

// formatHistoryTime renders at (local) as a bare time when it falls on the
// same local calendar day as now, or prefixes the date when it doesn't
// (#1747) — a bare time on an older response reads as "just now".
func formatHistoryTime(at, now time.Time) string {
	at = at.Local()
	now = now.Local()
	sameDay := at.Year() == now.Year() && at.Month() == now.Month() && at.Day() == now.Day()
	if sameDay {
		return at.Format("15:04:05")
	}
	return at.Format("2006-01-02 15:04:05")
}

// footerText renders the key hints plus the history position when older
// responses are browsable (#1251).
func (m *Model) footerText() string {
	// The open "/" prompt owns the footer line: the pattern being typed plus
	// the live match count (#1265).
	if m.searching {
		s := " /" + ui.CursorView(m.query, m.qcur)
		if m.query != "" {
			s += "  " + m.matchCount()
		}
		return s
	}
	if m.query != "" {
		return " /" + m.query + "  " + m.matchCount() + " · n/N next/prev · esc clear"
	}
	// The history hint is permanent while a response is shown (#1267) and
	// leads the line (#1471): the tail is what a narrow pane clips first,
	// and the hint used to appear only once a second response existed,
	// which is exactly when nobody was looking for it.
	s := ""
	if m.streaming {
		// A live stream (#1776): the abort key leads, history is empty anyway.
		s = " x cancel stream ·"
	}
	if len(m.hist) > 0 {
		s = fmt.Sprintf(" ←/→ history %d/%d", m.histIdx+1, len(m.hist))
		if at := m.hist[m.histIdx].At; !at.IsZero() {
			s += " (" + formatHistoryTime(at, time.Now()) + ")"
		}
		// The keep-scroll toggle (#1493): the anchor marks it active, the
		// key hint appears once there is history to step through.
		if m.KeepScroll() {
			s += " ⚓ keep pos"
		} else if len(m.hist) > 1 {
			s += " · s keep pos"
		}
		// Re-running the shown entry (#2247) needs the .http file it came
		// from — the pane's source — since the block is re-read and its
		// variables resolved afresh.
		if m.source != "" {
			s += " · R re-run"
		}
		// Diffing needs a second entry to compare against (#1992). The direct
		// previous-run shortcut (#2060) only makes sense while an earlier
		// entry actually exists below the shown one.
		if len(m.hist) > 1 {
			s += " · D diff"
			if m.histIdx < len(m.hist)-1 {
				s += " · P diff prev"
			}
		}
		s += " ·"
	}
	s += " j/k scroll · shift+←/→ pan · g/G top/bottom · / search · y copy (Y headers)"
	// Switching to another request's stored history (#1829) is only offered
	// once the pane knows which .http file the response came from.
	if m.source != "" {
		s += " · r request"
	}
	// The fold hint only appears when the body actually has ranges (#1330).
	if len(m.folds) > 0 {
		s += " · za/zM/zR fold"
	}
	// The body actions (#2157) come next: the raw toggle and the jq handoff
	// need a body at all, "load more" and "open as file" a spooled one.
	if !m.streaming && m.HasBodyText() {
		if m.raw {
			s += " · t pretty"
		} else {
			s += " · t raw"
		}
		s += " · q jq"
	}
	if m.CanLoadMore() {
		s += " · m more"
	}
	if m.BodyFilePath() != "" {
		s += " · o open file"
	}
	// The curl export and the file save (#2059) need an entry on show; a
	// live stream has neither a snapshot nor a finished body. They sit last:
	// they are the rarest actions, and the footer truncates from the right.
	if !m.streaming && len(m.hist) > 0 {
		s += " · C curl · S save"
	}
	return s
}

// runningFor formats how long the in-flight dispatch has been running.
func runningFor(since time.Time) string {
	if since.IsZero() {
		return "0ms"
	}
	d := time.Since(since)
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// matchCount renders the match position, "no matches" when the pattern hits
// nothing (#1265).
func (m *Model) matchCount() string {
	if len(m.matches) == 0 {
		return "no matches"
	}
	return fmt.Sprintf("%d/%d", m.cur+1, len(m.matches))
}

func (m *Model) emptyText() string {
	if m.loaded {
		return "(empty response)"
	}
	// The stored-response route is the non-obvious one (#1829): without it the
	// pane looks like it can only ever show what was just dispatched.
	return "(no response yet — run an .http request, or show a stored one with http.showResponse)"
}

// renderRow draws one composed line per kind, with the search-match overlay
// painted on top (#1265). Every kind renders through paintRow so a match
// highlights the same way in the status line, a header and the body.
func (m *Model) renderRow(pal *theme.Palette, i int) string {
	r := m.rows[i]
	if r.kind == kindBlank {
		return ""
	}
	// Only the window [left, left+width) of the row is on screen (#1290); the
	// columns keep their absolute index so highlight, matches and selection
	// stay aligned with the text they belong to.
	runes := []rune(r.text)
	from := min(m.left, len(runes))
	to, tail := len(runes), ""
	if w := m.rowWidth(); to-from > w {
		to, tail = from+w-1, "…"
	}
	// The leading gutter cell carries the fold marker (#1330); a collapsed
	// header states how many rows it hides, like the editor's placeholder.
	gutter := lipgloss.NewStyle().Foreground(pal.Secondary).Render(m.foldGutter(i))
	line := gutter + m.paintRow(i, runes[from:to], from, m.baseStyle(pal, r)) + tail
	if ph := m.foldPlaceholder(i); ph != "" {
		// A collapsed header also carries the copy affordance (#1787), one
		// space behind the placeholder — the only copy target on the row, so
		// clicking anywhere else still toggles or selects.
		line += lipgloss.NewStyle().Faint(true).Render(ph) +
			" " + lipgloss.NewStyle().Foreground(pal.Secondary).Render(foldCopyGlyph)
	}
	return line
}

// styleFn resolves the non-match styling of one column: the style plus a key
// that groups adjacent columns sharing it into a single rendered segment.
type styleFn func(col int) (lipgloss.Style, string)

// baseStyle returns the per-kind column styling of a row.
func (m *Model) baseStyle(pal *theme.Palette, r row) styleFn {
	plain := lipgloss.NewStyle()
	switch r.kind {
	case kindStatus:
		st := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
		return func(int) (lipgloss.Style, string) { return st, "status" }
	case kindWarn:
		st := lipgloss.NewStyle().Foreground(pal.Warning)
		return func(int) (lipgloss.Style, string) { return st, "warn" }
	case kindHeader:
		// The name (up to and including the colon) reads as a constant, the
		// value plain — the pre-#1265 look, now column-addressed.
		name, _, _ := strings.Cut(r.text, ": ")
		nameEnd := len([]rune(name)) + 1
		nameStyle, ok := m.hl.Style("constant")
		if !ok {
			nameStyle = plain
		}
		return func(col int) (lipgloss.Style, string) {
			if col < nameEnd {
				return nameStyle, "name"
			}
			return plain, "value"
		}
	case kindBody:
		// Identifier colors (#1626): UUIDs and hex hashes in the response
		// body take a rainbow color keyed on the identifier's own hash, so
		// the trace ID of this response matches the one in the log pane. The
		// gate is the idcolor package global (editor.id_colors) — this pane
		// has no config of its own.
		ids := m.bodyIDs(r.text)
		return func(col int) (lipgloss.Style, string) {
			capture := m.bodyIx.CaptureAt(r.body, col)
			if id, ok := idAt(ids, col); ok {
				if st, ok := m.hl.Style(idcolor.Capture(id.Slot)); ok {
					return st, idcolor.Capture(id.Slot)
				}
			}
			if st, ok := m.hl.Style(capture); ok {
				return st, capture
			}
			return plain, capture
		}
	}
	return func(int) (lipgloss.Style, string) { return plain, "" }
}

// bodyIDs scans one body row for identifiers (#1626); nil while the feature
// is off. Only the rows the pane actually draws are scanned.
func (m *Model) bodyIDs(text string) []idcolor.Span {
	if !idcolor.Enabled() {
		return nil
	}
	return idcolor.Scan(text, idcolor.MinLength())
}

// idAt returns the identifier span covering a column.
func idAt(spans []idcolor.Span, col int) (idcolor.Span, bool) {
	for _, s := range spans {
		if col >= s.Start && col < s.End {
			return s, true
		}
	}
	return idcolor.Span{}, false
}

// paintRow renders text with base styling per column and the search-match
// overlay: every match gets a muted background, the current one the
// selection background plus an underline — the editor's convention (#255).
// The columns are absolute row columns: runes holds the visible window and
// offset is the column its first rune sits at.
func (m *Model) paintRow(rowIx int, runes []rune, offset int, base styleFn) string {
	var b strings.Builder
	last := offset + len(runes)
	for col := offset; col < last; {
		st, key := base(col)
		state := m.matchState(rowIx, col)
		end := col + 1
		selected := m.selState(rowIx, col)
		for end < last {
			if _, k := base(end); k != key || m.matchState(rowIx, end) != state || m.selState(rowIx, end) != selected {
				break
			}
			end++
		}
		switch {
		case selected:
			// The mouse selection wins the background (#1266): it is the
			// thing the user is acting on right now.
			st = st.Background(m.theme().Selection).Foreground(m.theme().SelectionText)
		case state == 2:
			st = st.Background(m.theme().Selection).Underline(true)
		case state == 1:
			st = st.Background(m.theme().SelectionMuted)
		}
		b.WriteString(st.Render(string(runes[col-offset : end-offset])))
		col = end
	}
	return b.String()
}

// clip bounds one rendered line to the pane width.
func (m *Model) clip(s string) string {
	if m.width > 0 && len([]rune(s)) > m.width {
		return string([]rune(s)[:m.width-1]) + "…"
	}
	return s
}

// theme resolves the palette with the shared default fallback.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}
