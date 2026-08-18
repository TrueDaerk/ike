package explorer

// scratches.go implements the Scratches section (#1963, replacing the #1932
// tool pane): the scratch store listed as a divider-separated section docked
// at the bottom of the explorer pane. The section shares the explorer's whole
// interaction model — one unified cursor walks tree and section, enter and
// double-click open through the standard funnel, and delete/rename reuse the
// fileops prompt machinery — while the store operations go through the
// internal/scratch boundary guards instead of the project-tree trash. The
// divider is drag-resizable and click-collapsible; both persist via State.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/scratch"
)

// ScratchNewMsg asks the app to create a new scratch (#1963): the explorer's
// new-file affordance on the section delegates to the scratch.new language
// picker. Like FileDeletedMsg it is handled by the app, not the explorer, so
// it deliberately does not implement Msg.
type ScratchNewMsg struct{}

// scratchNewCmd dispatches ScratchNewMsg.
func scratchNewCmd() tea.Msg { return ScratchNewMsg{} }

// defaultScratchHeight is the section's initial body height in rows; the
// scratch.section_height setting and the divider drag both override it.
const defaultScratchHeight = 5

// scratchMinTree is the smallest tree viewport the section may squeeze the
// pane down to; drags and tiny panes clamp against it.
const scratchMinTree = 3

// scratchMinPane is the smallest pane height that still renders the section
// at all: anything shorter leaves no legible room for divider plus rows.
const scratchMinPane = 5

// EnableScratches attaches the scratch store to the explorer (#1963): dir is
// the store directory (joins the auto-refresh poll set; "" skips polling) and
// lister supplies the rows. The store's real Delete/Rename become the default
// operation seams; tests may override them via SetScratchOps.
func (m *Model) EnableScratches(dir string, lister func() ([]scratch.Entry, error)) {
	m.scrDir = dir
	m.scrLister = lister
	if m.scrRemove == nil {
		m.scrRemove = scratch.Delete
	}
	if m.scrRename == nil {
		m.scrRename = scratch.Rename
	}
	m.RefreshScratches()
}

// SetScratchOps overrides the delete/rename store seams (tests).
func (m *Model) SetScratchOps(remove func(string) error, rename func(string, string) (string, error)) {
	if remove != nil {
		m.scrRemove = remove
	}
	if rename != nil {
		m.scrRename = rename
	}
}

// RefreshScratches re-reads the store into the section, keeping the cursor on
// its entry where possible. The app calls it whenever a scratch is created;
// the poll loop calls it when the store dir's mtime moves (#1963).
func (m *Model) RefreshScratches() {
	if m.scrLister == nil {
		return
	}
	keep := ""
	if e, ok := m.scratchSelected(); ok {
		keep = e.Path
	}
	entries, err := m.scrLister()
	m.scrErr = ""
	if err != nil {
		m.scrErr = err.Error()
	}
	m.scrEntries = entries
	m.sortScratches()
	if m.scrDir != "" {
		if fi, statErr := os.Stat(m.scrDir); statErr == nil {
			m.scrDirMod = fi.ModTime()
		}
	}
	if m.inScratch() {
		m.scrCursor = 0
		if len(m.scrEntries) == 0 {
			m.exitScratch() // the section emptied under the cursor
		} else {
			for i, e := range m.scrEntries {
				if e.Path == keep {
					m.scrCursor = i
					break
				}
			}
			m.followScratchCursor()
		}
	}
	m.clampScratchTop()
	// The section's height follows its content, so the tree viewport may have
	// grown or shrunk: re-bound the tree scroll without moving its cursor.
	m.clampOffset()
}

