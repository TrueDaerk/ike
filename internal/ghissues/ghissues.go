// Package ghissues is the forge Issues tool window (#1934, restructured in
// #2090): a singleton pane with two full-area tabbed views — Issues and Pull
// Requests — over the current repository's listing. The issues view lists
// number, title, label chips, assignee, author and age with a fuzzy filter, a
// multi-select label picker, an open/closed/all state filter, sort orders and
// an optional grouping by label; enter opens the issue as a full-area detail
// that keeps the list's cursor and scroll and can walk to the next/previous
// issue without going back. The detail shows the issue's timeline under the
// body (#2084): comments rendered as markdown, label/state/assignee changes
// as compact events, fetched lazily page by page, and — with triage
// permission (#2088) — label, assignee and close/reopen mutations through two
// pickers and a comment prompt, applied optimistically and rolled back on a
// forge rejection. Texts that are the user's own can be edited from there too
// (#2087, edit.go): 'e' picks the issue body or one of your comments, 'c'
// composes a new one, and the app opens each in a markdown buffer that pushes
// when it is saved. The PR view lists the pull
// requests full width
// (number, title, head branch, CI rollup, review decision). Every action is
// discoverable through the footer and the action menu.
//
// It stays a pure consumer of internal/forge messages: the app injects the
// per-state fetch factory and routes forge.IssuesMsg results in; the pane
// itself never runs a subprocess — the edit actions emit a request message
// too, they do not talk to a forge.
package ghissues

import (
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
	"ike/internal/fuzzy"
	"ike/internal/host"
	"ike/internal/theme"
	"ike/internal/ui"
)

// OpenURLMsg asks the app to open url in the platform browser ('o').
type OpenURLMsg struct {
	URL string
}

// StartWorkRequestMsg asks the app to run the start-work flow for the
// selected issue ('s'); the app answers with forge.StartWorkCmd.
type StartWorkRequestMsg struct {
	Number int
	Title  string
}

// Tab selects one of the pane's two full-area views (#2090). The tab bar is
// the pane's first row; tab/shift+tab (and ctrl+pgdown/ctrl+pgup, the
// delivered chords IKE uses for tab walking elsewhere) switch between them,
// as does a click on a tab label.
type Tab int

const (
	// TabIssues is the issue list, the pane's default view.
	TabIssues Tab = iota
	// TabPRs is the pull-request list, full width. The PR *detail* is #2089's
	// scope; here enter opens the PR in the browser.
	TabPRs
)

// SortOrder is the list order both views share.
type SortOrder int

const (
	// SortRelevance ranks by fuzzy score while a filter pattern is typed and
	// falls back to SortNewest without one — the pane's pre-#2090 behaviour,
	// and the built-in default.
	SortRelevance SortOrder = iota
	// SortNewest orders by creation time, newest first.
	SortNewest
	// SortOldest orders by creation time, oldest first.
	SortOldest
	// SortUpdated orders by last update, most recently touched first.
	SortUpdated
	// SortNumber orders by issue/PR number, ascending.
	SortNumber
)

// sortOrders is the cycle order of the sort key ('a') and the value order of
// the issues.default_sort setting.
var sortOrders = []SortOrder{SortRelevance, SortNewest, SortOldest, SortUpdated, SortNumber}

// String is the name shown in the filter row and stored in the setting.
func (s SortOrder) String() string {
	switch s {
	case SortNewest:
		return "newest"
	case SortOldest:
		return "oldest"
	case SortUpdated:
		return "updated"
	case SortNumber:
		return "number"
	default:
		return "relevance"
	}
}

// parseSort reads a issues.default_sort value; anything unknown (an older
// config, a typo the config layer already warned about) reads as relevance.
func parseSort(s string) SortOrder {
	for _, o := range sortOrders {
		if o.String() == s {
			return o
		}
	}
	return SortRelevance
}

// StateFilter gates the listing by issue/PR state. For issues it also selects
// what the next fetch asks the forge for — closed issues are not in an open
// listing, so changing it refetches (the #2083 listing extension).
type StateFilter int

