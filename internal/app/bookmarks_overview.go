package app

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/bookmarks"
	"ike/internal/host"
	"ike/internal/ui"
)

// bookmarks_overview.go is the project-wide bookmarks overview (#2251): a
// floating list of every bookmark in the project, grouped by file, each row
// showing the bookmarked line's text and — where the bookmark carries one —
// its description. Where the palette picker (bookmarks.go) mixes bookmarks
// with the vim marks and ranks rows fuzzily, the overview is the bookmark
// set as a set: file headers, stable (path, line) order, and the three row
// actions the JetBrains tool window has — jump (enter, through the standard
// open funnel so the navigation history records), edit the description
// (ctrl+e, the annotate prompt on that bookmark rather than on the cursor
// line) and delete (delete/ctrl+d). Typing narrows the list through the
// shared speed search (#2111), matching path, line, preview and description
// alike.

// BookmarkOverviewMsg asks the root model to open the bookmarks overview
// (bookmark.overview).
type BookmarkOverviewMsg struct{}

// bmOverviewGroup is one file's block in the overview: the display path plus
// its bookmarks in line order.
type bmOverviewGroup struct {
	disp  string
	items []bookmarks.Bookmark
}

// bmOverview is the open overview. groups/flat are the *filtered* rows of the
// last rebuild — flat is the selection space (headers are never selectable),
// sel indexes it. previews caches the bookmarked line texts so a keystroke
// narrowing the list never re-reads a file.
type bmOverview struct {
	groups   []bmOverviewGroup
	flat     []bookmarks.Bookmark
	sel      int
	top      int
	search   ui.SpeedSearch
	previews map[string]string
}

// bmOverviewRows is the visible row budget of the list body; the window
// scrolls when the project holds more.
const bmOverviewRows = 14

// bookmarkOverviewContent implements ui.Content (not ModelContent) so the
// body learns the shell's content width and rows fill it.
type bookmarkOverviewContent struct{ m Model }

// Title implements ui.Content.
func (c bookmarkOverviewContent) Title() string {
	t := "Bookmarks"
	if ov := c.m.bmOverview; ov != nil {
		if hint := ov.search.Hint(); hint != "" {
			t += "  " + hint
		}
	}
	return t
}

// Render implements ui.Content.
func (c bookmarkOverviewContent) Render(width int) string { return c.m.bookmarkOverviewBody(width) }

