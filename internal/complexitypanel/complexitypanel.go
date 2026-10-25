// Package complexitypanel is the Complexity tool window (Roadmap 0550,
// #2917): a singleton bottom-split pane over the project complexity index
// (internal/complexity, #2916). Two tabs pick the population — Project
// (every function of the index snapshot) and File (the focused editor's
// file, with the function under the caret marked) — and a Changes tab slot
// is reserved for the diff view of a later issue, so the tab order never
// shifts. The rows are one grid (internal/gridview): file, function, line,
// CCN, cognitive, NLOC, params, depth, comment density (cmt%, #2934) — or,
// grouped with 'b' (#2937, group.go), one aggregate row per class or file;
// 'v' switches the Project tab to a directory tree with per-node aggregates
// (#2954, tree.go);
// 's' cycles the sort column, 'S' reverses it, the filter row (internal/filterbar over internal/filterexpr)
// narrows by file:, lang:, fn:, min:, max:, kind:, class:, depth:,
// params:, nloc:, metric: and test:, and bare words fuzzy
// over the function names; 't' hides the test files on every tab (#2950);
// 'space' expands a function into its complexity sites (#2946, sites.go);
// 'm' paints them in the editor once (#2947); 'i' accepts the selected
// function (or a group's over-threshold functions) into the accepted-debt
// baseline, or drops it again (#2958, baseline.go), and `baseline:`
// filters by that status; 'x' hides the selected function (or a group's /
// tree node's file through complexity.exclude) from every surface, 'X' the
// selected row's file, 'H' toggles the `hidden:yes` view that lists the
// hidden functions so one can be un-hidden (#3031, hidden.go).
//
// The pane is a pure consumer: the root model hands it the index's immutable
// View via Set on open and on every generation change, and tells it where
// the caret is via Follow. Enter / a double click asks the root model to
// open the function's file (OpenLocationMsg), 'r' to rebuild the index
// (RefreshMsg), 'e' to write the current tab as a CSV scratch (ExportMsg) and
// 'E' as a Markdown one (ExportMarkdownMsg, #2963, markdown.go),
// 'y' to copy the row (CopyMsg). Nothing here touches the filesystem.
package complexitypanel

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/complexity"
	"ike/internal/filterbar"
	"ike/internal/filterexpr"
	"ike/internal/gridview"
	"ike/internal/hiertree"
	"ike/internal/lang"
	"ike/internal/theme"
	"ike/internal/ui"
)

// Tab is one of the pane's populations.
type Tab int

const (
	// TabProject lists every function in the index snapshot.
	TabProject Tab = iota
	// TabFile lists the focused editor's file.
	TabFile
	// TabChanges lists the functions whose metrics differ between a base
	// revision and the working tree (#2918, changes.go).
	TabChanges
	// TabTrend lists the project's sums at each of the last N commits
	// (#2962, trend.go).
	TabTrend
)

// tabNames labels the tabs in display order.
var tabNames = []string{"Project", "File", "Changes", "Trend"}

// enabledTabs are the tabs the keyboard and the mouse can reach.
var enabledTabs = []Tab{TabProject, TabFile, TabChanges, TabTrend}

// Column is one grid column; the order is the display order.
type Column int

const (
	ColFile Column = iota
	ColFunc
	ColLine
	ColCCN
	ColCog
	ColNLOC
	ColParams
	ColDepth
	// ColChange is the Changes tab's section column: changed / added /
	// removed (#2918).
	ColChange
	// ColCmt is the comment density in whole percent (#2934). It follows
	// depth on screen; the value sits last so the older columns keep theirs.
	ColCmt
	// The group-row columns (#2937, group.go): the class, the unit count,
	// the CCN and cognitive average and maximum and the over-threshold
	// count. A group row reads ColNLOC, ColCCN, ColCog and ColCmt as the
	// group's NLOC, sums and aggregate density.
	ColClass
	ColFuncs
	ColCCNAvg
	ColCCNMax
	ColCogAvg
	ColCogMax
	ColOver
	// ColExcess is a group's excess above the CCN and cognitive thresholds
	// (#2945); it follows over on screen.
	ColExcess
	// ColCommits and ColHot are the hotspot columns (#2957, churn.go): a
	// file's commits in the churn window and, on a file group, commits ×
	// excess. They show only inside a git repository.
	ColCommits
	ColHot
	// ColChangedN, ColAddedN and ColRemovedN are a Changes file row's
	// classification counts (#2955, changesgroup.go).
	ColChangedN
	ColAddedN
	ColRemovedN
	// ColSincePin is the Pinned section's pinned → current metrics
	// (#2959, pins.go); it shows only while the section does.
	ColSincePin
	// The Trend tab's columns (#2962, trend.go): the commit, its date and
	// subject, and the project sums with their Δ against the previous
	// point; fns reuses ColFuncs.
	ColCommit
	ColDate
	ColSubject
	ColCCNDelta
	ColCogDelta
	ColExcessDelta
	ColOverDelta
)

// columnLabels are the column headers in Column order.
var columnLabels = []string{"file", "function", "line", "ccn", "cog", "nloc", "params", "depth", "change", "cmt%",
	"class", "funcs", "ccn avg", "ccn max", "cog avg", "cog max", "over", "excess", "commits", "hot",
	"changed", "added", "removed", "since pin",
	"commit", "date", "subject", "Σccn (Δ)", "Σcog (Δ)", "excess (Δ)", "over (Δ)"}

// numeric reports whether a column sorts as a number (descending by
// default) rather than as text.
func (c Column) numeric() bool {
	switch c {
	case ColFile, ColFunc, ColChange, ColClass, ColSubject:
		return false
	}
	return true
}

// lastColumn is the highest Column value.
const lastColumn = ColOverDelta

// The per-tab column layouts, in display order: the File tab hides the
// file column, the Changes tab leads with the section and drops params /
// depth for the before→after metrics.
var (
	projectLayout = []Column{ColFile, ColFunc, ColLine, ColCCN, ColCog, ColNLOC, ColParams, ColDepth, ColCmt}
	fileLayout    = projectLayout[1:]
	changesLayout = []Column{ColChange, ColFunc, ColFile, ColLine, ColCCN, ColCog, ColNLOC, ColCmt}
)

// DefaultWarn is the warning threshold both metrics fall back to
// (complexity.ccn_warn / complexity.cognitive_warn).
const DefaultWarn = 15

// DefaultCmtWarn is the comment-density threshold in percent
// (complexity.comment_density_warn); 0 turns the check off.
const DefaultCmtWarn = 40

// OpenLocationMsg asks the root model to open Path with the caret on the
// function header (enter / double click). Line is 0-based. An extraction
// candidate (#2948) sets EndLine past Line: the app then selects the lines
// Line–EndLine (a visual line selection, caret on the header) so the
// language server's Extract function can be offered on it.
type OpenLocationMsg struct {
	Path    string
	Line    int
	EndLine int
}

// RefreshMsg asks the root model to rebuild the index ('r').
type RefreshMsg struct{}

// ExportMsg asks the root model to write the current tab to a CSV scratch
// file ('e'). CSV is built here because the pane owns what "the current
// view" means; the root model owns where a scratch file goes.
type ExportMsg struct {
	// Label names the exported tab for the notification ("Project").
	Label string
	// CSV is the complete file content, header row included.
	CSV string
}

// CopyMsg asks the root model to put Text on the clipboard ('y' / the copy
// chord): "path:line: name ccn=.. cog=..".
type CopyMsg struct{ Text string }