const (
	// FilterOpen shows open issues / open PRs.
	FilterOpen StateFilter = iota
	// FilterClosed shows closed issues / merged and closed PRs.
	FilterClosed
	// FilterAll shows every state.
	FilterAll
)

// String is the name shown in the filter row.
func (s StateFilter) String() string {
	switch s {
	case FilterClosed:
		return "closed"
	case FilterAll:
		return "all"
	default:
		return "open"
	}
}

// issueState maps the state filter onto the forge listing state the fetch
// asks for.
func (s StateFilter) issueState() forge.IssueState {
	switch s {
	case FilterClosed:
		return forge.IssuesClosed
	case FilterAll:
		return forge.IssuesAll
	default:
		return forge.IssuesOpen
	}
}

// overlayKind is the modal that owns the keyboard on top of a view: the label
// multi-picker, the action menu, or the edit picker. All render as a centered
// box over the body and are dismissed with esc.
type overlayKind int

const (
	ovNone overlayKind = iota
	ovLabels
	ovActions
	// ovLabelEdit and ovAssignEdit are the mutation pickers (#2088): the
	// repository's labels / assignable users toggled against the selected
	// issue's current set, applied as a diff on enter.
	ovLabelEdit
	ovAssignEdit
	// ovComment is the one-line prompt of the close/reopen-with-comment
	// action (#2088).
	ovComment
	// ovTextEdit is the text-edit picker (#2087): which of the issue's texts
	// — the body, one of your comments — the markdown edit buffer opens.
	ovTextEdit
)

// listRow is one rendered row of a view: either a group header (header set,
// idx -1) or an entry pointing into the issue/PR slice. Grouping by label
// (#2090) inserts headers, so the cursor walks rows while the filter set
// stays a plain index list.
type listRow struct {
	header string
	idx    int
}

// Model is the tool window state. Value type with pointer-receiver mutators,
// embedded in a pane.Instance like the Usages panel (#1155).
type Model struct {
	width   int
	height  int
	focused bool
	pal     *theme.Palette

	// refresh is the app-injected per-state fetch factory
	// (forge.RefreshFactory at the workspace root); 'r' and every state-filter
	// change re-run it, and the pane stays subprocess-free.
	refresh func(forge.IssueState) tea.Cmd

	loading bool
	loaded  bool
	setup   string // unavailable state: no forge CLI, no matching remote
	errMsg  string // transient fetch error; prior content stays

	issues []forge.Issue
	prs    []forge.PR

	// tab is the active view; each view keeps its own cursor and scroll so
	// switching back lands where it left off.
	tab Tab

	// visible indexes the issues surviving the filters in display order;
	// rows adds the group headers the renderer and the cursor walk.
	visible []int
	rows    []listRow
	cursor  int
	top     int

	// The PR view's mirror of the same three.
	prVisible []int
	prRows    []listRow
	prCursor  int
	prTop     int

	// The filter line, the dataview pattern (#1777): while open it owns the
	// keyboard and the match set narrows live. It starts on 'f' — the '/' of
	// other tools needs Shift on a QWERTZ layout (#48) — with '/' kept as an
	// alias for muscle memory.
	fEditing bool
	fInput   string
	fCur     int

	// labels are the distinct label names across the listing; labelSel is the
	// multi-select set the label picker edits (an issue passes when it
	// carries *any* selected label).
	labels   []string
	labelSel map[string]bool

	state StateFilter
	sort  SortOrder
	group bool // group the issue list by label

	// Overlay state: which modal owns the keyboard, its cursor, and the
	// label selection esc restores.
	ov       overlayKind
	ovCursor int
	ovTop    int
	ovSaved  map[string]bool

	// Detail view: the selected issue's body rendered through glamour,
	// re-rendered lazily when the issue, the width or the timeline changes.
	// The list cursor and scroll are untouched while it is open, so esc
	// restores them exactly.
	detail      bool
	detailFor   int // issue number the lines were rendered for
	detailW     int // width they were rendered at
	detailRev   int // tlRev the lines were rendered at
	detailLines []string
	detailTop   int

	// Timeline (#2084): the open issue's history, fetched page by page
	// through the app-injected factory; tlRev bumps on every state change and
	// invalidates the rendered detail lines.
	timeline  func(issue, page int) tea.Cmd
	tl        []forge.TimelineEntry
	tlFor     int // issue number the entries belong to; 0 = none fetched
	tlPage    int // last fetched page; 0 = page one still loading (or unfetched)
	tlMore    bool
	tlLoading bool
	tlErr     string
	tlRev     int

	// Mutations (#2088): the app-injected write factory and the one-shot
	// repository metadata probe behind the label/assignee pickers and the
	// capability gate.
	mutate  func(forge.Mutation) tea.Cmd
	meta    func() tea.Cmd
	caps    forge.Capabilities
	capsOK  bool // a capability probe answered
	metaRun bool // the probe was started (and not retried unless it failed)

	repoLabels []forge.Label // the repository's whole label set
	repoUsers  []string      // the assignable logins

	// editSel is the working set of the open mutation picker; editFor is the
	// issue it edits. mutBusy counts the writes in flight, mutErr is the last
	// forge rejection, and rollback holds the pre-mutation issues an optimistic
	// update has to be undone to when one fails.
	editSel  map[string]bool
	editFor  int
	mutBusy  int
	mutErr   string
	rollback map[int]forge.Issue

	// The close/reopen-with-comment prompt's buffer and cursor.
	cmInput string
	cmCur   int

	// Config defaults apply only while the user has not overridden them in
	// this session, so a live config reload never yanks the view away.
	tabTouched  bool
	sortTouched bool

	// Double-click detection mirrors the Usages panel (#514).
	lastClickRow int
	lastClickAt  time.Time
	now          func() time.Time
}

