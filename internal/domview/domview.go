// Package domview is the DOM inspector tool window (#1929): a side pane
// showing the parsed DOM tree of the focused HTML buffer, with a CSS selector
// tester. The panel is pure presentation: the root model parses the buffer
// asynchronously (internal/htmldom) and feeds the document in; enter/double-
// click emit a NavigateMsg the root model turns into a standard cursor jump,
// and the selector's matches surface both here (row highlight) and in the
// editor (the root model routes the match ranges to every editor showing the
// file, keyed by MatchesRev).
package domview

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"golang.org/x/net/html"

	"ike/internal/htmldom"
	"ike/internal/theme"
	"ike/internal/ui"
)

// NavigateMsg asks the root model to move the editor cursor to a node's
// source position (0-based editor coordinates), through the standard open
// funnel so nav history records the jump.
type NavigateMsg struct {
	Path string
	Line int
	Col  int
}

// CopyMsg asks the root model to put Text on the system clipboard; What names
// the payload for the confirmation toast ("CSS selector", "outer HTML").
type CopyMsg struct {
	Text string
	What string
}

// textPreviewRunes caps a text node's display excerpt.
const textPreviewRunes = 40

// Row is one visible tree row: the node it stands for, its depth and whether
// it can fold.
type Row struct {
	Node    *html.Node
	Depth   int
	HasKids bool
}

// Model is the tool window state. Value type with pointer-receiver mutators,
// embedded in a pane.Instance like the Structure panel.
type Model struct {
	width   int
	height  int
	focused bool
	pal     *theme.Palette

	path       string // file the shown document belongs to; "" until the first delivery
	docVersion int    // editor document version the tree was parsed from
	doc        *htmldom.Document
	notHTML    bool // the active buffer is not an HTML file

	collapsed map[*html.Node]bool
	rows      []Row
	cursor    int
	top       int
	current   int // node enclosing the editor cursor (-1 none)

	selEditing   bool
	selector     string
	selCursor    int
	selErr       string
	matches      []*html.Node
	matchSet     map[*html.Node]bool
	matchIdx     int  // current match (-1 none)
	matchesRev   int  // bumped whenever matches or the current match change
	matchWrapped bool // the last step came back around an end (#2410)

	// Double-click detection: navigating to a node needs a second click on
	// the same row within ui.DoubleClickWindow; now is injectable so tests
	// control the clock.
	clicks ui.ClickTracker
	now    func() time.Time
}

// New returns an empty panel awaiting its first document delivery.
func New(pal *theme.Palette) Model {
	return Model{
		pal: pal, current: -1, matchIdx: -1,
		collapsed: map[*html.Node]bool{}, matchSet: map[*html.Node]bool{},
		now: time.Now,
	}
}

// SetSize records the interior content size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetFocused marks the panel focused (selection highlight); losing focus
// leaves selector editing.
func (m *Model) SetFocused(f bool) {
	m.focused = f
	if !f {
		m.selEditing = false
	}
}

// SetPalette re-threads the active theme.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// Path reports which file the shown document belongs to ("" before the first
// delivery) — the root model's staleness test.
func (m *Model) Path() string { return m.path }

// DocVersion reports the editor document version the tree was parsed from —
// the root model's reparse test.
func (m *Model) DocVersion() int { return m.docVersion }

// Rows exposes the visible rows (tests).
func (m *Model) Rows() []Row { return m.rows }

// Cursor reports the selected row index (tests).
func (m *Model) Cursor() int { return m.cursor }

// Current reports the cursor-follow row index, -1 when none (tests).
func (m *Model) Current() int { return m.current }

// Selector reports the committed selector text (tests).
func (m *Model) Selector() string { return m.selector }

// Editing reports whether the selector input owns the keys (tests).
func (m *Model) Editing() bool { return m.selEditing }

// SelectorError reports the compile error of the current selector ("" when it
// is valid or empty).
func (m *Model) SelectorError() string { return m.selErr }