// sortScratches orders the section per scratch.sort: by name (the default,
// matching the tree's ordering) or newest-first by modification time — the
// store's own order, kept reachable through the setting (#1963).
func (m *Model) sortScratches() {
	es := m.scrEntries
	if m.scrSort == "modified" {
		sort.SliceStable(es, func(i, j int) bool {
			if !es[i].ModTime.Equal(es[j].ModTime) {
				return es[i].ModTime.After(es[j].ModTime)
			}
			return es[i].Path < es[j].Path
		})
		return
	}
	sort.SliceStable(es, func(i, j int) bool {
		ni, nj := filepath.Base(es[i].Path), filepath.Base(es[j].Path)
		if ni != nj {
			return ni < nj
		}
		return es[i].Path < es[j].Path
	})
}

// ScratchEntries reports the section's current rows (tests).
func (m Model) ScratchEntries() []scratch.Entry { return m.scrEntries }

// ScratchCursor reports the section-local cursor index, -1 when the unified
// cursor is in the tree (tests).
func (m Model) ScratchCursor() int { return m.scrCursor }

// ScratchCollapsed reports whether the section is folded to its divider.
func (m Model) ScratchCollapsed() bool { return m.scrCollapsed }

// inScratch reports whether the unified cursor sits in the section.
func (m Model) inScratch() bool { return m.scrCursor >= 0 }

// exitScratch returns the unified cursor to the tree.
func (m *Model) exitScratch() { m.scrCursor = -1 }

// scratchSelected returns the section entry under the cursor.
func (m Model) scratchSelected() (scratch.Entry, bool) {
	if m.scrCursor < 0 || m.scrCursor >= len(m.scrEntries) {
		return scratch.Entry{}, false
	}
	return m.scrEntries[m.scrCursor], true
}

// scratchShown reports whether the section renders at all: a store must be
// attached, the scratch.section setting on, and the pane tall enough.
func (m Model) scratchShown() bool {
	return m.scrLister != nil && m.scrEnabled && m.height >= scratchMinPane
}

// scratchSelectable reports whether the unified cursor may enter the section.
func (m Model) scratchSelectable() bool {
	return m.scratchShown() && !m.scrCollapsed && len(m.scrEntries) > 0
}

// scratchBodyRows is the expanded section's row count: the configured (or
// dragged) height, never taller than the content and never squeezing the tree
// below its minimum.
func (m Model) scratchBodyRows() int {
	content := len(m.scrEntries)
	if content == 0 {
		content = 1 // the "(no scratches)" hint row
	}
	h := m.scrHeight
	if h < 1 {
		h = 1
	}
	if h > content {
		h = content
	}
	if lim := m.height - 1 - scratchMinTree; h > lim {
		h = lim
	}
	if h < 1 {
		h = 1
	}
	return h
}

// scratchAreaRows is the total pane rows the section occupies — the divider
// plus, when expanded, the body — and 0 when the section is hidden. viewport
// subtracts it, so the whole tree machinery sees only its own region.
func (m Model) scratchAreaRows() int {
	if !m.scratchShown() {
		return 0
	}
	if m.scrCollapsed {
		return 1
	}
	return 1 + m.scratchBodyRows()
}

// treeAreaRows is the pane row where the tree region ends: the divider's row.
func (m Model) treeAreaRows() int { return m.height - m.scratchAreaRows() }

// selCount is the unified cursor's row space: the tree rows plus, when the
// section is enterable, its entries.
func (m Model) selCount() int {
	n := len(m.rows)
	if m.scratchSelectable() {
		n += len(m.scrEntries)
	}
	return n
}

// vcur is the unified cursor position: tree rows first, section entries after.
func (m Model) vcur() int {
	if m.inScratch() {
		return len(m.rows) + clamp(m.scrCursor, 0, maxz(len(m.scrEntries)-1))
	}
	return m.cursor
}

// setVcur decomposes a unified cursor position back into the tree cursor or
// the section cursor, scrolling whichever region landed the cursor.
func (m *Model) setVcur(i int) {
	if i < len(m.rows) || !m.scratchSelectable() {
		m.exitScratch()
		m.cursor = clamp(i, 0, maxz(len(m.rows)-1))
		m.followCursor()
		return
	}
	m.scrCursor = clamp(i-len(m.rows), 0, len(m.scrEntries)-1)
	m.followScratchCursor()
}

