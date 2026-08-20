// Package debugdoctor is the Xdebug Doctor tool window (#1991): a singleton
// pane that makes the DBGp listener observable — whether it is running, on
// which port and with which filter configuration, plus a live trace of every
// incoming connection attempt with its accept/reject outcome and the concrete
// reason. It is a pure consumer of the app-owned Log — the root model feeds
// it from the bridge's ike.listenState / ike.debugConn events; the panel only
// renders and emits ClearMsg.
package debugdoctor

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/theme"
	"ike/internal/ui"
)

// maxEntries bounds the trace; the oldest attempts fall off first. Generous —
// a page load with assets is a handful of attempts, not hundreds.
const maxEntries = 200

// ClearMsg asks the root model to clear the connection trace.
type ClearMsg struct{}

// Listener is the doctor's view of the DBGp listener state.
type Listener struct {
	Running  bool
	Port     int
	Hostname string // host filter; empty = every request attaches
	Mappings int    // configured path mappings
}

// Entry is one traced connection attempt.
type Entry struct {
	Time     time.Time
	Accepted bool
	Reason   string // reject reason key: handshake|init|filter|busy|ended
	Detail   string // human-readable rejection detail from the bridge
	Remote   string // source address of the TCP connection
	IDEKey   string // idekey from the init packet, when one arrived
	FileURI  string // initial file URI from the init packet
	Host     string // probed $_SERVER['HTTP_HOST'], when the filter ran
	Local    string // accepted: the locally mapped entry file
	Mapped   bool   // accepted: whether the entry file resolved locally
}

// Log is the app-owned doctor state: the listener status plus a bounded
// trace of connection attempts. It survives the panel being closed.
type Log struct {
	listener Listener
	entries  []Entry
}

// NewLog returns an empty log (listener stopped, no attempts).
func NewLog() *Log { return &Log{} }

// SetListener replaces the listener status.
func (l *Log) SetListener(ls Listener) { l.listener = ls }

// Listener returns the current listener status.
func (l *Log) Listener() Listener { return l.listener }

// Add appends one attempt, dropping the oldest past maxEntries.
func (l *Log) Add(e Entry) {
	l.entries = append(l.entries, e)
	if len(l.entries) > maxEntries {
		l.entries = l.entries[len(l.entries)-maxEntries:]
	}
}

// Entries returns the attempts in arrival order.
func (l *Log) Entries() []Entry { return l.entries }

// Clear drops the trace; the listener status stays.
func (l *Log) Clear() { l.entries = nil }

// Model is the tool window state. Value type with pointer-receiver mutators,
// embedded in a pane.Instance like the Breakpoints panel.
type Model struct {
	width   int
	height  int
	focused bool
	pal     *theme.Palette

	log *Log

	cursor int
	top    int
}

// New returns an empty panel; the log arrives via SetLog.
func New(pal *theme.Palette) Model {
	return Model{pal: pal}
}

// SetLog shares the app-owned doctor log; the panel renders it live.
func (m *Model) SetLog(l *Log) { m.log = l }

// SetSize records the interior content size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetFocused marks the panel focused (header + selection highlight).
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the active theme.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// Rows reports the entry count (tests).
func (m *Model) Rows() int { return len(m.entries()) }

// Cursor reports the selected row index (tests).
func (m *Model) Cursor() int { return m.cursor }

// entries returns the trace newest-first — the freshest attempt is what the
// user came to see, and it must be visible without scrolling.
func (m *Model) entries() []Entry {
	if m.log == nil {
		return nil
	}
	src := m.log.Entries()
	out := make([]Entry, len(src))
	for i, e := range src {
		out[len(src)-1-i] = e
	}
	return out
}

// Update handles one message while the panel exists; only key presses reach
// it, focus-filtered by the pane layer.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	// Shared list semantics (#1666): steps wrap, page jumps clamp.
	if ui.ListNav(k.String(), &m.cursor, len(m.entries()), m.bodyHeight(), ui.NavFull) {
		m.clampScroll()
		return nil
	}
	switch k.String() {
	case "c":
		if m.log != nil && len(m.log.Entries()) > 0 {
			return func() tea.Msg { return ClearMsg{} }
		}
	}
	m.clampScroll()
	return nil
}

// View renders the listener status header, the scrolled trace, and the
// key-hint footer.
func (m *Model) View() string {
	pal := m.theme()
	var b strings.Builder
	b.WriteString(m.headerLine(pal))
	b.WriteString("\n")
	b.WriteString(m.statusLine(pal))
	b.WriteString("\n")
	b.WriteString(m.renderRows(pal, m.bodyHeight()))
	b.WriteString(m.footer(pal))
	return b.String()
}

// headerLine is the panel title with the attempt count.
func (m *Model) headerLine(pal *theme.Palette) string {
	n := len(m.entries())
	counts := strconv.Itoa(n) + " connection attempt"
	if n != 1 {
		counts += "s"
	}
	title := lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(" Xdebug Doctor")
	return title + lipgloss.NewStyle().Faint(true).Render("   "+counts)
}