// Matches exposes the matched nodes in document order.
func (m *Model) Matches() []*html.Node { return m.matches }

// MatchesRev is the change counter of the match set and current index; the
// root model re-routes editor highlights when it moves.
func (m *Model) MatchesRev() int { return m.matchesRev }

// MatchRanges returns the opening-tag range of every match in 0-based editor
// coordinates plus the current match index (-1 none) — the editor highlight
// payload.
func (m *Model) MatchRanges() ([]htmldom.Range, int) {
	if m.doc == nil {
		return nil, -1
	}
	out := make([]htmldom.Range, 0, len(m.matches))
	for _, n := range m.matches {
		sp, ok := m.doc.Span(n)
		if !ok {
			continue
		}
		out = append(out, m.doc.RangeOf(htmldom.Span{Start: sp.Start, End: sp.OpenEnd}))
	}
	return out, m.matchIdx
}

// SetNotHTML marks the active buffer as non-HTML: the tree clears and the
// empty notice explains itself.
func (m *Model) SetNotHTML(path string) {
	if m.notHTML && m.path == path {
		return
	}
	m.path, m.notHTML, m.doc = path, true, nil
	m.docVersion = -1
	m.rows, m.cursor, m.top, m.current = nil, 0, 0, -1
	m.clearMatches()
	m.matchesRev++
}

// SetDoc replaces the tree with a freshly parsed document. A delivery for the
// same file keeps folds and selection where possible; a new file resets them.
// The committed selector re-applies against the new document.
func (m *Model) SetDoc(path string, version int, doc *htmldom.Document) {
	if path != m.path {
		m.collapsed = map[*html.Node]bool{}
		m.cursor, m.top = 0, 0
	}
	m.path, m.docVersion, m.doc, m.notHTML = path, version, doc, false
	// Node pointers change with every parse; folds keyed by stale pointers
	// are dropped by the rebuild (the map only pins memory until then).
	m.collapsed = map[*html.Node]bool{}
	m.current = -1
	m.rebuildRows()
	if m.cursor >= len(m.rows) {
		m.cursor = maxInt(0, len(m.rows)-1)
	}
	m.recomputeMatches()
	m.scrollToCursor()
}

// rebuildRows flattens the document depth-first, skipping collapsed subtrees.
func (m *Model) rebuildRows() {
	m.rows = m.rows[:0]
	if m.doc == nil {
		return
	}
	var walk func(n *html.Node, depth int)
	walk = func(n *html.Node, depth int) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			kids := c.FirstChild != nil
			m.rows = append(m.rows, Row{Node: c, Depth: depth, HasKids: kids})
			if kids && !m.collapsed[c] {
				walk(c, depth+1)
			}
		}
	}
	walk(m.doc.Root, 0)
}

// clearMatches empties the match state without touching the revision.
func (m *Model) clearMatches() {
	m.matches, m.matchIdx, m.selErr = nil, -1, ""
	m.matchWrapped = false // a fresh match set starts a fresh walk (#2410)
	m.matchSet = map[*html.Node]bool{}
}

// recomputeMatches re-runs the committed selector against the document and
// bumps the revision so the root model refreshes the editor highlights.
func (m *Model) recomputeMatches() {
	m.matchesRev++
	m.clearMatches()
	if m.doc == nil || strings.TrimSpace(m.selector) == "" {
		return
	}
	sel, err := htmldom.Compile(m.selector)
	if err != nil {
		m.selErr = err.Error()
		return
	}
	m.matches = m.doc.Select(sel)
	for _, n := range m.matches {
		m.matchSet[n] = true
	}
	if len(m.matches) > 0 {
		m.matchIdx = 0
	}
}

