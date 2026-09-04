// Package ghissues is the forge Issues tool window (#1934, restructured in
// #2090): a singleton pane with two full-area tabbed views — Issues and Pull
// Requests — over the current repository's listing. The issues view lists
// number, title, label chips, assignee, author and age; every narrowing —
// fuzzy match, open/closed/all state, sort order, label multi-select and an
// optional grouping by label — lives in one filter overlay (#2104,
// filterov.go), with the active filters rendered as individually clearable
// chips in a permanent status row; enter opens the issue as a full-area detail
// that keeps the list's cursor and scroll and can walk to the next/previous
// issue without going back. The detail shows the issue's timeline under the
// body (#2084): comments rendered as markdown, label/state/assignee changes
// as compact events, fetched lazily page by page, and — with triage
// permission (#2088) — label, assignee and close/reopen mutations through two
// pickers and a comment prompt, applied optimistically and rolled back on a
// forge rejection. Texts that are the user's own can be edited from there too
// (#2087, textedit.go), and the app opens each in a markdown buffer that
// pushes when it is saved. Since #2114 every editing surface sits behind one
// key: 'e' raises the unified edit picker (labels, assignees, the body, your
// comments, a new comment), 'n' composes a comment directly, 'c'/'C'
// close/reopen (with a comment). The PR view lists the pull requests full width (number,
// title, head branch, CI rollup, review decision); enter opens a full-area PR
// detail (#2089, prdetail.go) — markdown description, per-check CI status,
// the linked Closes-#N issue — and, with push permission, the merge- and
// close-with-comment actions behind a confirm dialog, followed by an offered
// (never automatic) post-merge branch cleanup. Every action is discoverable
// through the footer and the action menu.
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
	"github.com/charmbracelet/x/ansi"

	"ike/internal/forge"
	"ike/internal/fuzzy"
	"ike/internal/host"
	"ike/internal/textsel"
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
}

// Tab selects one of the pane's two full-area views (#2090). The tab bar is
// the pane's first row; tab/shift+tab (and ctrl+pgdown/ctrl+pgup, the
// delivered chords IKE uses for tab walking elsewhere) switch between them,
// as does a click on a tab label.
type Tab int

