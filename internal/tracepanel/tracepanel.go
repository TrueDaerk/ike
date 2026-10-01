// Package tracepanel is the Agent Trace tool window (#2840, epic 0540): the
// coding-agent session of a tool pane as a tree of turn → assistant decision
// → tool call → file, built on hiertree. Rows that carry a file reference
// open it in the editor on enter or a double click, through the same
// path:line pipeline the terminal's file links use; the root model owns that
// pipeline and the session lookup, this package only draws and asks.
//
// The panel never reads the transcript itself. The host tails it
// (agenttrace.Reader) off the Update loop, groups the events
// (agenttrace.BuildTree) and hands the finished tree in through Set; the
// tree's node keys are stable across appends, so hiertree.Refresh keeps the
// user's expansion and selection while the agent keeps working.
package tracepanel

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/agenttrace"
	"ike/internal/hiertree"
	"ike/internal/theme"
	"ike/internal/ui"
)

// OpenLocationMsg asks the root model to open a file at a 0-based line and
// column — Line is -1 when the trace knows the file but not the line, which
// opens the file without a jump.
type OpenLocationMsg struct {
	Path string
	Line int
	Col  int
}

// RefreshMsg asks the root model to look the session up again and re-read
// it now ('r', or the dialog's Rescan).
type RefreshMsg struct{}

// InstallHooksMsg asks the root model to run agent.hooks.install — the
// dialog's action while no session is found ('i').
type InstallHooksMsg struct{}

// DiffMsg asks the root model to show what a change did ('D', #2859): the
// diff of the change box or file node keyed Key, reconstructed from the
// transcript — or the change feed's exact one when the node links to an
// entry (Linked, #2838).
type DiffMsg struct {
	Key  string
	Path string
	// Linked is the change-feed path the node links to; "" for none.
	Linked string
}

// AskMsg is 'a' in the pane (#2845): ask a fork of the traced session about
// the selected node — the root model runs agent.ask.
type AskMsg struct{}

// ChangeRevertMsg asks the root model to run the change feed's revert of the
// entry a linked node resolved to ('V', #2838).
type ChangeRevertMsg struct{ Path string }

// linkMark suffixes the detail of a node linked to a change-feed entry.
const linkMark = "Δ"

// Info describes the session the tree was built from.
type Info struct {
	ID         string
	Transcript string
	CWD        string
	// FromHook is true when a Claude Code hook announced the session; false
	// when discovery found it by working directory.
	FromHook bool
	// Ended is set once the harness reported SessionEnd.
	Ended bool
	Turns int
	// History is the date label of a stored session shown read-only (#2860),
	// "" for the live one.
	History string
}

// Model is the tool window.
type Model struct {
	pal     *theme.Palette
	width   int
	height  int
	focused bool

	tree hiertree.Tree[agenttrace.Node]
	// nodes is the last tree Set handed in; links the change-feed entries
	// its writing nodes resolved to (#2838).
	nodes []agenttrace.Node
	links agenttrace.Links
	// known holds the key of every node the last Set showed: a node that is
	// not in it arrived since and is expanded whole (#2857), so a decision
	// landing in the running turn shows its tool calls without a keystroke.
	known map[string]bool
	// follow keeps the cursor on the newest row while the agent works
	// (#2857): on until the user moves the cursor off the last row, on
	// again once it is back there.
	follow bool

	// Diagnostics for the header (#2857): when the host last read the
	// transcript, how many events that read added and which terminal the
	// trace follows and why — "no new lines" must look different from "not
	// reading".
	readAt    time.Time
	readAdded int
	following string

	info       Info
	hasSession bool
	loading    bool
	cwd        string
	err        string

	displayPath func(string) string
	now         func() time.Time
	clicks      ui.ClickTracker

	// view is the shown view (#2858); graph the graph view's state. The
	// zero value is the tree; the host applies the agent.trace.view setting.
	view  ViewMode
	graph graphState
	// picker is the stored-session picker (#2860, history.go).
	picker pickerState
}

// New builds an empty panel: nothing located yet.
func New(pal *theme.Palette) Model {
	return Model{pal: pal, loading: true, now: time.Now, follow: true, graph: graphState{follow: true}}
}

