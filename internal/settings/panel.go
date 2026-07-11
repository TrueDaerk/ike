package settings

import (
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/theme"
)

// PageModel is a self-rendered settings page. The panel hosts it on the form
// side and forwards keys while it is focused; tab and esc stay with the panel
// unless the page is capturing raw input (a chord rebind).
type PageModel interface {
	Update(key tea.KeyPressMsg) tea.Cmd
	View(width, height int) string
	SetPalette(p *theme.Palette)
	// Capturing reports the page wants every key verbatim (no panel chrome
	// keys) — chord capture must be able to record esc/tab too.
	Capturing() bool
}

// column names the focused panel column.
type column int

const (
	catColumn column = iota
	formColumn
)

// row is one visible form line: an entry plus the page it came from (the
// filter flattens entries across pages).
type row struct {
	page  int
	entry Entry
}

// Model is the settings panel state. Values are never cached: every render
// reads the live config (config.Get().Flat()), and every edit goes through the
// write-back layer and the reload pipeline.
type Model struct {
	pages []Page
	opts  config.Options
	pal   *theme.Palette

	width, height int
	open          bool
	focus         column
	cat           int
	sel           int // index into rows()

	catOff  int // scroll offset of the category column
	formOff int // scroll offset (in rendered lines) of the form column

	editing bool
	input   string
	invalid string // inline validation error for the current edit

	picking bool // enum picker open for the selected row
	pickIdx int  // highlighted option inside the picker

	filtering bool
	filter    string
}

// New builds a closed panel over pages, writing through opts.
func New(pages []Page, opts config.Options) *Model {
	return &Model{pages: pages, opts: opts}
}

// SetPalette threads the active theme palette (into custom pages too).
func (m *Model) SetPalette(p *theme.Palette) {
	m.pal = p
	for _, page := range m.pages {
		if page.Custom != nil {
			page.Custom.SetPalette(p)
		}
	}
}

// SetSize sets the full-window render size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// IsOpen reports whether the panel is visible (it owns the keyboard then).
func (m *Model) IsOpen() bool { return m.open }

// Open shows the panel on its first page.
func (m *Model) Open() {
	m.open = true
	m.focus = catColumn
	m.cat, m.sel = 0, 0
	m.catOff, m.formOff = 0, 0
	m.editing, m.filtering, m.picking = false, false, false
	m.filter, m.input, m.invalid = "", "", ""
}

// Close hides the panel.
func (m *Model) Close() { m.open = false }

// customPage returns the active page's PageModel, if it has one.
func (m *Model) customPage() PageModel {
	if m.cat >= 0 && m.cat < len(m.pages) {
		return m.pages[m.cat].Custom
	}
	return nil
}

// rows returns the visible form lines: the active page's entries, or — with a
// filter — every matching entry across all pages. Custom pages own their rows
// and are skipped here.
func (m *Model) rows() []row {
	var out []row
	if m.filter == "" {
		if m.cat >= 0 && m.cat < len(m.pages) {
			for _, e := range m.pages[m.cat].Entries {
				out = append(out, row{page: m.cat, entry: e})
			}
		}
		return out
	}
	needle := strings.ToLower(m.filter)
	for pi, p := range m.pages {
		if p.Custom != nil {
			continue
		}
		for _, e := range p.Entries {
			hay := strings.ToLower(p.Title + " " + e.Title + " " + e.Key)
			if strings.Contains(hay, needle) {
				out = append(out, row{page: pi, entry: e})
			}
		}
	}
	return out
}

// current returns the selected row, if any.
func (m *Model) current() (row, bool) {
	rows := m.rows()
	if m.sel < 0 || m.sel >= len(rows) {
		return row{}, false
	}
	return rows[m.sel], true
}

// value reads an entry's effective value from the live config.
func value(key string) string {
	return config.Get().Flat()[key]
}

