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
		m.bodyIx = highlight.NewIndex(highlight.HighlightFenced(tag, body))
		for i, line := range body {
			m.rows = append(m.rows, row{kind: kindBody, text: line, body: i})
		}
	}
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
	case ct == "text/html":
		return "html"
	case ct == "text/css":
		return "css"
	case ct == "application/javascript" || ct == "text/javascript":
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
	switch msg.String() {
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
	s := " j/k scroll · g/G top/bottom"
	if len(m.hist) > 1 {
		s += fmt.Sprintf(" · h/l history %d/%d", m.histIdx+1, len(m.hist))
		if at := m.hist[m.histIdx].At; !at.IsZero() {
			s += " (" + at.Local().Format("15:04:05") + ")"
		}
	}
	return s
}

func (m *Model) emptyText() string {
	if m.loaded {
		return "(empty response)"
	}
	return "(no response yet — run an .http request)"
}

// renderRow draws one composed line per kind.
func (m *Model) renderRow(pal *theme.Palette, i int) string {
	r := m.rows[i]
	switch r.kind {
	case kindStatus:
		return lipgloss.NewStyle().Foreground(pal.Accent).Bold(true).Render(m.clip(" " + r.text))
	case kindHeader:
		name, value, _ := strings.Cut(r.text, ": ")
		nm := " " + name + ":"
		if st, ok := m.hl.Style("constant"); ok {
			nm = st.Render(m.clip(nm))
		}
		return nm + " " + m.clip(value)
	case kindWarn:
		return lipgloss.NewStyle().Foreground(pal.Warning).Render(m.clip(" " + r.text))
	case kindBody:
		return " " + m.styledBodyLine(r.body, m.clipRaw(r.text))
	default:
		return ""
	}
}

// styledBodyLine applies capture styles from the body highlight index,
// grouping adjacent runes with the same capture into one styled segment.
func (m *Model) styledBodyLine(ln int, line string) string {
	if m.bodyIx.Empty() {
		return line
	}
	runes := []rune(line)
	var b strings.Builder
	for col := 0; col < len(runes); {
		capture := m.bodyIx.CaptureAt(ln, col)
		end := col + 1
		for end < len(runes) && m.bodyIx.CaptureAt(ln, end) == capture {
			end++
		}
		seg := string(runes[col:end])
		if st, ok := m.hl.Style(capture); ok {
			seg = st.Render(seg)
		}
		b.WriteString(seg)
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