// SetPalette follows a theme switch.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// SetSize records the pane's content size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetFocused records whether the pane holds the keyboard.
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetDisplayPath installs the project-relative path formatter.
func (m *Model) SetDisplayPath(f func(string) string) { m.displayPath = f }

// SetNow installs the clock the double-click window reads (tests).
func (m *Model) SetNow(f func() time.Time) { m.now = f }

// SetLoading marks a lookup or read in flight; the empty state says so
// instead of offering the install action.
func (m *Model) SetLoading(b bool) { m.loading = b }

// HasSession reports whether a tree is shown.
func (m *Model) HasSession() bool { return m.hasSession }

// Info returns the session the tree was built from.
func (m *Model) Info() Info { return m.info }

// Set replaces the tree with a fresh grouping of the session (#2840). Rows
// the user expanded stay expanded and the selection stays on the same node
// (by key); nodes the panel has not shown before — the newest turn on the
// first Set, every new turn, decision, call and file afterwards — are
// expanded whole so the latest activity is visible without a keystroke.
// While following (#2857) the cursor moves onto the newest row, which
// scrolls it into view: the tree grows below the fold otherwise, and a
// growing session looked exactly like a frozen one.
func (m *Model) Set(nodes []agenttrace.Node, info Info) {
	m.loading = false
	m.err = ""
	m.hasSession = true
	m.info = info
	m.nodes = nodes
	m.ensureFetch()
	first := !m.tree.HasNodes()
	m.tree.Refresh(m.rows(nodes), nodeKey)
	roots := m.tree.Roots()
	if first {
		if len(roots) > 0 {
			m.tree.ExpandDeep(roots[len(roots)-1])
		}
	} else {
		m.expandNew(roots)
	}
	m.known = map[string]bool{}
	rememberKeys(m.known, nodes)
	if m.follow {
		m.tree.SetCursor(len(m.tree.Visible()) - 1)
	}
}

// expandNew expands, whole, every row whose node the last Set did not show
// and walks on below the expanded rows that it did.
func (m *Model) expandNew(rows []*hiertree.Row[agenttrace.Node]) {
	for _, r := range rows {
		switch {
		case r.Item.Kind == agenttrace.NodeRewind:
			// An abandoned branch (#2860) stays folded behind its marker.
		case !m.known[r.Item.Key]:
			m.tree.ExpandDeep(r)
		case r.Expanded():
			m.expandNew(r.Children())
		}
	}
}

func rememberKeys(into map[string]bool, nodes []agenttrace.Node) {
	for i := range nodes {
		into[nodes[i].Key] = true
		rememberKeys(into, nodes[i].Children)
	}
}

// SetRead records one finished read of the transcript for the header: its
// time and how many events it added or completed (#2857).
func (m *Model) SetRead(at time.Time, added int) { m.readAt, m.readAdded = at, added }

// SetFollowing names the terminal the trace follows and why ("agent ·
// focused", "terminal · hook", "project root · scan"), shown in the header.
func (m *Model) SetFollowing(s string) { m.following = s }

// Following reports whether the cursor sticks to the newest row.
func (m *Model) Following() bool { return m.follow }

// noteCursor re-derives follow after the user moved: it holds while the
// cursor is on the last row.
func (m *Model) noteCursor() {
	if n := len(m.tree.Visible()); n > 0 {
		m.follow = m.tree.Cursor() == n-1
	}
}

// SetNoSession switches to the empty state: no transcript for cwd. err is
// shown when it is more than "not found".
func (m *Model) SetNoSession(cwd string, err error) {
	m.loading = false
	m.hasSession = false
	m.cwd = cwd
	m.err = ""
	if err != nil && err != agenttrace.ErrNotFound {
		m.err = err.Error()
	}
	m.tree.Clear()
	m.nodes, m.links = nil, agenttrace.Links{}
	m.known = nil
	m.follow = true
	m.graph = graphState{follow: true}
	m.readAt, m.readAdded = time.Time{}, 0
}

// Reset forgets the shown session ahead of a switch to another transcript:
// node keys are per session, so the expansion state must not carry over.
// The pane shows the locating notice until the next Set.
func (m *Model) Reset() {
	m.tree.Clear()
	m.nodes, m.links = nil, agenttrace.Links{}
	m.known = nil
	m.follow = true
	m.graph = graphState{follow: true}
	m.readAt, m.readAdded = time.Time{}, 0
	m.hasSession = false
	m.loading = true
	m.err = ""
}