const (
	// TabIssues is the issue list, the pane's default view.
	TabIssues Tab = iota
	// TabPRs is the pull-request list, full width; enter opens the full-area
	// PR detail (#2089).
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

// overlayKind is the modal that owns the keyboard on top of a view: the
// unified filter overlay, the action menu, or the edit picker. All render as
// a centered box over the body and are dismissed with esc.
type overlayKind int

const (
	ovNone overlayKind = iota
	// ovFilter is the unified filter overlay (#2104, filterov.go): match
	// text, state gate, sort order, grouping and the label multi-select in
	// one modal, applied live.
	ovFilter
	ovActions
	// ovLabelEdit and ovAssignEdit are the mutation pickers (#2088): the
	// repository's labels / assignable users toggled against the selected
	// issue's current set, applied as a diff on enter.
	ovLabelEdit
	ovAssignEdit
	// ovComment is the one-line prompt of the close/reopen-with-comment
	// action (#2088).
	ovComment
	// ovEdit is the unified edit picker (#2087, consolidated in #2114): what
	// 'e' edits — the label and assignee pickers, the issue's own texts (the
	// body, one of your comments) opening as markdown edit buffers, a new
	// comment.
	ovEdit
	// ovPRAct is the merge/close-with-comment dialog (#2089): an optional
	// comment stage followed by an explicit confirm naming PR, branches and
	// merge method — the action is irreversible.
	ovPRAct
	// ovCleanup is the post-merge offer of the change-workflow branch cleanup
	// (#2089) — offered, never automatic.
	ovCleanup
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
	// change re-run it, and the pane stays subprocess-free. It takes the
	// generation the request is dispatched at (#2107) and echoes it back on
	// the result, plus whether the fetch must be a full resync (#2108) — true
	// for everything the user asked for, false only for the on-open fetch,
	// which may take the incremental path.
	refresh func(forge.IssueState, int, bool) tea.Cmd

	// gen counts the listing fetches the pane has started (#2107). Fetches
	// resolve off the Update loop and out of order, so two rapid 't' presses
	// leave two in flight; only the answer tagged with the newest generation
	// may write the listing — the older one was asked for a state filter the
	// pane no longer shows.
	gen int

	loading bool
	loaded  bool
	cached  bool   // the listing is the persisted snapshot (#2108), not a fetch
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

	// The fuzzy match pattern, edited on the filter overlay's match row
	// (#2104, filterov.go). It starts on 'f' — the '/' of other tools needs
	// Shift on a QWERTZ layout (#48) — with '/' kept as an alias.
	fInput ui.Field
	// matchStatus is where the cmd+g walk over the narrowed list stands
	// (#2410), shown on the match row; every edit of fInput drops it.
	matchStatus string
	// fSaved is what esc inside the filter overlay restores; fetched is the
	// state the current listing was fetched for, so a state-filter change
	// only refetches when the listing cannot answer it.
	fSaved  filterSnapshot
	fetched forge.IssueState

	// labels are the distinct label names across the listing; labelSel is the
	// multi-select set the label picker edits. labelAll switches the
	// selection's semantics (#2112): off it is an OR filter (an issue passes
	// when it carries *any* selected label), on it is an AND filter (it must
	// carry *all* of them).
	labels   []string
	labelSel map[string]bool
	labelAll bool

	state StateFilter
	sort  SortOrder
	group bool // group the issue list by label

	// Overlay state: which modal owns the keyboard and its cursor.
	ov       overlayKind
	ovCursor int
	ovTop    int
	// ovSearch is the open picker's type-ahead (#2111): printable keys
	// narrow the visible rows live, esc clears the query before it closes
	// the modal. Reset whenever a modal opens, so one picker never inherits
	// the previous one's query.
	ovSearch ui.SpeedSearch

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
	tlWant    int // pages the current fetch run owes (depth kept across 'r', #2113)
	tlMore    bool
	tlLoading bool
	tlErr     string
	tlRev     int

	// PR detail (#2089): the app-injected per-PR fetch and write factories,
	// the open detail's fetched data, its rendered lines and scroll, and the
	// merge/close dialog's state. prdRev bumps on every data change and
	// invalidates the rendered lines, mirroring the issue detail.
	prFetch  func(pr int) tea.Cmd
	prAction func(forge.PRAction) tea.Cmd

	prDetail   bool
	prd        *forge.PRDetail
	prdFor     int // PR number the fetch belongs to; 0 = none
	prdLoading bool
	prdErr     string
	prdRev     int

	prdRenderFor int // PR number the lines were rendered for
	prdW         int // width they were rendered at
	prdRenderRev int // prdRev they were rendered at
	prdLines     []string
	prdTop       int

	// The merge/close dialog: which action, its two stages (comment, then
	// confirm), and — after a successful merge — the head branch the cleanup
	// offer names.
	prActKind     string
	prActStage    int
	prActFor      int
	prActHead     string
	prActBase     string
	cleanupBranch string

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
	cmInput ui.Field

	// saved are the configured named filters (#2115, issues.saved_filters).
	// Which one is "active" is derived from the live filter, not remembered —
	// see SavedFilter.
	saved []savedFilter

	// Config defaults apply only while the user has not overridden them in
	// this session, so a live config reload never yanks the view away.
	tabTouched    bool
	sortTouched   bool
	filterTouched bool

	// Double-click detection mirrors the Usages panel (#514).
	clicks ui.ClickTracker
	now    func() time.Time

	// Mouse text selection in the two detail views (#2374, selection.go),
	// on the shared engine the diff and merge views use. selTab/selNum are
	// the view and the issue/PR number the span was taken in, so a stale
	// selection retires itself; selDrag marks a drag in progress and
	// selX/selY the last pointer cell, which a wheel during the drag
	// re-resolves against the new scroll offset.
	sel        textsel.Selection
	selTab     Tab
	selNum     int
	selDrag    bool
	selX, selY int
}

// New returns an empty pane; content arrives via SetResult.
func New(pal *theme.Palette) Model {
	return Model{pal: pal, labelSel: map[string]bool{}, now: time.Now}
}

// SetSize records the interior content size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetFocused marks the pane focused (header + selection highlight).
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the active theme.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// SetRefresh injects the per-state fetch factory 'r' and the state filter
// re-run.
func (m *Model) SetRefresh(fn func(forge.IssueState, int, bool) tea.Cmd) { m.refresh = fn }

// Configure applies the pane's settings (#2090, #2115): issues.default_tab,
// issues.default_sort, issues.default_filter and issues.saved_filters. The
// three defaults only seed the session — once the user switched the tab, the
// sort order or a filter by hand, a later config reload leaves the view where
// it is. The saved filter list is not a default but a menu, so it always
// re-reads.
func (m *Model) Configure(cfg host.Config) {
	if cfg == nil {
		return
	}
	if v, ok := cfg.Get("issues.saved_filters"); ok {
		m.seedSavedFilters(v)
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
	// The filter seeds last: its optional sort: qualifier is the more
	// specific setting and wins over issues.default_sort.
	if !m.filterTouched {
		if v, ok := cfg.Get("issues.default_filter"); ok {
			m.seedFilter(v)
		}
	}
	m.applyFilter()
}

// MarkLoading flips the pane into its fetching state (the app calls it when
// it dispatches the refresh command itself, on open).
func (m *Model) MarkLoading() { m.loading = true }

// Refresh marks the pane loading and returns the fetch for its current state
// filter — the app's on-open first fetch. Unlike 'r' it does not demand a
// full resync: with a fresh persisted snapshot the fetch may be the
// incremental updated-since page (#2108).
func (m *Model) Refresh() tea.Cmd { return m.startFetch(false) }

// SetCached seeds the pane with the persisted snapshot (#2108): rendered
// immediately and marked stale ("cached · updating…") until a real listing
// lands through SetResult. A pane that already holds a fetched listing — the
// seed resolves off the Update loop and can lose the race against a fast
// fetch — ignores it: the cache never overwrites fresh data.
func (m *Model) SetCached(issues []forge.Issue, prs []forge.PR) {
	if m.loaded {
		return
	}
	m.issues, m.prs = issues, prs
	m.loaded = true
	m.cached = true
	m.rebuildLabels()
	m.applyFilter()
}

// Cached reports whether the listing on show is the persisted snapshot
// (tests, and the stale marker in the view).
func (m *Model) Cached() bool { return m.cached }

// SetResult applies one finished fetch.
//
// A background poll (#2085) lands here exactly like a manual refresh, minus
// the parts that would fight the user: it does not clear a pending loading
// state a fresh 'r' set, and the selection is restored by issue number after
// the swap, so a newer issue appearing above the cursor leaves it on the
// issue it was on. The filter line, the label filter and the detail view are
// untouched either way — they are derived from state SetResult never writes.
//
// A result the pane has since superseded (#2107) never reaches the listing at
// all — see staleResult. Only its Setup/Err is surfaced, because those
// describe the forge rather than the request: a missing CLI or a dead network
// is worth saying whichever state filter asked.
func (m *Model) SetResult(msg forge.IssuesMsg) {
	if m.staleResult(msg) {
		if msg.Setup != "" {
			m.setup = msg.Setup
		} else if msg.Err != nil {
			m.errMsg = msg.Err.Error()
		}
		// The loading flag belongs to the request still in flight, so it
		// stays set: the indicator must keep standing in for the listing
		// until the answer the active filter asked for arrives.
		return
	}
	if !msg.Poll {
		m.loading = false
	}
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
	if msg.State != "" {
		m.fetched = msg.State
	}
	// A real listing replaces the persisted snapshot seed (#2108) — the
	// "cached · updating…" marker comes down with it.
	m.cached = false
	selected, selectedBody := 0, ""
	if is := m.Selected(); is != nil {
		selected, selectedBody = is.Number, is.Body
	}
	m.issues = msg.Issues
	if msg.PRErr == nil {
		// A failed PR listing (PRErr) is a partial result: keeping the last
		// known PR states beats blanking the linked-PR column over a blip.
		m.prs = msg.PRs
	}
	m.rebuildLabels()
	m.applyFilter()
	m.restoreCursor(selected)
	if m.detail && m.detailFor == selected {
		// The body may have been edited on the forge since it was rendered.
		// Only an actual edit drops the cache — re-rendering an unchanged
		// body every poll would be pure work — and the cache is dropped by
		// clearing the lines rather than the issue number, so the re-render
		// keeps the offset the user scrolled to (ensureDetail only rewinds
		// when the issue itself changes).
		if is := m.Selected(); is != nil && is.Body != selectedBody {
			m.detailLines = nil
		}
	}
}

// staleResult reports whether an arriving listing answers a request the pane
// has since superseded (#2107) — the fix for the rapid 't t' race, where two
// fetches are in flight and the one that lands last used to win no matter
// which state filter it was started for.
//
// A foreground fetch carries the generation startRefresh dispatched it at, so
// only the newest one may write the listing. Untagged foreground results
// (Gen 0 — a caller that does not count its requests, and the pane's own
// tests) are always accepted: the counter can only invalidate fetches the
// pane itself started.
//
// A background poll carries no generation and always asks for the *open*
// listing (#2085), so it is an answer for the pane exactly while the pane's
// own filter is open. Landing after a switch to closed/all it would replace
// the listing with a differently-scoped one — drop it; the state change's own
// refetch is already on its way, and the next poll round lands normally once
// the filter is back on open.
func (m *Model) staleResult(msg forge.IssuesMsg) bool {
	if msg.Poll {
		state := msg.State
		if state == "" {
			state = forge.IssuesOpen
		}
		return state != m.state.issueState()
	}
	return msg.Gen != 0 && msg.Gen != m.gen
}

// restoreCursor puts the cursor back on the issue numbered n after a listing
// swap, keeping the scroll window around it. An issue that left the listing
// (closed since the last poll) leaves the cursor at its old row, clamped —
// the same place a manual refresh would have left it.
func (m *Model) restoreCursor(n int) {
	if n == 0 {
		return
	}
	for i, r := range m.rows {
		if r.idx >= 0 && m.issues[r.idx].Number == n {
			m.cursor = i
			m.clampScroll()
			return
		}
	}
	m.clampScroll()
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
func (m *Model) Filter() string { return m.fInput.Text }

// Filtering reports whether the filter overlay is open with its match row
// focused — the mode where typing edits the pattern (tests, paste routing).
func (m *Model) Filtering() bool { return m.ov == ovFilter && m.ovCursor == fovMatch }

// LabelFilter returns the selected labels sorted by name. The selection is
// sticky (#2104): a label that left the listing keeps filtering — and keeps
// its chip — until it is cleared.
func (m *Model) LabelFilter() []string {
	var out []string
	for name, on := range m.labelSel {
		if on {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// LabelMatchAll reports whether the label filter is in all-of (AND) mode
// (tests).
func (m *Model) LabelMatchAll() bool { return m.labelAll }

// toggleLabelMode flips the label filter between any-of and all-of (#2112),
// keeping the cursor on its entry.
func (m *Model) toggleLabelMode() {
	m.filterTouched = true
	m.labelAll = !m.labelAll
	m.keepSelection()
}

// StateFilter returns the active open/closed/all gate (tests).
func (m *Model) StateFilter() StateFilter { return m.state }

// SortOrder returns the active list order (tests).
func (m *Model) SortOrder() SortOrder { return m.sort }

// Grouped reports whether the issue list groups by label (tests).
func (m *Model) Grouped() bool { return m.group }

// DetailOpen reports whether the detail view is showing (tests).
func (m *Model) DetailOpen() bool { return m.detail }

// PickerOpen reports whether the filter overlay owns the keyboard (tests).
func (m *Model) PickerOpen() bool { return m.ov == ovFilter }

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

// rebuildLabels recomputes the distinct label names the listing carries. The
// selection is left alone (#2104): a state-filter refetch must not silently
// drop an active label chip.
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

// handleKey routes one key to whichever layer owns the keyboard: an open
// overlay first, then the detail view, then the list.
func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case m.ov != ovNone:
		return m.overlayKey(msg)
	case m.detail && m.tab == TabIssues:
		return m.detailKey(msg)
	case m.prDetail && m.tab == TabPRs:
		return m.prDetailKey(msg)
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
	// The full shared list semantics (#1666), g/G extremes included: since
	// #2114 the grouping toggle no longer sits on 'g' (it lives in the filter
	// overlay and the action menu), so the letter means "jump to the extremes"
	// here exactly as it does in the detail views. Group headers are skipped
	// in the direction the key moved.
	if m.navList(key) {
		return nil
	}
	switch key {
	case "r":
		return m.startRefresh()
	case "f", "/", "ctrl+f", "cmd+f", "super+f":
		// ctrl+f is deliberately unbound in the keymap table (#2409) so
		// vim's page-forward survives in the editor; the panes that have a
		// search answer the chord themselves.
		m.openFilterOverlay(fovMatch)
	case "l":
		m.openLabelSection()
	case "t":
		return m.cycleState()
	case "a":
		m.cycleSort()
	case "e", "E":
		return m.startEdit()
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
	if cmd := m.mutationKey(key); cmd != nil {
		return cmd
	}
	return m.prActionKey(key)
}

// mutationKey routes the state-write keys (#2088), which the list and the
// detail view share: 'c' close/reopen, 'C' the same with a comment. Each
// checks the capability gate itself, so a key without permission explains
// rather than doing nothing. The label and assignee pickers moved behind the
// unified 'e' edit picker (#2114).
func (m *Model) mutationKey(key string) tea.Cmd {
	if m.mutate == nil || m.tab != TabIssues {
		return nil
	}
	switch key {
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
	if !ui.ListNav(key, &cur, len(rows), m.bodyHeight(), ui.NavFull) {
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
	case "up", "k", "ctrl+p", "pgup", "end", "G":
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
	m.prDetail = false
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
	m.prDetail = false
	m.tabTouched = true
	m.tab = t
	m.clampScroll()
}

// detailKey handles the full-area detail view: scroll, back to the list with
// the cursor untouched, the issue-walking chords, and the actions that stay
// meaningful with an issue on screen.
func (m *Model) detailKey(msg tea.KeyPressMsg) tea.Cmd {
	page := m.bodyHeight()
	// A scroll that lands on the end of the loaded detail pulls the next
	// timeline page on its own (#2113).
	scrolled := false
	switch msg.String() {
	case "esc", "q", "backspace":
		m.detail = false
	case "ctrl+j":
		return m.stepIssue(1)
	case "ctrl+k":
		return m.stepIssue(-1)
	case "p":
		return m.loadMoreTimeline()
	case "e", "E":
		return m.startEdit()
	case "n":
		return m.startComment()
	case "j", "down":
		m.detailTop++
		scrolled = true
	case "k", "up":
		m.detailTop--
	case "pgdown", "ctrl+d", "space":
		m.detailTop += page
		scrolled = true
	case "pgup", "ctrl+u":
		m.detailTop -= page
	case "g", "home":
		m.detailTop = 0
	case "G", "end":
		m.detailTop = len(m.detailLines) - page
		scrolled = true
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
	case "y":
		// The shared yank of the read-only surfaces (#2071, #2374): the mouse
		// selection goes to the clipboard.
		return m.copySelection()
	default:
		if ui.CopyChord(msg.String()) {
			return m.copySelection()
		}
		if cmd := m.mutationKey(msg.String()); cmd != nil {
			return cmd
		}
	}
	m.clampDetail()
	if scrolled {
		return m.autoLoadTimeline()
	}
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
// Every caller of this path — 'r', a state cycle, a mutation's refetch — is a
// full resync (#2108): the user asked, so the answer must be authoritative,
// never an incremental merge.
func (m *Model) startRefresh() tea.Cmd { return m.startFetch(true) }

// startFetch dispatches one listing fetch, full or incremental-capable.
func (m *Model) startFetch(full bool) tea.Cmd {
	meta := m.startMeta()
	if m.refresh == nil {
		return meta
	}
	m.loading = true
	// Each request carries the generation it was started at, so SetResult can
	// recognise its own newest one and drop everything older (#2107).
	m.gen++
	return tea.Batch(m.refresh(m.state.issueState(), m.gen, full), meta)
}

// cycleState advances the open/closed/all filter ('t', the one-key
// accelerator of the overlay's state row). Closed issues are not part of an
// open listing, so widening may refetch — setState skips the fetch when the
// listing already covers the new gate (#2104).
func (m *Model) cycleState() tea.Cmd {
	return m.setState(StateFilter((int(m.state) + 1) % 3))
}

// dropListingForRefetch clears the issue listing ahead of the refetch a state
// filter change forces (#2107).
//
// This is the deliberate loading presentation of a state-changing refetch:
// clear plus the loading indicator, not "keep the old rows". What is on
// screen was fetched for the *previous* filter, so keeping it would show an
// open-only set under a "closed" filter — and applyFilter's client-side state
// gate would then hide those rows one by one, which is exactly the
// half-empty, mis-scrolled list the race produced. An honest "(fetching
// issues…)" for the width of one fetch beats rows that lie about the filter.
//
// The pull requests are *not* dropped: they are always fetched in every state
// and split purely client-side, so the new filter is already correct for them
// and blanking the PR tab would be a regression, not a fix. Nothing is
// cleared when no fetch factory is injected either — that would leave a bare
// pane empty with nothing on its way to refill it.
func (m *Model) dropListingForRefetch() {
	if m.refresh == nil {
		return
	}
	m.issues = nil
}

// cycleSort advances the list order, keeping the cursor on its entry.
func (m *Model) cycleSort() {
	m.sortTouched = true
	m.sort = SortOrder((int(m.sort) + 1) % len(sortOrders))
	m.keepSelection()
}

// toggleGroup flips the issue list's grouping by label.
func (m *Model) toggleGroup() {
	m.group = !m.group
	m.keepSelection()
}

// clearFilters is esc on a list (#2104): it peels one narrowing at a time
// instead of nuking a carefully built filter in one keypress — first a
// mutation error the filter row holds (#2088), then the match text, then the
// label selection, then the state gate. Each press clears the next layer;
// clicking a chip clears any one directly.
func (m *Model) clearFilters() tea.Cmd {
	switch {
	case m.mutErr != "":
		m.mutErr = ""
	case !m.fInput.Empty():
		m.filterTouched = true
		m.fInput.Clear()
		m.keepSelection()
	case len(m.LabelFilter()) > 0:
		m.filterTouched = true
		m.labelSel = map[string]bool{}
		m.labelAll = false
		m.keepSelection()
	case m.state != FilterOpen:
		return m.setState(FilterOpen)
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
// its lazy timeline fetch, #2084) or the PR detail (with its full fetch,
// #2089).
func (m *Model) activate() tea.Cmd {
	if m.tab == TabPRs {
		return m.openPRDetail()
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
	m.tab, m.ov = TabIssues, ovNone
	m.prDetail = false
	m.fInput.Clear()
	m.labelSel = map[string]bool{}
	m.labelAll = false
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
	msg := StartWorkRequestMsg{Number: is.Number}
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
	if m.detailShown() {
		n++
	}
	return n
}

// detailShown reports whether a full-area detail (issue or PR) is on screen,
// which costs the position-header row.
func (m *Model) detailShown() bool {
	return (m.detail && m.tab == TabIssues) || (m.prDetail && m.tab == TabPRs)
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
	if m.detailShown() {
		n++
	}
	return n
}

// clampScroll keeps the active view's cursor valid and inside its window.
func (m *Model) clampScroll() {
	rows := m.rowsOf(m.tab)
	cur, top := ui.ClampIndex(m.Cursor(), len(rows)), m.Top()
	if len(rows) > 0 && rows[cur].idx < 0 {
		cur = snapRow(rows, cur, 1)
	}
	ui.ClampWindow(&cur, &top, len(rows), m.bodyHeight())
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

// clip bounds one rendered line to the pane width, measuring display cells
// rather than bytes so styled lines are not cut short by their escape
// sequences (#2106).
func (m *Model) clip(s string) string {
	if m.width > 0 && ansi.StringWidth(s) > m.width {
		return ansi.Truncate(s, m.width-1, "…")
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

// PasteText inserts a pasted block into whichever text input is open: the
// close/reopen comment prompt and the PR merge/close dialog's comment stage
// (both share m.cmInput), or the filter overlay's match input, which
// re-narrows the list exactly like typing there does.
func (m *Model) PasteText(text string) bool {
	if m.ov == ovComment || (m.ov == ovPRAct && m.prActStage == 0) {
		return m.cmInput.Paste(text)
	}
	if !m.Filtering() {
		return false
	}
	if !m.fInput.Paste(text) {
		return false
	}
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
	if m.fInput.Empty() {
		return 0, true
	}
	res, ok := fuzzy.Match(m.fInput.Text, hay)
	if !ok {
		return 0, false
	}
	return res.Score, true
}