// FollowCursor highlights the deepest node enclosing the editor's cursor
// (0-based line, rune column). Inside a collapsed subtree the nearest visible
// ancestor is highlighted instead — following never unfolds. The highlight
// scrolls into view while the panel is unfocused, so it never fights the
// user's own scrolling.
func (m *Model) FollowCursor(line, col int) {
	if m.doc == nil {
		return
	}
	n := m.doc.NodeAt(m.doc.Offset(line, col))
	idx := -1
	for ; n != nil && idx < 0; n = n.Parent {
		idx = m.rowIndexOf(n)
	}
	m.current = idx
	if idx >= 0 && !m.focused {
		m.cursor = idx
		m.scrollToCursor()
	}
}

// rowIndexOf finds a node's visible row, -1 when folded away.
func (m *Model) rowIndexOf(n *html.Node) int {
	for i, r := range m.rows {
		if r.Node == n {
			return i
		}
	}
	return -1
}

// Update handles one message while the panel exists; the pane layer only
// routes key presses of the focused pane here.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if m.selEditing {
		// The copy chord outranks the selector prompt (#2062): the node the
		// copy acts on stays highlighted behind the prompt, so swallowing
		// the chord would put a visible target out of reach — the response
		// pane's #2051. The bare keys cannot be reserved the same way: "c"
		// and "Y" are valid selector input, the chord never is.
		if copyChord(key) {
			return m.copySelectorPath()
		}
		return m.selectorKey(key)
	}
	// Shared list semantics (#1666): steps wrap, page jumps clamp.
	if ui.ListNav(key.String(), &m.cursor, len(m.rows), m.bodyHeight(), ui.NavFull) {
		m.scrollToCursor()
		return nil
	}
	switch key.String() {
	case "enter":
		return m.navigate(m.cursor)
	case "space":
		m.toggleFold(m.cursor)
	case "l", "right":
		m.expand(m.cursor)
	case "h", "left":
		m.collapseOrParent()
	case "/":
		m.openSelectorInput()
	case "ctrl+f", "cmd+f", "super+f":
		// ctrl+f is deliberately unbound in the keymap table (#2409) so
		// vim's page-forward survives in the editor; the panes that have a
		// search answer the chord themselves.
		m.openSelectorInput()
	case "n":
		return m.stepMatch(1)
	case "N":
		return m.stepMatch(-1)
	case "c":
		return m.copySelectorPath()
	case "Y":
		return m.copyOuterHTML()
	}
	if copyChord(key) {
		// The muscle-memory copy chord aliases "c" (#2062), so the pane
		// answers the shortcut every other selectable surface answers.
		return m.copySelectorPath()
	}
	return nil
}

// copyChord reports whether key is the pane's modified copy chord. ctrl+c is
// deliberately absent: the tree has no text selection to protect, so the key
// keeps its global quit meaning here (#2062) — unlike in the response pane,
// where a live selection claims it.
func copyChord(key tea.KeyPressMsg) bool { return ui.CopyChord(key.String()) }

// openSelectorInput puts the cursor in the selector line, the pane's search.
func (m *Model) openSelectorInput() { m.selEditing = true }

// OpenSearch implements the pane's Searchable capability (#2409): the shared
// find chord opens the same selector input "/" does.
func (m *Model) OpenSearch() bool {
	m.openSelectorInput()
	return true
}

// selectorKey edits the selector input; every change re-matches live.
func (m *Model) selectorKey(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "enter", "esc":
		m.selEditing = false
		return nil
	}
	text, cur, handled, changed := ui.EditKey(key, m.selector, m.selCursor)
	if !handled {
		return nil
	}
	m.selector, m.selCursor = text, cur
	if changed {
		m.recomputeMatches()
	}
	return nil
}

// toggleFold folds/unfolds the element at row i.
func (m *Model) toggleFold(i int) {
	if i < 0 || i >= len(m.rows) || !m.rows[i].HasKids {
		return
	}
	n := m.rows[i].Node
	if m.collapsed[n] {
		delete(m.collapsed, n)
	} else {
		m.collapsed[n] = true
	}
	m.rebuildRows()
	m.cursor = clampInt(m.cursor, 0, maxInt(0, len(m.rows)-1))
	m.scrollToCursor()
}

