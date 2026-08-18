// Package scratchpanel is the Scratch Files tool window (#1932): a slim
// singleton pane, by default a bottom split of the editor, listing the scratch
// store's files newest-first with open and delete actions. It is a pure view
// over internal/scratch — the store stays the single owner of scratch naming,
// location and removal — and it never touches the filesystem itself: the row
// data arrives through an injected lister and the delete leaves as a message
// the root model runs, so the app can close the file's open tabs in the same
// step.
package scratchpanel

import (
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/lang"
	"ike/internal/scratch"
	"ike/internal/theme"
	"ike/internal/ui"
)

// OpenMsg asks the root model to open a scratch through the standard open
// funnel — the same seam scratch.list uses, so the file lands as a focused
// editor tab with highlighting and LSP live.
type OpenMsg struct{ Path string }

// DeleteMsg asks the root model to delete a scratch: it calls scratch.Delete,
// closes the file's open tabs across panes and refreshes the list. The panel
// only ever emits it after its own confirmation step.
type DeleteMsg struct{ Path string }

// Model is the tool window state. Value type with pointer-receiver mutators,
// embedded in a pane.Instance like the Problems panel.
type Model struct {
	width   int
	height  int
	focused bool
	pal     *theme.Palette

	// lister supplies the rows; it defaults to scratch.Entries and is
	// injectable so tests drive the panel without a scratch dir.
	lister func() ([]scratch.Entry, error)

	entries []scratch.Entry
	err     string
	cursor  int
	top     int

	// confirm holds the path of the row awaiting delete confirmation ("" when
	// none): 'd' arms it, y/enter deletes, any other key cancels (#1932).
	confirm string

	// Double-click detection mirrors the Problems panel (#514); now is
	// injectable so tests control the clock.
	lastClickRow int
	lastClickAt  time.Time
	now          func() time.Time
}

// New returns an empty panel listing the real scratch store; Refresh fills it.
func New(pal *theme.Palette) Model {
	return Model{pal: pal, lister: scratch.Entries, lastClickRow: -1, now: time.Now}
}

// SetLister replaces the row source (tests, and any future non-default store).
func (m *Model) SetLister(f func() ([]scratch.Entry, error)) {
	if f == nil {
		return
	}
	m.lister = f
	m.Refresh()
}

// SetSize records the interior content size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetFocused marks the panel focused (selection highlight, key hints).
func (m *Model) SetFocused(f bool) {
	m.focused = f
	if !f {
		m.confirm = "" // an unfocused panel must not keep a live y/n prompt
	}
}

// SetPalette re-threads the active theme.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// Entries reports the listed scratches (tests).
func (m *Model) Entries() []scratch.Entry { return m.entries }

// Cursor reports the selected row index (tests).
func (m *Model) Cursor() int { return m.cursor }

// Confirming reports the path awaiting delete confirmation, "" when none.
func (m *Model) Confirming() string { return m.confirm }

// Selected returns the scratch under the cursor.
func (m *Model) Selected() (scratch.Entry, bool) {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return scratch.Entry{}, false
	}
	return m.entries[m.cursor], true
}

// Refresh re-reads the store, keeping the cursor on the same file where
// possible. The root model calls it whenever a scratch is created or deleted,
// so the list never needs a restart to be current.
func (m *Model) Refresh() {
	keep := ""
	if e, ok := m.Selected(); ok {
		keep = e.Path
	}
	m.entries, m.err = nil, ""
	if m.lister != nil {
		entries, err := m.lister()
		if err != nil {
			m.err = err.Error()
		}
		m.entries = entries
	}
	// A pending confirmation dies with its row: the file may be gone already.
	if m.confirm != "" && !m.has(m.confirm) {
		m.confirm = ""
	}
	m.cursor = 0
	for i, e := range m.entries {
		if e.Path == keep {
			m.cursor = i
			break
		}
	}
	m.clampScroll()
}

// has reports whether path is still listed.
func (m *Model) has(path string) bool {
	for _, e := range m.entries {
		if e.Path == path {
			return true
		}
	}
	return false
}