// Schema is the pane's filter language.
var Schema = filterexpr.Schema{Fields: []filterexpr.Field{
	{Name: "file", Aliases: []string{"path"}, ValueDoc: "a path substring or glob", Doc: "file: narrows by project-relative path (substring or glob)"},
	{Name: "lang", Aliases: []string{"language"}, ValueDoc: "a language id", Doc: "lang: narrows by language id (go, php, …)"},
	{Name: "fn", Aliases: []string{"func", "function"}, ValueDoc: "a function name part", Doc: "fn: narrows by function name (substring) — on group rows by class or file"},
	{Name: "min", ValueDoc: "a number", Doc: "min: keeps rows at or above n on the sort column"},
	{Name: "max", ValueDoc: "a number", Doc: "max: keeps rows at or below n on the sort column"},
	{Name: "kind", ValueDoc: "function, method or lambda", Doc: "kind: keeps functions, methods or lambdas"},
	{Name: "class", ValueDoc: "a class name part", Doc: "class: narrows by class (substring)"},
	{Name: "depth", ValueDoc: "a number", Doc: "depth: keeps rows nesting at least n deep (groups: the deepest function)"},
	{Name: "params", ValueDoc: "a number", Doc: "params: keeps rows with at least n parameters (groups: the most of a function)"},
	{Name: "nloc", ValueDoc: "a number", Doc: "nloc: keeps rows with at least n code lines (groups: the sum)"},
	{Name: "metric", Values: []string{"ccn", "cog", "cmt", "hot", "commits"}, Doc: "metric: sorts by cyclomatic or cognitive complexity, comment density (cmt%), hotspot score (hot) or commits in the churn window"},
	{Name: "test", Values: []string{"yes", "no"}, Doc: "test: no hides test files, test: yes keeps only them"},
	{Name: "baseline", Values: []string{"new", "accepted", "worsened"}, Doc: "baseline: keeps functions the accepted-debt baseline classifies as new, accepted or worsened (groups: with such a member)"},
	{Name: "pinned", Aliases: []string{"pin"}, Values: []string{"yes", "no"}, Doc: "pinned: yes keeps the pinned functions (groups: with a pinned member), pinned: no the rest"},
	{Name: "hidden", Values: []string{"yes", "no"}, Doc: "hidden: yes lists the hidden functions (muted, ✕) so one can be un-hidden with x, hidden: no the rest; without the term hidden functions are left out everywhere"},
}}

// Row is one function of the current tab, kept alongside the cells so the
// filter, the CSV export, the copy text and the view all agree.
type Row struct {
	// Path is the absolute file path, Rel the project-relative one shown.
	Path string
	Rel  string
	// Lang is the language id the path resolves to.
	Lang string
	// Fn is the measured unit.
	Fn complexity.Function
	// Caret marks the File tab's row whose range contains the caret.
	Caret bool
	// Change classifies a Changes-tab row (#2918); 0 on the other tabs.
	// Fn is the head side (the base side for a removed unit), Before the
	// base side of a changed or removed unit.
	Change complexity.ChangeKind
	Before complexity.Function
	// Dirty marks a Changes row whose head side is an unsaved buffer.
	Dirty bool
	// Test marks a row of a test file (complexity.IsTestFile, #2950).
	Test bool
	// Group is set on a group row (#2937): the class or file aggregate the
	// row stands for. Fn is then zero; Path / Rel / Lang name the file.
	Group *complexity.Group
	// FileChange is set on a Changes file row (#2955, changesgroup.go):
	// the file's diff folded; Members are its unit rows.
	FileChange *complexity.FileChange
	// Site is set on a site row (#2946, sites.go): one increment of the
	// expanded function, whose row sits at index Parent in the rows; Fn,
	// Path, Rel, Lang and Change are the parent's.
	Site *complexity.Site
	// Cand is set on an extraction-candidate row (#2948, candidates.go),
	// a child row like a site row.
	Cand   *complexity.Candidate
	Parent int
	// Commits is the row's file's commit count in the churn window and Hot
	// its hotspot score (#2957, churn.go); both 0 outside git.
	Commits int
	Hot     int
	// Ordinal is the function's index among its file's same-named units
	// in source order — the baseline's key (#2958, baseline.go).
	Ordinal int
	// Members are a group row's function rows (#2958), so 'i' can accept
	// every over-threshold member.
	Members []Row
	// Pin is the function's pin (#2959, pins.go), nil when unpinned;
	// PinRow places the row in the Project tab's Pinned section, and Gone
	// marks a pin whose function the snapshot no longer holds (Fn then
	// carries the pinned name and metrics).
	Pin    *complexity.Pin
	PinRow bool
	Gone   bool
	// Trend is set on a Trend tab row (#2962, trend.go): one commit with
	// the project's sums; every function field is zero.
	Trend *TrendRow
	// Hidden marks a function the hidden set drops (#3031, hidden.go);
	// such a row is listed only under hidden:yes.
	Hidden bool
}

// child reports whether r is a child row of an expanded function — a site
// row or a candidate row — which the totals, the caret mark, the CSV
// export and the group rows skip.
func (r Row) child() bool { return r.Site != nil || r.Cand != nil }

// value is the row's sort key on column c: a number for the metric
// columns, the text for file / function. On the Changes tab the metric
// columns sort by the absolute delta, so min: reads |Δ| there.
func (r Row) value(c Column) (float64, string) {
	if r.Trend != nil {
		return r.trendValue(c)
	}
	if r.Group != nil {
		return r.groupValue(c)
	}
	if r.FileChange != nil {
		return r.fileChangeValue(c)
	}
	if r.Cand != nil {
		if c == ColLine {
			return float64(r.Cand.Line), ""
		}
		return 0, r.candCell(c)
	}
	if r.Site != nil {
		switch c {
		case ColLine:
			return float64(r.Site.Line), ""
		case ColCCN:
			return float64(r.Site.CCN), ""
		case ColCog:
			return float64(r.Site.Cognitive), ""
		}
		return 0, r.siteCell(c)
	}
	n, s := r.intValue(c)
	return float64(n), s
}

