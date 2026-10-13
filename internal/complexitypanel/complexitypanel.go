// Package complexitypanel is the Complexity tool window (Roadmap 0550,
// #2917): a singleton bottom-split pane over the project complexity index
// (internal/complexity, #2916). Two tabs pick the population — Project
// (every function of the index snapshot) and File (the focused editor's
// file, with the function under the caret marked) — and a Changes tab slot
// is reserved for the diff view of a later issue, so the tab order never
// shifts. The rows are one grid (internal/gridview): file, function, line,
// CCN, cognitive, NLOC, params, depth, comment density (cmt%, #2934) — or,
// grouped with 'b' (#2937, group.go), one aggregate row per class or file;
// 's' cycles the sort column, 'S' reverses it, the filter row (internal/filterbar over internal/filterexpr)
// narrows by file:, lang:, fn:, min:, max:, kind:, class:, depth:,
// params:, nloc:, metric: and test:, and bare words fuzzy
// over the function names; 't' hides the test files on every tab (#2950);
// 'space' expands a function into its complexity sites (#2946, sites.go);
// 'm' paints them in the editor once (#2947).
//
// The pane is a pure consumer: the root model hands it the index's immutable
// View via Set on open and on every generation change, and tells it where
// the caret is via Follow. Enter / a double click asks the root model to
// open the function's file (OpenLocationMsg), 'r' to rebuild the index
// (RefreshMsg), 'e' to write the current tab as a CSV scratch (ExportMsg),
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
)

// tabNames labels the tabs in display order.
var tabNames = []string{"Project", "File", "Changes"}

// enabledTabs are the tabs the keyboard and the mouse can reach.
var enabledTabs = []Tab{TabProject, TabFile, TabChanges}

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
)

// columnLabels are the column headers in Column order.
var columnLabels = []string{"file", "function", "line", "ccn", "cog", "nloc", "params", "depth", "change", "cmt%",
	"class", "funcs", "ccn avg", "ccn max", "cog avg", "cog max", "over", "excess", "commits", "hot"}

// numeric reports whether a column sorts as a number (descending by
// default) rather than as text.
func (c Column) numeric() bool {
	switch c {
	case ColFile, ColFunc, ColChange, ColClass:
		return false
	}
	return true
}

// lastColumn is the highest Column value.
const lastColumn = ColHot

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
// function header (enter / double click). Line is 0-based.
type OpenLocationMsg struct {
	Path string
	Line int
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
	// Site is set on a site row (#2946, sites.go): one increment of the
	// expanded function, whose row sits at index Parent in the rows; Fn,
	// Path, Rel, Lang and Change are the parent's.
	Site   *complexity.Site
	Parent int
	// Commits is the row's file's commit count in the churn window and Hot
	// its hotspot score (#2957, churn.go); both 0 outside git.
	Commits int
	Hot     int
}

