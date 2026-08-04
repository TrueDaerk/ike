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
	"encoding/json"
	"fmt"
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
	"ike/internal/theme"
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
	// selected one (n/N step through them).
	searching bool
	query     string
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

// compose rebuilds the display rows for one response.
func (m *Model) compose(resp *httpclient.Response) {
	m.loaded = true
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

	body, tag, notice := formatBody(resp)
	if notice != "" {
		m.rows = append(m.rows, row{kind: kindWarn, text: notice})
	}
	if len(body) > 0 {
		if tag != "" && !highlight.FencedSupported(tag) {
			// A recognized Content-Type whose grammar is not in this build
			// (CGo-free, or the plugin not linked) renders plain — say so
			// instead of failing silently (#1270).
			m.rows = append(m.rows, row{kind: kindWarn, text: fmt.Sprintf("(no %s highlighter in this build — showing plain text)", tag)})
		}
		m.bodyIx = highlight.NewIndex(highlight.HighlightFenced(tag, body))
		bodyStart := len(m.rows)
		for i, line := range body {
			m.rows = append(m.rows, row{kind: kindBody, text: line, body: i})
		}
		// The body folds by its own language's rules (#1330).
		m.setFolds(highlight.FencedFolds(tag, body), bodyStart)
	}
	m.syncVisible()
	m.ClearSelection() // the rows changed: an old selection means nothing
	m.research()       // the search survives history browsing and new responses
}

// formatBody renders the response body for display: pretty-printed JSON,
// raw text for other recognized/unknown text types, a notice instead of
// content for binary bodies. tag is the fence tag used for highlighting.
func formatBody(resp *httpclient.Response) (lines []string, tag, notice string) {
	if len(resp.Body) == 0 {
		return nil, "", ""
	}
	if isBinary(resp.Body) {
		return nil, "", fmt.Sprintf("(binary body, %d bytes — not shown)", len(resp.Body))
	}
	tag = contentTag(resp.Headers.Get("Content-Type"))
	text := string(resp.Body)
	if tag == "json" {
		var buf bytes.Buffer
		if err := json.Indent(&buf, resp.Body, "", "  "); err == nil {
			text = buf.String()
		}
	}
	return strings.Split(strings.TrimRight(text, "\n"), "\n"), tag, ""
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
		m.searchKey(msg)
		return nil
	}
	if m.pendingZ {
		m.pendingZ = false
		m.foldKey(msg.String())
		return nil
	}
	switch msg.String() {
	case "/":
		// Open the in-pane search prompt (#1265), editor conventions.
		m.searching = true
		m.query = ""
		m.matches = nil
		m.cur = 0
	case "n":
		m.step(1)
	case "N":
		m.step(-1)
	case "esc":
		m.clearSearch()
		m.ClearSelection()
	case "y", "ctrl+c", "cmd+c", "super+c":
		// Copy the selection, or the whole body when nothing is selected —
		// the common case is "give me this response" (#1266).
		if m.sel.on {
			text := m.SelectionText()
			m.ClearSelection()
			return copyCmd(text, "selection")
		}
		return copyCmd(m.BodyText(), "response body")
	case "Y":
		return copyCmd(m.HeadersText(), "response headers")
	case "x":
		// Cancel the in-flight dispatch (#1272). ctrl+c is taken by copy
		// (#1266), so the abort key is its own.
		return func() tea.Msg { return CancelMsg{} }
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
	case "s":
		// Keep the scroll position while stepping through history (#1493),
		// per request; the footer anchor marks the active state.
		m.ToggleKeepScroll()
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

// searchKey handles one key while the "/" prompt is open.
func (m *Model) searchKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "esc":
		m.clearSearch()
	case "enter":
		m.searching = false
		m.scrollToMatch()
	case "backspace":
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
		}
		m.research()
		m.scrollToMatch()
	default:
		if t := msg.Text; t != "" {
			m.query += t
			m.research()
			m.scrollToMatch()
		}
	}
}

// clearSearch drops the prompt, the pattern and every match (Esc).
func (m *Model) clearSearch() {
	m.searching = false
	m.query = ""
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

// View renders the title header, the scrolled rows, and the key-hint footer.
func (m *Model) View() string {
	pal := m.theme()
	var b strings.Builder
	head := "HTTP Response"
	if m.request != "" {
		head += ": " + m.request
	}
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(" " + head))
	if m.status != "" {
		b.WriteString(lipgloss.NewStyle().Faint(true).Render("   " + m.status))
	}
	if m.pending != "" {
		// An in-flight dispatch (#1272): the rows below are the *previous*
		// response until the new one lands.
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Warning).Render(
			fmt.Sprintf("   ⟳ running %s (%s)", m.pending, runningFor(m.pendingSince))))
	}
	if m.histIdx > 0 {
		// An older history entry is on show (#1473): the footer hint alone is
		// easy to miss while reading the body, so the header carries the
		// marker where the eye actually rests.
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Warning).Render("   " + m.historyMarker()))
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
		s += " (" + at.Local().Format("15:04:05") + ")"
	}
	return s
}

// footerText renders the key hints plus the history position when older
// responses are browsable (#1251).
func (m *Model) footerText() string {
	// The open "/" prompt owns the footer line: the pattern being typed plus
	// the live match count (#1265).
	if m.searching {
		s := " /" + m.query + "▏"
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
	if len(m.hist) > 0 {
		s = fmt.Sprintf(" ←/→ history %d/%d", m.histIdx+1, len(m.hist))
		if at := m.hist[m.histIdx].At; !at.IsZero() {
			s += " (" + at.Local().Format("15:04:05") + ")"
		}
		// The keep-scroll toggle (#1493): the anchor marks it active, the
		// key hint appears once there is history to step through.
		if m.KeepScroll() {
			s += " ⚓ keep pos"
		} else if len(m.hist) > 1 {
			s += " · s keep pos"
		}
		s += " ·"
	}
	s += " j/k scroll · shift+←/→ pan · g/G top/bottom · / search · y copy (Y headers)"
	// The fold hint only appears when the body actually has ranges (#1330).
	if len(m.folds) > 0 {
		s += " · za/zM/zR fold"
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
	return "(no response yet — run an .http request)"
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
		line += lipgloss.NewStyle().Faint(true).Render(ph)
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
		return func(col int) (lipgloss.Style, string) {
			capture := m.bodyIx.CaptureAt(r.body, col)
			if st, ok := m.hl.Style(capture); ok {
				return st, capture
			}
			return plain, capture
		}
	}
	return func(int) (lipgloss.Style, string) { return plain, "" }
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