// expand unfolds the element at row i.
func (m *Model) expand(i int) {
	if i < 0 || i >= len(m.rows) || !m.collapsed[m.rows[i].Node] {
		return
	}
	delete(m.collapsed, m.rows[i].Node)
	m.rebuildRows()
	m.scrollToCursor()
}

// collapseOrParent folds the element at the cursor, or moves to its parent
// when it is already folded (or has no children) — archview's h semantics.
func (m *Model) collapseOrParent() {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return
	}
	r := m.rows[m.cursor]
	if r.HasKids && !m.collapsed[r.Node] {
		m.collapsed[r.Node] = true
		m.rebuildRows()
		m.cursor = clampInt(m.rowIndexOf(r.Node), 0, maxInt(0, len(m.rows)-1))
		m.scrollToCursor()
		return
	}
	if p := r.Node.Parent; p != nil {
		if i := m.rowIndexOf(p); i >= 0 {
			m.cursor = i
			m.scrollToCursor()
		}
	}
}

// stepMatch moves the current match by delta (wrapping), selects its row —
// unfolding ancestors so it is visible — and jumps the editor to it.
func (m *Model) stepMatch(delta int) tea.Cmd { return m.stepMatchStat(delta).Cmd }

// NextMatch implements the pane's match-step capability (#2410): cmd+g steps
// the selector's matches while the selector line keeps the keyboard, so a
// selector can be refined and walked without leaving the input. n / N do the
// same once the line is closed.
func (m *Model) NextMatch() ui.MatchStep { return m.stepSelector(1) }

// PrevMatch steps backwards; see NextMatch.
func (m *Model) PrevMatch() ui.MatchStep { return m.stepSelector(-1) }

// stepSelector owns the chord whenever a selector exists at all — the open
// input or the applied one the footer still counts.
func (m *Model) stepSelector(delta int) ui.MatchStep {
	if !m.selEditing && m.selector == "" {
		return ui.NoStep
	}
	return m.stepMatchStat(delta)
}

// stepMatchStat is stepMatch's reporting form: it says where the walk landed
// and whether it wrapped, so the selector line can show it (#2410).
func (m *Model) stepMatchStat(delta int) ui.MatchStep {
	if len(m.matches) == 0 {
		m.matchWrapped = false
		return ui.NoMatches()
	}
	m.matchIdx, m.matchWrapped = ui.StepWrap(m.matchIdx, len(m.matches), delta)
	m.matchesRev++ // the editor's current-match underline follows
	n := m.matches[m.matchIdx]
	for p := n.Parent; p != nil; p = p.Parent {
		delete(m.collapsed, p)
	}
	m.rebuildRows()
	st := ui.Stepped(m.matchIdx, len(m.matches), m.matchWrapped)
	if i := m.rowIndexOf(n); i >= 0 {
		m.cursor = i
		m.scrollToCursor()
		st.Cmd = m.navigate(i)
	}
	return st
}

// navigate emits the jump to row i's node source position.
func (m *Model) navigate(i int) tea.Cmd {
	if i < 0 || i >= len(m.rows) || m.doc == nil || m.path == "" {
		return nil
	}
	sp, ok := m.doc.Span(m.rows[i].Node)
	if !ok {
		return nil
	}
	line, col := m.doc.Position(sp.Start)
	msg := NavigateMsg{Path: m.path, Line: line, Col: col}
	return func() tea.Msg { return msg }
}

// copySelectorPath emits the cursor node's shortest-unique CSS selector (a
// text/comment row copies its enclosing element's).
func (m *Model) copySelectorPath() tea.Cmd {
	n := m.cursorElement()
	if n == nil || m.doc == nil {
		return nil
	}
	sel := m.doc.SelectorPath(n)
	if sel == "" {
		return nil
	}
	msg := CopyMsg{Text: sel, What: "CSS selector"}
	return func() tea.Msg { return msg }
}