// New returns an empty pane; content arrives via SetResult.
func New(pal *theme.Palette) Model {
	return Model{pal: pal, labelSel: map[string]bool{}, lastClickRow: -1, now: time.Now}
}

// SetSize records the interior content size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetFocused marks the pane focused (header + selection highlight).
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the active theme.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// SetRefresh injects the per-state fetch factory 'r' and the state filter
// re-run.
func (m *Model) SetRefresh(fn func(forge.IssueState) tea.Cmd) { m.refresh = fn }

// Configure applies the pane's settings (#2090): issues.default_tab and
// issues.default_sort. Both only seed the session — once the user switched
// the tab or the sort order by hand, a later config reload leaves the view
// where it is.
func (m *Model) Configure(cfg host.Config) {
	if cfg == nil {
		return
	}
	if !m.tabTouched {
		if v, ok := cfg.Get("issues.default_tab"); ok && v == "prs" {
			m.tab = TabPRs
		} else if ok {
			m.tab = TabIssues
		}
	}
	if !m.sortTouched {
		if v, ok := cfg.Get("issues.default_sort"); ok {
			m.sort = parseSort(v)
		}
	}
	m.applyFilter()
}

// MarkLoading flips the pane into its fetching state (the app calls it when
// it dispatches the refresh command itself, on open).
func (m *Model) MarkLoading() { m.loading = true }

// Refresh marks the pane loading and returns the fetch for its current state
// filter — the app's on-open first fetch, identical to what 'r' runs.
func (m *Model) Refresh() tea.Cmd { return m.startRefresh() }

// SetResult applies one finished fetch.
func (m *Model) SetResult(msg forge.IssuesMsg) {
	m.loading = false
	m.setup = msg.Setup
	if msg.Setup != "" {
		return
	}
	if msg.Err != nil {
		m.errMsg = msg.Err.Error()
		return
	}
	m.errMsg = ""
	m.loaded = true
	m.issues = msg.Issues
	m.prs = msg.PRs
	m.rebuildLabels()
	m.applyFilter()
}

// Loaded reports whether a listing ever arrived (tests).
func (m *Model) Loaded() bool { return m.loaded }

// Visible reports how many issues survive the filters (tests).
func (m *Model) Visible() int { return len(m.visible) }

