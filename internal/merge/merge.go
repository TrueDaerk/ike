// Package merge is the three-way merge view for git-conflicted files
// (#1478): ours (left) and theirs (right) as read-only columns around an
// editable result editor in the middle. The result buffer is seeded with a
// base-aware three-way merge (diff.Merge3 against the :1 stage): regions only
// one side touched resolve automatically, true conflicts remain as diff3
// marker blocks — which the embedded editor's inline conflict machinery
// (#1149) already renders tinted and resolves per block (accept ours /
// theirs / both, next/previous navigation), each as one undo unit. Free
// editing of the result is the full editor. The side columns follow the
// result editor's scroll offset.
package merge

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/diff"
	"ike/internal/editor"
	"ike/internal/textsel"
	"ike/internal/theme"
)

// Model is one live merge view. Like the other pane components it is a value
// type with pointer-receiver mutators; the result editor is heap-held so
// editor state survives the value copies.
type Model struct {
	key  string
	path string
	pal  *theme.Palette

	w, h    int
	focused bool

	ed     *editor.Model // the editable result (middle column)
	ours   []string      // left column, read-only
	theirs []string      // right column, read-only
	total  int           // conflict blocks at load time

	// Mouse text selection over the read-only side columns (#2070); the
	// result editor owns its own. selTheirs pins the selection to one column.
	sel       textsel.Selection
	selTheirs bool
}

// New builds a merge view over path with ed as the result editor.
func New(key, path string, ed *editor.Model, pal *theme.Palette) Model {
	return Model{key: key, path: path, ed: ed, pal: pal}
}

// SetContents seeds the view from the three index stages: the side columns
// show ours/theirs verbatim, the result buffer gets the auto-merged text.
func (m *Model) SetContents(base, ours, theirs string) {
	m.ours = splitDoc(ours)
	m.theirs = splitDoc(theirs)
	m.sel.Clear()
	merged, conflicts := diff.Merge3(base, ours, theirs)
	m.total = conflicts
	m.ed.RestoreText(merged)
}

// splitDoc splits a document into lines, dropping the trailing terminator
// line like the diff engine does.
func splitDoc(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// Editor exposes the result editor (key routing, save, conflict actions).
func (m *Model) Editor() *editor.Model { return m.ed }

// Path returns the conflicted file backing the view.
func (m Model) Path() string { return m.path }

// Unresolved returns the number of conflict blocks still in the result.
func (m Model) Unresolved() int { return m.ed.ConflictCount() }

// Total returns the number of conflict blocks the merge started with.
func (m Model) Total() int { return m.total }

// ConflictIndex returns the 1-based position of the block the result editor's
// caret sits in among the remaining ones (0 outside a block) and how many are
// left (#2258) — the "conflict 2/3" reading of the header and status line.
func (m Model) ConflictIndex() (idx, remaining int) { return m.ed.ConflictIndexAtCursor() }

// MarkerLines counts the conflict-marker lines left in the result, complete
// blocks or not (#2258) — the finish guard's last check before the result is
// written, so a half-edited block cannot reach the file.
func (m Model) MarkerLines() int { return m.ed.ConflictMarkerLines() }

// SetPalette re-threads the theme.
func (m *Model) SetPalette(p *theme.Palette) {
	m.pal = p
	m.ed.SetPalette(p)
}

// SetFocused marks the view (and its result editor) focused.
func (m *Model) SetFocused(f bool) {
	m.focused = f
	m.ed.SetFocused(f)
}

// SetSize lays the three columns out over w×h; the header takes one row.
func (m *Model) SetSize(w, h int) {
	m.w, m.h = w, h
	_, mid, _ := m.colWidths()
	eh := h - 1
	if eh < 1 {
		eh = 1
	}
	m.ed.SetSize(mid, eh)
}

// colWidths splits the width into ours | result | theirs, two separator
// columns wide each.
func (m Model) colWidths() (left, mid, right int) {
	usable := m.w - 2*sepWidth
	if usable < 3 {
		usable = 3
	}
	left = usable / 3
	right = usable / 3
	mid = usable - left - right
	return left, mid, right
}

const sepWidth = 3 // " │ "

// Update forwards a message to the result editor. A live side-column
// selection intercepts the copy chords first (#2070) — the audit rule from
// #2062: the copy must work even though the editor otherwise owns the keys.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		if cmd, consumed := m.selKey(k); consumed {
			return cmd
		}
	}
	ed, cmd := m.ed.Update(msg)
	*m.ed = ed
	return cmd
}