// value is the row's sort key on column c: a number for the metric
// columns, the text for file / function. On the Changes tab the metric
// columns sort by the absolute delta, so min: reads |Δ| there.
func (r Row) value(c Column) (float64, string) {
	if r.Group != nil {
		return r.groupValue(c)
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
	if r.Group != nil {
		return r.groupCell(c)
	}
	if r.Site != nil {
		return r.siteCell(c)
	}
	switch c {
	case ColFile:
		return r.Rel
	case ColFunc:
		if r.Dirty {
			return r.Fn.Name + dirtyMark
		}
		return r.Fn.Name
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

	root     string
	view     complexity.View
	stats    complexity.Stats
	hasView  bool
	ccnWarn  int
	cogWarn  int
	cmtWarn  int
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

	// The grouping level of the Project and File tabs (#2937, group.go).
	projectGroup complexity.Level
	fileGroup    complexity.Level

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
}

// New returns an empty panel on the Project tab sorted by CCN descending;
// the snapshot arrives via Set.
func New(pal *theme.Palette) Model {
	m := Model{pal: pal, now: time.Now, filter: filterbar.New(Schema),
		sortCol: ColCCN, desc: true, ccnWarn: DefaultWarn, cogWarn: DefaultWarn, cmtWarn: DefaultCmtWarn, pathLang: langOf}
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

// SetWarn applies the warning thresholds (complexity.ccn_warn /
// complexity.cognitive_warn / complexity.comment_density_warn); a CCN or
// cognitive value below 1 keeps the default, a comment-density value of 0
// turns that check off and a negative one keeps the default.
func (m *Model) SetWarn(ccn, cog, cmt int) {
	if ccn < 1 {
		ccn = DefaultWarn
	}
	if cog < 1 {
		cog = DefaultWarn
	}
	if cmt < 0 {
		cmt = DefaultCmtWarn
	}
	if ccn == m.ccnWarn && cog == m.cogWarn && cmt == m.cmtWarn {
		return
	}
	m.ccnWarn, m.cogWarn, m.cmtWarn = ccn, cog, cmt
	m.Refresh()
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
	if m.Grouping() != complexity.ByNone {
		m.fitSort()
	}
	m.Refresh()
}

// layout is the selected tab's column order.
func (m *Model) layout() []Column {
	if g := m.groupLayout(); g != nil {
		return g
	}
	switch m.tab {
	case TabFile:
		return m.churnLayout(fileLayout, fileChurnLayout)
	case TabChanges:
		return changesLayout
	}
	return m.churnLayout(projectLayout, projectChurnLayout)
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
	all := m.markTests(m.population())
	fns := make([]complexity.Function, 0, len(all))
	for _, r := range all {
		if r.Change == complexity.Removed {
			continue // the Changes summary is the head side's
		}
		fns = append(fns, r.Fn)
	}
	// The totals stay the whole tab's; a grouped tab then lists groups.
	m.summary = complexity.Summarize(fns, m.ccnWarn, m.cogWarn, m.cmtWarn)
	if m.Grouping() != complexity.ByNone {
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
		for _, fn := range fns {
			out = append(out, Row{Path: path, Rel: rel, Lang: id, Fn: fn, Commits: m.churn[rel], Hot: m.fnHot(m.churn[rel], fn)})
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
	}
	return out
}

// markTests flags the rows of test files and, while tests are hidden,
// drops them — so the totals follow the visible population (#2950).
func (m *Model) markTests(rows []Row) []Row {
	out := rows[:0]
	test := map[string]bool{}
	for _, r := range rows {
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

// matches gates one row through the filter.
func (m *Model) matches(q filterexpr.Query, r Row) bool {
	if q.Empty() {
		return true
	}
	for _, t := range q.Terms {
		switch t.Field {
		case "file":
			if !filterexpr.MatchPath(t.Value, r.Rel) {
				return false
			}
		case "lang":
			if !strings.EqualFold(t.Value, r.Lang) {
				return false
			}
		case "fn":
			if !strings.Contains(strings.ToLower(r.label()), strings.ToLower(t.Value)) {
				return false
			}
		case "min", "max":
			n, err := strconv.Atoi(t.Value)
			if err != nil {
				return false
			}
			col := m.sortCol
			if !col.numeric() {
				col = ColCCN
			}
			v, _ := r.value(col)
			if (t.Field == "min" && v < float64(n)) || (t.Field == "max" && v > float64(n)) {
				return false
			}
		case "kind":
			if r.Group != nil || !strings.EqualFold(t.Value, r.Fn.Kind) {
				return false
			}
		case "class":
			class := r.Fn.Class
			if r.Group != nil {
				class = r.Group.Class
			}
			if !strings.Contains(strings.ToLower(class), strings.ToLower(t.Value)) {
				return false
			}
		case "depth", "params", "nloc":
			n, err := strconv.Atoi(t.Value)
			if err != nil || r.attr(t.Field) < n {
				return false
			}
		case "test":
			if want, ok := testValue(t.Value); !ok || r.Test != want {
				return false
			}
		}
	}
	_, ok := filterexpr.MatchText(q.Match, r.label())
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
		return a.Fn.Line < b.Fn.Line
	})
}

// markCaret flags the File tab's row whose range contains the caret: the
// innermost one when units nest (a closure inside a method).
func (m *Model) markCaret() {
	best := -1
	for i := range m.rows {
		m.rows[i].Caret = false
		if m.tab != TabFile || m.rows[i].Group != nil || m.rows[i].Site != nil {
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
	// The file column is capped so a deep path never pushes the metrics off
	// the pane; the rest are short by nature.
	if fi := m.layoutIndex(ColFile); fi >= 0 {
		if max := m.width / 2; max > 12 && ws[fi] > max {
			ws[fi] = max
		}
	}
	return ws
}

// caretMark prefixes the function under the caret on the File tab.
const caretMark = "▸ "

// headerLabels are the tab's column labels with the sort marker on the
// sort column.
func (m *Model) headerLabels() []string {
	layout := m.layout()
	out := make([]string, len(layout))
	grouped := m.Grouping() != complexity.ByNone
	for i, c := range layout {
		out[i] = columnLabels[c]
		if l, ok := groupSumLabels[c]; ok && grouped {
			out[i] = l
		}
	}
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
	if ui.ListNav(msg.String(), &m.cursor, len(m.rows), m.bodyHeight(), ui.NavFull) {
		m.clampScroll()
		return nil
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
	case "t":
		m.ToggleTests()
	case "space":
		m.ToggleSites()
	case "m":
		return m.showSites()
	case "enter":
		return m.open()
	case "e":
		return m.export()
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
	if r.Site != nil {
		return m.openSite(r)
	}
	msg := OpenLocationMsg{Path: r.Path, Line: r.Fn.Line}
	switch {
	case r.Group != nil:
		msg.Line = r.Group.Line // the group's earliest unit
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
	if r.Group != nil {
		return groupCopyText(r)
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
	if m.tab == TabChanges {
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
		if r.Site != nil {
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
// column header, the scrolled rows and the status row.
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
	b.WriteString(gridview.HeaderRow(pal, m.headerLabels(), m.widths, 0, m.width))
	b.WriteString("\n")
	b.WriteString(m.renderRows(pal, m.bodyHeight()))
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
	s := m.summary
	// The index state leads, so a narrow pane still shows it.
	var parts []string
	switch {
	case m.stats.Scanning:
		parts = append(parts, "scanning…")
	case m.hasView:
		parts = append(parts, "gen "+itoa(m.view.Gen))
	}
	parts = append(parts,
		itoa(s.Functions)+" fns",
		"NLOC "+itoa(s.NLOC),
		"CCN Σ"+itoa(s.CCNSum)+" avg "+fmt1(s.CCNAvg)+" max "+itoa(s.CCNMax),
		"cog Σ"+itoa(s.CogSum)+" avg "+fmt1(s.CogAvg)+" max "+itoa(s.CogMax),
		"cmt "+itoa(complexity.DensityPercent(s.CommentDensity))+"%",
		itoa(s.OverThreshold)+" over",
		"excess "+itoa(s.Excess),
	)
	return strings.Join(parts, " · ")
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
	switch {
	case m.hasView && m.stats.Unavailable:
		return "(no syntax tree in this build — the index cannot measure)"
	case m.hasView && !m.stats.Enabled:
		return "(complexity.enabled is off)"
	case m.stats.Scanning && m.total == 0:
		return "(scanning the project…)"
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
	if r.Group != nil {
		for c, col := range layout {
			cells[c].Warn = m.groupWarn(r.Group, col) || col == ColHot && m.hotWarns(r)
		}
		return gridview.DataRow(pal, cells, m.widths, 0, m.width, i == m.cursor, m.focused)
	}
	if r.Site != nil {
		return gridview.DataRow(pal, cells, m.widths, 0, m.width, i == m.cursor, m.focused)
	}
	if fi := m.layoutIndex(ColFunc); fi >= 0 && r.Caret {
		cells[fi].Text = caretMark + cells[fi].Text
		cells[fi].Match = true
	}
	if ci := m.layoutIndex(ColCCN); ci >= 0 {
		cells[ci].Warn = r.Fn.CCN >= m.ccnWarn
	}
	if ci := m.layoutIndex(ColCog); ci >= 0 {
		cells[ci].Warn = r.Fn.Cognitive >= m.cogWarn
	}
	if ci := m.layoutIndex(ColCmt); ci >= 0 {
		cells[ci].Warn = r.Fn.CommentsOver(m.cmtWarn)
	}
	// The section column reads like a heading: named on the first row of
	// each section, blank below it.
	if si := m.layoutIndex(ColChange); si >= 0 && i > 0 && m.rows[i-1].Change == r.Change {
		cells[si].Text = ""
	}
	return gridview.DataRow(pal, cells, m.widths, 0, m.width, i == m.cursor, m.focused)
}

// footer shows the filtered / total counts and the key hints.
func (m *Model) footer() string {
	count := itoa(len(m.rows)) + "/" + itoa(m.total) + " rows"
	hints := " · tab view · s sort · S reverse · enter open · e export · y copy · r rescan · / filter"
	switch {
	case m.tab == TabChanges:
		hints += " · R base · space sites · m marks"
	case m.Grouping() == complexity.ByNone:
		hints += " · b group · space sites · m marks"
	default:
		hints += " · b group: " + m.Grouping().String()
	}
	if m.hideTests {
		hints += " · t tests: hidden"
	} else {
		hints += " · t tests"
	}
	if m.tab != TabChanges && m.churn != nil {
		hints += " · churn " + itoa(m.churnDays) + "d"
	}
	return lipgloss.NewStyle().Faint(true).Render(m.clip(" " + count + hints))
}

// chromeRows counts the header, filter, column-header and footer lines.
const chromeRows = 4

// bodyHeight is the room the list gets between the chrome lines.
func (m *Model) bodyHeight() int {
	h := m.height - chromeRows
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
// row, y 2 the column header, list rows start at y 3.
const headerRows = 3

// Wheel scrolls the list by delta rows (positive = down).
func (m *Model) Wheel(delta int) {
	ui.WheelWindow(&m.top, &m.cursor, delta, len(m.rows), m.bodyHeight())
}

// Click selects a row and opens it on a double click; a click on the
// header's tab bar switches the tab, one on a column header sorts by it.
func (m *Model) Click(x, y int) tea.Cmd {
	switch y {
	case 0:
		m.clickHeader(x)
		return nil
	case 2:
		m.clickColumn(x)
		return nil
	}
	return m.clicks.ClickRow(y, m.top, headerRows, m.bodyHeight(), len(m.rows), m.now(), &m.cursor,
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