func (r Row) intValue(c Column) (int, string) {
	switch c {
	case ColFile:
		return 0, r.Rel
	case ColFunc:
		return 0, r.Fn.Name
	case ColLine:
		return r.Fn.Line, ""
	case ColChange:
		return r.sectionRank(), ""
	case ColCommits:
		return r.Commits, ""
	case ColHot:
		return r.Hot, ""
	case ColSincePin:
		return r.sinceValue(), ""
	}
	if r.Change != 0 {
		switch c {
		case ColCCN:
			return abs(r.DeltaCCN()), ""
		case ColCog:
			return abs(r.DeltaCognitive()), ""
		case ColNLOC:
			return abs(r.DeltaNLOC()), ""
		case ColCmt:
			return abs(r.DeltaCmt()), ""
		}
	}
	switch c {
	case ColCCN:
		return r.Fn.CCN, ""
	case ColCog:
		return r.Fn.Cognitive, ""
	case ColNLOC:
		return r.Fn.NLOC, ""
	case ColParams:
		return r.Fn.Params, ""
	case ColCmt:
		return cmtOf(r.Fn), ""
	}
	return r.Fn.Depth, ""
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// cell renders the row's text for column c: the plain metric on the
// Project / File tabs, "before→after (Δ)" on the Changes tab.
func (r Row) cell(c Column) string {
	if r.Trend != nil {
		return r.trendCell(c)
	}
	if r.Group != nil {
		return r.groupCell(c)
	}
	if r.FileChange != nil {
		return r.fileChangeCell(c)
	}
	if r.Cand != nil {
		return r.candCell(c)
	}
	if r.Site != nil {
		return r.siteCell(c)
	}
	switch c {
	case ColFile:
		return r.Rel
	case ColFunc:
		name := r.Fn.Name
		if r.Fn.Baseline == complexity.BaselineAccepted {
			name += acceptedMark
		}
		if r.Hidden {
			name += hiddenMark // #3031: shown by hidden:yes only
		}
		if r.Pin != nil {
			name += pinMark
		}
		if r.Dirty {
			return name + dirtyMark
		}
		return name
	case ColLine:
		return strconv.Itoa(r.Fn.Line + 1)
	case ColParams:
		return strconv.Itoa(r.Fn.Params)
	case ColDepth:
		return strconv.Itoa(r.Fn.Depth)
	case ColChange:
		return r.Change.String()
	case ColCommits:
		return strconv.Itoa(r.Commits)
	case ColHot:
		return strconv.Itoa(r.Hot)
	case ColSincePin:
		return r.sinceCell()
	}
	if r.Gone {
		return "-" // no current metrics: the function is gone
	}
	if r.Change != 0 {
		switch c {
		case ColCCN:
			return r.metricCell(ccnOf, true)
		case ColCog:
			return r.metricCell(cogOf, true)
		case ColCmt:
			return r.metricCell(cmtOf, true)
		}
		return r.metricCell(nlocOf, false)
	}
	switch c {
	case ColCCN:
		return strconv.Itoa(r.Fn.CCN)
	case ColCog:
		return strconv.Itoa(r.Fn.Cognitive)
	case ColCmt:
		return strconv.Itoa(cmtOf(r.Fn))
	}
	return strconv.Itoa(r.Fn.NLOC)
}

// dirtyMark suffixes the function of a row measured from an unsaved
// buffer, as the editor tabs mark a dirty file.
const dirtyMark = "*"

// cells renders the row's grid cells for the given layout.
func (r Row) cells(layout []Column) []string {
	out := make([]string, len(layout))
	for i, c := range layout {
		out[i] = r.cell(c)
	}
	return out
}

// Model is the tool window state. Value type with pointer-receiver mutators,
// embedded in a pane.Instance like the other tool windows.
type Model struct {
	width   int
	height  int
	focused bool
	pal     *theme.Palette

	root    string
	view    complexity.View
	stats   complexity.Stats
	hasView bool
	// The warning thresholds (#2960): global the configured ones, resolve
	// the per-file resolver (nil: global everywhere), thCache its answers.
	global   complexity.Thresholds
	resolve  complexity.Resolver
	thCache  map[string]complexity.Thresholds
	thGen    int // bumped by every SetThresholds that changes them
	tab      Tab
	sortCol  Column
	desc     bool
	file     string
	caret    int
	filter   filterbar.Model
	rows     []Row
	total    int
	summary  complexity.Summary
	widths   []int
	cursor   int
	top      int
	clicks   ui.ClickTracker
	now      func() time.Time
	pathLang func(string) string

	// The Changes tab (#2918): its data, and the base-revision prompt 'R'
	// opens in the filter row's place.
	changes   Changes
	revField  ui.Field
	prompting bool

	// The Trend tab (#2962, trend.go): its data.
	trend Trend

	// The grouping level of the Project and File tabs (#2937, group.go).
	projectGroup complexity.Level
	fileGroup    complexity.Level
	// changesGroup is the Changes tab's level: none or file (#2955).
	changesGroup complexity.Level

	// hideTests drops the test files from every tab ('t', #2950).
	hideTests bool

	// expanded is the expandKey of the function row whose sites are shown
	// ('space', #2946, sites.go); "" when none is.
	expanded string

	// The hotspot columns (#2957, churn.go): commits per project-relative
	// path in the last churnDays days, nil outside git (the columns hide);
	// hotWarn is the top-decile hot score of the visible rows.
	churn     map[string]int
	churnDays int
	hotWarn   int

	// baseline is the accepted-debt baseline in effect (#2958,
	// baseline.go), nil without one.
	baseline *complexity.Baseline

	// hidden is the hidden set in effect (#3031, hidden.go), nil without
	// one; hiddenN counts the current tab's hidden functions.
	hidden  *complexity.HiddenSet
	hiddenN int

	// hist shows the distribution row ('D', #2953, histogram.go).
	hist bool

	// The tree view of the Project tab ('v', #2954, tree.go): whether it
	// is on, and the hiertree it shows.
	treeView bool
	tree     hiertree.Tree[treeNode]

	// The pins (#2959, pins.go): the list in effect, whether the Project
	// tab shows its Pinned section, and the footer note of a refused 'p'.
	pins       *complexity.Pins
	pinSection bool
	note       string
	// The session Δ (#2959): the session's first settled snapshot, the
	// excess it measures under the current settings (cached per
	// sessionKey) and the Project tab's delta against it.
	sessionBase    complexity.View
	hasSessionBase bool
	sessionKey     *sessionCacheKey
	sessionExcess  int
	sessionDelta   int

	// agentRunning holds the AgentKey of every function the simplify
	// agent is working on (#3009, askagent.go): its row carries agentMark
	// and esc on it cancels the run.
	agentRunning map[string]bool
}

// New returns an empty panel on the Project tab sorted by CCN descending;
// the snapshot arrives via Set.
func New(pal *theme.Palette) Model {
	m := Model{pal: pal, now: time.Now, filter: filterbar.New(Schema),
		sortCol: ColCCN, desc: true, pathLang: langOf,
		global: complexity.Thresholds{CCN: DefaultWarn, Cog: DefaultWarn, Cmt: DefaultCmtWarn}}
	m.Refresh()
	return m
}

// langOf resolves a path's language id through the registry.
func langOf(path string) string {
	if l, ok := lang.ByPath(path); ok {
		return l.ID
	}
	return ""
}

// SetNow injects the clock (tests).
func (m *Model) SetNow(f func() time.Time) {
	if f != nil {
		m.now = f
	}
}

// SetRoot records the project root the file column is shown relative to.
func (m *Model) SetRoot(root string) {
	if root != m.root {
		m.root = root
		m.Refresh()
	}
}

// SetWarn applies global warning thresholds for every file (see
// SetThresholds).
func (m *Model) SetWarn(ccn, cog, cmt int) {
	m.SetThresholds(complexity.Thresholds{CCN: ccn, Cog: cog, Cmt: cmt}, nil)
}

// SetThresholds applies the warning thresholds: global the configured
// complexity.ccn_warn / complexity.cognitive_warn /
// complexity.comment_density_warn, res the per-file resolver of the
// complexity.overrides entries (#2960), nil when there are none. A global
// CCN or cognitive value below 1 keeps the default, a comment-density value
// of 0 turns that check off and a negative one keeps the default.
func (m *Model) SetThresholds(global complexity.Thresholds, res complexity.Resolver) {
	if global.CCN < 1 {
		global.CCN = DefaultWarn
	}
	if global.Cog < 1 {
		global.Cog = DefaultWarn
	}
	if global.Cmt < 0 {
		global.Cmt = DefaultCmtWarn
	}
	if res == nil && m.resolve == nil && global == m.global {
		return
	}
	m.global, m.resolve, m.thCache = global, res, nil
	m.thGen++
	m.Refresh()
}

// th is the thresholds path's functions are judged by: the resolver's
// answer, cached per path, or the global thresholds.
func (m *Model) th(path string) complexity.Thresholds {
	if m.resolve == nil {
		return m.global
	}
	if t, ok := m.thCache[path]; ok {
		return t
	}
	if m.thCache == nil {
		m.thCache = map[string]complexity.Thresholds{}
	}
	t := m.resolve(path)
	m.thCache[path] = t
	return t
}

// thresholdsLabel is the File tab's "thresholds ccn 30 cog 30 (legacy/**)"
// when an override sets the followed file's thresholds (#2960); "" on the
// other tabs and for a file judged by the globals.
func (m *Model) thresholdsLabel() string {
	if m.tab != TabFile || m.file == "" {
		return ""
	}
	path := m.file
	if _, ok := m.view.Files[path]; !ok {
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
	}
	if l := m.th(path).Label(m.global); l != "" {
		return "thresholds " + l
	}
	return ""
}

// Set replaces the index snapshot and its stats and rebuilds the rows.
func (m *Model) Set(v complexity.View, st complexity.Stats) {
	m.view, m.stats, m.hasView = v, st, true
	m.Refresh()
}

// SetSize records the interior content size.
func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	m.widths = m.columnWidths()
}