// theme returns the active palette, falling back to the default.
func (m Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

// View renders the header plus the three columns. The side columns follow
// the result editor's vertical scroll offset, so the neighborhood of the
// edited region stays in view on all three panes.
func (m Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	pal := m.theme()
	left, mid, right := m.colWidths()
	rows := m.h - 1
	if rows < 1 {
		rows = 1
	}
	top := m.ed.ScrollTop()

	edLines := strings.Split(m.ed.View(), "\n")
	sepStyle := lipgloss.NewStyle().Foreground(pal.Border)
	sep := sepStyle.Render(" │ ")
	faint := lipgloss.NewStyle().Faint(true)
	selSt := lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText)

	var b strings.Builder
	b.WriteString(m.header(pal, left, mid, right))
	for r := 0; r < rows; r++ {
		b.WriteByte('\n')
		la, lb := m.selCols(top+r, false)
		b.WriteString(sideCell(m.ours, top+r, left, faint, la, lb, selSt))
		b.WriteString(sep)
		mcell := ""
		if r < len(edLines) {
			mcell = edLines[r]
		}
		b.WriteString(padCell(mcell, mid))
		b.WriteString(sep)
		ra, rb := m.selCols(top+r, true)
		b.WriteString(sideCell(m.theirs, top+r, right, faint, ra, rb, selSt))
	}
	return b.String()
}

// header renders the column titles and the conflict counter: how many blocks
// are still unresolved out of the total, prefixed by the caret's position in
// the cycle while it stands in one (#2258), and the finish hint once the last
// block is gone.
func (m Model) header(pal *theme.Palette, left, mid, right int) string {
	title := lipgloss.NewStyle().Bold(true)
	count := " ✓ resolved — apply to finish"
	countStyle := lipgloss.NewStyle().Foreground(pal.Success)
	if n := m.Unresolved(); n > 0 {
		count = " ⚠ "
		if idx, _ := m.ConflictIndex(); idx > 0 {
			count += "conflict " + strconv.Itoa(idx) + "/" + strconv.Itoa(n) + " · "
		}
		count += strconv.Itoa(n) + "/" + strconv.Itoa(m.total) + " unresolved"
		countStyle = lipgloss.NewStyle().Foreground(pal.Warning)
	}
	mtitle := "Result (" + m.pathBase() + ")"
	return padCell(title.Render("Ours"), left) + strings.Repeat(" ", sepWidth) +
		padCell(title.Render(mtitle)+countStyle.Render(count), mid) + strings.Repeat(" ", sepWidth) +
		padCell(title.Render("Theirs"), right)
}

// pathBase is the file name of the merged path.
func (m Model) pathBase() string {
	if i := strings.LastIndexByte(m.path, '/'); i >= 0 {
		return m.path[i+1:]
	}
	return m.path
}

// sideCell renders line i of a read-only column, clipped and padded to w.
// [selA, selB) is the mouse selection's covered display-column interval
// (#2070); covered runes take sel over the column style.
func sideCell(lines []string, i, w int, style lipgloss.Style, selA, selB int, sel lipgloss.Style) string {
	if i < 0 || i >= len(lines) {
		return strings.Repeat(" ", w)
	}
	text := expandTabs(lines[i])
	if selA < selB {
		runes := []rune(text)
		a := min(selA, len(runes))
		b := min(selB, len(runes))
		return padCell(style.Render(string(runes[:a]))+sel.Render(string(runes[a:b]))+
			style.Render(string(runes[b:])), w)
	}
	return padCell(style.Render(text), w)
}

// padCell truncates s to w display cells and pads with spaces.
func padCell(s string, w int) string {
	s = ansi.Truncate(s, w, "")
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// expandTabs widens tabs for the plain side columns.
func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }
