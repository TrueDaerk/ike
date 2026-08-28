// Package archview is the archive viewer pane (#1762): it lists the entries
// of an archive file (tar, tar.gz, tar.bz2 — see internal/archive) as a
// collapsible tree and asks the root model to open the entry under the cursor
// in a read-only buffer. The pane never writes anything: it holds the entry
// headers only, and the bytes of one entry are extracted on demand.
//
// Nothing here is tar-specific — the model speaks internal/archive's Entry —
// so a later zip reader needs no change in the pane or its UI strings.
package archview

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/archive"
	"ike/internal/theme"
	"ike/internal/ui"
)

// OpenEntryMsg asks the root model to open Entry of the archive at Archive in
// a read-only buffer. Emitted by enter on a file row.
type OpenEntryMsg struct {
	Archive string
	Entry   string
}

// ExtractMsg asks the root model to extract members of the archive at Archive
// to a directory on disk (#2249). Members holds the archive-relative names to
// extract — a directory name stands for its whole subtree — and an empty slice
// means the whole archive. The pane never writes anything itself: it names
// what to extract, the root model asks for the target directory and owns the
// safety checks.
type ExtractMsg struct {
	Archive string
	Members []string
}

// row is one rendered line of the flattened tree.
type row struct {
	name  string // display label (the path segment)
	full  string // full entry path inside the archive
	depth int
	isDir bool
	entry archive.Entry // zero for a synthesized directory
	// synth marks a directory the archive never named explicitly; it has no
	// mode or mtime to show.
	synth bool
}

// node is a directory in the tree built from the flat entry list.
type node struct {
	name     string
	full     string
	isDir    bool
	entry    archive.Entry
	synth    bool
	children map[string]*node
}

// Model is one archive viewer pane bound to an archive file.
type Model struct {
	key  string
	path string
	pal  *theme.Palette

	listing archive.Listing
	err     error

	root      *node
	rows      []row
	collapsed map[string]bool
	cursor    int
	top       int

	w, h    int
	focused bool

	// Double-click detection (#1852) mirrors the explorer: activating a row
	// with the mouse needs a second click on it within ui.DoubleClickWindow;
	// now is injectable so tests control the clock.
	clicks ui.ClickTracker
	now    func() time.Time
}

// New opens the archive at path and builds its entry tree. A listing error is
// kept for View — the pane opens either way and explains itself, so a
// truncated or corrupt archive degrades to a notice instead of a crash.
func New(key, p string, pal *theme.Palette) Model {
	m := Model{key: key, path: p, pal: pal, collapsed: map[string]bool{}, now: time.Now}
	m.listing, m.err = archive.List(p)
	m.build()
	return m
}

// Path returns the archive file's path.
func (m *Model) Path() string { return m.path }

// Format reports the detected archive format ("" when the listing failed).
func (m *Model) Format() archive.Format { return m.listing.Format }

// Err returns the listing error, if any.
func (m *Model) Err() error { return m.err }

// Entries reports the number of listed entries (tests).
func (m *Model) Entries() int { return len(m.listing.Entries) }

// Rows reports the number of visible rows (tests).
func (m *Model) Rows() int { return len(m.rows) }

// Cursor reports the selected row index (tests).
func (m *Model) Cursor() int { return m.cursor }

// Top reports the first visible row index (tests).
func (m *Model) Top() int { return m.top }

// RowName returns the full entry path of row i, "" when out of range (tests).
func (m *Model) RowName(i int) string {
	if i < 0 || i >= len(m.rows) {
		return ""
	}
	return m.rows[i].full
}

// SetSize records the pane interior in cells.
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h; m.clampScroll() }

// SetFocused records focus for the header and the selection highlight.
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the theme palette.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// build derives the directory tree and the flattened rows from the listing.
func (m *Model) build() {
	m.root = &node{isDir: true, children: map[string]*node{}}
	for _, e := range m.listing.Entries {
		m.insert(e)
	}
	m.flatten()
}

// insert places one entry in the tree, synthesizing the directories on its
// path that the archive never named (a common tar shape).
func (m *Model) insert(e archive.Entry) {
	segs := strings.Split(e.Name, "/")
	cur := m.root
	for i, seg := range segs {
		last := i == len(segs)-1
		child, ok := cur.children[seg]
		if !ok {
			child = &node{
				name:     seg,
				full:     path.Join(cur.full, seg),
				isDir:    !last || e.IsDir,
				synth:    true,
				children: map[string]*node{},
			}
			cur.children[seg] = child
		}
		if last {
			child.entry, child.synth = e, false
			child.isDir = e.IsDir
		} else {
			child.isDir = true
		}
		cur = child
	}
}

