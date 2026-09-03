package remote

// model.go is the browser pane. It follows the archive viewer's tree shape
// (rows flattened from lazily expanded nodes) and the ES console's async
// discipline: no network round trip ever runs inside Update — the connect and
// every directory scan are background commands whose results funnel into the
// one ResultMsg the root model routes back by pane key. A result landing
// after its pane closed is discarded (closing the just-dialed connection).

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/theme"
	"ike/internal/ui"
)

// OpenFileMsg asks the root model to open the remote file at Path on the
// pane's host: download to the cache, then editor (read-only) or viewer.
// Emitted by enter on a file row.
type OpenFileMsg struct {
	Key   string
	Alias string
	Path  string
	Size  int64
}

// ResultMsg carries one background result to its pane: exactly one of the
// pointers is set. The root model routes it by Key.
type ResultMsg struct {
	Key  string
	conn *connResult
	scan *scanResult
}

// Discard drops an unroutable result (its pane is gone). A connect that
// landed too late still holds a live session — close it, or the ssh
// subprocess would linger for the rest of the session.
func (msg ResultMsg) Discard() {
	if msg.conn != nil && msg.conn.conn != nil {
		_ = msg.conn.conn.Close()
	}
}

type connResult struct {
	conn Conn
	home string
	err  error
}

type scanResult struct {
	path    string
	entries []Entry
	err     error
}

// node is one remote directory or file in the lazily loaded tree.
type node struct {
	name     string
	path     string
	entry    Entry
	children []*node
	expanded bool
	loaded   bool
	loading  bool
}

// row is one rendered line of the flattened tree.
type row struct {
	*node
	depth int
}

// Model is one remote browser pane bound to an ssh host alias.
type Model struct {
	key   string
	alias string
	dial  DialFunc
	pal   *theme.Palette

	conn       Conn
	connecting bool
	connErr    error
	closed     bool

	root       *node
	rows       []row
	cursor     int
	top        int
	showHidden bool
	// pendingReveal descends toward the remote home directory after the
	// connect, one scan per not-yet-loaded ancestor — the explorer's reveal
	// walk, so the pane opens on $HOME instead of a bare "/".
	pendingReveal string
	lastErr       error

	// Double-click detection (#2259) mirrors the archive viewer: activating a
	// row with the mouse needs a second click on it within
	// ui.DoubleClickWindow; now is injectable so tests control the clock.
	clicks ui.ClickTracker
	now    func() time.Time

	w, h    int
	focused bool
}

// New builds a browser for alias. dial is the connection seam (nil means the
// real ssh Dial); the connect itself happens in Init, never here.
func New(key, alias string, dial DialFunc, pal *theme.Palette) Model {
	if dial == nil {
		dial = Dial
	}
	return Model{
		key:   key,
		alias: alias,
		dial:  dial,
		pal:   pal,
		now:   time.Now,
		root:  &node{name: "/", path: "/", expanded: true},
	}
}

// Key returns the pane's routing key.
func (m *Model) Key() string { return m.key }

// Alias returns the ssh host alias the pane browses.
func (m *Model) Alias() string { return m.alias }

// Conn returns the live connection, nil while connecting or failed. The app
// uses it to fetch a file on the same session.
func (m *Model) Conn() Conn { return m.conn }

// Err returns the connect error, if any (tests).
func (m *Model) Err() error { return m.connErr }

// Rows reports the number of visible rows (tests).
func (m *Model) Rows() int { return len(m.rows) }

// Cursor reports the selected row index (tests).
func (m *Model) Cursor() int { return m.cursor }

// RowPath returns the remote path of row i, "" when out of range (tests).
func (m *Model) RowPath(i int) string {
	if i < 0 || i >= len(m.rows) {
		return ""
	}
	return m.rows[i].path
}

// ShowingHidden reports the hidden-entry toggle state (tests).
func (m *Model) ShowingHidden() bool { return m.showHidden }

// SetShowHidden seeds the hidden-entry filter (the explorer.show_hidden
// default); the runtime "." toggle stays authoritative afterwards.
func (m *Model) SetShowHidden(v bool) {
	m.showHidden = v
	m.rebuild()
}

// SetSize records the pane interior in cells.
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h; m.clampScroll() }

// SetFocused records focus for the header and the selection highlight.
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the theme palette.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// Close marks the pane dead and ends its session. Late background results
// route nowhere (the registry entry is gone) and are discarded by the app.
func (m *Model) Close() {
	m.closed = true
	if m.conn != nil {
		_ = m.conn.Close()
		m.conn = nil
	}
}