// openBookmarkOverview opens the overview, explaining on the notification
// line when the project has no bookmarks yet.
func (m *Model) openBookmarkOverview() {
	if m.bmarks.Count() == 0 {
		m.host.Notify(host.Info, "no bookmarks yet — set one with Toggle Bookmark")
		return
	}
	m.bmOverview = &bmOverview{previews: map[string]string{}}
	m.rebuildBookmarkOverview()
	m.shell.SetContent(bookmarkOverviewContent{m: *m})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// bookmarkOverviewOpen reports whether the shell shows the overview.
func (m Model) bookmarkOverviewOpen() bool { return m.bmOverview != nil && m.shell.IsOpen() }

// closeBookmarkOverview drops the overview state and the shell.
func (m *Model) closeBookmarkOverview() {
	m.bmOverview = nil
	m.shell.Close()
}

// bookmarkPreview returns the bookmarked line's text: from the focused editor
// when the bookmark sits in the open file, from disk otherwise. Results are
// cached for the overview's lifetime — the list rebuilds on every keystroke.
func (m *Model) bookmarkPreview(bm bookmarks.Bookmark) string {
	ov := m.bmOverview
	cache := bm.Path + "\x00" + strconv.Itoa(bm.Line)
	if ov != nil {
		if text, ok := ov.previews[cache]; ok {
			return text
		}
	}
	path := bookmarkPath(bm.Path)
	text := ""
	if ed := m.focusedEditor(); ed != nil && ed.HasFile() && canonicalPath(ed.Path()) == canonicalPath(path) {
		text = ed.LineText(bm.Line)
	} else {
		text = fileLine(path, bm.Line)
	}
	text = markPreview(text)
	if ov != nil {
		ov.previews[cache] = text
	}
	return text
}

// bookmarkOverviewHaystack is the text one row is matched against: its path,
// 1-based line, mnemonic, line preview and description, so the speed search
// finds a bookmark by any of them.
func (m *Model) bookmarkOverviewHaystack(bm bookmarks.Bookmark, disp string) string {
	parts := []string{disp, strconv.Itoa(bm.Line + 1), m.bookmarkPreview(bm), bm.Note}
	if !bm.Anonymous() {
		parts = append(parts, string(bm.Mnemonic))
	}
	return strings.Join(parts, " ")
}

// rebuildBookmarkOverview refreshes the grouped rows from the store through
// the active speed search, keeping the selection on the same bookmark where
// the narrowed list still holds it.
func (m *Model) rebuildBookmarkOverview() {
	ov := m.bmOverview
	if ov == nil {
		return
	}
	var keep bookmarks.Bookmark
	hasKeep := false
	if ov.sel >= 0 && ov.sel < len(ov.flat) {
		keep, hasKeep = ov.flat[ov.sel], true
	}
	ov.groups, ov.flat = nil, nil
	for _, bm := range m.bmarks.All() {
		disp := displayPath(bookmarkPath(bm.Path))
		if !ov.search.Matches(m.bookmarkOverviewHaystack(bm, disp)) {
			continue
		}
		if n := len(ov.groups); n == 0 || ov.groups[n-1].disp != disp {
			ov.groups = append(ov.groups, bmOverviewGroup{disp: disp})
		}
		g := &ov.groups[len(ov.groups)-1]
		g.items = append(g.items, bm)
		ov.flat = append(ov.flat, bm)
	}
	ov.sel = ui.ClampIndex(ov.sel, len(ov.flat))
	if hasKeep {
		for i, bm := range ov.flat {
			if bm.Path == keep.Path && bm.Line == keep.Line {
				ov.sel = i
				break
			}
		}
	}
	ov.top = ui.ScrollToShow(ov.top, ov.sel, bmOverviewRows, len(ov.flat))
}

// bookmarkOverviewBody renders the grouped list: a file header per group and
// one row per bookmark ("12  ⚑3  line text — description"), the selected row
// filled with the theme's selection colors, plus the key legend.
func (m Model) bookmarkOverviewBody(width int) string {
	ov := m.bmOverview
	if ov == nil {
		return ""
	}
	if width < 20 {
		width = 20
	}
	pal := m.pal()
	head := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	dim := lipgloss.NewStyle().Foreground(pal.Hint)
	note := lipgloss.NewStyle().Foreground(pal.Info)
	sel := lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Width(width)

	var b strings.Builder
	if len(ov.flat) == 0 {
		b.WriteString(dim.Render("no bookmark matches the filter") + "\n")
	}
	// The window is a slice of the flat selection space; a header prints
	// whenever its group's first visible bookmark shows.
	idx, shown, printed := 0, 0, map[string]bool{}
	for _, g := range ov.groups {
		for _, bm := range g.items {
			if idx < ov.top || shown >= bmOverviewRows {
				idx++
				continue
			}
			if !printed[g.disp] {
				printed[g.disp] = true
				// A long path is clipped from the left (clipPathLeft, the
				// recovery list's treatment): the tail names the file, the
				// prefix two files share does not.
				b.WriteString(head.Render(clipPathLeft(g.disp, width)) + "\n")
			}
			text := m.bookmarkPreview(bm)
			if text == "" {
				text = "—"
			}
			base := "  " + strconv.Itoa(bm.Line+1) + "  " + bm.Sign() + "  " + text
			desc := ""
			if bm.Note != "" {
				desc = "  — " + bm.Note
			}
			var line string
			if idx == ov.sel {
				// The selection fill spans the row, so its description is
				// part of the filled text rather than separately colored.
				line = sel.Render(ansi.Truncate(base+desc, width, "…"))
			} else {
				line = ansi.Truncate(base, width, "…")
				if room := width - lipgloss.Width(line); desc != "" && room > 3 {
					line += note.Render(ansi.Truncate(desc, room, "…"))
				}
			}
			b.WriteString(line + "\n")
			idx++
			shown++
		}
	}
	if len(ov.flat) > bmOverviewRows {
		b.WriteString(dim.Render(strconv.Itoa(ov.sel+1)+"/"+strconv.Itoa(len(ov.flat))) + "\n")
	}
	legend := "[enter] jump   [ctrl+e] edit description   [del/ctrl+d] delete   [type] filter   [esc] close"
	b.WriteString("\n" + dim.Render(ansi.Truncate(legend, width, "…")))
	return b.String()
}

// updateBookmarkOverview consumes every key while the overview is open: enter
// jumps, ctrl+e edits the description, delete/ctrl+d removes the bookmark,
// the arrows/page keys move, printable keys narrow the list and esc clears a
// running filter before it closes the overview.
func (m Model) updateBookmarkOverview(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ov := m.bmOverview
	if ov == nil {
		return m, nil
	}
	current := func() (bookmarks.Bookmark, bool) {
		if ov.sel < 0 || ov.sel >= len(ov.flat) {
			return bookmarks.Bookmark{}, false
		}
		return ov.flat[ov.sel], true
	}
	switch msg.String() {
	case "esc":
		if ov.search.EscClears() {
			m.rebuildBookmarkOverview()
			return m, nil
		}
		m.closeBookmarkOverview()
		return m, nil
	case "enter":
		bm, ok := current()
		if !ok {
			return m, nil
		}
		m.closeBookmarkOverview()
		return m.openPathAt(bookmarkPath(bm.Path), bm.Line, 0)
	case "ctrl+e":
		bm, ok := current()
		if !ok {
			return m, nil
		}
		m.closeBookmarkOverview()
		m.startBookmarkNotePrompt(bm, true)
		return m, nil
	case "delete", "ctrl+d":
		bm, ok := current()
		if !ok {
			return m, nil
		}
		m.bmarks.Remove(bm.Path, bm.Line)
		m.saveBookmarks()
		if m.bmarks.Count() == 0 {
			m.closeBookmarkOverview()
			m.host.Notify(host.Info, "bookmark removed — none left")
			return m, nil
		}
		m.rebuildBookmarkOverview()
		return m, nil
	}
	if ui.ListNav(msg.String(), &ov.sel, len(ov.flat), bmOverviewRows, ui.NavDefault) {
		ov.top = ui.ScrollToShow(ov.top, ov.sel, bmOverviewRows, len(ov.flat))
		return m, nil
	}
	if _, changed := ov.search.Key(msg); changed {
		m.rebuildBookmarkOverview()
	}
	return m, nil
}

// bookmarkOverviewRowAt maps a rendered body row onto a flat bookmark index
// (#2275). The overview interleaves a file header above each group's first
// visible bookmark and shows only a window of bmOverviewRows rows, so the
// mapping replays bookmarkOverviewBody's own loop rather than doing
// arithmetic; headers, the counter line and the legend report no item.
func (m Model) bookmarkOverviewRowAt(row int) (int, bool) {
	ov := m.bmOverview
	if ov == nil || row < 0 || len(ov.flat) == 0 {
		return 0, false
	}
	line, idx, shown := 0, 0, 0
	printed := map[string]bool{}
	for _, g := range ov.groups {
		for range g.items {
			if idx < ov.top || shown >= bmOverviewRows {
				idx++
				continue
			}
			if !printed[g.disp] {
				printed[g.disp] = true
				if line == row {
					return 0, false // the group's file header
				}
				line++
			}
			if line == row {
				return idx, true
			}
			line++
			idx++
			shown++
		}
	}
	return 0, false
}

// bookmarkOverviewClickRow selects the clicked bookmark; a click on the
// already-selected one jumps to it, the overview's enter (#2275).
func (m Model) bookmarkOverviewClickRow(row int) (tea.Model, tea.Cmd) {
	ov := m.bmOverview
	i, ok := m.bookmarkOverviewRowAt(row)
	if !ok {
		return m, nil
	}
	if i == ov.sel {
		bm := ov.flat[i]
		m.closeBookmarkOverview()
		return m.openPathAt(bookmarkPath(bm.Path), bm.Line, 0)
	}
	ov.sel = i
	ov.top = ui.ScrollToShow(ov.top, ov.sel, bmOverviewRows, len(ov.flat))
	return m, nil
}