// flatten walks the tree in display order (directories first, then files,
// each alphabetically) into the visible row list, skipping the subtrees of
// collapsed directories.
func (m *Model) flatten() {
	keep := ""
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		keep = m.rows[m.cursor].full
	}
	m.rows = nil
	m.walk(m.root, 0)
	m.cursor = 0
	for i, r := range m.rows {
		if r.full == keep {
			m.cursor = i
			break
		}
	}
	m.clampScroll()
}

// walk appends n's children as rows, recursing into expanded directories.
func (m *Model) walk(n *node, depth int) {
	for _, c := range sortedChildren(n) {
		m.rows = append(m.rows, row{
			name:  c.name,
			full:  c.full,
			depth: depth,
			isDir: c.isDir,
			entry: c.entry,
			synth: c.synth,
		})
		if c.isDir && !m.collapsed[c.full] {
			m.walk(c, depth+1)
		}
	}
}

// sortedChildren orders one level: directories first, then files, each by
// name (case-insensitive, ties broken by the raw name for stability).
func sortedChildren(n *node) []*node {
	out := make([]*node, 0, len(n.children))
	for _, c := range n.children {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.isDir != b.isDir {
			return a.isDir
		}
		la, lb := strings.ToLower(a.name), strings.ToLower(b.name)
		if la != lb {
			return la < lb
		}
		return a.name < b.name
	})
	return out
}

// Update handles one message; only key presses reach it, focus-filtered by
// the pane layer. Mouse input arrives through Wheel and Click instead (#1852),
// which the root model calls with pane-content-local coordinates.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		return m.handleKey(k)
	}
	return nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	// Shared list semantics (#1666): steps wrap, page jumps clamp.
	if ui.ListNav(msg.String(), &m.cursor, len(m.rows), m.bodyHeight(), ui.NavFull) {
		m.clampScroll()
		return nil
	}
	switch msg.String() {
	case "enter", "l", "right":
		return m.activate()
	case "h", "left":
		m.collapseOrParent()
	case " ", "space":
		m.toggleDir()
	case "e":
		return m.extractSelected()
	case "E":
		return m.extractAll()
	}
	m.clampScroll()
	return nil
}

// activate opens the file under the cursor, or expands/collapses a directory.
func (m *Model) activate() tea.Cmd {
	r, ok := m.selected()
	if !ok {
		return nil
	}
	if r.isDir {
		m.toggleDir()
		return nil
	}
	msg := OpenEntryMsg{Archive: m.path, Entry: r.full}
	return func() tea.Msg { return msg }
}

// extractSelected asks for the row under the cursor to be written to disk: one
// file, or — on a directory row — that directory's whole subtree.
func (m *Model) extractSelected() tea.Cmd {
	r, ok := m.selected()
	if !ok {
		return nil
	}
	msg := ExtractMsg{Archive: m.path, Members: []string{r.full}}
	return func() tea.Msg { return msg }
}

// extractAll asks for every member of the archive to be written to disk.
func (m *Model) extractAll() tea.Cmd {
	msg := ExtractMsg{Archive: m.path}
	return func() tea.Msg { return msg }
}

// toggleDir flips the collapsed state of the directory under the cursor.
func (m *Model) toggleDir() {
	r, ok := m.selected()
	if !ok || !r.isDir {
		return
	}
	m.collapsed[r.full] = !m.collapsed[r.full]
	m.flatten()
}

// collapseOrParent collapses an expanded directory, else jumps to the parent
// row — the explorer's left-key feel.
func (m *Model) collapseOrParent() {
	r, ok := m.selected()
	if !ok {
		return
	}
	if r.isDir && !m.collapsed[r.full] {
		m.collapsed[r.full] = true
		m.flatten()
		return
	}
	parent := path.Dir(r.full)
	if parent == "." || parent == "/" {
		return
	}
	for i, cand := range m.rows {
		if cand.full == parent {
			m.cursor = i
			m.clampScroll()
			return
		}
	}
}

// selected returns the row under the cursor.
func (m *Model) selected() (row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return row{}, false
	}
	return m.rows[m.cursor], true
}