// copyOuterHTML emits the cursor node's verbatim source text.
func (m *Model) copyOuterHTML() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.rows) || m.doc == nil {
		return nil
	}
	text := m.doc.OuterHTML(m.rows[m.cursor].Node)
	if text == "" {
		return nil
	}
	msg := CopyMsg{Text: text, What: "outer HTML"}
	return func() tea.Msg { return msg }
}

// cursorElement resolves the cursor row to its element (itself, or the
// enclosing element for text/comment rows).
func (m *Model) cursorElement() *html.Node {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	for n := m.rows[m.cursor].Node; n != nil; n = n.Parent {
		if n.Type == html.ElementNode {
			return n
		}
	}
	return nil
}

// headerRows is how many lines sit above the first tree row: the title and
// the CSS selector line.
const headerRows = 2

// Wheel scrolls the list by delta rows (positive = down), like the Structure
// panel; the shared list-mouse layer (#2259) keeps the last page full and
// drags the cursor along.
func (m *Model) Wheel(delta int) {
	ui.WheelWindow(&m.top, &m.cursor, delta, len(m.rows), m.bodyHeight())
}

// Click handles one left click at content-local (x, y): the selector line
// starts editing, a fold glyph toggles, a row click selects and a second
// click on the same row within the double-click window navigates.
func (m *Model) Click(x, y int) tea.Cmd {
	if y == 1 {
		m.selEditing = true
		return nil
	}
	i, ok := ui.RowAt(y, m.top, headerRows, m.bodyHeight(), len(m.rows))
	if !ok {
		m.clicks.Reset()
		return nil
	}
	m.selEditing = false
	r := m.rows[i]
	if r.HasKids && x >= 1+2*r.Depth && x < 3+2*r.Depth {
		m.cursor = i
		m.clicks.Reset()
		m.toggleFold(i)
		return nil
	}
	double := m.clicks.Double(i, m.now())
	m.cursor = i
	if double {
		m.clicks.Reset()
		return m.navigate(i)
	}
	return nil
}

// View renders the header, the selector line and the tree rows.
func (m *Model) View() string {
	pal := m.theme()
	var b strings.Builder
	b.WriteString(m.header(pal))
	b.WriteString("\n")
	b.WriteString(m.selectorLine(pal))
	height := m.bodyHeight()
	if len(m.rows) == 0 {
		b.WriteString("\n " + lipgloss.NewStyle().Faint(true).Render(m.emptyNotice()))
		for k := 1; k < height; k++ {
			b.WriteString("\n")
		}
		return b.String()
	}
	m.scrollToCursor()
	base := lipgloss.NewStyle().Foreground(pal.Foreground) // built once (#1100)
	for k := 0; k < height; k++ {
		b.WriteString("\n")
		i := m.top + k
		if i >= len(m.rows) {
			continue
		}
		b.WriteString(m.renderRow(pal, base, i))
	}
	return b.String()
}

// header names the file the tree belongs to plus the node count.
func (m *Model) header(pal *theme.Palette) string {
	return ui.FileHeader(pal, m.path, len(m.rows), m.focused)
}

// selectorLine renders the selector input with its match count or error.
func (m *Model) selectorLine(pal *theme.Palette) string {
	label := lipgloss.NewStyle().Faint(true).Render("sel ")
	body := m.selector
	switch {
	case m.selEditing:
		body = ui.CursorView(m.selector, m.selCursor)
	case m.selector == "":
		return " " + label + lipgloss.NewStyle().Faint(true).Render("/ to test a selector")
	}
	note := ""
	switch {
	case m.selErr != "":
		note = " " + lipgloss.NewStyle().Foreground(pal.Error).Render("✗ "+clip(m.selErr, maxInt(8, m.width-lipgloss.Width(body)-8)))
	case m.selector != "":
		cur := ""
		if m.matchIdx >= 0 {
			cur = strconv.Itoa(m.matchIdx+1) + "/"
		}
		count := cur + strconv.Itoa(len(m.matches)) + " matches"
		if m.matchWrapped {
			// The step that came back around says so, like every other
			// pane's search line (#2410).
			count += " (wrapped)"
		}
		note = " " + lipgloss.NewStyle().Foreground(pal.Secondary).Render(count)
	}
	return " " + label + body + note
}