// Click handles a mouse press at panel-local coordinates (0,0 = the box's
// top-left border cell, #127): a category row selects that page; a form row
// selects its entry, and a press on the already-selected entry activates it —
// the same semantics as enter.
func (m *Model) Click(x, y int) tea.Cmd {
	if !m.open || m.editing || m.filtering || m.picking {
		return nil
	}
	const bodyTop = 2 // border row + title row
	row := y - bodyTop
	if row < 0 || row >= m.height-4 {
		return nil
	}
	// Category column.
	if x >= 1 && x < 1+catWidth && m.filter == "" {
		if idx := row + m.catOff; idx < len(m.pages) {
			m.cat, m.sel, m.focus = idx, 0, catColumn
		}
		return nil
	}
	// Form column (schema-driven pages only; custom pages stay keyboard-driven).
	if m.customPage() != nil && m.filter == "" {
		return nil
	}
	if x < 1+catWidth+3 {
		return nil
	}
	// Recreate renderForm's row layout: the selected entry carries one extra
	// detail line that shifts everything below it.
	rows := m.rows()
	target := row + m.formOff
	line := 0
	for i := range rows {
		if line == target {
			if i == m.sel && m.focus == formColumn {
				return m.activate()
			}
			m.sel, m.focus = i, formColumn
			return nil
		}
		line++
		if i == m.sel {
			line++ // the detail line under the selection
		}
	}
	return nil
}

// Deliver forwards a non-key message (async probe results) to every custom
// page that consumes messages.
func (m *Model) Deliver(msg tea.Msg) {
	for _, page := range m.pages {
		if r, ok := page.Custom.(MsgReceiver); ok {
			r.Receive(msg)
		}
	}
}

// Update handles one key while the panel is open. Returned commands carry
// write-back reloads.
func (m *Model) Update(key tea.KeyPressMsg) tea.Cmd {
	if !m.open {
		return nil
	}
	if m.editing {
		return m.updateEdit(key)
	}
	if m.picking {
		return m.updatePick(key)
	}
	if m.filtering {
		return m.updateFilter(key)
	}
	// A custom page in capture mode gets every key verbatim; otherwise it gets
	// everything but the panel's own chrome keys (tab / esc / arrow-left back
	// to the categories — plain "h" stays with the page, it may be filter text).
	if page := m.customPage(); page != nil && m.filter == "" {
		if page.Capturing() {
			return page.Update(key)
		}
		if m.focus == formColumn {
			switch key.String() {
			case "tab", "left":
				m.focus = catColumn
				return nil
			case "esc":
				m.Close()
				return nil
			}
			return page.Update(key)
		}
	}
	switch key.String() {
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.sel = 0
			return nil
		}
		m.Close()
	case "tab":
		if m.focus == catColumn {
			m.focus = formColumn
		} else {
			m.focus = catColumn
		}
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "right", "l":
		if m.focus == catColumn && m.filter == "" {
			m.focus = formColumn
			m.sel = 0
			return nil
		}
		if m.focus == formColumn {
			return m.cycleEnum(1) // quick next on an enum row; no-op otherwise
		}
	case "left", "h":
		if m.focus != formColumn {
			return nil
		}
		// Arrow-left on an enum row is the quick prev-cycle; "h" (and left on
		// any other row) returns to the category column.
		if key.String() == "left" {
			if cmd := m.cycleEnum(-1); cmd != nil {
				return cmd
			}
		}
		if m.filter == "" {
			m.focus = catColumn
		}
	case "enter":
		if m.focus == catColumn && m.filter == "" {
			m.focus = formColumn
			m.sel = 0
			return nil
		}
		return m.activate()
	case "r":
		if r, ok := m.current(); ok && m.focus == formColumn {
			return config.RemoveAndReload(m.opts, r.entry.Scope, r.entry.Key)
		}
	case "/":
		m.filtering = true
		m.focus = formColumn
	}
	return nil
}

// move shifts the focused column's selection.
func (m *Model) move(dir int) {
	if m.focus == catColumn && m.filter == "" {
		m.cat = clamp(m.cat+dir, 0, len(m.pages)-1)
		m.sel = 0
		return
	}
	if n := len(m.rows()); n > 0 {
		m.sel = clamp(m.sel+dir, 0, n-1)
	}
}

// activate begins editing the selected entry — Bool and Enum apply
// immediately, the text-shaped types open an inline input, Chord captures the
// next key.
func (m *Model) activate() tea.Cmd {
	r, ok := m.current()
	if !ok {
		return nil
	}
	e := r.entry
	switch e.Type {
	case Bool:
		next := "true"
		if value(e.Key) == "true" {
			next = "false"
		}
		return config.WriteAndReload(m.opts, e.Scope, e.Key, next == "true")
	case Enum:
		if len(e.Options) == 0 {
			return nil
		}
		m.picking = true
		m.pickIdx = optionIndex(e, value(e.Key))
		return nil
	default:
		m.editing = true
		m.invalid = ""
		m.input = value(e.Key)
		if e.Type == Chord {
			m.input = ""
		}
		return nil
	}
}