// Current returns the selected node, nil on an empty tree. In the graph
// view it is the selected box seen as a node: a change box as its file
// node (same key, so the change-feed links and the ask context apply), a
// prompt as its turn, an answer as a decision.
func (m *Model) Current() *agenttrace.Node {
	if m.view == ViewGraph {
		if st := m.CurrentStop(); st != nil {
			return stopNode(st)
		}
		return nil
	}
	r := m.tree.Current()
	if r == nil {
		return nil
	}
	return &r.Item
}

// Rows returns the visible tree rows as "depth:key" strings (tests).
func (m *Model) Rows() []string {
	var out []string
	for _, r := range m.tree.Visible() {
		out = append(out, strings.Repeat(" ", r.Depth())+r.Item.Key)
	}
	return out
}

// Nodes returns the tree the last Set handed in.
func (m *Model) Nodes() []agenttrace.Node { return m.nodes }

// Links returns the change-feed links the rows show.
func (m *Model) Links() agenttrace.Links { return m.links }

// SetLinks installs the change-feed links of the shown tree (#2838): linked
// rows carry the Δ mark and answer D (mini-diff) and V (revert). The tree
// is re-laid only when the links changed, keeping expansion and selection.
func (m *Model) SetLinks(l agenttrace.Links) {
	if sameLinks(m.links, l) {
		return
	}
	m.links = l
	if m.hasSession {
		m.ensureFetch()
		m.tree.Refresh(m.rows(m.nodes), nodeKey)
	}
}

func sameLinks(a, b agenttrace.Links) bool {
	if len(a.ByNode) != len(b.ByNode) || len(a.ByPath) != len(b.ByPath) {
		return false
	}
	for k, v := range a.ByNode {
		if b.ByNode[k] != v {
			return false
		}
	}
	for k, v := range a.ByPath {
		if b.ByPath[k] != v {
			return false
		}
	}
	return true
}

// Select moves the cursor onto the node keyed key, unfolding its ancestors,
// and reports whether the node exists — the change feed's jump to the trace
// node behind a write (#2838).
func (m *Model) Select(key string) bool {
	if m.view == ViewGraph {
		return m.graphSelect(key)
	}
	chain := keyChain(m.nodes, key)
	if chain == nil {
		return false
	}
	m.ensureFetch()
	level := m.tree.Roots()
	var target *hiertree.Row[agenttrace.Node]
	for i, k := range chain {
		target = nil
		for _, r := range level {
			if r.Item.Key == k {
				target = r
				break
			}
		}
		if target == nil {
			return false
		}
		if i < len(chain)-1 {
			m.tree.Expand(target)
			level = target.Children()
		}
	}
	for i, r := range m.tree.Visible() {
		if r == target {
			m.tree.SetCursor(i)
			m.noteCursor()
			return true
		}
	}
	return false
}

// keyChain returns the keys from a root down to the node keyed key, nil when
// no node has it.
func keyChain(nodes []agenttrace.Node, key string) []string {
	for i := range nodes {
		if nodes[i].Key == key {
			return []string{key}
		}
		if rest := keyChain(nodes[i].Children, key); rest != nil {
			return append([]string{nodes[i].Key}, rest...)
		}
	}
	return nil
}

// nodeKey is the identity hiertree.Refresh carries state by.
func nodeKey(n agenttrace.Node) string { return n.Key }

// rows converts trace nodes to tree rows; the fetch below expands them from
// the children they already carry.
func (m *Model) rows(nodes []agenttrace.Node) []hiertree.Row[agenttrace.Node] {
	out := make([]hiertree.Row[agenttrace.Node], len(nodes))
	for i, n := range nodes {
		out[i] = hiertree.Row[agenttrace.Node]{Entry: entry(n, m.links.Node(n.Key) != ""), Item: n}
	}
	return out
}