// SelectedEntry returns the full entry path under the cursor (tests, mouse).
func (m *Model) SelectedEntry() (string, bool) {
	r, ok := m.selected()
	if !ok || r.isDir {
		return "", false
	}
	return r.full, true
}

// SelectedMember returns the full entry path under the cursor — a directory
// included, which stands for its subtree when extracting (#2249).
func (m *Model) SelectedMember() (string, bool) {
	r, ok := m.selected()
	if !ok {
		return "", false
	}
	return r.full, true
}

// View renders the header, the scrolled rows, and the key-hint footer.
func (m *Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	pal := m.theme()
	if len(m.rows) == 0 {
		return m.emptyView(pal)
	}
	var b strings.Builder
	b.WriteString(m.headerLine(pal))
	b.WriteString("\n")
	b.WriteString(m.renderRows(pal, m.bodyHeight()))
	b.WriteString(m.footer(pal))
	return b.String()
}

// emptyView is the whole-pane notice for an archive that listed nothing: the
// read error for a corrupt or truncated file, "empty archive" otherwise.
func (m *Model) emptyView(pal *theme.Palette) string {
	title := lipgloss.NewStyle().Bold(true).Foreground(pal.Foreground).Render(path.Base(m.path))
	var note string
	if m.err != nil {
		note = lipgloss.NewStyle().Foreground(pal.Error).Render("cannot read archive: " + m.err.Error())
	} else {
		note = lipgloss.NewStyle().Faint(true).Render("empty archive")
	}
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, title+"\n"+note)
}

// headerLine names the archive, its format and its entry count, plus the read
// error when a partial listing survived one.
func (m *Model) headerLine(pal *theme.Palette) string {
	n := len(m.listing.Entries)
	counts := fmt.Sprintf("%d entr%s", n, plural(n))
	if m.listing.Truncated {
		counts += " (truncated)"
	}
	if f := m.listing.Format.String(); f != "" {
		counts = f + " · " + counts
	}
	title := lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(" " + path.Base(m.path))
	line := title + lipgloss.NewStyle().Faint(true).Render("   "+counts)
	if m.err != nil {
		line += lipgloss.NewStyle().Foreground(pal.Error).Render("   " + m.err.Error())
	}
	return line
}

// plural is the entry-count suffix.
func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// renderRows draws the flattened tree scrolled around the cursor.
func (m *Model) renderRows(pal *theme.Palette, height int) string {
	m.clampScroll()
	var b strings.Builder
	for k := 0; k < height; k++ {
		if i := m.top + k; i < len(m.rows) {
			b.WriteString(m.renderRow(pal, i))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderRow draws one entry: the tree glyph and name on the left, the meta
// columns (size, mode, mtime) right-aligned when the pane is wide enough.
func (m *Model) renderRow(pal *theme.Palette, i int) string {
	r := m.rows[i]
	style := lipgloss.NewStyle().Foreground(pal.Foreground)
	glyph := "  "
	if r.isDir {
		glyph = "▾ "
		if m.collapsed[r.full] {
			glyph = "▸ "
		}
		style = style.Foreground(pal.Accent).Bold(true)
	}
	left := " " + strings.Repeat("  ", r.depth) + glyph + r.name
	if r.isDir {
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

// metaColumns is the right-hand block: size (or link target), mode and mtime.
// A synthesized directory has no header of its own, so it shows nothing.
func (m *Model) metaColumns(r row) string {
	if r.synth {
		return ""
	}
	size := HumanSize(r.entry.Size)
	if r.isDir {
		size = ""
	}
	if r.entry.Link != "" {
		size = "→ " + r.entry.Link
	}
	return fmt.Sprintf("%10s  %-10s  %s", size, r.entry.Mode.String(), stamp(r.entry.ModTime))
}

// stamp formats an entry's modification time; a zero time renders blank.
func stamp(t time.Time) string {
	if t.IsZero() {
		return strings.Repeat(" ", len("2006-01-02 15:04"))
	}
	return t.Local().Format("2006-01-02 15:04")
}

// HumanSize formats a byte count for the size column.
func HumanSize(n int64) string {
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

// footer shows the key hints.
func (m *Model) footer(pal *theme.Palette) string {
	return lipgloss.NewStyle().Faint(true).Render(m.clip(" enter open (read-only) · e/E extract entry/all · space fold · h/l collapse/expand · j/k move"))
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
	if m.cursor > len(m.rows)-1 {
		m.cursor = len(m.rows) - 1
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
