package settings

import (
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/pathcomplete"
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

// PageClicker is an optional PageModel extension (#674): pages implementing
// it receive left presses at page-local coordinates — (0,0) is the top-left
// cell of the area their View renders into.
type PageClicker interface {
	Click(x, y int) tea.Cmd
}

// PageWheeler is an optional PageModel extension (#674): pages implementing
// it receive wheel deltas (negative = up) while the pointer hovers the form
// column.
type PageWheeler interface {
	Wheel(delta int)
}

// column names the focused panel column.
type column int

const (
	catColumn column = iota
	formColumn
)

// row is one visible form line: an entry plus the page it came from (the
// filter flattens entries across pages). Filter results may also be page
// jumps or custom-page items (#886).
type row struct {
	page     int
	entry    Entry
	kind     rowKind
	label    string // display text for rowPage/rowItem
	activate func() // rowItem positioning
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
	edit    textField   // shared cursor input (#888)
	invalid string      // inline validation error for the current edit
	notice  string      // transient info line (clamp feedback, #889)
	suggest pathSuggest // live path completion while editing a Path entry (#541)

	picking bool // enum picker open for the selected row
	pickIdx int  // highlighted option inside the picker

	filtering bool
	filter    string

	// writeScope is the explicit write-target selector (0380, #794): auto
	// follows each entry's conventional Scope, user/project force the layer
	// for every write and reset. Cycled with "s", shown in the hint row.
	writeScope scopeSel

	// stack is the open sub-panel levels (#883), topmost last; see subpanel.go.
	stack []SubPanel

	// Pointer state (#885): the hovered category/form row (-1 = none), the
	// scope chip's title-row span and the hint row's clickable segments (both
	// recomputed at render), and the follow flags — the offsets track the
	// selection only when it moved, so wheel scrolling the viewport away is
	// not snapped back by the next render.
	hoverCat, hoverRow int
	chipSpan           span
	hintHits           []hintAction
	followCat          bool
	followForm         bool
}

// scopeSel names the panel's write-scope selector states.
type scopeSel int

const (
	scopeAuto scopeSel = iota
	scopeUser
	scopeProject
)

// scopeFor resolves the effective write scope for an entry: the selector's
// forced layer, or the entry's conventional Scope on auto.
func (m *Model) scopeFor(e Entry) config.Scope {
	switch m.writeScope {
	case scopeUser:
		return config.UserScope
	case scopeProject:
		return config.ProjectScope
	}
	return e.Scope
}

// scopeLabel names the selector state for the hint row.
func (m *Model) scopeLabel() string {
	switch m.writeScope {
	case scopeUser:
		return "user"
	case scopeProject:
		return "project"
	}
	return "auto"
}

// New builds a closed panel over pages, writing through opts. Custom pages
// implementing the hostAware seam get the panel injected as their
// SubPanelHost (#883), so they can push forms and wizards.
func New(pages []Page, opts config.Options) *Model {
	m := &Model{pages: pages, opts: opts}
	for _, page := range pages {
		if h, ok := page.Custom.(hostAware); ok {
			h.SetSubPanelHost(m)
		}
	}
	return m
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
	m.hoverCat, m.hoverRow = -1, -1
	m.followCat, m.followForm = true, true
	m.editing, m.filtering, m.picking = false, false, false
	m.filter, m.invalid = "", ""
	m.edit = textField{}
	m.stack = nil
}

// Close hides the panel (and any open sub-panels).
func (m *Model) Close() {
	m.open = false
	m.stack = nil
}

// Capturing reports whether the panel currently needs every key verbatim —
// an edit/pick/filter input or a custom page's capture mode — so the host
// must not intercept chrome chords like the resize keys (#774).
func (m *Model) Capturing() bool {
	if top := m.topSub(); top != nil {
		return top.Capturing()
	}
	if m.editing || m.picking || m.filtering {
		return true
	}
	if page := m.customPage(); page != nil {
		return page.Capturing()
	}
	return false
}

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
		// Category titles match as jump rows (#886).
		if strings.Contains(strings.ToLower(p.Title), needle) {
			out = append(out, row{page: pi, kind: rowPage, label: p.Title})
		}
		if p.Custom != nil {
			// Custom pages export their items through the Searchable seam
			// (#886); enter navigates there.
			if sp, ok := p.Custom.(Searchable); ok {
				for _, it := range sp.SearchItems() {
					hay := strings.ToLower(p.Title + " " + it.Label + " " + it.Keywords)
					if strings.Contains(hay, needle) {
						out = append(out, row{page: pi, kind: rowItem, label: p.Title + " › " + it.Label, activate: it.Activate})
					}
				}
			}
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
	if !m.open {
		return nil
	}
	if m.SubOpen() {
		return m.clickSub(x, y)
	}
	const bodyTop = 2 // border row + title row
	if m.picking {
		return m.clickPick(x, y-bodyTop)
	}
	if m.editing {
		return m.clickEdit(x, y-bodyTop)
	}
	if m.filtering {
		// A click ends filter typing (like enter) and then hit-tests normally.
		m.filtering = false
	}
	if cmd, handled := m.clickChrome(x, y); handled {
		return cmd
	}
	row := y - bodyTop
	if row < 0 || row >= m.height-4 {
		return nil
	}
	// Category column. A rail click while filtering clears the filter and
	// jumps to the page (#886 — the rail is never dead).
	if x >= 1 && x < 1+catWidth {
		if idx := row + m.catOff; idx < len(m.pages) {
			m.filter, m.filtering = "", false
			m.cat, m.sel, m.focus = idx, 0, catColumn
			m.followCat, m.followForm = true, true
		}
		return nil
	}
	// Custom pages own their form column: forward the press page-locally
	// through the optional PageClicker seam (#674).
	if page := m.customPage(); page != nil && m.filter == "" {
		if c, ok := page.(PageClicker); ok && x >= 1+catWidth+3 {
			m.focus = formColumn
			return c.Click(x-(1+catWidth+3), row)
		}
		return nil
	}
	if x < 1+catWidth+3 {
		return nil
	}
	// The description sits in a pinned footer (#535, wrapped over
	// detailLines lines #549), so list lines map 1:1 to rows; the footer
	// lines themselves are not clickable.
	if row >= m.height-4-detailLines {
		return nil
	}
	if idx := row + m.formOff; idx < len(m.rows()) {
		if idx == m.sel && m.focus == formColumn {
			return m.activate()
		}
		m.sel, m.focus = idx, formColumn
		m.followForm = true
	}
	return nil
}

// formLine maps a body-local click to a form-column line index (the same
// indexing renderForm uses, so inline expansions like the picker line up).
// ok is false when the click is outside the form list area.
func (m *Model) formLine(x, row int) (int, bool) {
	if x < 1+catWidth+3 || row < 0 || row >= m.height-4-detailLines {
		return 0, false
	}
	return row + m.formOff, true
}

// clickPick handles a press while the enum picker is open: an option chooses
// it (like enter), anything else closes the picker. The options render
// directly under the selected row, which occupies line m.sel — rows above the
// selection are 1:1 with lines, expansions only happen at the selection.
func (m *Model) clickPick(x, row int) tea.Cmd {
	m.picking = false
	r, ok := m.current()
	if !ok || len(r.entry.Options) == 0 {
		return nil
	}
	idx, ok := m.formLine(x, row)
	if !ok {
		return nil
	}
	if opt := idx - m.sel - 1; opt >= 0 && opt < len(r.entry.Options) {
		e := r.entry
		return config.WriteAndReload(m.opts, m.scopeFor(e), e.Key, e.Options[opt])
	}
	return nil
}

// clickEdit handles a press while an inline edit is active: a click on the
// row being edited keeps the edit, a click on a completion suggestion takes
// it (#885), anything else commits the input (falling back to cancel when
// the value does not validate — a click cannot fix it).
func (m *Model) clickEdit(x, row int) tea.Cmd {
	r, ok := m.current()
	if !ok {
		m.editing = false
		return nil
	}
	if idx, hit := m.formLine(x, row); hit && idx == m.sel {
		return nil // stay in the edit
	} else if hit && r.entry.Type == Path {
		// Suggestion lines render directly under the edited row (#885): a
		// press takes the candidate outright (the rendered line only shows
		// the last path component, so index into the candidates).
		if opt := idx - m.sel - 1; opt >= 0 && opt < len(m.suggest.candidates) && opt < maxSuggestLines {
			m.edit.Set(m.suggest.candidates[opt])
			m.suggest.refresh(m.edit.text)
			return nil
		}
	}
	if r.entry.Type == Chord {
		// There is nothing to commit mid-capture; the click cancels.
		m.editing = false
		return nil
	}
	cmd := m.commit(r.entry)
	if m.editing { // validation rejected the input: cancel instead
		m.editing = false
		m.invalid = ""
		m.suggest.clear()
	}
	return cmd
}

// Wheel scrolls the column under the pointer by moving its selection (the
// lists follow their selection, like every other overlay panel): the category
// column when hovered, the form column otherwise. x is panel-local.
func (m *Model) Wheel(x, delta int) {
	if !m.open {
		return
	}
	if m.SubOpen() {
		m.wheelSub(delta)
		return
	}
	if m.editing || m.picking || m.filtering {
		return
	}
	if x >= 1 && x < 1+catWidth && m.filter == "" {
		// Viewport scroll, one category per notch (#885): the wheel browses,
		// it does not yank the selection around.
		step := 1
		if delta < 0 {
			step = -1
		}
		m.catOff = clamp(m.catOff+step, 0, maxOff(len(m.pages), m.height-4))
		return
	}
	// Custom pages own their scrolling: forward through the optional
	// PageWheeler seam (#674).
	if page := m.customPage(); page != nil && m.filter == "" {
		if w, ok := page.(PageWheeler); ok {
			w.Wheel(delta)
		}
		return
	}
	// Schema form: viewport scroll, decoupled from the selection (#885).
	m.formOff = clamp(m.formOff+delta, 0, maxOff(len(m.rows()), m.height-4-detailLines))
}

// maxOff is the largest sensible scroll offset for n lines in an h window.
func maxOff(n, h int) int {
	if h <= 0 || n <= h {
		return 0
	}
	return n - h
}

// CmdReceiver is an optional receiver extension (#884): consumers that need
// to answer a delivered message with a follow-up command (spinner ticks,
// chained fetches) implement it instead of MsgReceiver.
type CmdReceiver interface {
	ReceiveCmd(msg tea.Msg) tea.Cmd
}

// Deliver forwards a non-key message (async probe results) to every custom
// page that consumes messages — and to open sub-panels (#883), so wizard
// steps receive their async results too. Returned commands (CmdReceiver)
// batch back into the app's update.
func (m *Model) Deliver(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	receive := func(v any) {
		switch r := v.(type) {
		case CmdReceiver:
			if c := r.ReceiveCmd(msg); c != nil {
				cmds = append(cmds, c)
			}
		case MsgReceiver:
			r.Receive(msg)
		}
	}
	for _, page := range m.pages {
		receive(page.Custom)
	}
	for _, sp := range m.stack {
		receive(sp)
	}
	return tea.Batch(cmds...)
}

// Update handles one key while the panel is open. Returned commands carry
// write-back reloads.
func (m *Model) Update(key tea.KeyPressMsg) tea.Cmd {
	if !m.open {
		return nil
	}
	if m.SubOpen() {
		return m.updateSub(key)
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
			case "?":
				// The key-help overlay (#887) is panel chrome on every page.
				m.openKeyHelp()
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
	case "pgup", "pgdown", "home", "end":
		m.moveNav(key.String())
	case "space":
		// Space toggles booleans (#887); other rows keep enter semantics.
		if r, ok := m.current(); ok && m.focus == formColumn && r.kind == rowEntry && r.entry.Type == Bool {
			return m.activate()
		}
	case "?":
		m.openKeyHelp()
	case "right", "l":
		if m.focus == catColumn && m.filter == "" {
			m.focus = formColumn
			m.sel = 0
			return nil
		}
		if m.focus == formColumn {
			if cmd := m.stepInt(1); cmd != nil {
				return cmd
			}
			return m.cycleEnum(1) // quick next on an enum row; no-op otherwise
		}
	case "left", "h":
		// On enum/int rows ← is →'s mirror as a value change (#889); on
		// every other row it returns to the category column (#533).
		if m.focus == formColumn && m.filter == "" {
			if cmd := m.stepInt(-1); cmd != nil {
				return cmd
			}
			if r, ok := m.current(); ok && r.kind == rowEntry && r.entry.Type == Enum {
				return m.cycleEnum(-1)
			}
			m.focus = catColumn
		}
	case "+", "=":
		if m.focus == formColumn {
			return m.stepInt(1)
		}
	case "-":
		if m.focus == formColumn {
			return m.stepInt(-1)
		}
	case "enter":
		if m.focus == catColumn && m.filter == "" {
			m.focus = formColumn
			m.sel = 0
			return nil
		}
		return m.activate()
	case "r":
		if r, ok := m.current(); ok && m.focus == formColumn && r.kind == rowEntry {
			return config.RemoveAndReload(m.opts, m.scopeFor(r.entry), r.entry.Key)
		}
	case "s":
		// Cycle the write-scope selector (0380, #794): auto → user → project.
		m.writeScope = (m.writeScope + 1) % 3
	case "/":
		m.filtering = true
		m.focus = formColumn
	}
	return nil
}

// moveNav applies pgup/pgdn/home/end to the focused column (#887).
func (m *Model) moveNav(key string) {
	if m.focus == catColumn && m.filter == "" {
		if listNav(key, &m.cat, len(m.pages), navPage) {
			m.sel = 0
			m.followCat, m.followForm = true, true
		}
		return
	}
	if listNav(key, &m.sel, len(m.rows()), navPage) {
		m.followForm = true
	}
}

// move shifts the focused column's selection.
func (m *Model) move(dir int) {
	m.notice = ""
	if m.focus == catColumn && m.filter == "" {
		m.cat = clamp(m.cat+dir, 0, len(m.pages)-1)
		m.sel = 0
		m.followCat, m.followForm = true, true
		return
	}
	if n := len(m.rows()); n > 0 {
		m.sel = clamp(m.sel+dir, 0, n-1)
		m.followForm = true
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
	if r.kind != rowEntry {
		m.activateResult(r)
		return nil
	}
	e := r.entry
	switch e.Type {
	case Bool:
		next := "true"
		if value(e.Key) == "true" {
			next = "false"
		}
		return config.WriteAndReload(m.opts, m.scopeFor(e), e.Key, next == "true")
	case Enum:
		if len(e.Options) == 0 {
			return nil
		}
		m.picking = true
		m.pickIdx = optionIndex(e, value(e.Key))
		return nil
	case Chord:
		// The shared capture sub-panel (#887): keymap-page semantics —
		// multi-step, enter confirms — instead of grab-the-next-keypress.
		m.Push(newChordCapture(m, m.opts, m.scopeFor(e), e.Key, e.Title, m.pal))
		return nil
	default:
		m.editing = true
		m.invalid = ""
		m.edit = newTextField(value(e.Key))
		if e.Type == Path {
			m.suggest.refresh(m.edit.text)
		}
		return nil
	}
}

// stepInt adjusts the selected Int row by delta (steppers, #889), clamped to
// the entry's range with visible feedback; nil when the selection is not an
// Int row.
func (m *Model) stepInt(delta int) tea.Cmd {
	r, ok := m.current()
	if !ok || r.kind != rowEntry || r.entry.Type != Int {
		return nil
	}
	e := r.entry
	n, err := strconv.Atoi(strings.TrimSpace(value(e.Key)))
	if err != nil {
		n = 0
	}
	next := n + delta
	if e.Min != 0 || e.Max != 0 {
		clamped := clamp(next, e.Min, e.Max)
		if clamped != next {
			m.notice = "clamped to " + strconv.Itoa(clamped)
		}
		next = clamped
	}
	if next == n {
		return nil
	}
	return config.WriteAndReload(m.opts, m.scopeFor(e), e.Key, next)
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
	if !ok || r.kind != rowEntry || r.entry.Type != Enum || len(r.entry.Options) == 0 {
		return nil
	}
	e := r.entry
	n := len(e.Options)
	next := (optionIndex(e, value(e.Key)) + dir + n) % n
	return config.WriteAndReload(m.opts, m.scopeFor(e), e.Key, e.Options[next])
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
		return config.WriteAndReload(m.opts, m.scopeFor(e), e.Key, e.Options[m.pickIdx])
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
	switch key.Code {
	case tea.KeyEscape:
		m.editing = false
		m.invalid = ""
		m.suggest.clear()
		return nil
	case tea.KeyEnter:
		return m.commit(e)
	case tea.KeyTab:
		if e.Type == Path {
			m.edit.Set(m.suggest.complete(m.edit.text))
		}
		return nil
	}
	if _, changed := m.edit.Handle(key); changed && e.Type == Path {
		m.suggest.refresh(m.edit.text)
	}
	return nil
}

// commit validates the inline input and writes it.
func (m *Model) commit(e Entry) tea.Cmd {
	switch e.Type {
	case Int:
		n, err := strconv.Atoi(strings.TrimSpace(m.edit.text))
		if err != nil {
			m.invalid = "not a number"
			return nil
		}
		if e.Min != 0 || e.Max != 0 {
			clamped := clamp(n, e.Min, e.Max)
			if clamped != n {
				// The silent clamp committed a different number than typed
				// (#889) — say so.
				m.notice = "clamped to " + strconv.Itoa(clamped)
			}
			n = clamped
		}
		m.editing = false
		return config.WriteAndReload(m.opts, m.scopeFor(e), e.Key, n)
	case Path:
		p := strings.TrimSpace(m.edit.text)
		if p != "" {
			if _, err := os.Stat(expandHome(p)); err != nil {
				m.invalid = "path does not exist"
				return nil
			}
		}
		m.editing = false
		m.suggest.clear()
		return config.WriteAndReload(m.opts, m.scopeFor(e), e.Key, p)
	default: // String
		m.editing = false
		return config.WriteAndReload(m.opts, m.scopeFor(e), e.Key, m.edit.text)
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

// expandHome resolves a leading ~ against the home directory (delegates to
// the shared helper, #541).
func expandHome(p string) string { return pathcomplete.Expand(p) }

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