// entry is the hiertree presentation of a node: the label, the faint detail
// (with the Δ mark when the node links to a change-feed entry) and — for the
// rows that carry a file — the location, with the 1-based FileRef line
// mapped onto hiertree's 0-based Line (-1 = unknown).
func entry(n agenttrace.Node, linked bool) hiertree.Entry {
	e := hiertree.Entry{Name: n.Label, Detail: n.Detail, Line: -1}
	if linked {
		e.Detail = strings.TrimSpace(e.Detail + " " + linkMark)
	}
	if n.Path != "" {
		e.Path = n.Path
		if n.Ref != nil && n.Ref.Line > 0 {
			e.Line = n.Ref.Line - 1
		}
	}
	return e
}

// Update handles one key while the pane is focused.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if m.picker.open {
		return m.pickerKey(k)
	}
	cmd := m.handleKey(k)
	if m.hasSession && m.view == ViewTree {
		m.noteCursor()
	}
	return cmd
}

// toggleView switches between the graph and the tree ('t', or the
// agent.trace.view command).
func (m *Model) toggleView() {
	if m.view == ViewGraph {
		m.SetViewMode(ViewTree)
	} else {
		m.SetViewMode(ViewGraph)
	}
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if !m.hasSession {
		switch key {
		case "s":
			return func() tea.Msg { return HistoryMsg{} }
		case "i", "enter":
			if m.loading {
				return nil
			}
			return func() tea.Msg { return InstallHooksMsg{} }
		case "r":
			return func() tea.Msg { return RefreshMsg{} }
		}
		return nil
	}
	if key == "t" {
		m.toggleView()
		return nil
	}
	if m.view == ViewGraph {
		if cmd, ok := m.graphKey(key); ok {
			return cmd
		}
	} else {
		m.ensureFetch()
		onEnter := func(r hiertree.Row[agenttrace.Node]) tea.Cmd {
			if cmd := m.open(r.Item); cmd != nil {
				return cmd
			}
			if cur := m.tree.Current(); cur != nil {
				return m.tree.Toggle(cur)
			}
			return nil
		}
		if cmd, ok := m.tree.Key(key, m.treeHeight(), onEnter, func() tea.Cmd { return nil }); ok {
			return cmd
		}
	}
	switch key {
	case "s":
		return func() tea.Msg { return HistoryMsg{} }
	case "esc":
		if m.Stored() {
			return func() tea.Msg { return LiveMsg{} }
		}
	case "r":
		if m.Stored() {
			return func() tea.Msg { return LiveMsg{} }
		}
		return func() tea.Msg { return RefreshMsg{} }
	case "i":
		return func() tea.Msg { return InstallHooksMsg{} }
	case "a":
		return func() tea.Msg { return AskMsg{} }
	case "D":
		// Every change has a diff (#2859); reads and the other rows have none.
		cur := m.Current()
		if cur == nil || cur.Ref == nil || cur.Ref.Op == agenttrace.OpRead {
			return nil
		}
		msg := DiffMsg{Key: cur.Key, Path: cur.Ref.Path, Linked: m.links.Node(cur.Key)}
		return func() tea.Msg { return msg }
	case "V":
		cur := m.Current()
		if cur == nil {
			return nil
		}
		path := m.links.Node(cur.Key)
		if path == "" {
			return nil // unlinked rows stay plain
		}
		return func() tea.Msg { return ChangeRevertMsg{Path: path} }
	}
	return nil
}

// ensureFetch wires the tree's synchronous expansion; the Tree is a value
// inside the Model, so the closure over its address is taken lazily, after
// the model has settled in the pane instance.
func (m *Model) ensureFetch() {
	m.tree.SetFetch(hiertree.Static(&m.tree, func(n agenttrace.Node) []hiertree.Row[agenttrace.Node] {
		return m.rows(n.Children)
	}))
}

// open is the click-to-code half: a node with a file reference yields the
// OpenLocationMsg the root model turns into openPathAt. nil for the rest.
func (m *Model) open(n agenttrace.Node) tea.Cmd {
	if n.Ref == nil || n.Ref.Path == "" {
		return nil
	}
	ref := *n.Ref
	return func() tea.Msg {
		return OpenLocationMsg{Path: ref.Path, Line: ref.Line - 1, Col: 0}
	}
}

// Wheel scrolls the tree by delta rows.
func (m *Model) Wheel(delta int) {
	if m.picker.open {
		m.pickerWheel(delta)
		return
	}
	if !m.hasSession {
		return
	}
	if m.view == ViewGraph {
		m.graphScroll(delta)
		return
	}
	m.ensureFetch()
	m.tree.Wheel(delta, m.treeHeight())
	m.noteCursor()
}