// followScratchCursor pulls the section viewport to its cursor.
func (m *Model) followScratchCursor() {
	body := m.scratchBodyRows()
	if m.scrCursor >= 0 && m.scrTop > m.scrCursor {
		m.scrTop = m.scrCursor
	}
	if m.scrCursor >= m.scrTop+body {
		m.scrTop = m.scrCursor - body + 1
	}
	m.clampScratchTop()
}

// clampScratchTop bounds the section scroll offset to the renderable range.
func (m *Model) clampScratchTop() {
	if maxOff := len(m.scrEntries) - m.scratchBodyRows(); m.scrTop > maxOff {
		m.scrTop = maxOff
	}
	if m.scrTop < 0 {
		m.scrTop = 0
	}
}

// scratchIndexAt resolves a content-local pane row to a visible section entry
// index; ok is false on the divider, the empty-hint row, or past the list.
func (m Model) scratchIndexAt(y int) (int, bool) {
	if !m.scratchShown() || m.scrCollapsed {
		return 0, false
	}
	i := m.scrTop + (y - m.treeAreaRows() - 1)
	if y <= m.treeAreaRows() || i < 0 || i >= len(m.scrEntries) {
		return 0, false
	}
	return i, true
}

// scratchAnchorRow locates a prompt anchor among the visible section rows
// (#1884 anchoring for the section's dialogs); has is false when the path is
// no section entry or scrolled out of its window.
func (m Model) scratchAnchorRow(anchor string) (row int, has bool) {
	if !m.scratchShown() || m.scrCollapsed {
		return 0, false
	}
	for i, e := range m.scrEntries {
		if e.Path != anchor {
			continue
		}
		if i < m.scrTop || i >= m.scrTop+m.scratchBodyRows() {
			return 0, false
		}
		return m.treeAreaRows() + 1 + (i - m.scrTop), true
	}
	return 0, false
}

// scratchClick handles a left press inside the section area (MouseClick's
// section branch): the divider toggles the collapse, a row selects, and a
// second click within the double-click window opens — tree click semantics.
func (m Model) scratchClick(x, y int) (Model, tea.Cmd) {
	if y == m.treeAreaRows() {
		m.ToggleScratchCollapsed()
		m.resetClick()
		return m, nil
	}
	i, ok := m.scratchIndexAt(y)
	if !ok {
		return m, nil
	}
	m.clearSel()
	m.scrCursor = i
	m.followScratchCursor()
	// Row identity for the double-click pairing lives past the tree rows so a
	// tree click and a section click never pair up.
	rowID := len(m.rows) + i
	clickAt := m.now()
	if rowID == m.lastClickRow && clickAt.Sub(m.lastClickAt) <= doubleClickWindow {
		m.resetClick()
		return m.activate()
	}
	m.lastClickRow, m.lastClickAt = rowID, clickAt
	return m, nil
}

// ScratchDividerHit reports whether a content-local press lands on the
// section divider — the app checks it before the row click so a press can
// start a resize drag, mirroring the scrollbar (#1036).
func (m Model) ScratchDividerHit(x, y int) bool {
	return m.scratchShown() && y == m.treeAreaRows() && x >= 0 && x < m.width
}

// ScratchDividerPress arms a divider gesture: until the pointer moves it is a
// click (release toggles the collapse), once it moves it is a resize drag.
func (m *Model) ScratchDividerPress() { m.scrDragMoved = false }

// ScratchDividerDrag follows a divider drag: the divider lands on pane row y,
// so the section body gets the rows below it. Dragging to the bottom edge
// collapses; dragging a collapsed section up re-expands it.
func (m *Model) ScratchDividerDrag(y int) {
	if !m.scratchShown() {
		return
	}
	m.scrDragMoved = true
	body := m.height - y - 1
	if body < 1 {
		if !m.scrCollapsed {
			m.ToggleScratchCollapsed()
		}
		return
	}
	if m.scrCollapsed {
		m.scrCollapsed = false
	}
	m.scrHeight = clamp(body, 1, maxz(m.height-1-scratchMinTree))
	m.followScratchCursor()
	m.clampOffset()
}