// SetFocused marks the panel focused (header + selection highlight).
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the active theme.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// Tab reports the selected population.
func (m *Model) Tab() Tab { return m.tab }

// SetTab selects a population and rebuilds the rows.
func (m *Model) SetTab(t Tab) {
	if !tabEnabled(t) || t == m.tab {
		return
	}
	m.tab = t
	m.cursor, m.top = 0, 0
	m.fitSort()
	m.Refresh()
}

// layout is the selected tab's column order.
func (m *Model) layout() []Column {
	if m.treeActive() {
		return m.churnLayout(treeLayout, treeChurnLayout) // #2954
	}
	if g := m.groupLayout(); g != nil {
		return g
	}
	switch m.tab {
	case TabFile:
		return m.churnLayout(fileLayout, fileChurnLayout)
	case TabChanges:
		return changesLayout
	case TabTrend:
		return trendLayout
	}
	l := m.churnLayout(projectLayout, projectChurnLayout)
	if m.pinSection {
		l = append(l[:len(l):len(l)], ColSincePin) // #2959
	}
	return l
}

// layoutIndex is the display position of column c on the selected tab, -1
// when the tab does not show it.
func (m *Model) layoutIndex(c Column) int {
	for i, l := range m.layout() {
		if l == c {
			return i
		}
	}
	return -1
}

// tabEnabled reports whether t can be selected.
func tabEnabled(t Tab) bool {
	for _, e := range enabledTabs {
		if e == t {
			return true
		}
	}
	return false
}

// cycleTab moves to the next (delta +1) or previous enabled tab.
func (m *Model) cycleTab(delta int) {
	idx := 0
	for i, e := range enabledTabs {
		if e == m.tab {
			idx = i
		}
	}
	n := len(enabledTabs)
	m.SetTab(enabledTabs[((idx+delta)%n+n)%n])
}

// SortColumn reports the sort column and whether the order is descending.
func (m *Model) SortColumn() (Column, bool) { return m.sortCol, m.desc }

// SetSort selects the sort column (a numeric column descending, a text one
// ascending) and rebuilds the rows.
func (m *Model) SetSort(c Column) {
	if c < ColFile || c > lastColumn {
		return
	}
	m.sortCol, m.desc = c, c.numeric()
	m.Refresh()
}

// cycleSort moves the sort column to the next one the tab shows ('s'),
// wrapping; a sort column the tab hides restarts at its first column.
func (m *Model) cycleSort() {
	layout := m.layout()
	i := m.layoutIndex(m.sortCol)
	m.SetSort(layout[(i+1)%len(layout)])
}

// Follow tells the File tab which file the focused editor shows and where
// its caret is (0-based line). A changed file rebuilds the rows; a moved
// caret only re-marks the row.
func (m *Model) Follow(path string, line int) {
	if path == m.file && line == m.caret {
		return
	}
	changed := path != m.file
	m.file, m.caret = path, line
	if m.tab != TabFile {
		return
	}
	if changed {
		m.Refresh()
		return
	}
	m.markCaret()
}

// File reports the followed path (tests).
func (m *Model) File() string { return m.file }

// Rows exposes the visible rows (tests).
func (m *Model) Rows() []Row { return m.rows }

// Total reports the tab's row count before the filter.
func (m *Model) Total() int { return m.total }

// Cursor reports the selected row index (tests).
func (m *Model) Cursor() int { return m.cursor }

// Summary reports the current tab's totals.
func (m *Model) Summary() complexity.Summary { return m.summary }

// Selected returns the row under the cursor.
func (m *Model) Selected() (Row, bool) {
	if m.treeActive() {
		return m.treeSelected()
	}
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return Row{}, false
	}
	return m.rows[m.cursor], true
}

// Refresh re-derives the rows from the snapshot for the selected tab, the
// filter and the sort, keeping the cursor on the same function where
// possible.
func (m *Model) Refresh() {
	keep := ""
	if r, ok := m.Selected(); ok {
		keep = r.key()
	}
	all := m.markHidden(m.markTests(m.markPins(m.population())))
	var sum complexity.Summer
	for _, r := range all {
		if r.Change == complexity.Removed || r.Gone || r.Trend != nil {
			continue // the Changes summary is the head side's; a Trend row is no function
		}
		sum.Add(r.Fn, m.th(r.Path))
	}
	// The totals stay the whole tab's; a grouped tab then lists groups.
	m.summary = sum.Summary()
	m.updateSessionDelta()
	switch {
	case m.treeActive(): // the tree groups per directory itself (#2954)
	case m.Grouping() == complexity.ByNone:
	case m.tab == TabChanges:
		all = m.changesFileRows(all)
	default:
		all = m.groupRows(all)
	}
	m.total = len(all)
	q := m.filter.Query()
	switch q.Value("metric") {
	case "ccn":
		m.sortCol, m.desc = ColCCN, true
	case "cog":
		m.sortCol, m.desc = ColCog, true
	case "cmt":
		m.sortCol, m.desc = ColCmt, true
	case "hot":
		m.sortCol, m.desc = ColHot, true
	case "commits":
		m.sortCol, m.desc = ColCommits, true
	}
	m.rows = m.rows[:0]
	for _, r := range all {
		if m.matches(q, r) {
			m.rows = append(m.rows, r)
		}
	}
	m.sortRows()
	m.markHot()
	m.spliceSites()
	m.markCaret()
	if m.treeActive() {
		m.buildTree() // keeps its own expansion and selection
	}
	m.widths = m.columnWidths()
	m.cursor = 0
	for i, r := range m.rows {
		if r.key() == keep {
			m.cursor = i
			break
		}
	}
	m.clampScroll()
}

// population lists the current tab's rows before the filter.
func (m *Model) population() []Row {
	var out []Row
	add := func(path string, fns []complexity.Function) {
		rel, id := m.relPath(path), m.pathLang(path)
		fns, ords := m.classify(rel, fns)
		mask := m.hidden.Mask(rel, fns) // #3031
		for i, fn := range fns {
			out = append(out, Row{Path: path, Rel: rel, Lang: id, Fn: fn, Commits: m.churn[rel], Hot: m.fnHot(path, m.churn[rel], fn), Ordinal: ords[i], Hidden: hiddenOf(mask, i)})
		}
	}
	switch m.tab {
	case TabProject:
		for _, p := range m.view.Paths() {
			add(p, m.view.Files[p])
		}
	case TabFile:
		if m.file != "" {
			if fns, ok := m.view.Files[m.file]; ok {
				add(m.file, fns)
			} else if abs, err := filepath.Abs(m.file); err == nil {
				add(abs, m.view.Files[abs])
			}
		}
	case TabChanges:
		out = m.changesRows()
	case TabTrend:
		out = m.trendRows()
	}
	return out
}

// markTests flags the rows of test files and, while tests are hidden,
// drops them — so the totals follow the visible population (#2950).
func (m *Model) markTests(rows []Row) []Row {
	out := rows[:0]
	test := map[string]bool{}
	for _, r := range rows {
		if r.Trend != nil {
			out = append(out, r) // a commit row (#2962) is no file
			continue
		}
		t, ok := test[r.Path]
		if !ok {
			t = complexity.IsTestFile(m.root, r.Path)
			test[r.Path] = t
		}
		if t && m.hideTests {
			continue
		}
		r.Test = t
		out = append(out, r)
	}
	return out
}