// Click handles a left click at pane-content-local (x, y): on the tree a
// click selects, a click on a row's marker cell toggles it, and a second
// click on the same row within ui.DoubleClickWindow opens its file (or
// toggles a row without one). In the empty state the dialog's buttons act.
func (m *Model) Click(x, y int) tea.Cmd {
	if m.picker.open {
		return m.pickerClick(x, y)
	}
	if !m.hasSession {
		return m.dialogClick(x, y)
	}
	if m.view == ViewGraph {
		return m.graphClick(x, y)
	}
	m.ensureFetch()
	visible := m.tree.Visible()
	i, ok := ui.RowAt(y, m.tree.Top(), headerRows, m.treeHeight(), len(visible))
	if !ok {
		m.clicks.Reset()
		return nil
	}
	row := visible[i]
	double := m.clicks.Double(i, m.now())
	m.tree.SetCursor(i)
	m.noteCursor()
	if x >= row.Depth()*2 && x < row.Depth()*2+2 && !double {
		// The marker cell: unfold or fold like the tree's own arrow.
		m.clicks.Reset()
		return m.tree.Toggle(row)
	}
	if !double {
		return nil
	}
	m.clicks.Reset()
	if cmd := m.open(row.Item); cmd != nil {
		return cmd
	}
	return m.tree.Toggle(row)
}

// headerRows is the session line above the tree.
const headerRows = 1

// treeHeight is the rows the tree lays out into: everything between the
// header and the hint line.
func (m *Model) treeHeight() int {
	h := m.height - headerRows - 1
	if h < 1 {
		h = 1
	}
	return h
}

func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

func (m *Model) display(p string) string {
	if m.displayPath != nil {
		return m.displayPath(p)
	}
	return p
}

// View draws the session line, the tree and the hint row, or the empty
// state's centered dialog.
func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	pal := m.theme()
	if m.picker.open {
		return m.pickerView(pal)
	}
	if !m.hasSession {
		return m.emptyView(pal)
	}
	clip := lipgloss.NewStyle().MaxWidth(m.width)
	lines := []string{clip.Render(m.headerLine(pal))}
	hint := "enter/double-click opens · space expands · h/l fold · t graph · s sessions · D diff · a ask · r rescan · Δ: V revert"
	if m.view == ViewGraph {
		lines = append(lines, m.graphRows(pal)...)
		hint = graphHint
	} else {
		m.ensureFetch()
		rows := m.tree.RenderRows(m.width, m.treeHeight(), pal, m.display, "(no events yet)")
		lines = append(lines, rows...)
	}
	if m.Stored() {
		hint = "esc/r back to live · " + hint
	}
	for len(lines) < headerRows+m.treeHeight() {
		lines = append(lines, "")
	}
	lines = append(lines, clip.Render(lipgloss.NewStyle().Faint(true).Render(hint)))
	return strings.Join(lines, "\n")
}

// headerLine names the session: id, how it was found, turn count, file.
func (m *Model) headerLine(pal *theme.Palette) string {
	id := m.info.ID
	if len(id) > 8 {
		id = id[:8]
	}
	source := "scan"
	if m.info.FromHook {
		source = "hook"
	}
	state := ""
	if m.info.Ended {
		state = " · ended"
	}
	turns := " · " + itoa(m.info.Turns) + " turn"
	if m.info.Turns != 1 {
		turns += "s"
	}
	if m.info.History != "" {
		// A stored session (#2860): the date is the headline, nothing is
		// read or followed.
		title := lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(" history · " + m.info.History)
		return title + lipgloss.NewStyle().Faint(true).Render(" · "+id+turns+" · esc live  "+m.display(m.info.Transcript))
	}
	title := lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(" " + id)
	return title + lipgloss.NewStyle().Faint(true).Render(" · "+source+turns+state+m.readStatus()+m.followStatus()+"  "+m.display(m.info.Transcript))
}

// readStatus is the header's liveness segment (#2857): the time of the last
// read and what it brought, "+0" included — a clock that keeps moving says
// the pane reads and the agent wrote nothing new.
func (m *Model) readStatus() string {
	if m.readAt.IsZero() {
		return " · not read yet"
	}
	return " · read " + m.readAt.Format("15:04:05") + " +" + itoa(m.readAdded)
}