// Init is the one-shot background connect.
func (m *Model) Init() tea.Cmd {
	if m.connecting || m.conn != nil || m.connErr != nil {
		return nil
	}
	m.connecting = true
	dial, alias, key := m.dial, m.alias, m.key
	return func() tea.Msg {
		conn, err := dial(alias)
		if err != nil {
			return ResultMsg{Key: key, conn: &connResult{err: err}}
		}
		home, err := conn.Home()
		if err != nil {
			// A session that cannot resolve its start directory still
			// browses; the tree just opens at "/".
			home = "/"
		}
		return ResultMsg{Key: key, conn: &connResult{conn: conn, home: home}}
	}
}

// Update handles key input and the pane's own background results.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case ResultMsg:
		return m.applyResult(msg)
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return nil
}

// applyResult lands one background result.
func (m *Model) applyResult(msg ResultMsg) tea.Cmd {
	if m.closed {
		msg.Discard()
		return nil
	}
	switch {
	case msg.conn != nil:
		return m.applyConn(msg.conn)
	case msg.scan != nil:
		return m.applyScan(msg.scan)
	}
	return nil
}

// applyConn installs the connection and starts the reveal walk toward the
// remote home directory.
func (m *Model) applyConn(r *connResult) tea.Cmd {
	m.connecting = false
	if r.err != nil {
		m.connErr = r.err
		return nil
	}
	m.conn = r.conn
	m.pendingReveal = path.Clean(r.home)
	return m.scanCmd(m.root)
}

// scanCmd lists one directory in the background. The node is marked loading
// so a held-down expand cannot fan out duplicate scans.
func (m *Model) scanCmd(n *node) tea.Cmd {
	if m.conn == nil || n.loading {
		return nil
	}
	n.loading = true
	conn, key, p := m.conn, m.key, n.path
	return func() tea.Msg {
		entries, err := conn.ReadDir(p)
		return ResultMsg{Key: key, scan: &scanResult{path: p, entries: entries, err: err}}
	}
}

// applyScan installs one directory listing, merging by path so expansion
// state and loaded subtrees survive a refresh, then resumes a pending reveal.
func (m *Model) applyScan(r *scanResult) tea.Cmd {
	n := m.find(r.path)
	if n == nil {
		return nil
	}
	n.loading = false
	if r.err != nil {
		m.lastErr = fmt.Errorf("%s: %w", r.path, r.err)
		m.rebuild()
		return nil
	}
	m.lastErr = nil
	n.loaded = true
	m.setChildren(n, r.entries)
	m.rebuild()
	return m.continueReveal()
}

// setChildren merges a fresh listing into n, reusing existing child nodes
// (matched by path and kind) so their expansion and loaded subtrees survive.
func (m *Model) setChildren(n *node, entries []Entry) {
	old := make(map[string]*node, len(n.children))
	for _, c := range n.children {
		old[c.path] = c
	}
	children := make([]*node, 0, len(entries))
	for _, e := range entries {
		p := path.Join(n.path, e.Name)
		if prev, ok := old[p]; ok && prev.entry.IsDir() == e.IsDir() {
			prev.entry = e
			children = append(children, prev)
			continue
		}
		children = append(children, &node{name: e.Name, path: p, entry: e})
	}
	sort.SliceStable(children, func(i, j int) bool {
		a, b := children[i], children[j]
		if a.entry.IsDir() != b.entry.IsDir() {
			return a.entry.IsDir()
		}
		return strings.ToLower(a.name) < strings.ToLower(b.name)
	})
	n.children = children
}

// continueReveal advances the descent toward the pending reveal target: every
// loaded ancestor expands in place, the first unloaded one scans, and the
// walk resumes when its listing lands. A target that left the tree abandons
// the reveal.
func (m *Model) continueReveal() tea.Cmd {
	target := m.pendingReveal
	if target == "" {
		return nil
	}
	n := m.root
	for n.path != target {
		if !n.loaded {
			if n.loading {
				return nil
			}
			return m.scanCmd(n)
		}
		next := m.childToward(n, target)
		if next == nil || !next.entry.IsDir() {
			m.pendingReveal = ""
			m.rebuild()
			return nil
		}
		next.expanded = true
		n = next
	}
	// Arrived: land the listing of the target itself, then select it.
	if !n.loaded {
		if n.loading {
			return nil
		}
		return m.scanCmd(n)
	}
	m.pendingReveal = ""
	m.rebuild()
	m.selectPath(target)
	return nil
}