// relPath renders a path relative to the project root, the path itself
// when it lies outside or no root is known.
func (m *Model) relPath(path string) string {
	if m.root != "" {
		if rel, err := filepath.Rel(m.root, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

// matches gates one row through the filter. Each field's terms go through
// filterexpr's KeepsAll — repeats AND, a negated term (-file:_test) drops
// what it matches — and the free text through MatchFree, so a negated word
// drops the rows it fuzzy-matches (#2951).
func (m *Model) matches(q filterexpr.Query, r Row) bool {
	if q.Empty() {
		return true
	}
	if r.Trend != nil {
		return m.trendMatches(q, r) // #2962
	}
	class := r.Fn.Class
	if r.Group != nil {
		class = r.Group.Class
	}
	atLeast := func(field string) func(string) bool {
		return func(v string) bool {
			n, err := strconv.Atoi(v)
			return err == nil && r.attr(field) >= n
		}
	}
	bound := func(min bool) func(string) bool {
		return func(v string) bool {
			n, err := strconv.Atoi(v)
			if err != nil {
				return false
			}
			col := m.sortCol
			if !col.numeric() {
				col = ColCCN
			}
			x, _ := r.value(col)
			if m.treeActive() && r.Group == nil {
				x = float64(m.fnTreeValue(r, col)) // the tree's aggregate columns on a leaf
			}
			if min {
				return x >= float64(n)
			}
			return x <= float64(n)
		}
	}
	gates := []struct {
		field string
		match func(string) bool
	}{
		{"file", func(v string) bool { return filterexpr.MatchPath(v, r.Rel) }},
		{"lang", func(v string) bool { return strings.EqualFold(v, r.Lang) }},
		{"fn", func(v string) bool { return strings.Contains(strings.ToLower(r.label()), strings.ToLower(v)) }},
		{"min", bound(true)},
		{"max", bound(false)},
		{"kind", func(v string) bool { return !r.aggregate() && strings.EqualFold(v, r.Fn.Kind) }},
		{"class", func(v string) bool { return strings.Contains(strings.ToLower(class), strings.ToLower(v)) }},
		{"depth", atLeast("depth")},
		{"params", atLeast("params")},
		{"nloc", atLeast("nloc")},
		{"test", func(v string) bool { want, ok := testValue(v); return ok && r.Test == want }},
		{"baseline", func(v string) bool { return baselineMatches(v, r) }},
		{"pinned", func(v string) bool { return pinnedMatches(v, r) }},
		{"hidden", func(v string) bool { return hiddenMatches(v, r) }},
	}
	for _, g := range gates {
		if !q.KeepsAll(g.field, g.match) {
			return false
		}
	}
	_, ok := q.MatchFree(r.label())
	return ok
}

// attr is the row's depth / params / nloc for the at-least filter keys:
// the function's value, on a group row the deepest depth, the most
// params and the NLOC sum.
func (r Row) attr(field string) int {
	if g := r.Group; g != nil {
		switch field {
		case "depth":
			return g.DepthMax
		case "params":
			return g.ParamsMax
		}
		return g.NLOC
	}
	if fc := r.FileChange; fc != nil {
		if field == "nloc" {
			return fc.After.NLOC
		}
		return 0
	}
	switch field {
	case "depth":
		return r.Fn.Depth
	case "params":
		return r.Fn.Params
	}
	return r.Fn.NLOC
}

// testValue reads a test: value: yes keeps only test files, no hides them.
func testValue(v string) (want, ok bool) {
	switch strings.ToLower(v) {
	case "yes":
		return true, true
	case "no":
		return false, true
	}
	return false, false
}

// sortRows orders the visible rows by the sort column, ties by path and
// line so the order is stable across refreshes. The Changes tab keeps its
// sections (changed, added, removed) and sorts inside each.
func (m *Model) sortRows() {
	col, desc := m.sortCol, m.desc
	sort.SliceStable(m.rows, func(i, j int) bool {
		a, b := m.rows[i], m.rows[j]
		if a.PinRow != b.PinRow {
			return a.PinRow // the Pinned section leads (#2959)
		}
		if a.Gone != b.Gone {
			return b.Gone // and closes with its gone pins
		}
		if a.Change != b.Change {
			return a.sectionRank() < b.sectionRank()
		}
		ai, as := a.value(col)
		bi, bs := b.value(col)
		if col.numeric() {
			if ai != bi {
				if desc {
					return ai > bi
				}
				return ai < bi
			}
		} else if as != bs {
			if desc {
				return as > bs
			}
			return as < bs
		}
		if a.Rel != b.Rel {
			return a.Rel < b.Rel
		}
		if a.Group != nil && b.Group != nil {
			return a.Group.Class < b.Group.Class
		}
		if a.Gone && a.Fn.Name != b.Fn.Name {
			return a.Fn.Name < b.Fn.Name
		}
		if a.Trend != nil && b.Trend != nil {
			return a.Trend.Index > b.Trend.Index // ties newest first (#2962)
		}
		return a.Fn.Line < b.Fn.Line
	})
}

// markCaret flags the File tab's row whose range contains the caret: the
// innermost one when units nest (a closure inside a method).
func (m *Model) markCaret() {
	best := -1
	for i := range m.rows {
		m.rows[i].Caret = false
		if m.tab != TabFile || m.rows[i].Group != nil || m.rows[i].child() {
			continue
		}
		fn := m.rows[i].Fn
		if m.caret < fn.Line || m.caret > fn.EndLine {
			continue
		}
		if best < 0 || fn.Line >= m.rows[best].Fn.Line && fn.EndLine <= m.rows[best].Fn.EndLine {
			best = i
		}
	}
	if best >= 0 {
		m.rows[best].Caret = true
	}
}

// CaretRow reports the index of the row marked under the caret, -1 when
// none is.
func (m *Model) CaretRow() int {
	for i, r := range m.rows {
		if r.Caret {
			return i
		}
	}
	return -1
}

// columnWidths sizes every column of the tab's layout to its longest
// visible value or label, the sort marker included.
func (m *Model) columnWidths() []int {
	layout := m.layout()
	labels := m.headerLabels()
	ws := make([]int, len(labels))
	for i, l := range labels {
		ws[i] = lipgloss.Width(l)
	}
	if m.treeActive() {
		return m.treeWidths(ws)
	}
	fnCol := m.layoutIndex(ColFunc)
	for _, r := range m.rows {
		for i, c := range r.cells(layout) {
			if i == fnCol && r.Caret {
				c = caretMark + c
			}
			if w := lipgloss.Width(c); w > ws[i] {
				ws[i] = w
			}
		}
	}
	// The text columns share what the numeric ones leave (#2992), so a deep
	// path or a long name never pushes the metrics off the pane.
	return m.fitWidths(ws)
}

// caretMark prefixes the function under the caret on the File tab.
const caretMark = "▸ "

// columnTitles are layout's labels: the group sums and the tree's name
// column relabelled, no sort marker.
func (m *Model) columnTitles(layout []Column) []string {
	out := make([]string, len(layout))
	grouped := m.Grouping() != complexity.ByNone
	tree := m.treeActive()
	for i, c := range layout {
		out[i] = columnLabels[c]
		if l, ok := groupSumLabels[c]; ok && grouped && !tree {
			out[i] = l
		}
		if l, ok := treeLabels[c]; ok && tree {
			out[i] = l
		}
	}
	return out
}

// headerLabels are the tab's column labels with the sort marker on the
// sort column.
func (m *Model) headerLabels() []string {
	out := m.columnTitles(m.layout())
	mark := " ▲"
	if m.desc {
		mark = " ▼"
	}
	if i := m.layoutIndex(m.sortCol); i >= 0 {
		out[i] += mark
	}
	return out
}

// Update handles one message while the panel exists; only key presses reach
// it, focus-filtered by the pane layer.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		return m.handleKey(k)
	}
	return nil
}

// Filtering reports whether the filter row holds the keyboard (#2409).
func (m *Model) Filtering() bool { return m.filter.Active() }

// FilterText is the filter row's raw text.
func (m *Model) FilterText() string { return m.filter.Text() }

// OpenSearch implements the pane's Searchable capability (#2409): the shared
// find chord focuses the filter row, exactly as "/" does.
func (m *Model) OpenSearch() bool {
	m.filter.Focus()
	return true
}

// NextMatch implements the pane's match-step capability (#2410).
func (m *Model) NextMatch() ui.MatchStep { return m.stepFiltered(1) }

// PrevMatch steps backwards; see NextMatch.
func (m *Model) PrevMatch() ui.MatchStep { return m.stepFiltered(-1) }

// LineInputOpen implements the pane's Searchable capability (#2634): the
// focused filter row — or the base-revision prompt — owns the caret chords.
func (m *Model) LineInputOpen() bool { return m.filter.Active() || m.prompting }

func (m *Model) stepFiltered(delta int) ui.MatchStep {
	if !m.filter.Active() {
		return ui.NoStep
	}
	next, st := ui.StepOver(m.cursor, len(m.rows), delta, func(int) bool { return true })
	m.cursor = next
	m.clampScroll()
	return m.filter.ShowStep(st)
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	m.note = ""
	if m.prompting {
		return m.promptKey(msg)
	}
	if m.filter.Active() {
		handled, changed := m.filter.Key(msg)
		if changed {
			m.Refresh()
		}
		if handled {
			return nil
		}
	}
	if ui.FindChord(msg.String()) {
		m.OpenSearch()
		return nil
	}
	if ui.CopyChord(msg.String()) {
		return m.copyRow()
	}
	if m.treeActive() {
		if cmd, handled := m.treeKeyPress(msg.String()); handled {
			return cmd
		}
	}
	if ui.ListNav(msg.String(), &m.cursor, len(m.rows), m.bodyHeight(), ui.NavFull) {
		m.clampScroll()
		return nil
	}
	if m.tab == TabTrend {
		if cmd, handled := m.trendKey(msg.String()); handled {
			return cmd // #2962
		}
	}
	switch msg.String() {
	case "/":
		m.filter.Focus()
	case "tab", "l", "right":
		m.cycleTab(1)
	case "shift+tab", "h", "left":
		m.cycleTab(-1)
	case "s":
		m.cycleSort()
	case "S":
		m.desc = !m.desc
		m.Refresh()
	case "b":
		m.CycleGroup()
	case "v":
		m.ToggleTreeView()
	case "t":
		m.ToggleTests()
	case "D":
		m.ToggleHistogram()
	case "space":
		m.ToggleSites()
	case "m":
		return m.showSites()
	case "i":
		return m.toggleBaseline()
	case "x":
		return m.toggleHide() // #3031
	case "X":
		return m.hideFile()
	case "H":
		m.ToggleHiddenView()
	case "p":
		return m.togglePin()
	case "d":
		return m.functionDiff()
	case "a":
		return m.askAgent() // #2964
	case "esc":
		return m.cancelAgent() // #3009: only on a running row
	case "enter":
		return m.open()
	case "e":
		return m.export()
	case "E":
		return m.exportMarkdown() // #2963
	case "y":
		return m.copyRow()
	case "r":
		return func() tea.Msg { return RefreshMsg{} }
	case "R":
		if m.tab == TabChanges && !m.changes.NotGit {
			m.promptBase()
		}
	}
	m.clampScroll()
	return nil
}

// open asks the root model to open the selected function's file; a removed
// function has no line in the working tree, so its file opens at the top.
func (m *Model) open() tea.Cmd {
	r, ok := m.Selected()
	if !ok {
		return nil
	}
	if r.Cand != nil {
		return m.openCandidate(r)
	}
	if r.Site != nil {
		return m.openSite(r)
	}
	msg := OpenLocationMsg{Path: r.Path, Line: r.Fn.Line}
	switch {
	case r.Group != nil:
		msg.Line = r.Group.Line // the group's earliest unit
	case r.FileChange != nil:
		msg.Line = r.fileChangeLine()
	case r.Change == complexity.Removed:
		msg.Line = 0
	}
	return func() tea.Msg { return msg }
}

// copyRow hands the selected row's one-line summary to the root model.
func (m *Model) copyRow() tea.Cmd {
	r, ok := m.Selected()
	if !ok {
		return nil
	}
	text := CopyText(r)
	return func() tea.Msg { return CopyMsg{Text: text} }
}

// CopyText renders a row as "path:line: name ccn=.. cog=.."; a Changes
// row carries both sides: "changed ccn=3→5 (+2) cog=1→1 (0)"; a group row
// its aggregate (groupCopyText).
func CopyText(r Row) string {
	if r.Trend != nil {
		return trendCopyText(r)
	}
	if r.Group != nil {
		return groupCopyText(r)
	}
	if r.FileChange != nil {
		return fileChangeCopyText(r)
	}
	if r.Cand != nil {
		return candCopyText(r)
	}
	if r.Site != nil {
		return siteCopyText(r)
	}
	head := r.Rel + ":" + strconv.Itoa(r.Fn.Line+1) + ": " + r.Fn.Name
	if r.Change != 0 {
		return head + " " + r.Change.String() + " ccn=" + r.metricCell(ccnOf, true) + " cog=" + r.metricCell(cogOf, true)
	}
	return head + " ccn=" + strconv.Itoa(r.Fn.CCN) + " cog=" + strconv.Itoa(r.Fn.Cognitive)
}

// export builds the current tab's CSV and hands it to the root model.
func (m *Model) export() tea.Cmd {
	csv := m.CSV()
	label := tabNames[m.tab]
	return func() tea.Msg { return ExportMsg{Label: label, CSV: csv} }
}

// csvHeader is the export's first row.
var csvHeader = []string{"file", "function", "kind", "line", "ccn", "cognitive", "nloc", "params", "depth", "comment_lines", "cmt_pct"}

// CSV renders the current tab as comma-separated values: a header row and
// one row per visible function, the file column included on every tab so
// an export stays self-describing.
func (m *Model) CSV() string {
	if m.treeActive() {
		return m.treeCSV()
	}
	if m.tab == TabTrend {
		return m.trendCSV() // #2962
	}
	if m.tab == TabChanges {
		if m.Grouping() == complexity.ByFile {
			return m.changesFileCSV()
		}
		return m.changesCSV()
	}
	if m.Grouping() != complexity.ByNone {
		return m.groupCSV()
	}
	var b strings.Builder
	header := csvHeader
	if m.churn != nil {
		header = append(header[:len(header):len(header)], "commits") // #2957
	}
	writeCSVRow(&b, header)
	for _, r := range m.rows {
		if r.child() || r.Gone {
			continue
		}
		c := r.cells(projectLayout)
		fields := []string{c[0], r.Fn.Name, r.Fn.Kind, c[2], c[3], c[4], c[5], c[6], c[7], strconv.Itoa(r.Fn.CommentLines), c[8]}
		if m.churn != nil {
			fields = append(fields, strconv.Itoa(r.Commits))
		}
		writeCSVRow(&b, fields)
	}
	return b.String()
}

// writeCSVRow appends one quoted row.
func writeCSVRow(b *strings.Builder, fields []string) {
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(csvField(f))
	}
	b.WriteByte('\n')
}