// Update handles one message while the panel exists; only key presses reach
// it, focus-filtered by the pane layer.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		return m.handleKey(k)
	}
	return nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if m.confirm != "" {
		// The confirmation swallows every key: only y/enter deletes, anything
		// else cancels — an accidental key never removes a file.
		path := m.confirm
		m.confirm = ""
		if key == "y" || key == "Y" || key == "enter" {
			return func() tea.Msg { return DeleteMsg{Path: path} }
		}
		return nil
	}
	// Shared list semantics (#1666): steps wrap, page jumps clamp.
	if ui.ListNav(key, &m.cursor, len(m.entries), m.bodyHeight(), ui.NavFull) {
		m.clampScroll()
		return nil
	}
	switch key {
	case "enter":
		return m.activate(m.cursor)
	case "d", "delete", "backspace":
		if e, ok := m.Selected(); ok {
			m.confirm = e.Path
		}
	case "r":
		m.Refresh()
	}
	m.clampScroll()
	return nil
}

// activate opens the scratch under row i.
func (m *Model) activate(i int) tea.Cmd {
	if i < 0 || i >= len(m.entries) {
		return nil
	}
	path := m.entries[i].Path
	return func() tea.Msg { return OpenMsg{Path: path} }
}

// View renders the scrolled rows plus the key-hint / confirmation footer.
func (m *Model) View() string {
	pal := m.theme()
	return m.renderRows(pal, m.bodyHeight()) + m.footer(pal)
}

// renderRows draws the list scrolled around the cursor, padding to height so
// the footer always sits on the panel's last row.
func (m *Model) renderRows(pal *theme.Palette, height int) string {
	if len(m.entries) == 0 {
		return lipgloss.NewStyle().Faint(true).Render(" "+m.emptyText()) + strings.Repeat("\n", height)
	}
	m.clampScroll()
	base := lipgloss.NewStyle().Foreground(pal.Foreground)
	var b strings.Builder
	for k := 0; k < height; k++ {
		if i := m.top + k; i < len(m.entries) {
			b.WriteString(m.renderRow(pal, base, i))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// emptyText explains an empty list, naming the way out.
func (m *Model) emptyText() string {
	if m.err != "" {
		return "(" + m.err + ")"
	}
	return "(no scratch files — New Scratch File… creates one)"
}

// renderRow draws one scratch: name, language, age. The columns are laid out
// from the panel width so the name gets the room the other two do not need.
func (m *Model) renderRow(pal *theme.Palette, base lipgloss.Style, i int) string {
	e := m.entries[i]
	name := filepath.Base(e.Path)
	kind := KindLabel(e.Path)
	age := ui.RelTime(e.ModTime, m.now())
	line := " " + pad(name, m.nameWidth()) + "  " + pad(kind, kindWidth) + "  " + age
	style := base
	if i == m.cursor {
		if m.focused {
			style = style.Background(pal.Selection).Bold(true)
		} else {
			// Muted cursor row while unfocused (#1034), like the siblings.
			style = style.Background(pal.SelectionMuted)
		}
	}
	return style.Render(m.clip(strings.TrimRight(line, " ")))
}

// kindWidth is the language/extension column's width; "plaintext" is the
// longest built-in label worth aligning for.
const kindWidth = 10

// nameWidth is the file-name column: whatever the fixed columns leave, within
// sane bounds so a narrow pane still shows a readable name.
func (m *Model) nameWidth() int {
	w := m.width - kindWidth - len(" just now") - 6
	if w < 8 {
		w = 8
	}
	if w > 40 {
		w = 40
	}
	return w
}

// KindLabel names a scratch's language, falling back to the bare extension
// for files no language claims. Exported for the app's status line.
func KindLabel(path string) string {
	if l, ok := lang.ByPath(path); ok && l.ID != "" {
		return l.ID
	}
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
}

// pad right-pads (or truncates) s to exactly w cells.
func pad(s string, w int) string {
	r := []rune(s)
	if len(r) > w {
		if w < 2 {
			return string(r[:w])
		}
		return string(r[:w-1]) + "…"
	}
	return s + strings.Repeat(" ", w-len(r))
}

// footer shows the key hints — or, once delete is armed, the confirmation the
// next key answers.
func (m *Model) footer(pal *theme.Palette) string {
	if m.confirm != "" {
		text := " Delete " + filepath.Base(m.confirm) + "?  [y]es  [n]o"
		return lipgloss.NewStyle().Foreground(pal.Error).Bold(true).Render(m.clip(text))
	}
	return lipgloss.NewStyle().Faint(true).Render(m.clip(" enter open · d delete · r refresh · j/k move"))
}

// bodyHeight is the room the rows get: everything but the footer line.
func (m *Model) bodyHeight() int {
	h := m.height - 1
	if h < 1 {
		h = 1
	}
	return h
}

// clampScroll keeps the cursor valid and inside the visible window.
func (m *Model) clampScroll() {
	if m.cursor > len(m.entries)-1 {
		m.cursor = len(m.entries) - 1
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
