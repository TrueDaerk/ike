package settings

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/pathcomplete"
	"ike/internal/theme"
	"ike/internal/ui"
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
	detailColumn
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

	// editor is the detail column's typed editor for the selected entry
	// (0460, #1295), rebuilt whenever the selection moves to another key; it
	// owns the input state that used to live on the panel.
	editor    Editor
	editorKey string

	// changes is the staged-apply buffer (0460, #1296), in edit order: schema
	// edits collect here and reach disk only when the batch is applied.
	changes []change

	notice   string // transient info line (clamp feedback #889, saved flash #891)
	writeErr string // inline write/reload error (#891), cleared on the next action

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
	// hitSel is the nav column's selection while a filter is active (#1297):
	// an index into hitPages, not into m.pages.
	hitSel    int
	chipSpan  span
	countSpan span
	hintHits  []hintAction
	// detailBodyTop is the editor body's first line inside the detail column,
	// recorded at render (#1325): the head above it — title, wrapped
	// description, meta, rule — has no fixed height, so a click can only be
	// mapped onto an editor row with what the last frame actually drew.
	detailBodyTop int
	followCat     bool
	followForm    bool
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
// SubPanelHost (#883), so they can push forms and wizards. The last-visited
// page is restored from the per-project state file (#890).
func New(pages []Page, opts config.Options) *Model {
	m := &Model{pages: pages, opts: opts, hoverCat: -1, hoverRow: -1}
	for _, page := range pages {
		if h, ok := page.Custom.(hostAware); ok {
			h.SetSubPanelHost(m)
		}
	}
	if title := loadLastPage(); title != "" {
		for i, p := range pages {
			if p.Title == title {
				m.cat = i
			}
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

// Size returns the panel's rendered outer dimensions — the box View draws at
// exactly width × height — without rendering it (#1396): the app's mouse
// dispatch needs the geometry on every motion/wheel event for hit-testing,
// and measuring View() there made each pointer move pay a full panel render.
// (0, 0) when the panel is closed or too small to render, mirroring View.
func (m *Model) Size() (w, h int) {
	if !m.open || m.width < 24 || m.height < 8 {
		return 0, 0
	}
	return m.width, m.height
}

// IsOpen reports whether the panel is visible (it owns the keyboard then).
func (m *Model) IsOpen() bool { return m.open }

// Open shows the panel on the page it was last on (#890 — the panel
// remembers across opens and, via the state file, across sessions).
func (m *Model) Open() {
	m.open = true
	m.focus = catColumn
	m.cat = clamp(m.cat, 0, len(m.pages)-1)
	m.sel = 0
	m.catOff, m.formOff = 0, 0
	m.hoverCat, m.hoverRow = -1, -1
	m.followCat, m.followForm = true, true
	m.filtering = false
	m.filter, m.notice, m.writeErr = "", "", ""
	m.editor, m.editorKey = nil, ""
	m.stack = nil
}

// Close hides the panel (and any open sub-panels), remembering the page.
func (m *Model) Close() {
	m.open = false
	m.stack = nil
	if m.cat >= 0 && m.cat < len(m.pages) {
		saveLastPage(m.pages[m.cat].Title)
	}
}

// Capturing reports whether the panel currently needs every key verbatim —
// an edit/pick/filter input or a custom page's capture mode — so the host
// must not intercept chrome chords like the resize keys (#774).
func (m *Model) Capturing() bool {
	if top := m.topSub(); top != nil {
		return top.Capturing()
	}
	if m.filtering {
		return true
	}
	if m.focus == detailColumn && m.editor != nil && m.editor.Capturing() {
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

// syncEditor keeps the detail column's editor in step with the selection
// (#1295): moving to another key rebuilds it, so the third column always shows
// the editor for what is highlighted — and staying on a key keeps the input
// state the user was typing into.
func (m *Model) syncEditor() {
	r, ok := m.current()
	if !ok || r.kind != rowEntry {
		m.editor, m.editorKey = nil, ""
		return
	}
	if m.editor == nil || m.editorKey != r.entry.Key {
		m.editor, m.editorKey = newEditor(m, r.entry), r.entry.Key
	}
}

// formX is the settings column's first panel-local x.
func formX() int { return 1 + catWidth + sepWidth }

// detailX is the detail column's first panel-local x in the three-column grid.
func (m *Model) detailX() int { return formX() + m.gridFor().formW + sepWidth }

// Click handles a mouse press at panel-local coordinates (0,0 = the box's
// top-left border cell, #127): a category row selects that page; a settings
// row selects its entry, and a press on the already-selected entry activates
// it — the same semantics as enter. A press in the detail column focuses the
// editor there.
func (m *Model) Click(x, y int) tea.Cmd {
	if !m.open {
		return nil
	}
	if m.SubOpen() {
		return m.clickSub(x, y)
	}
	if m.filtering {
		// A click ends filter typing (like enter) and then hit-tests normally.
		m.filtering = false
	}
	if cmd, handled := m.clickChrome(x, y); handled {
		return cmd
	}
	row := y - bodyTop
	if row < 0 || row >= m.height-chromeRows {
		return nil
	}
	// Category column. A rail click while filtering clears the filter and
	// jumps to the page (#886 — the rail is never dead); section headers
	// (#890) are not click targets.
	if x >= 1 && x < 1+catWidth {
		if m.filter != "" {
			// Search mode: the rail lists the pages with hits (#1297), and a
			// press jumps the match list to that page's first match. The
			// header row is not a target.
			if idx := row - 1 + m.catOff; idx >= 0 && idx < len(m.hitPages()) {
				m.focus = catColumn
				m.jumpToHitPage(idx)
			}
			return nil
		}
		rows := m.railRows()
		if idx := row + m.catOff; idx >= 0 && idx < len(rows) && rows[idx].header == "" {
			m.cat, m.sel, m.focus = rows[idx].page, 0, catColumn
			m.followCat, m.followForm = true, true
		}
		return nil
	}
	// Custom pages own everything right of the rail: forward the press
	// page-locally through the optional PageClicker seam (#674).
	if page := m.customPage(); page != nil && m.filter == "" {
		if c, ok := page.(PageClicker); ok && x >= formX() {
			m.focus = formColumn
			return c.Click(x-formX(), row)
		}
		return nil
	}
	if x < formX() {
		return nil
	}
	g := m.gridFor()
	// Detail column (or, on a narrow panel, the detail band under the list):
	// a press focuses the editor and, on the editor's own rows, operates the
	// control under the pointer (#1325).
	if (g.side && x >= m.detailX()) || (!g.side && row > g.listH) {
		return m.clickDetail(x, row, g)
	}
	if !g.side && row == g.listH {
		return nil // the divider row
	}
	if row >= g.listH {
		return nil
	}
	// Rows map 1:1 to lines now that nothing expands inline (#1295).
	if idx := row + m.formOff; idx < len(m.rows()) {
		if idx == m.sel && m.focus == formColumn {
			return m.activate()
		}
		m.sel, m.focus = idx, formColumn
		m.followForm = true
		m.syncEditor()
	}
	return nil
}

// Wheel scrolls the column under the pointer by moving its selection (the
// lists follow their selection, like every other overlay panel): the category
// column when hovered, the form column otherwise. x/y are panel-local; y
// picks the stacked detail band apart from the list above it (#1664).
func (m *Model) Wheel(x, y, delta int) {
	if !m.open {
		return
	}
	if m.SubOpen() {
		m.wheelSub(delta)
		return
	}
	if m.filtering {
		return
	}
	if x >= 1 && x < 1+catWidth {
		// Viewport scroll, one row per notch (#885): the wheel browses, it
		// does not yank the selection around.
		step := 1
		if delta < 0 {
			step = -1
		}
		n := len(m.railRows())
		if m.filter != "" {
			n = len(m.hitPages()) + 1 // the "pages with hits" header row
		}
		m.catOff = clamp(m.catOff+step, 0, maxOff(n, m.height-chromeRows))
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
	// The detail column scrolls its own editor (#1325): a long enum list is
	// the one thing over there with more content than room. In the stacked
	// fallback the band under the divider routes there too (#1664).
	g := m.gridFor()
	if (g.side && x >= m.detailX()) || (!g.side && y-bodyTop > g.listH) {
		if w, ok := m.editor.(wheelEditor); ok {
			w.Wheel(delta)
		}
		return
	}
	// Schema form: viewport scroll, decoupled from the selection (#885).
	m.formOff = clamp(m.formOff+delta, 0, maxOff(len(m.rows()), g.listH))
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

// NoteReloadDiags surfaces config write/reload diagnostics inline (#891):
// the first one shows error-styled in the detail footer until the next
// action, instead of only flashing as a toast.
func (m *Model) NoteReloadDiags(diags []config.Diagnostic) {
	if !m.open || len(diags) == 0 {
		return
	}
	d := diags[0]
	m.writeErr = d.Field + ": " + d.Message
	m.notice = ""
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
	if m.filtering {
		return m.updateFilter(key)
	}
	m.syncEditor()
	// The detail column's editor owns every key while it has the focus
	// (#1295) — including esc, which each editor uses to cancel its own input
	// before handing the focus back to the settings column. Only tab is panel
	// chrome, so the three-column cycle always works.
	if m.focus == detailColumn && m.editor != nil {
		// Tab is the grid's column cycle — unless the editor is a text input,
		// where tab is completion (a path editor would otherwise never
		// complete, #541).
		if key.String() == "tab" && !m.editor.Capturing() {
			m.focus = catColumn
			return nil
		}
		return m.editor.Update(key)
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
				return m.closeOrApply()
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
		return m.closeOrApply()
	case "tab":
		if m.filter != "" && m.focus == formColumn {
			// Search mode: tab leaves for the match's own page (#1297).
			m.openHitPage()
			return nil
		}
		// The grid cycles nav → settings → detail → nav (#1295).
		switch {
		case m.focus == catColumn:
			m.focus = formColumn
		case m.editor != nil:
			m.focus = detailColumn
		default:
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
			e := r.entry
			return m.writeValue(e, m.value(e.Key) != "true")
		}
	case "?":
		m.openKeyHelp()
	default:
		// First-letter jump on the rail (#890, menu parity): a letter hops to
		// the next page starting with it, cycling.
		if m.focus == catColumn && m.filter == "" && len(key.String()) == 1 {
			m.jumpToLetter(key.String())
		}
	case "right", "l":
		if m.focus == catColumn {
			m.focus = formColumn
			if m.filter == "" {
				m.sel = 0
			}
			m.syncEditor()
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
		if m.focus == catColumn {
			m.focus = formColumn
			if m.filter == "" {
				m.sel = 0
			}
			m.syncEditor()
			return nil
		}
		return m.activate()
	case "r":
		if r, ok := m.current(); ok && m.focus == formColumn && r.kind == rowEntry {
			m.rebuildEditor() // rebuild against the restored value
			return m.stageReset(r.entry)
		}
	case "ctrl+s", "cmd+s":
		// Apply the staged batch (#1296). Enter cannot do it — it is the
		// editor key on every row — so the panel borrows the universal save
		// chord, and the header's change counter is clickable for the mouse.
		return m.openApply()
	case "s":
		// Cycle the write-scope selector (0380, #794): auto → user → project.
		m.writeScope = (m.writeScope + 1) % 3
	case "/":
		m.filtering = true
		m.focus = formColumn
	}
	return nil
}

// closeOrApply handles esc on the panel: with staged edits it opens the diff
// instead of throwing them away, so a batch is never lost to a stray key
// (#1296). The diff's own buttons then close the panel.
func (m *Model) closeOrApply() tea.Cmd {
	if m.Dirty() {
		m.Push(&applyPanel{m: m, pal: m.pal, closeAfter: true})
		return nil
	}
	m.Close()
	return nil
}

// jumpToLetter selects the next page whose title starts with letter (#890).
func (m *Model) jumpToLetter(letter string) {
	letter = strings.ToLower(letter)
	n := len(m.pages)
	for i := 1; i <= n; i++ {
		idx := (m.cat + i) % n
		if strings.HasPrefix(strings.ToLower(m.pages[idx].Title), letter) {
			m.cat, m.sel = idx, 0
			m.followCat, m.followForm = true, true
			return
		}
	}
}

// moveNav applies pgup/pgdn/home/end to the focused column (#887).
func (m *Model) moveNav(key string) {
	if m.focus == catColumn {
		if m.filter != "" {
			sel := m.hitSel
			if listNav(key, &sel, len(m.hitPages()), navPage) {
				m.jumpToHitPage(sel)
			}
			return
		}
		if listNav(key, &m.cat, len(m.pages), navPage) {
			m.sel = 0
			m.followCat, m.followForm = true, true
		}
		return
	}
	if listNav(key, &m.sel, len(m.rows()), navPage) {
		m.followForm = true
		m.syncHitSel()
	}
}

// move shifts the focused column's selection.
func (m *Model) move(dir int) {
	m.notice, m.writeErr = "", ""
	if m.focus == catColumn {
		if m.filter != "" {
			// In search mode the rail walks the pages with hits (#1297) and
			// the match list follows it.
			m.jumpToHitPage(clamp(m.hitSel+dir, 0, len(m.hitPages())-1))
			return
		}
		m.cat = clamp(m.cat+dir, 0, len(m.pages)-1)
		m.sel = 0
		m.followCat, m.followForm = true, true
		return
	}
	if n := len(m.rows()); n > 0 {
		m.sel = clamp(m.sel+dir, 0, n-1)
		m.followForm = true
		m.syncHitSel()
	}
}

// activate opens the selected entry's editor: the focus moves to the detail
// column, where the typed editor already renders (#1295). Chord entries skip
// straight into the shared capture sub-panel, which is the only editor that
// needs every key including esc and tab.
func (m *Model) activate() tea.Cmd {
	r, ok := m.current()
	if !ok {
		return nil
	}
	if r.kind != rowEntry {
		m.activateResult(r)
		return nil
	}
	m.syncEditor()
	e := r.entry
	switch e.Type {
	case Chord:
		m.Push(newChordCapture(m, m.opts, m.scopeFor(e), e.Key, e.Title, m.pal))
		return nil
	case Bool:
		// A toggle has one obvious action; making it a two-step trip through
		// the detail column would be worse, not clearer. The ◉/○ rows still
		// render there, and tab reaches them.
		if b, ok := m.editor.(*boolEditor); ok {
			b.idx = 1 - b.idx
		}
		return m.writeValue(e, m.value(e.Key) != "true")
	}
	m.focus = detailColumn
	return nil
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
	n, err := strconv.Atoi(strings.TrimSpace(m.value(e.Key)))
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
	m.rebuildEditor()
	return m.writeValue(e, next)
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
	next := (optionIndex(e, m.value(e.Key)) + dir + n) % n
	m.rebuildEditor()
	return m.writeValue(e, e.Options[next])
}

// Paste inserts a pasted block into whichever of the panel's own text inputs
// is active (#1273): the focused detail-column editor when it is text-backed,
// else the category/entry filter. It returns false when neither is active —
// the panel is then navigating, and the caller drops the paste rather than
// typing into a list. Sub-panels and custom pages (the keymap page captures
// chords, not text) are deliberately not routed.
func (m *Model) Paste(text string) (handled bool) {
	if m.topSub() != nil {
		return false
	}
	if m.filtering {
		out, _, changed := ui.PasteText(m.filter, len([]rune(m.filter)), text)
		if !changed {
			return false
		}
		m.filter = out
		m.sel = 0
		return true
	}
	if m.focus == detailColumn && m.editor != nil {
		if p, ok := m.editor.(pasteEditor); ok {
			return p.Paste(text)
		}
	}
	// A custom page with a text input of its own (the colour-token entry,
	// #1238) opts in through the same seam.
	if page := m.customPage(); page != nil && m.filter == "" && page.Capturing() {
		if p, ok := page.(pasteEditor); ok {
			return p.Paste(text)
		}
	}
	return false
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

// clickDetail routes a press inside the detail column (#1325): it focuses the
// column and forwards the press to the editor at editor-body-local
// coordinates, so an editor decides what its own rows mean. row is the
// body-local y the caller already computed; g the grid this frame drew.
func (m *Model) clickDetail(x, row int, g grid) tea.Cmd {
	if m.editor == nil {
		return nil
	}
	m.focus = detailColumn
	dx, dy := x-m.detailX(), row
	if !g.side {
		// Stacked fallback: the detail band sits under the list and the
		// divider row, and spans the full width.
		dx, dy = x-formX(), row-g.listH-1
	}
	c, ok := m.editor.(clickEditor)
	if !ok {
		return nil
	}
	if m.detailBodyTop < 0 {
		// The last frame drew the page/jump explanation, not an editor body
		// (#1664): the press only focuses, there is no control to hit.
		return nil
	}
	if dy -= m.detailBodyTop; dy < 0 {
		return nil // the head: documentation, not a control
	}
	return c.Click(dx, dy)
}