// csvField quotes a value that would otherwise break the row.
func csvField(s string) string {
	if !strings.ContainsAny(s, ",\"\n") {
		return s
	}
	return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
}

// View renders the header with its tab bar and totals, the filter row, the
// distribution row while it is on, the column header, the scrolled rows and the status row.
func (m *Model) View() string {
	pal := m.theme()
	var b strings.Builder
	b.WriteString(m.headerLine(pal))
	b.WriteString("\n")
	if m.prompting {
		b.WriteString(m.promptView())
	} else {
		b.WriteString(m.filter.View(m.width, pal))
	}
	b.WriteString("\n")
	if m.histShown() {
		b.WriteString(m.histView(pal))
		b.WriteString("\n")
	}
	if m.sparkShown() {
		b.WriteString(m.sparkView(pal)) // #2962
		b.WriteString("\n")
	}
	b.WriteString(gridview.HeaderRow(pal, m.fitLabels(m.headerLabels()), m.widths, 0, m.width))
	b.WriteString("\n")
	if m.treeActive() {
		b.WriteString(m.renderTree(pal, m.bodyHeight()))
	} else {
		b.WriteString(m.renderRows(pal, m.bodyHeight()))
	}
	b.WriteString(m.footer())
	return b.String()
}

// headerLine names the pane, draws the tab bar and the tab's totals.
func (m *Model) headerLine(pal *theme.Palette) string {
	title := lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(" Complexity")
	var tabs strings.Builder
	for i, name := range tabNames {
		if i > 0 {
			tabs.WriteString(" ")
		}
		// Reverse video, not an underline: lipgloss renders an underlined
		// run one escape sequence per cell, which would break every "does
		// the header say File" assertion for no visual gain.
		st := lipgloss.NewStyle().Faint(true)
		if Tab(i) == m.tab {
			st = lipgloss.NewStyle().Foreground(pal.Background).Background(pal.Accent).Bold(true)
		}
		tabs.WriteString(st.Render(name))
	}
	// The totals are clipped as plain text before styling: the grid clip
	// counts runes, which a styled run would overflow.
	lead := title + "   " + tabs.String()
	totals := "   " + m.totalsText()
	if m.width > 0 {
		if room := m.width - lipgloss.Width(lead); room <= 0 {
			return lead
		} else {
			totals = gridview.ClipTo(totals, room)
		}
	}
	return lead + lipgloss.NewStyle().Faint(true).Render(totals)
}