// ScratchDividerRelease ends the divider gesture: an unmoved press-release is
// a click, which toggles the collapse.
func (m *Model) ScratchDividerRelease() {
	if !m.scrDragMoved {
		m.ToggleScratchCollapsed()
	}
	m.scrDragMoved = false
}

// ScratchHeight reports the section's configured/dragged body height (persisted
// via State).
func (m Model) ScratchHeight() int { return m.scrHeight }

// FocusScratches puts the unified cursor on the section's first entry,
// re-expanding a collapsed section — the scratch.panel redirect (#1963).
func (m *Model) FocusScratches() {
	if !m.scratchShown() {
		return
	}
	m.scrCollapsed = false
	if len(m.scrEntries) > 0 {
		m.clearSel()
		m.scrCursor = 0
		m.followScratchCursor()
	}
	m.clampOffset()
}

// ToggleScratchCollapsed folds the section to its divider line or re-expands
// it. A collapse with the unified cursor inside returns the cursor to the
// tree — a hidden row must not keep the selection.
func (m *Model) ToggleScratchCollapsed() {
	m.scrCollapsed = !m.scrCollapsed
	if m.scrCollapsed && m.inScratch() {
		m.exitScratch()
	}
	m.clampOffset()
}

// promptScratchDelete opens the explorer's confirm dialog for the selected
// scratch (#1963); the accept removes it through the store's guarded delete.
func (m *Model) promptScratchDelete() {
	e, ok := m.scratchSelected()
	if !ok {
		return
	}
	path := e.Path
	m.prompt = &prompt{
		kind:   promptConfirm,
		title:  fmt.Sprintf("Delete scratch %q?", filepath.Base(path)),
		anchor: path,
		accept: func(mm *Model, _ string) tea.Cmd {
			return mm.deleteScratch(path)
		},
	}
}

// deleteScratch removes one scratch through the store (permanently — the
// scratch dir has no trash) and announces the removal so the app closes the
// file's open tabs, the same plumbing a tree delete uses.
func (m *Model) deleteScratch(path string) tea.Cmd {
	if err := m.scrRemove(path); err != nil {
		m.fail(err)
		m.RefreshScratches()
		return nil
	}
	m.RefreshScratches()
	return deletedCmd(path, false)
}

// promptScratchRename opens the explorer's rename prompt for the selected
// scratch, name stem preselected (#1047); the accept renames through the
// store's boundary-guarded rename.
func (m *Model) promptScratchRename() {
	e, ok := m.scratchSelected()
	if !ok {
		return
	}
	path := e.Path
	name := filepath.Base(path)
	sel := len([]rune(name))
	if stem := strings.TrimSuffix(name, filepath.Ext(name)); stem != "" {
		sel = len([]rune(stem))
	}
	m.prompt = &prompt{
		kind:   promptInput,
		title:  fmt.Sprintf("Rename scratch %q to:", name),
		input:  name,
		pos:    sel,
		selEnd: sel,
		anchor: path,
		accept: func(mm *Model, newName string) tea.Cmd {
			return mm.renameScratch(path, newName)
		},
	}
}

// renameScratch renames one scratch through the store and announces the move
// so open editors re-point instead of closing (#175 plumbing).
func (m *Model) renameScratch(path, name string) tea.Cmd {
	newPath, err := m.scrRename(path, name)
	if err != nil {
		m.fail(err)
		return nil
	}
	m.RefreshScratches()
	m.scratchSnapTo(newPath)
	if newPath == path {
		return nil
	}
	return movedCmd(path, newPath, false)
}