// VisiblePRs reports how many pull requests survive the filters (tests).
func (m *Model) VisiblePRs() int { return len(m.prVisible) }

// Cursor reports the selected row of the active view (tests).
func (m *Model) Cursor() int {
	if m.tab == TabPRs {
		return m.prCursor
	}
	return m.cursor
}

// Top reports the scroll offset of the active view (tests).
func (m *Model) Top() int {
	if m.tab == TabPRs {
		return m.prTop
	}
	return m.top
}

// ActiveTab reports the visible view (tests).
func (m *Model) ActiveTab() Tab { return m.tab }

// Filter returns the fuzzy pattern, "" when unfiltered (tests).
func (m *Model) Filter() string { return m.fInput }

// Filtering reports whether the filter line is open (tests).
func (m *Model) Filtering() bool { return m.fEditing }

// LabelFilter returns the selected labels in listing order (tests).
func (m *Model) LabelFilter() []string {
	var out []string
	for _, name := range m.labels {
		if m.labelSel[name] {
			out = append(out, name)
		}
	}
	return out
}

// StateFilter returns the active open/closed/all gate (tests).
func (m *Model) StateFilter() StateFilter { return m.state }

// SortOrder returns the active list order (tests).
func (m *Model) SortOrder() SortOrder { return m.sort }

// Grouped reports whether the issue list groups by label (tests).
func (m *Model) Grouped() bool { return m.group }

// DetailOpen reports whether the detail view is showing (tests).
func (m *Model) DetailOpen() bool { return m.detail }

// PickerOpen reports whether the label picker owns the keyboard (tests).
func (m *Model) PickerOpen() bool { return m.ov == ovLabels }

// ActionMenuOpen reports whether the action menu is showing (tests).
func (m *Model) ActionMenuOpen() bool { return m.ov == ovActions }

// Selected returns the issue under the issue view's cursor, nil when the row
// is a group header or the list is empty.
func (m *Model) Selected() *forge.Issue {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	i := m.rows[m.cursor].idx
	if i < 0 || i >= len(m.issues) {
		return nil
	}
	return &m.issues[i]
}

// SelectedPR returns the pull request under the PR view's cursor, nil when
// the list is empty.
func (m *Model) SelectedPR() *forge.PR {
	if m.prCursor < 0 || m.prCursor >= len(m.prRows) {
		return nil
	}
	i := m.prRows[m.prCursor].idx
	if i < 0 || i >= len(m.prs) {
		return nil
	}
	return &m.prs[i]
}

// Position is the selected issue's 1-based place in the filtered listing and
// that listing's size — the "issue x/y" the detail header shows so the list
// context survives opening one.
func (m *Model) Position() (int, int) {
	total := len(m.visible)
	sel := m.Selected()
	if sel == nil {
		return 0, total
	}
	for n, idx := range m.visible {
		if idx == m.rows[m.cursor].idx {
			return n + 1, total
		}
	}
	return 0, total
}

// rebuildLabels recomputes the distinct label names, dropping selections of
// labels the new listing no longer has.
func (m *Model) rebuildLabels() {
	seen := map[string]bool{}
	m.labels = nil
	for _, is := range m.issues {
		for _, l := range is.Labels {
			if !seen[l.Name] {
				seen[l.Name] = true
				m.labels = append(m.labels, l.Name)
			}
		}
	}
	sort.Strings(m.labels)
	for name := range m.labelSel {
		if !seen[name] {
			delete(m.labelSel, name)
		}
	}
}

// labelCount is how many issues carry the named label, shown in the picker.
func (m *Model) labelCount(name string) int {
	n := 0
	for i := range m.issues {
		if hasLabel(&m.issues[i], name) {
			n++
		}
	}
	return n
}

// hasLabel reports whether the issue carries the named label.
func hasLabel(is *forge.Issue, name string) bool {
	for _, l := range is.Labels {
		if l.Name == name {
			return true
		}
	}
	return false
}