// optionIndex returns the position of val in e.Options (0 when absent).
func optionIndex(e Entry, val string) int {
	for i, o := range e.Options {
		if o == val {
			return i
		}
	}
	return 0
}

// cycleEnum writes the selected enum row's next (dir=1) or previous (dir=-1)
// option; nil when the selection is not an enum.
func (m *Model) cycleEnum(dir int) tea.Cmd {
	r, ok := m.current()
	if !ok || r.entry.Type != Enum || len(r.entry.Options) == 0 {
		return nil
	}
	e := r.entry
	n := len(e.Options)
	next := (optionIndex(e, value(e.Key)) + dir + n) % n
	return config.WriteAndReload(m.opts, e.Scope, e.Key, e.Options[next])
}

// updatePick handles keys while the enum picker is open.
func (m *Model) updatePick(key tea.KeyPressMsg) tea.Cmd {
	r, ok := m.current()
	if !ok || len(r.entry.Options) == 0 {
		m.picking = false
		return nil
	}
	e := r.entry
	switch key.String() {
	case "esc":
		m.picking = false
	case "up", "k":
		m.pickIdx = clamp(m.pickIdx-1, 0, len(e.Options)-1)
	case "down", "j":
		m.pickIdx = clamp(m.pickIdx+1, 0, len(e.Options)-1)
	case "enter":
		m.picking = false
		return config.WriteAndReload(m.opts, e.Scope, e.Key, e.Options[m.pickIdx])
	}
	return nil
}

// updateEdit handles keys during an inline edit.
func (m *Model) updateEdit(key tea.KeyPressMsg) tea.Cmd {
	r, ok := m.current()
	if !ok {
		m.editing = false
		return nil
	}
	e := r.entry
	if e.Type == Chord {
		// The next chord-shaped press is the value; esc cancels.
		if key.Code == tea.KeyEscape {
			m.editing = false
			return nil
		}
		m.editing = false
		return config.WriteAndReload(m.opts, e.Scope, e.Key, key.String())
	}
	switch key.Code {
	case tea.KeyEscape:
		m.editing = false
		m.invalid = ""
	case tea.KeyEnter:
		return m.commit(e)
	case tea.KeyBackspace:
		if m.input != "" {
			m.input = m.input[:len(m.input)-1]
		}
	default:
		if key.Text != "" {
			m.input += key.Text
		}
	}
	return nil
}

// commit validates the inline input and writes it.
func (m *Model) commit(e Entry) tea.Cmd {
	switch e.Type {
	case Int:
		n, err := strconv.Atoi(strings.TrimSpace(m.input))
		if err != nil {
			m.invalid = "not a number"
			return nil
		}
		if e.Min != 0 || e.Max != 0 {
			n = clamp(n, e.Min, e.Max)
		}
		m.editing = false
		return config.WriteAndReload(m.opts, e.Scope, e.Key, n)
	case Path:
		p := strings.TrimSpace(m.input)
		if p != "" {
			if _, err := os.Stat(expandHome(p)); err != nil {
				m.invalid = "path does not exist"
				return nil
			}
		}
		m.editing = false
		return config.WriteAndReload(m.opts, e.Scope, e.Key, p)
	default: // String
		m.editing = false
		return config.WriteAndReload(m.opts, e.Scope, e.Key, m.input)
	}
}

// updateFilter handles keys while the filter input is active.
func (m *Model) updateFilter(key tea.KeyPressMsg) tea.Cmd {
	switch key.Code {
	case tea.KeyEscape:
		m.filtering = false
		m.filter = ""
		m.sel = 0
	case tea.KeyEnter:
		m.filtering = false
	case tea.KeyBackspace:
		if m.filter != "" {
			m.filter = m.filter[:len(m.filter)-1]
		}
	default:
		if key.Text != "" {
			m.filter += key.Text
			m.sel = 0
		}
	}
	return nil
}

// expandHome resolves a leading ~/ against the home directory.
func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