// totalsText renders the tab's Summary and the index state; the Changes
// tab renders its base → working tree line instead.
func (m *Model) totalsText() string {
	if m.tab == TabChanges {
		return m.changesTotals()
	}
	if m.tab == TabTrend {
		return m.trendTotals() // #2962
	}
	s := m.summary
	// The index state leads, so a narrow pane still shows it.
	var parts []string
	switch {
	case m.stats.Scanning:
		parts = append(parts, "scanning…")
	case m.hasView && m.stats.Cached > 0:
		// A walk seeded from the persisted cache (#2967) says how much of
		// it was not measured this session.
		parts = append(parts, "gen "+itoa(m.view.Gen)+" ("+itoa(m.stats.Cached)+" cached)")
	case m.hasView:
		parts = append(parts, "gen "+itoa(m.view.Gen))
	}
	if t := m.thresholdsLabel(); t != "" {
		parts = append(parts, t)
	}
	parts = append(parts,
		itoa(s.Functions)+" fns",
		"NLOC "+itoa(s.NLOC),
		metricTotals("CCN", s.CCNSum, s.CCNAvg, s.CCNMax, s.CCNP50, s.CCNP90, s.CCNP99, m.sortCol != ColCog),
		metricTotals("cog", s.CogSum, s.CogAvg, s.CogMax, s.CogP50, s.CogP90, s.CogP99, m.sortCol == ColCog),
		"cmt "+itoa(complexity.DensityPercent(s.CommentDensity))+"%",
		itoa(s.OverThreshold)+" over",
		"excess "+itoa(s.Excess),
	)
	if t := m.baselineTotals(); t != "" {
		parts = append(parts, t)
	}
	if t := m.hiddenTotals(); t != "" {
		parts = append(parts, t) // #3031
	}
	if t := m.sessionTotals(); t != "" && m.tab == TabProject {
		parts = append(parts, t)
	}
	return strings.Join(parts, " · ")
}

// metricTotals is one metric's "Σ avg max"; the sort metric's (#2953) adds
// the percentiles between avg and max — "CCN Σ137301 avg 4.0 p50 2 p90 9
// p99 28 max 170" — the other keeps the short form so the line stays short.
func metricTotals(name string, sum int, avg float64, maxV, p50, p90, p99 int, pct bool) string {
	s := name + " Σ" + itoa(sum) + " avg " + fmt1(avg)
	if pct {
		s += " p50 " + itoa(p50) + " p90 " + itoa(p90) + " p99 " + itoa(p99)
	}
	return s + " max " + itoa(maxV)
}

// renderRows draws the list scrolled around the cursor.
func (m *Model) renderRows(pal *theme.Palette, height int) string {
	m.clampScroll()
	empty := lipgloss.NewStyle().Faint(true).Render(" " + m.emptyText())
	return ui.RenderWindow(m.top, height, len(m.rows), empty, func(i int) string { return m.renderRow(pal, i) })
}

// emptyText explains an empty list per index state, tab and filter.
func (m *Model) emptyText() string {
	if m.tab == TabChanges {
		return m.changesEmpty()
	}
	if m.tab == TabTrend {
		return m.trendEmpty() // #2962
	}
	switch {
	case m.hasView && m.stats.Unavailable:
		return "(no syntax tree in this build — the index cannot measure)"
	case m.hasView && !m.stats.Enabled:
		return "(complexity.enabled is off)"
	case m.stats.Scanning && m.total == 0:
		return "(scanning the project…)"
	case m.ShowingHidden() && m.hiddenN == 0:
		return "(nothing hidden on this tab — x on a row hides it, H leaves this view)"
	case !m.filter.Empty():
		return "(no row matches the filter)"
	case m.Grouping() == complexity.ByClass && m.summary.Functions > 0:
		return "(no class method among the measured functions)"
	case m.tab == TabFile && m.file == "":
		return "(no file focused)"
	case m.tab == TabFile:
		return "(no measured function in this file)"
	}
	return "(no measured function in the project)"
}