// Update handles one message while the pane exists; only key presses reach
// it, focus-filtered by the pane layer.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		return m.handleKey(k)
	}
	return nil
}

// handleKey routes one key to whichever layer owns the keyboard: the filter
// line first, then an open overlay, then the detail view, then the list.
func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case m.fEditing:
		m.filterKey(msg)
		return nil
	case m.ov != ovNone:
		return m.overlayKey(msg)
	case m.detail:
		return m.detailKey(msg)
	default:
		return m.listKey(msg)
	}
}

// listKey handles the active list view.
func (m *Model) listKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	switch key {
	case "tab", "ctrl+pgdown":
		m.switchTab(1)
		return nil
	case "shift+tab", "ctrl+pgup":
		m.switchTab(-1)
		return nil
	}
	// Shared list semantics (#1666) minus the g/G extremes: the pane spends
	// 'g' on the grouping toggle, home/end still jump. Group headers are
	// skipped in the direction the key moved.
	if m.navList(key) {
		return nil
	}
	switch key {
	case "r":
		return m.startRefresh()
	case "f", "/":
		m.openFilter()
	case "l":
		m.openLabelPicker()
	case "t":
		return m.cycleState()
	case "a":
		m.cycleSort()
	case "g":
		m.toggleGroup()
	case "m", "?":
		m.openActionMenu()
	case "esc":
		return m.clearFilters()
	case "enter":
		return m.activate()
	case "s":
		return m.startWork()
	case "o":
		return m.openInBrowser()
	}
	return m.mutationKey(key)
}

// mutationKey routes the write keys (#2088), which the list and the detail
// view share: 'e' labels, 'u' assignees, 'c' close/reopen, 'C' the same with
// a comment. Each checks the capability gate itself, so a key without
// permission explains rather than doing nothing.
func (m *Model) mutationKey(key string) tea.Cmd {
	if m.mutate == nil || m.tab != TabIssues {
		return nil
	}
	switch key {
	case "e":
		return m.openLabelEditor()
	case "u":
		return m.openAssigneeEditor()
	case "c":
		return m.toggleIssueState()
	case "C":
		return m.openCommentPrompt()
	}
	return nil
}

// navList routes one navigation key against the active view's cursor and
// reports whether it consumed it, snapping off a group header afterwards.
func (m *Model) navList(key string) bool {
	rows, cur := m.rowsOf(m.tab), m.Cursor()
	if !ui.ListNav(key, &cur, len(rows), m.bodyHeight(), ui.NavDefault|ui.NavVim) {
		return false
	}
	m.setCursor(snapRow(rows, cur, navDir(key)))
	m.clampScroll()
	return true
}

// navDir is the direction a navigation key moved in, used to step off a group
// header the cursor landed on. Wrapping makes the before/after comparison
// unreliable, so the key itself decides.
func navDir(key string) int {
	switch key {
	case "up", "k", "ctrl+p", "pgup", "end":
		return -1
	}
	return 1
}