// statusLine renders the listener state: running (port, filter, mappings) or
// stopped with the way to start it.
func (m *Model) statusLine(pal *theme.Palette) string {
	ls := Listener{}
	if m.log != nil {
		ls = m.log.Listener()
	}
	if !ls.Running {
		mark := lipgloss.NewStyle().Faint(true).Render(" ○ ")
		text := "listener stopped — run \"Listen for PHP Debug Connections\" to start it"
		return mark + lipgloss.NewStyle().Faint(true).Render(text)
	}
	mark := lipgloss.NewStyle().Foreground(pal.Success).Render(" ● ")
	text := "listening on port " + strconv.Itoa(ls.Port)
	if ls.Hostname != "" {
		text += " · host filter " + ls.Hostname
	} else {
		text += " · no host filter"
	}
	switch ls.Mappings {
	case 0:
		text += " · no path mappings"
	case 1:
		text += " · 1 path mapping"
	default:
		text += " · " + strconv.Itoa(ls.Mappings) + " path mappings"
	}
	return mark + lipgloss.NewStyle().Foreground(pal.Foreground).Render(m.clip(text))
}

// renderRows draws the trace newest-first, scrolled around the cursor.
func (m *Model) renderRows(pal *theme.Palette, height int) string {
	entries := m.entries()
	if len(entries) == 0 {
		empty := lipgloss.NewStyle().Faint(true).Render(" (no connection attempts yet — trigger a request with Xdebug active)")
		return empty + strings.Repeat("\n", height)
	}
	m.clampScroll()
	var b strings.Builder
	for k := 0; k < height; k++ {
		i := m.top + k
		if i < len(entries) {
			b.WriteString(m.renderRow(pal, entries[i], i))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderRow draws one attempt: timestamp, outcome glyph (✓ accepted, ⚠
// accepted with an unmapped entry file, ✗ rejected), the source address and
// the outcome text with enough identity to fix a rejection.
func (m *Model) renderRow(pal *theme.Palette, e Entry, i int) string {
	style := lipgloss.NewStyle().Foreground(pal.Foreground)
	glyph := "✓"
	switch {
	case e.Accepted && !e.Mapped:
		glyph = "⚠"
		style = style.Foreground(pal.Warning)
	case e.Accepted:
		style = style.Foreground(pal.Success)
	default:
		glyph = "✗"
		style = style.Foreground(pal.Error)
	}
	line := " " + e.Time.Format("15:04:05") + "  " + glyph + " " + e.Remote + "  " + m.describe(e)
	if i == m.cursor {
		if m.focused {
			style = style.Background(pal.Selection).Bold(true)
		} else {
			style = style.Background(pal.SelectionMuted)
		}
	}
	return style.Render(m.clip(line))
}

// describe words one attempt's outcome, leading with the diagnosis and
// trailing with the request's identity (idekey, file, host) when known.
func (m *Model) describe(e Entry) string {
	var text string
	if e.Accepted {
		text = "accepted → " + e.Local
		if !e.Mapped {
			text = "accepted — entry file has no local path mapping (" + e.FileURI + ")"
		}
	} else {
		text = rejectLabel(e.Reason)
		if e.Detail != "" {
			text += " — " + e.Detail
		}
	}
	var id []string
	if e.IDEKey != "" {
		id = append(id, "idekey "+e.IDEKey)
	}
	if !e.Accepted && e.FileURI != "" {
		id = append(id, "file "+e.FileURI)
	}
	if e.Host != "" {
		id = append(id, "host "+e.Host)
	}
	if len(id) > 0 {
		text += "  [" + strings.Join(id, ", ") + "]"
	}
	return text
}

// rejectLabel words a bridge reject reason key.
func rejectLabel(reason string) string {
	switch reason {
	case "filter":
		return "rejected: hostname filter mismatch"
	case "busy":
		return "rejected: listener busy"
	case "handshake":
		return "rejected: no DBGp handshake"
	case "init":
		return "rejected: malformed init packet"
	case "ended":
		return "rejected: debug session over"
	}
	return "rejected: " + reason
}

// footer shows the key hints.
func (m *Model) footer(pal *theme.Palette) string {
	return lipgloss.NewStyle().Faint(true).Render(m.clip(" c clear trace"))
}

// bodyHeight is the room between the two header lines and the footer.
func (m *Model) bodyHeight() int {
	h := m.height - 3
	if h < 1 {
		h = 1
	}
	return h
}

// clampScroll keeps the cursor valid and inside the visible window.
func (m *Model) clampScroll() {
	n := len(m.entries())
	if m.cursor > n-1 {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.top > m.cursor {
		m.top = m.cursor
	}
	if h := m.bodyHeight(); m.cursor >= m.top+h {
		m.top = m.cursor - h + 1
	}
	if m.top < 0 {
		m.top = 0
	}
}

// clip bounds one rendered line to the panel width.
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