// scratchSnapTo puts the section cursor on the entry with the given path.
func (m *Model) scratchSnapTo(path string) {
	for i, e := range m.scrEntries {
		if e.Path == path {
			m.scrCursor = i
			m.followScratchCursor()
			return
		}
	}
}

// scratchLines renders the section: the divider, then the visible rows (or
// the empty hint). Row styling mirrors the tree's recipes — Selection for the
// focused cursor, SelectionMuted idle, Panel on hover, underline for open
// files, the filetype suffix tint on plain rows.
func (m Model) scratchLines() []string {
	if !m.scratchShown() {
		return nil
	}
	lines := []string{m.scratchDividerLine()}
	if m.scrCollapsed {
		return lines
	}
	body := m.scratchBodyRows()
	if len(m.scrEntries) == 0 {
		hint := "(no scratches)"
		if m.scrErr != "" {
			hint = "(" + m.scrErr + ")"
		}
		empty := lipgloss.NewStyle().Foreground(m.theme().InlayHint).
			Render(ansi.Truncate("  "+hint, maxz(m.width), "…"))
		lines = append(lines, empty)
		for len(lines) < body+1 {
			lines = append(lines, strings.Repeat(" ", maxz(m.width)))
		}
		return lines
	}
	top := clamp(m.scrTop, 0, maxz(len(m.scrEntries)-body))
	for k := 0; k < body; k++ {
		i := top + k
		if i >= len(m.scrEntries) {
			lines = append(lines, strings.Repeat(" ", maxz(m.width)))
			continue
		}
		lines = append(lines, m.scratchRow(i))
	}
	return lines
}

// scratchDividerLine draws the horizontal rule separating tree and section:
// a collapse caret, the section label, and the rule filling the pane width.
func (m Model) scratchDividerLine() string {
	pal := m.theme()
	caret := "▾"
	if m.scrCollapsed {
		caret = "▸"
	}
	label := caret + " Scratches "
	fill := m.width - ansi.StringWidth(label) - 1
	line := lipgloss.NewStyle().Foreground(pal.Foreground).Render(label)
	if fill > 0 {
		line += lipgloss.NewStyle().Foreground(pal.IndentGuide).Render(strings.Repeat("─", fill))
	}
	return ansi.Truncate(line, maxz(m.width), "")
}

// scratchRow renders one section entry with the tree's highlight recipes.
func (m Model) scratchRow(i int) string {
	e := m.scrEntries[i]
	name := filepath.Base(e.Path)
	ss := m.styleSet()
	style := ss.plain
	if isHidden(name) {
		style = style.Italic(true)
	}
	selected := i == m.scrCursor && m.inScratch()
	switch {
	case selected && m.focused:
		style = style.Background(ss.pal.Selection).Bold(true)
	case selected:
		style = style.Background(ss.pal.SelectionMuted)
	case i == m.scrHover:
		style = style.Background(ss.pal.Panel)
	}
	nameStyle := style
	if m.open[e.Path] {
		nameStyle = nameStyle.Underline(true) // open files underline, like the tree
	}
	rendered := nameStyle.Render(name)
	if !selected || !m.focused {
		// The suffix-tint model (#1051) applies to plain section rows too.
		if c := m.colors.suffixColor(&node{name: name, path: e.Path}, m.colorGlobs, m.colorVals); c != nil {
			if dot := strings.LastIndex(name, "."); dot > 0 {
				rendered = nameStyle.Render(name[:dot]) + nameStyle.Foreground(c).Render(name[dot:])
			} else {
				rendered = nameStyle.Foreground(c).Render(name)
			}
		}
	}
	line := style.Render("  ") + rendered
	w := 2 + ansi.StringWidth(name)
	if w > m.width && m.width > 0 {
		return ansi.Cut(line, 0, maxz(m.width-1)) + style.Bold(false).Render("…")
	}
	if pad := m.width - w; pad > 0 {
		line += style.Render(strings.Repeat(" ", pad))
	}
	return line
}