// snapRow moves idx off a group header, first in dir, then the other way (a
// header is always followed by at least one entry, so the first pass wins
// unless the list is header-only).
func snapRow(rows []listRow, idx, dir int) int {
	if len(rows) == 0 {
		return 0
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(rows) {
		idx = len(rows) - 1
	}
	for i := idx; i >= 0 && i < len(rows); i += dir {
		if rows[i].idx >= 0 {
			return i
		}
	}
	for i := idx; i >= 0 && i < len(rows); i -= dir {
		if rows[i].idx >= 0 {
			return i
		}
	}
	return idx
}

// rowsOf returns the rendered rows of one view.
func (m *Model) rowsOf(t Tab) []listRow {
	if t == TabPRs {
		return m.prRows
	}
	return m.rows
}

// setCursor writes the active view's cursor.
func (m *Model) setCursor(v int) {
	if m.tab == TabPRs {
		m.prCursor = v
		return
	}
	m.cursor = v
}

// setTop writes the active view's scroll offset.
func (m *Model) setTop(v int) {
	if m.tab == TabPRs {
		m.prTop = v
		return
	}
	m.top = v
}

// switchTab moves delta tabs along, wrapping. Each view keeps its cursor and
// scroll, and leaving the issues view closes an open detail so returning
// lands on the list.
func (m *Model) switchTab(delta int) {
	m.detail = false
	m.tabTouched = true
	next := (int(m.tab) + delta + 2) % 2
	m.tab = Tab(next)
	m.clampScroll()
}

// SetTab switches to t directly (the tab-bar click target, #2090).
func (m *Model) SetTab(t Tab) {
	if t == m.tab {
		return
	}
	m.detail = false
	m.tabTouched = true
	m.tab = t
	m.clampScroll()
}

// openFilter opens the filter line with the cursor at the end of the pattern.
func (m *Model) openFilter() {
	m.fEditing = true
	m.fCur = len([]rune(m.fInput))
}

// filterKey feeds one key to the open filter line, which owns the keyboard:
// esc clears and closes, enter keeps the filter and closes, everything else
// edits and re-narrows live.
func (m *Model) filterKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "esc":
		m.fEditing, m.fInput, m.fCur = false, "", 0
		m.applyFilter()
	case "enter":
		m.fEditing = false
	default:
		if out, ncur, handled, changed := ui.EditKey(msg, m.fInput, m.fCur); handled {
			m.fInput, m.fCur = out, ncur
			if changed {
				m.resetCursors()
				m.applyFilter()
			}
		}
	}
}

// detailKey handles the full-area detail view: scroll, back to the list with
// the cursor untouched, the issue-walking chords, and the actions that stay
// meaningful with an issue on screen.
func (m *Model) detailKey(msg tea.KeyPressMsg) tea.Cmd {
	page := m.bodyHeight()
	switch msg.String() {
	case "esc", "q", "backspace":
		m.detail = false
	case "ctrl+j":
		return m.stepIssue(1)
	case "ctrl+k":
		return m.stepIssue(-1)
	case "L":
		return m.loadMoreTimeline()
	case "E":
		return m.startTextEdit()
	case "n":
		return m.startComment()
	case "j", "down":
		m.detailTop++
	case "k", "up":
		m.detailTop--
	case "pgdown", "ctrl+d", "space":
		m.detailTop += page
	case "pgup", "ctrl+u":
		m.detailTop -= page
	case "g", "home":
		m.detailTop = 0
	case "G", "end":
		m.detailTop = len(m.detailLines) - page
	case "m", "?":
		m.openActionMenu()
	case "tab", "ctrl+pgdown":
		m.switchTab(1)
	case "shift+tab", "ctrl+pgup":
		m.switchTab(-1)
	case "r":
		return m.refreshDetail()
	case "s":
		return m.startWork()
	case "o":
		return m.openInBrowser()
	default:
		if cmd := m.mutationKey(msg.String()); cmd != nil {
			return cmd
		}
	}
	m.clampDetail()
	return nil
}

// refreshDetail is 'r' inside the detail view: refetch the listing and the
// open issue's timeline together (#2084).
func (m *Model) refreshDetail() tea.Cmd {
	return tea.Batch(m.startRefresh(), m.refetchTimeline())
}

// nextIssue / prevIssue are the action-menu spellings of the walking chords.
func (m *Model) nextIssue() tea.Cmd { return m.stepIssue(1) }
func (m *Model) prevIssue() tea.Cmd { return m.stepIssue(-1) }

// stepIssue walks to the next/previous issue from inside the detail view,
// moving the list cursor with it so esc still returns to the issue on screen.
// It returns the timeline fetch the newly shown issue needs.
func (m *Model) stepIssue(delta int) tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	cur := m.cursor
	for i := 0; i < len(m.rows); i++ {
		cur = ui.StepIndex(cur, delta, len(m.rows))
		if m.rows[cur].idx >= 0 {
			break
		}
	}
	m.cursor = cur
	m.clampScroll()
	m.detailTop = 0
	return m.PendingTimelineCmd()
}