// childToward returns n's child on the path toward target.
func (m *Model) childToward(n *node, target string) *node {
	rest := strings.TrimPrefix(target, strings.TrimSuffix(n.path, "/")+"/")
	seg, _, _ := strings.Cut(rest, "/")
	for _, c := range n.children {
		if c.name == seg {
			return c
		}
	}
	return nil
}

// find resolves a path to its node, nil when absent.
func (m *Model) find(p string) *node {
	if p == m.root.path {
		return m.root
	}
	n := m.root
	for n != nil && n.path != p {
		n = m.childToward(n, p)
	}
	return n
}

// handleKey drives navigation; only key presses reach it, focus-filtered by
// the pane layer.
func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if ui.ListNav(msg.String(), &m.cursor, len(m.rows), m.bodyHeight(), ui.NavFull) {
		m.clampScroll()
		return nil
	}
	switch msg.String() {
	case "enter", "l", "right":
		return m.activate()
	case "h", "left":
		m.collapseOrParent()
	case ".":
		m.toggleHidden()
	case "r":
		return m.refresh()
	}
	m.clampScroll()
	return nil
}

// activate opens the file under the cursor, or expands/collapses a directory
// (scanning it on first expand).
func (m *Model) activate() tea.Cmd {
	r, ok := m.selected()
	if !ok {
		return nil
	}
	if r.entry.IsDir() {
		r.expanded = !r.expanded
		m.rebuild()
		if r.expanded && !r.loaded {
			return m.scanCmd(r.node)
		}
		return nil
	}
	msg := OpenFileMsg{Key: m.key, Alias: m.alias, Path: r.path, Size: r.entry.Size}
	return func() tea.Msg { return msg }
}

// collapseOrParent collapses an expanded directory, else jumps to the parent
// row — the explorer's left-key feel; never ascends above the root.
func (m *Model) collapseOrParent() {
	r, ok := m.selected()
	if !ok {
		return
	}
	if r.entry.IsDir() && r.expanded {
		r.expanded = false
		m.rebuild()
		return
	}
	m.selectPath(path.Dir(r.path))
}

// toggleHidden flips the dot-entry filter; children are cached on their
// nodes, so this is a pure rebuild, no re-scan — the explorer's semantics.
func (m *Model) toggleHidden() {
	keep := ""
	if r, ok := m.selected(); ok {
		keep = r.path
	}
	m.showHidden = !m.showHidden
	m.rebuild()
	m.selectPath(keep)
}

// refresh re-scans the selected directory (a file's parent), merging in
// place.
func (m *Model) refresh() tea.Cmd {
	r, ok := m.selected()
	if !ok {
		return m.scanCmd(m.root)
	}
	n := r.node
	if !n.entry.IsDir() {
		if parent := m.find(path.Dir(n.path)); parent != nil {
			n = parent
		}
	}
	return m.scanCmd(n)
}

// selected returns the row under the cursor.
func (m *Model) selected() (row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return row{}, false
	}
	return m.rows[m.cursor], true
}

// selectPath puts the cursor on the row showing p, when visible.
func (m *Model) selectPath(p string) {
	for i, r := range m.rows {
		if r.path == p {
			m.cursor = i
			break
		}
	}
	m.clampScroll()
}

// rebuild flattens the tree into the visible rows, keeping the cursor on its
// entry where it survives.
func (m *Model) rebuild() {
	keep := ""
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		keep = m.rows[m.cursor].path
	}
	m.rows = m.rows[:0]
	m.walk(m.root, 0)
	m.cursor = 0
	if keep != "" {
		for i, r := range m.rows {
			if r.path == keep {
				m.cursor = i
				break
			}
		}
	}
	m.clampScroll()
}

// walk appends n's visible children as rows, recursing into expanded
// directories.
func (m *Model) walk(n *node, depth int) {
	for _, c := range n.children {
		if !m.showHidden && strings.HasPrefix(c.name, ".") {
			continue
		}
		m.rows = append(m.rows, row{node: c, depth: depth})
		if c.entry.IsDir() && c.expanded {
			m.walk(c, depth+1)
		}
	}
}