// emptyNotice explains an empty tree.
func (m *Model) emptyNotice() string {
	switch {
	case m.path == "":
		return "open an HTML file to inspect its DOM"
	case m.notHTML:
		return "the focused buffer is not an HTML file"
	default:
		return "no nodes"
	}
}

// renderRow draws one node row: indent, fold glyph, label.
func (m *Model) renderRow(pal *theme.Palette, base lipgloss.Style, i int) string {
	r := m.rows[i]
	glyph := "  "
	if r.HasKids {
		if m.collapsed[r.Node] {
			glyph = "▸ "
		} else {
			glyph = "▾ "
		}
	}
	line := " " + strings.Repeat("  ", r.Depth) + glyph + nodeLabel(r.Node)
	line = clip(line, m.width)
	style := base
	matched := m.matchSet[r.Node]
	switch {
	case i == m.cursor && m.focused:
		style = style.Background(pal.Selection).Bold(true)
	case i == m.cursor:
		// Unfocused panes keep a muted cursor row (#1034) so refocusing
		// lands visibly, matching the explorer and the other list panes.
		style = style.Background(pal.SelectionMuted)
	case matched && m.matchIdx >= 0 && m.matchIdx < len(m.matches) && m.matches[m.matchIdx] == r.Node:
		// The current match stands apart, like the editor's search style.
		style = style.Background(pal.SelectionMuted).Underline(true)
	case matched:
		style = style.Background(pal.SelectionMuted)
	case i == m.current:
		// The node enclosing the editor cursor (auto-follow).
		style = style.Foreground(pal.Accent)
	}
	return style.Render(line)
}

// nodeLabel formats one node for its row: devtools-style tag#id.classes for
// elements, a truncated excerpt for text, comment/doctype markers.
func nodeLabel(n *html.Node) string {
	switch n.Type {
	case html.ElementNode:
		label := "<" + n.Data
		for _, a := range n.Attr {
			switch a.Key {
			case "id":
				if a.Val != "" {
					label += "#" + a.Val
				}
			case "class":
				for _, c := range strings.Fields(a.Val) {
					label += "." + c
				}
			}
		}
		return label + ">"
	case html.TextNode:
		return "“" + truncate(strings.Join(strings.Fields(n.Data), " "), textPreviewRunes) + "”"
	case html.CommentNode:
		return "<!-- " + truncate(strings.Join(strings.Fields(n.Data), " "), textPreviewRunes) + " -->"
	case html.DoctypeNode:
		return "<!doctype " + n.Data + ">"
	default:
		return "?"
	}
}

// truncate caps s at max runes with an ellipsis.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// scrollToCursor keeps the selected row inside the visible window.
func (m *Model) scrollToCursor() {
	m.top = ui.ScrollToShow(m.top, m.cursor, m.bodyHeight(), len(m.rows))
}

// bodyHeight is the room below the header and selector lines.
func (m *Model) bodyHeight() int {
	h := m.height - 2
	if h < 1 {
		h = 1
	}
	return h
}

// theme resolves the palette with the shared default fallback.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

// clip truncates a line to width terminal cells (rune-approximated), matching
// the other tool panels' clipping.
func clip(s string, width int) string {
	if width <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

// baseName is filepath.Base without the import, over slash or backslash.
func baseName(p string) string {
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// PasteText inserts a pasted block into the open selector input at its cursor
// (#2002) and re-matches live, exactly like typing there does.
func (m *Model) PasteText(text string) bool {
	if !m.selEditing {
		return false
	}
	out, ncur, changed := ui.PasteText(m.selector, m.selCursor, text)
	if !changed {
		return false
	}
	m.selector, m.selCursor = out, ncur
	m.recomputeMatches()
	return true
}