// renderRow draws one line as grid cells: the file (Project tab only),
// function, line and the six metrics; a metric at or above its warning
// threshold takes the warning colour, the File tab's caret row is marked
// on the function cell.
func (m *Model) renderRow(pal *theme.Palette, i int) string {
	r := m.rows[i]
	layout := m.layout()
	texts := r.cells(layout)
	cells := make([]gridview.Cell, len(texts))
	for c, t := range texts {
		cells[c] = gridview.Cell{Text: t}
	}
	if tr := r.Trend; tr != nil {
		// A commit that added excess takes the warning colour, one that
		// removed some the success colour (#2962).
		if ei := m.layoutIndex(ColExcessDelta); ei >= 0 {
			cells[ei].Warn, cells[ei].Good = tr.DeltaExcess() > 0, tr.DeltaExcess() < 0
		}
		return m.dataRow(cells, i == m.cursor)
	}
	if r.Group != nil {
		for c, col := range layout {
			cells[c].Warn = m.groupWarn(r.Group, col) || col == ColHot && m.hotWarns(r)
		}
		return m.dataRow(cells, i == m.cursor)
	}
	if fc := r.FileChange; fc != nil {
		// A file that got heavier takes the warning colour.
		for c, col := range layout {
			cells[c].Warn = fileChangeDelta(fc, col) > 0
		}
		return m.dataRow(cells, i == m.cursor)
	}
	if r.child() {
		return m.dataRow(cells, i == m.cursor)
	}
	if fi := m.layoutIndex(ColFunc); fi >= 0 && r.Caret {
		cells[fi].Text = caretMark + cells[fi].Text
		cells[fi].Match = true
	}
	if fi := m.layoutIndex(ColFunc); fi >= 0 && m.agentRunningOn(r) {
		cells[fi].Text = agentMark + cells[fi].Text // #3009
	}
	// An accepted function (#2958) keeps its over-threshold cells, muted
	// instead of in the warning colour.
	accepted := r.Fn.Baseline == complexity.BaselineAccepted
	if ci := m.layoutIndex(ColCCN); ci >= 0 {
		cells[ci].Warn = r.Fn.CCN >= m.th(r.Path).CCN && !accepted
		cells[ci].Muted = r.Fn.CCN >= m.th(r.Path).CCN && accepted
	}
	if ci := m.layoutIndex(ColCog); ci >= 0 {
		cells[ci].Warn = r.Fn.Cognitive >= m.th(r.Path).Cog && !accepted
		cells[ci].Muted = r.Fn.Cognitive >= m.th(r.Path).Cog && accepted
	}
	if ci := m.layoutIndex(ColCmt); ci >= 0 {
		cells[ci].Warn = m.th(r.Path).CommentsOver(r.Fn)
	}
	if r.Gone || r.Hidden { // a hidden row (#3031) shows muted under hidden:yes
		for c := range cells {
			cells[c].Warn, cells[c].Muted = false, true
		}
	}
	if si := m.layoutIndex(ColSincePin); si >= 0 {
		rose, fell := r.sinceTone()
		cells[si].Warn, cells[si].Good = rose, fell
	}
	// The section column reads like a heading: named on the first row of
	// each section, blank below it.
	if si := m.layoutIndex(ColChange); si >= 0 && i > 0 && m.rows[i-1].Change == r.Change {
		cells[si].Text = ""
	}
	return m.dataRow(cells, i == m.cursor)
}

// footer shows the filtered / total counts and the key hints.
func (m *Model) footer() string {
	count := itoa(len(m.rows)) + "/" + itoa(m.total) + " rows"
	if full := m.clippedText(); full != "" {
		// The cursor row's truncated cells in full (#2992) lead the footer.
		count = full + " · " + count
	}
	if m.tab == TabTrend {
		return lipgloss.NewStyle().Faint(true).Render(m.clip(" " + count + m.trendFooter())) // #2962
	}
	hints := " · tab view · s sort · S reverse · enter open · e csv · E md · y copy · r rescan · / filter"
	switch {
	case m.treeActive():
		hints = m.treeFooter()
	case m.tab == TabProject:
		hints = " · tab view · v tree" + hints[len(" · tab view"):]
	}
	switch {
	case m.treeActive():
	case m.tab == TabChanges && m.Grouping() == complexity.ByNone:
		hints += " · d diff · R base · b group · space sites · m marks"
	case m.tab == TabChanges:
		hints += " · d diff · R base · b group: file"
	case m.Grouping() == complexity.ByNone:
		hints += " · b group · space sites · m marks · i accept"
	default:
		hints += " · b group: " + m.Grouping().String() + " · i accept"
	}
	if !m.treeActive() && m.tab != TabTrend && m.Grouping() == complexity.ByNone {
		hints += " · a agent" // #2964: function rows only
		if m.selectedAgentRunning() {
			hints += " · esc cancel agent" // #3009
		}
	}
	hints += m.pinFooter()
	if m.tab != TabTrend {
		hints += m.hiddenFooter() // #3031
	}
	if m.hideTests {
		hints += " · t tests: hidden"
	} else {
		hints += " · t tests"
	}
	if m.tab != TabChanges {
		hints += " · D dist"
	}
	if m.tab != TabChanges && m.churn != nil {
		hints += " · churn " + itoa(m.churnDays) + "d"
	}
	return lipgloss.NewStyle().Faint(true).Render(m.clip(" " + count + hints))
}

// chromeRows counts the header, filter, column-header and footer lines, and
// the distribution row while it shows (#2953).
func (m *Model) chromeRows() int {
	if m.histShown() || m.sparkShown() {
		return 5
	}
	return 4
}

// bodyHeight is the room the list gets between the chrome lines.
func (m *Model) bodyHeight() int {
	h := m.height - m.chromeRows()
	if h < 1 {
		h = 1
	}
	return h
}

// clampScroll keeps the cursor valid and inside the visible window.
func (m *Model) clampScroll() {
	ui.ClampWindow(&m.cursor, &m.top, len(m.rows), m.bodyHeight())
}

// clip bounds one rendered line to the panel width.
func (m *Model) clip(s string) string {
	if m.width > 0 {
		return gridview.ClipTo(s, m.width)
	}
	return s
}

func itoa(n int) string { return strconv.Itoa(n) }

// fmt1 renders an average with one decimal.
func fmt1(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) }

// theme resolves the palette with the shared default fallback.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

// Mouse control, mirroring the Usage pane: y 0 is the header, y 1 the filter
// row, then the distribution row while it shows (#2953), the column header
// and the list rows.
func (m *Model) headerRows() int { return m.chromeRows() - 1 }

// Wheel scrolls the list by delta rows (positive = down).
func (m *Model) Wheel(delta int) {
	if m.treeActive() {
		m.tree.Wheel(delta, m.bodyHeight())
		return
	}
	ui.WheelWindow(&m.top, &m.cursor, delta, len(m.rows), m.bodyHeight())
}

// Click selects a row and opens it on a double click; a click on the
// header's tab bar switches the tab, one on a column header sorts by it,
// one on a distribution bucket narrows the list to it.
func (m *Model) Click(x, y int) tea.Cmd {
	hr := m.headerRows()
	switch {
	case y == 0:
		m.clickHeader(x)
		return nil
	case y == 2 && m.histShown():
		m.clickHistogram(x)
		return nil
	case y == 2 && m.sparkShown():
		m.clickSpark(x) // #2962
		return nil
	case y == hr-1:
		m.clickColumn(x)
		return nil
	case m.treeActive():
		return m.treeClick(y)
	}
	return m.clicks.ClickRow(y, m.top, hr, m.bodyHeight(), len(m.rows), m.now(), &m.cursor,
		func(int) tea.Cmd { return m.open() })
}

// clickHeader maps a header-row x to a tab. The tab bar starts after the
// " Complexity" title and its three-space gap (headerLine).
func (m *Model) clickHeader(x int) {
	start := lipgloss.Width(" Complexity") + 3
	for i, name := range tabNames {
		end := start + lipgloss.Width(name)
		if x >= start && x < end {
			m.SetTab(Tab(i))
			return
		}
		start = end + 1
	}
}

// clickColumn maps a column-header x to its column: a leading space, then
// each column padded to its width and two spaces apart (gridview.HeaderRow).
func (m *Model) clickColumn(x int) {
	start := 1
	layout := m.layout()
	for c := 0; c < len(m.widths) && c < len(layout); c++ {
		end := start + m.widths[c]
		if x >= start && x < end {
			col := layout[c]
			if col == m.sortCol {
				m.desc = !m.desc
				m.Refresh()
			} else {
				m.SetSort(col)
			}
			return
		}
		start = end + 2
	}
}

// PasteText inserts a pasted block into the open filter row at its cursor
// (#2460), re-deriving the rows exactly like typing there does. A closed
// filter row lets the paste fall through.
func (m *Model) PasteText(text string) bool {
	if m.prompting {
		return m.revField.Paste(text)
	}
	if !m.filter.Active() {
		return false
	}
	if !m.filter.Paste(text) {
		return false
	}
	m.Refresh()
	return true
}

// SelectCaret moves the cursor onto the File tab's caret row (#2965, the
// alt+enter entries) and reports whether there was one.
func (m *Model) SelectCaret() bool {
	i := m.CaretRow()
	if i < 0 {
		return false
	}
	m.cursor = i
	m.clampScroll()
	return true
}