// startRefresh re-runs the injected fetch for the current state filter, plus
// the repository-metadata probe (#2088) while it is still owed — a capability
// probe that failed is retried by the same 'r' that retries the listing.
func (m *Model) startRefresh() tea.Cmd {
	meta := m.startMeta()
	if m.refresh == nil {
		return meta
	}
	m.loading = true
	return tea.Batch(m.refresh(m.state.issueState()), meta)
}

// cycleState advances the open/closed/all filter. Closed issues are not part
// of an open listing, so the change also refetches (#2083).
func (m *Model) cycleState() tea.Cmd {
	m.state = StateFilter((int(m.state) + 1) % 3)
	m.resetCursors()
	m.applyFilter()
	return m.startRefresh()
}

// cycleSort advances the list order.
func (m *Model) cycleSort() {
	m.sortTouched = true
	m.sort = SortOrder((int(m.sort) + 1) % len(sortOrders))
	m.resetCursors()
	m.applyFilter()
}

// toggleGroup flips the issue list's grouping by label.
func (m *Model) toggleGroup() {
	m.group = !m.group
	m.resetCursors()
	m.applyFilter()
}

// clearFilters drops every narrowing the list carries (esc on the list): the
// fuzzy pattern, the label selection and a non-open state filter. It returns
// nothing to run — the state filter change alone would need a refetch, so it
// is folded into the caller's refresh through startRefresh.
func (m *Model) clearFilters() tea.Cmd {
	refetch := m.state != FilterOpen
	// esc also dismisses a mutation error the filter row is holding (#2088).
	m.mutErr = ""
	m.fInput, m.fCur = "", 0
	m.labelSel = map[string]bool{}
	m.state = FilterOpen
	m.resetCursors()
	m.applyFilter()
	if refetch {
		return m.startRefresh()
	}
	return nil
}

// resetCursors sends both views back to the top, the response to any change
// of the match set.
func (m *Model) resetCursors() {
	m.cursor, m.top = 0, 0
	m.prCursor, m.prTop = 0, 0
}

// activate runs the enter action of the active view: the issue detail (with
// its lazy timeline fetch, #2084), or — until #2089 brings the PR detail —
// the PR's page in the browser.
func (m *Model) activate() tea.Cmd {
	if m.tab == TabPRs {
		return m.openInBrowser()
	}
	m.openDetail()
	return m.PendingTimelineCmd()
}

// openDetail flips into the detail view for the selected issue.
func (m *Model) openDetail() {
	if m.Selected() == nil {
		return
	}
	m.detail = true
	m.detailTop = 0
}

