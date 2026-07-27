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

	rows []row
	// bodyIx indexes the highlight spans of the body lines only.
	bodyIx highlight.Index
	top    int

	// In-pane search (#1265) over the whole composed view — status line,
	// headers and formatted body alike. searching marks the open "/" prompt,
	// query is the live pattern, matches every hit in row order and cur the
	// selected one (n/N step through them).
	searching bool
	query     string
	matches   []search.Span
	cur       int
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

// HistoryIndex reports the shown entry and the history length (tests).
func (m *Model) HistoryIndex() (int, int) { return m.histIdx, len(m.hist) }

// compose rebuilds the display rows for one response.
func (m *Model) compose(resp *httpclient.Response) {
	m.loaded = true
	m.top = 0
	m.rows = nil
	m.bodyIx = highlight.Index{}
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
		for i, line := range body {
			m.rows = append(m.rows, row{kind: kindBody, text: line, body: i})
		}
	}
	m.research() // the search survives history browsing and new responses
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
		m.handleKey(k)
	}
	return nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) {
	if m.searching {
		m.searchKey(msg)
		return
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
	case "G", "end":
		m.top = m.maxTop()
	case "h", "left":
		// Older stored response of the same request (#1251).
		m.showHistory(m.histIdx + 1)
	case "l", "right":
		// Newer stored response.
		m.showHistory(m.histIdx - 1)
	}
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
	line := m.matches[m.cur].Line
	switch h := m.bodyHeight(); {
	case line < m.top:
		m.top = line
	case line >= m.top+h:
		m.top = line - h + 1
	}
	m.Scroll(0) // clamp
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
func (m *Model) showHistory(i int) {
	if i < 0 || i >= len(m.hist) || i == m.histIdx {
		return
	}
	m.histIdx = i
	m.compose(m.hist[i].Resp)
}

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

func (m *Model) maxTop() int {
	return max(0, len(m.rows)-m.bodyHeight())
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
	b.WriteString("\n")

	height := m.bodyHeight()
	if len(m.rows) == 0 {
		b.WriteString(lipgloss.NewStyle().Faint(true).Render(" " + m.emptyText()))
		b.WriteString(strings.Repeat("\n", height))
	} else {
		for k := 0; k < height; k++ {
			i := m.top + k
			if i < len(m.rows) {
				b.WriteString(m.renderRow(pal, i))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString(lipgloss.NewStyle().Faint(true).Render(m.clip(m.footerText())))
	return b.String()
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
	s := " j/k scroll · g/G top/bottom · / search"
	if len(m.hist) > 1 {
		s += fmt.Sprintf(" · h/l history %d/%d", m.histIdx+1, len(m.hist))
		if at := m.hist[m.histIdx].At; !at.IsZero() {
			s += " (" + at.Local().Format("15:04:05") + ")"
		}
	}
	return s
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
	text := m.clipRaw(r.text)
	return " " + m.paintRow(i, text, m.baseStyle(pal, r))
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
func (m *Model) paintRow(rowIx int, text string, base styleFn) string {
	runes := []rune(text)
	var b strings.Builder
	for col := 0; col < len(runes); {
		st, key := base(col)
		state := m.matchState(rowIx, col)
		end := col + 1
		for end < len(runes) {
			if _, k := base(end); k != key || m.matchState(rowIx, end) != state {
				break
			}
			end++
		}
		switch state {
		case 2:
			st = st.Background(m.theme().Selection).Underline(true)
		case 1:
			st = st.Background(m.theme().SelectionMuted)
		}
		b.WriteString(st.Render(string(runes[col:end])))
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

// clipRaw bounds a body line, leaving one column for the leading space.
func (m *Model) clipRaw(s string) string {
	if m.width > 1 && len([]rune(s)) > m.width-1 {
		return string([]rune(s)[:m.width-2]) + "…"
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