// View renders the header, the scrolled rows, and the key-hint footer.
func (m *Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	pal := m.theme()
	if m.connErr != nil {
		return m.errView(pal)
	}
	if m.connecting || (m.pendingReveal != "" && len(m.rows) == 0) {
		return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Bold(true).Foreground(pal.Foreground).Render(m.alias)+"\n"+
				lipgloss.NewStyle().Faint(true).Render("connecting…"))
	}
	var b strings.Builder
	b.WriteString(m.headerLine(pal))
	b.WriteString("\n")
	b.WriteString(m.renderRows(pal, m.bodyHeight()))
	b.WriteString(m.footer(pal))
	return b.String()
}

// errView is the whole-pane connect failure: the alias, ssh's own message,
// and where the fix lives.
func (m *Model) errView(pal *theme.Palette) string {
	title := lipgloss.NewStyle().Bold(true).Foreground(pal.Foreground).Render(m.alias)
	note := lipgloss.NewStyle().Foreground(pal.Error).Width(m.w - 2).Render(m.connErr.Error())
	hint := lipgloss.NewStyle().Faint(true).Render("fix ~/.ssh/config, keys or agent, then reopen (Remote Host…)")
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, title+"\n"+note+"\n"+hint)
}

// headerLine names the host and the browsed root.
func (m *Model) headerLine(pal *theme.Palette) string {
	title := lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(" " + m.alias)
	return title + lipgloss.NewStyle().Faint(true).Render("   sftp")
}

// renderRows draws the flattened tree scrolled around the cursor.
func (m *Model) renderRows(pal *theme.Palette, height int) string {
	m.clampScroll()
	return ui.RenderWindow(m.top, height, len(m.rows), "", func(i int) string { return m.renderRow(pal, i) })
}

// renderRow draws one entry: caret and name on the left, size and mtime
// right-aligned when the pane is wide enough; a scanning directory shows an
// ellipsis in the size column.
func (m *Model) renderRow(pal *theme.Palette, i int) string {
	r := m.rows[i]
	style := lipgloss.NewStyle().Foreground(pal.Foreground)
	glyph := "  "
	if r.entry.IsDir() {
		glyph = "▾ "
		if !r.expanded {
			glyph = "▸ "
		}
		style = style.Foreground(pal.Accent).Bold(true)
	}
	left := " " + strings.Repeat("  ", r.depth) + glyph + r.name
	if r.entry.IsDir() {
		left += "/"
	}
	line := left
	if meta := m.metaColumns(r); meta != "" {
		pad := m.w - lipgloss.Width(left) - lipgloss.Width(meta) - 1
		if pad >= 1 {
			line = left + strings.Repeat(" ", pad) + meta
		}
	}
	if i == m.cursor {
		if m.focused {
			style = style.Background(pal.Selection).Bold(true)
		} else {
			style = style.Background(pal.SelectionMuted)
		}
	}
	return style.Render(m.clip(line))
}

// metaColumns is the right-hand block: size and mtime for files, the load
// state for directories.
func (m *Model) metaColumns(r row) string {
	if r.entry.IsDir() {
		if r.loading {
			return "…"
		}
		return ""
	}
	stamp := ""
	if !r.entry.ModTime.IsZero() {
		stamp = r.entry.ModTime.Local().Format("2006-01-02 15:04")
	}
	return fmt.Sprintf("%10s  %s", humanSize(r.entry.Size), stamp)
}

// humanSize formats a byte count for the size column.
func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// footer shows the key hints — or the last scan error, which outranks them
// until the next successful scan.
func (m *Model) footer(pal *theme.Palette) string {
	if m.lastErr != nil {
		return lipgloss.NewStyle().Foreground(pal.Error).Render(m.clip(" " + m.lastErr.Error()))
	}
	return lipgloss.NewStyle().Faint(true).Render(m.clip(" enter open (read-only) · h/l fold · . hidden · r refresh · j/k move"))
}

// bodyHeight is the room between the header and footer lines.
func (m *Model) bodyHeight() int {
	h := m.h - 2
	if h < 1 {
		h = 1
	}
	return h
}

// clampScroll keeps the cursor valid and inside the visible window.
func (m *Model) clampScroll() {
	ui.ClampWindow(&m.cursor, &m.top, len(m.rows), m.bodyHeight())
}

// clip bounds one rendered line to the pane width.
func (m *Model) clip(s string) string {
	if m.w > 0 && lipgloss.Width(s) > m.w {
		r := []rune(s)
		if len(r) > m.w {
			return string(r[:m.w-1]) + "…"
		}
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