// followStatus names the followed terminal and why.
func (m *Model) followStatus() string {
	if m.following == "" {
		return ""
	}
	return " · ⇢ " + m.following
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// dialog is the empty state's box: heading, explanation, the action strip
// and the key hints. It is laid out once per View/Click so the click hit
// test uses the geometry that was drawn.
type dialog struct {
	lines   []string // content lines, unstyled widths
	actions ui.Segmented
	actRow  int // index into lines of the action strip
	x, y    int // top-left of the content area on the pane
	w       int // content width
}

// actions of the dialog, in strip order.
const (
	actInstall = iota
	actRescan
)

func (m *Model) layoutDialog() (dialog, bool) {
	d := dialog{actions: ui.Segmented{Segments: []ui.Segment{
		{Label: "Install Claude hooks", On: true},
		{Label: "Rescan"},
	}}}
	where := m.cwd
	if where == "" {
		where = "this project"
	} else {
		where = m.display(where)
	}
	d.lines = []string{
		"No agent session",
		"",
		"No Claude Code transcript was found for " + where + ".",
		"Run claude in a tool pane, or install the hooks so sessions",
		"announce themselves to IKE the moment they start.",
	}
	if m.err != "" {
		d.lines = append(d.lines, "", m.err)
	}
	d.lines = append(d.lines, "")
	d.actRow = len(d.lines)
	// The strip itself is drawn from d.actions; the placeholder keeps the row.
	d.lines = append(d.lines, "", "", "i install · r rescan")
	for _, l := range d.lines {
		if w := lipgloss.Width(l); w > d.w {
			d.w = w
		}
	}
	if w := d.actions.Width(); w > d.w {
		d.w = w
	}
	// Rounded border + two cells of padding each side.
	boxW, boxH := d.w+6, len(d.lines)+2
	if boxW > m.width || boxH > m.height {
		return d, false
	}
	d.x = (m.width-boxW)/2 + 3
	d.y = (m.height-boxH)/2 + 1
	return d, true
}

// emptyView is the centered dialog (#2840): the missing session is
// actionable — start the agent or install the hooks — so it gets the
// prominent box the missing-tool states use, never a one-line notice. A
// pane too small for the box falls back to the plain notice.
func (m *Model) emptyView(pal *theme.Palette) string {
	if m.loading {
		note := lipgloss.NewStyle().Faint(true).Render("(locating the agent session…)")
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, note)
	}
	d, fits := m.layoutDialog()
	if !fits {
		note := lipgloss.NewStyle().Foreground(pal.Warning).Render("no agent session · i installs the Claude hooks · r rescans")
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().MaxWidth(m.width).Render(note))
	}
	// Lines are padded by hand rather than through Style.Width, which would
	// re-wrap them and move the action strip off the row the click test uses.
	styled := make([]string, len(d.lines))
	for i, l := range d.lines {
		pad := strings.Repeat(" ", max(0, d.w-lipgloss.Width(l)))
		switch {
		case i == 0:
			styled[i] = lipgloss.NewStyle().Bold(true).Foreground(pal.Warning).Render(l) + pad
		case i == d.actRow:
			styled[i] = d.actions.View(d.w, pal)
		case i == len(d.lines)-1:
			styled[i] = lipgloss.NewStyle().Faint(true).Render(l) + pad
		case m.err != "" && l == m.err:
			styled[i] = lipgloss.NewStyle().Foreground(pal.Error).Render(l) + pad
		default:
			styled[i] = lipgloss.NewStyle().Foreground(pal.Foreground).Render(l) + pad
		}
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Padding(0, 2).
		Render(strings.Join(styled, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// dialogClick maps a click onto the dialog's action strip.
func (m *Model) dialogClick(x, y int) tea.Cmd {
	if m.loading {
		return nil
	}
	d, fits := m.layoutDialog()
	if !fits || y != d.y+d.actRow {
		return nil
	}
	switch d.actions.At(x-d.x, d.w) {
	case actInstall:
		return func() tea.Msg { return InstallHooksMsg{} }
	case actRescan:
		return func() tea.Msg { return RefreshMsg{} }
	}
	return nil
}