// Reveal jumps straight to the issue with this number and opens its detail
// view (#2086): the forge event dialog's "open" action lands the user on the
// issue that was announced, not on whatever row the cursor happened to sit on.
// Active filters are dropped first — an issue hidden by a filter must still be
// reachable. False when the listing does not (yet) carry the number, so the
// caller can leave the pane as it is.
func (m *Model) Reveal(number int) bool {
	idx := -1
	for i := range m.issues {
		if m.issues[i].Number == number {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false
	}
	m.tab, m.ov, m.ovSaved = TabIssues, ovNone, nil
	m.fEditing, m.fInput, m.fCur = false, "", 0
	m.labelSel = map[string]bool{}
	// The issue is already in the listing, so widening the state gate needs no
	// refetch — and a closed issue announced by an event must still be shown.
	if !stateAllows(m.state, m.issues[idx].State) {
		m.state = FilterAll
	}
	m.applyFilter()
	for pos, r := range m.rows {
		if r.idx == idx {
			m.cursor = pos
			break
		}
	}
	m.clampScroll()
	m.openDetail()
	return true
}

// startWork asks the app to branch off for the selected issue.
func (m *Model) startWork() tea.Cmd {
	is := m.Selected()
	if is == nil {
		return nil
	}
	msg := StartWorkRequestMsg{Number: is.Number, Title: is.Title}
	return func() tea.Msg { return msg }
}

// openInBrowser asks the app to open the selected row's page.
func (m *Model) openInBrowser() tea.Cmd {
	url := ""
	if m.tab == TabPRs {
		if pr := m.SelectedPR(); pr != nil {
			url = pr.URL
		}
	} else if is := m.Selected(); is != nil {
		url = is.URL
	}
	if url == "" {
		return nil
	}
	msg := OpenURLMsg{URL: url}
	return func() tea.Msg { return msg }
}

// chromeRows counts the non-body rows: the tab bar, the footer, the filter
// row when it shows, and the detail view's position header.
func (m *Model) chromeRows() int {
	n := 2
	if m.filterRowShown() {
		n++
	}
	if m.detail && m.tab == TabIssues {
		n++
	}
	return n
}

// bodyHeight is the room the list or the detail body gets.
func (m *Model) bodyHeight() int {
	h := m.height - m.chromeRows()
	if h < 1 {
		h = 1
	}
	return h
}

// bodyTop is the first body row's pane-local y, the offset mouse hits need.
func (m *Model) bodyTop() int {
	n := 1
	if m.filterRowShown() {
		n++
	}
	if m.detail && m.tab == TabIssues {
		n++
	}
	return n
}

// clampScroll keeps the active view's cursor valid and inside its window.
func (m *Model) clampScroll() {
	rows := m.rowsOf(m.tab)
	cur, top := m.Cursor(), m.Top()
	if cur > len(rows)-1 {
		cur = len(rows) - 1
	}
	if cur < 0 {
		cur = 0
	}
	if len(rows) > 0 && cur < len(rows) && rows[cur].idx < 0 {
		cur = snapRow(rows, cur, 1)
	}
	if top > cur {
		top = cur
	}
	if h := m.bodyHeight(); cur >= top+h {
		top = cur - h + 1
	}
	if top < 0 {
		top = 0
	}
	m.setCursor(cur)
	m.setTop(top)
}

// clampDetail bounds the detail scroll.
func (m *Model) clampDetail() {
	max := len(m.detailLines) - m.bodyHeight()
	if max < 0 {
		max = 0
	}
	if m.detailTop > max {
		m.detailTop = max
	}
	if m.detailTop < 0 {
		m.detailTop = 0
	}
}

// clip bounds one rendered line to the pane width (plain text only).
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

// clock is the pane's time source, injectable for the age column's tests.
func (m *Model) clock() time.Time {
	if m.now == nil {
		return time.Now()
	}
	return m.now()
}

// PasteText inserts a pasted block into the open filter line at its cursor
// (#2002) and re-narrows the list, exactly like typing there does.
func (m *Model) PasteText(text string) bool {
	if !m.fEditing {
		return false
	}
	out, ncur, changed := ui.PasteText(m.fInput, m.fCur, text)
	if !changed {
		return false
	}
	m.fInput, m.fCur = out, ncur
	m.resetCursors()
	m.applyFilter()
	return true
}

// matchText is the haystack one issue exposes to the fuzzy filter: number,
// title, labels, assignees and author, so "#19 fable" or a label name both
// narrow.
func matchText(is *forge.Issue) string {
	var b strings.Builder
	b.WriteString("#" + strconv.Itoa(is.Number) + " " + is.Title)
	for _, l := range is.Labels {
		b.WriteString(" " + l.Name)
	}
	for _, a := range is.Assignees {
		b.WriteString(" " + a)
	}
	if is.Author != "" {
		b.WriteString(" " + is.Author)
	}
	return b.String()
}

// prMatchText is the PR view's haystack: number, title, head branch, author.
func prMatchText(pr *forge.PR) string {
	s := "#" + strconv.Itoa(pr.Number) + " " + pr.Title + " " + pr.HeadRef
	if pr.Author != "" {
		s += " " + pr.Author
	}
	return s
}

// fuzzyGate scores one haystack against the current pattern. Without a
// pattern everything passes with score 0.
func (m *Model) fuzzyGate(hay string) (int, bool) {
	if m.fInput == "" {
		return 0, true
	}
	res, ok := fuzzy.Match(m.fInput, hay)
	if !ok {
		return 0, false
	}
	return res.Score, true
}
