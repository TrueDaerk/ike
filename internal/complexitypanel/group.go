package complexitypanel

import (
	"strconv"
	"strings"

	"ike/internal/complexity"
)

// group.go is the grouping of the Project and File tabs (#2937): 'b' (the
// complexity.group command) cycles none → class → file on the Project tab
// and none → class on the File tab, replacing the function rows with one
// row per class or file carrying the aggregate (complexity.GroupFunctions).
// The header totals stay the whole tab's; sort, filter, open, copy and the
// CSV export work on the group rows. Each tab keeps its own level, and
// GroupMode / SetGroupMode carry them through a layout save. The Changes
// tab groups per file (#2955, changesgroup.go).

// The group layouts, in display order: the File tab drops the file column.
var (
	classGroupLayout     = []Column{ColFile, ColClass, ColFuncs, ColNLOC, ColCCN, ColCCNAvg, ColCCNMax, ColCog, ColCogAvg, ColCogMax, ColCmt, ColOver, ColExcess}
	fileGroupLayout      = append([]Column{ColFile}, classGroupLayout[2:]...)
	fileTabClassLayout   = classGroupLayout[1:]
	groupSumLabels       = map[Column]string{ColCCN: "Σccn", ColCog: "Σcog"}
	groupCycleProjectTab = []complexity.Level{complexity.ByNone, complexity.ByClass, complexity.ByFile}
	groupCycleFileTab    = []complexity.Level{complexity.ByNone, complexity.ByClass}
)

// Grouping reports the selected tab's grouping level.
func (m *Model) Grouping() complexity.Level {
	switch m.tab {
	case TabProject:
		return m.projectGroup
	case TabFile:
		return m.fileGroup
	case TabChanges:
		return m.changesGroup
	}
	return complexity.ByNone
}

// CycleGroup moves the selected tab to its next grouping level and reports
// whether the tab groups at all.
func (m *Model) CycleGroup() bool {
	var cycle []complexity.Level
	var cur *complexity.Level
	switch m.tab {
	case TabProject:
		cycle, cur = groupCycleProjectTab, &m.projectGroup
	case TabFile:
		cycle, cur = groupCycleFileTab, &m.fileGroup
	case TabChanges:
		cycle, cur = groupCycleChanges, &m.changesGroup
	default:
		return false
	}
	if m.treeActive() {
		m.note = "the tree groups per directory, file and class; v returns to the grid"
		return true
	}
	next := cycle[0]
	for i, l := range cycle {
		if l == *cur {
			next = cycle[(i+1)%len(cycle)]
		}
	}
	*cur = next
	m.cursor, m.top = 0, 0
	m.fitSort()
	m.Refresh()
	return true
}

// GroupPickMsg asks the root model to open the grouping picker ('B',
// #3082): the pane cannot host the palette itself.
type GroupPickMsg struct{}

// GroupChoice is one row of the grouping picker (complexity.groupPick,
// #3082): a level, or the Project tab's directory tree (Tree set, Level
// none). Current marks what the tab shows now.
type GroupChoice struct {
	Label   string
	Level   complexity.Level
	Tree    bool
	Current bool
}

// GroupChoices lists what the selected tab can show, in cycle order — the
// Project tab adds the tree view after its levels; nil on a tab that does
// not group.
func (m *Model) GroupChoices() []GroupChoice {
	var cycle []complexity.Level
	switch m.tab {
	case TabProject:
		cycle = groupCycleProjectTab
	case TabFile:
		cycle = groupCycleFileTab
	case TabChanges:
		cycle = groupCycleChanges
	default:
		return nil
	}
	tree := m.treeActive()
	out := make([]GroupChoice, 0, len(cycle)+1)
	for _, l := range cycle {
		out = append(out, GroupChoice{Label: groupChoiceLabel(l), Level: l, Current: !tree && l == m.Grouping()})
	}
	if m.tab == TabProject {
		out = append(out, GroupChoice{Label: "tree view", Tree: true, Current: tree})
	}
	return out
}

// groupChoiceLabel names a level in the picker: "none", "class", "file".
func groupChoiceLabel(l complexity.Level) string {
	if l == complexity.ByNone {
		return "none"
	}
	return l.String()
}

// PickGroup applies one GroupChoice to the selected tab and reports whether
// the tab takes it: a level leaves the tree view and sets the level, the
// tree choice turns the tree on (the grid's level is kept for later).
func (m *Model) PickGroup(c GroupChoice) bool {
	ok := false
	for _, have := range m.GroupChoices() {
		ok = ok || (have.Tree == c.Tree && have.Level == c.Level)
	}
	if !ok {
		return false
	}
	if c.Tree {
		if !m.treeView {
			m.ToggleTreeView()
		}
		return true
	}
	if m.tab == TabProject {
		m.treeView = false
	}
	switch m.tab {
	case TabProject:
		m.projectGroup = c.Level
	case TabFile:
		m.fileGroup = c.Level
	case TabChanges:
		m.changesGroup = c.Level
	}
	m.cursor, m.top = 0, 0
	m.fitSort()
	m.Refresh()
	return true
}

// fitSort resets a sort column the selected layout does not show to CCN
// descending — a group layout has no line / params / depth column, a
// function layout no group column.
func (m *Model) fitSort() {
	if m.layoutIndex(m.sortCol) < 0 {
		m.sortCol, m.desc = ColCCN, true
		if m.tab == TabTrend {
			m.sortCol = ColCommit // newest first (#2962)
		}
		if m.tab == TabDuplicates {
			m.sortCol = ColScore // tokens × occurrences (#3044)
		}
	}
}

// GroupMode renders the tabs' levels for the layout store: "project:class,
// file:class,changes:file", the tabs left at none omitted, "" when none
// groups.
func (m *Model) GroupMode() string {
	var parts []string
	if m.projectGroup != complexity.ByNone {
		parts = append(parts, "project:"+m.projectGroup.String())
	}
	if m.fileGroup != complexity.ByNone {
		parts = append(parts, "file:"+m.fileGroup.String())
	}
	if m.changesGroup != complexity.ByNone {
		parts = append(parts, "changes:"+m.changesGroup.String())
	}
	return strings.Join(parts, ",")
}

// SetGroupMode restores GroupMode's form; an unknown tab or level (a newer
// build's) is ignored, the File tab never takes the file level and the
// Changes tab takes only it.
func (m *Model) SetGroupMode(mode string) {
	m.projectGroup, m.fileGroup, m.changesGroup = complexity.ByNone, complexity.ByNone, complexity.ByNone
	for _, part := range strings.Split(mode, ",") {
		tab, val, _ := strings.Cut(part, ":")
		l, ok := complexity.ParseLevel(val)
		if !ok {
			continue
		}
		switch {
		case tab == "project":
			m.projectGroup = l
		case tab == "file" && l != complexity.ByFile:
			m.fileGroup = l
		case tab == "changes" && l == complexity.ByFile:
			m.changesGroup = l
		}
	}
	m.fitSort()
	m.Refresh()
}

// HideTests reports whether the test files are hidden ('t', #2950).
func (m *Model) HideTests() bool { return m.hideTests }

// ToggleTests hides or shows the test files on every tab (#2950).
func (m *Model) ToggleTests() {
	m.hideTests = !m.hideTests
	m.cursor, m.top = 0, 0
	m.Refresh()
}

// testsHidden is the Mode token of the hide-tests toggle.
const testsHidden = "tests:hidden"

// Mode renders the pane's persisted display state for the layout store:
// GroupMode, then "tests:hidden" while the test files are hidden (#2950),
// "hist:on" while the distribution row shows (#2953) and "view:tree" while
// the Project tab shows the tree (#2954) —
// "project:file,file:class,tests:hidden,hist:on,view:tree".
func (m *Model) Mode() string {
	var parts []string
	if g := m.GroupMode(); g != "" {
		parts = append(parts, g)
	}
	if m.hideTests {
		parts = append(parts, testsHidden)
	}
	if m.hist {
		parts = append(parts, histOn)
	}
	if m.treeView {
		parts = append(parts, viewTree)
	}
	return strings.Join(parts, ",")
}

// SetMode restores Mode's form; unknown tokens (a newer build's) are
// ignored.
func (m *Model) SetMode(mode string) {
	m.hideTests, m.hist, m.treeView = false, false, false
	for _, part := range strings.Split(mode, ",") {
		switch part {
		case testsHidden:
			m.hideTests = true
		case histOn:
			m.hist = true
		case viewTree:
			m.treeView = true
		}
	}
	m.SetGroupMode(mode)
}

// groupLayout is the selected tab's group layout, nil when it does not
// group.
func (m *Model) groupLayout() []Column {
	if m.tab == TabChanges && m.Grouping() == complexity.ByFile {
		return changesFileLayout
	}
	switch m.Grouping() {
	case complexity.ByClass:
		if m.tab == TabFile {
			return fileTabClassLayout
		}
		return classGroupLayout
	case complexity.ByFile:
		return m.churnLayout(fileGroupLayout, fileGroupChurnLayout)
	}
	return nil
}

// groupRows folds function rows into the selected level's group rows,
// keyed by absolute path.
func (m *Model) groupRows(fnRows []Row) []Row {
	files := map[string][]complexity.Function{}
	meta := map[string]Row{}
	for _, r := range fnRows {
		files[r.Path] = append(files[r.Path], r.Fn)
		meta[r.Path] = r
	}
	groups := complexity.GroupFunctions(files, m.Grouping(), m.th)
	out := make([]Row, len(groups))
	for i := range groups {
		g := &groups[i]
		src := meta[g.File]
		out[i] = Row{Path: g.File, Rel: src.Rel, Lang: src.Lang, Test: src.Test, Group: g, Members: members(fnRows, g)}
		if g.Class == "" {
			// A file group carries its file's hotspot numbers (#2957).
			out[i].Commits = m.churn[src.Rel]
			out[i].Hot = complexity.Hot(out[i].Commits, g.Summary)
		}
	}
	return out
}

// members lists the function rows a group aggregates (#2958): the file's
// units, narrowed to the class on a class group.
func members(fnRows []Row, g *complexity.Group) []Row {
	var out []Row
	for _, r := range fnRows {
		if r.Path == g.File && (g.Class == "" || r.Fn.Class == g.Class) {
			out = append(out, r)
		}
	}
	return out
}

// aggregate reports a group row or a Changes file row: a row standing for
// several functions, with no unit of its own.
func (r Row) aggregate() bool { return r.Group != nil || r.FileChange != nil }

// label is what fn: and the bare-word match read: the function name, a
// class group's class, a file group's or a Changes file row's path.
func (r Row) label() string {
	switch {
	case r.Dup != nil:
		return r.dupLabel()
	case r.Trend != nil:
		return r.trendLabel()
	case r.FileChange != nil:
		return r.Rel
	case r.Group == nil:
		return r.Fn.Name
	case r.Group.Class != "":
		return r.Group.Class
	}
	return r.Rel
}

// key identifies a row across refreshes, so the cursor stays on it.
func (r Row) key() string {
	if r.Dup != nil {
		return r.dupKey()
	}
	if r.Trend != nil {
		return r.trendKey()
	}
	if r.Cand != nil {
		return r.candKey()
	}
	if r.Site != nil {
		return r.siteKey()
	}
	if r.FileChange != nil {
		return "c\x00" + r.Path
	}
	if r.Group != nil {
		return "g\x00" + r.Path + "\x00" + r.Group.Class
	}
	if r.Gone {
		return "x\x00" + r.Rel + "\x00" + r.Fn.Name + "\x00" + strconv.Itoa(r.Ordinal)
	}
	return "f\x00" + r.Path + "\x00" + strconv.Itoa(r.Fn.Line) + "\x00" + r.Fn.Name + "\x00" + strconv.Itoa(int(r.Change))
}

// groupValue is value for a group row.
func (r Row) groupValue(c Column) (float64, string) {
	g := r.Group
	switch c {
	case ColFile:
		return 0, r.Rel
	case ColClass:
		return 0, g.Class
	case ColFuncs:
		return float64(g.Functions), ""
	case ColNLOC:
		return float64(g.NLOC), ""
	case ColCCN:
		return float64(g.CCNSum), ""
	case ColCCNAvg:
		return g.CCNAvg, ""
	case ColCCNMax:
		return float64(g.CCNMax), ""
	case ColCog:
		return float64(g.CogSum), ""
	case ColCogAvg:
		return g.CogAvg, ""
	case ColCogMax:
		return float64(g.CogMax), ""
	case ColCmt:
		return float64(complexity.DensityPercent(g.CommentDensity)), ""
	case ColOver:
		return float64(g.OverThreshold), ""
	case ColExcess:
		return float64(g.Excess), ""
	case ColCommits:
		return float64(r.Commits), ""
	case ColHot:
		return float64(r.Hot), ""
	}
	return 0, ""
}

// groupCell is cell for a group row: averages with one decimal.
func (r Row) groupCell(c Column) string {
	switch c {
	case ColFile, ColClass:
		_, s := r.groupValue(c)
		return s
	case ColCCNAvg:
		return fmt1(r.Group.CCNAvg)
	case ColCogAvg:
		return fmt1(r.Group.CogAvg)
	}
	n, _ := r.groupValue(c)
	return strconv.Itoa(int(n))
}

// groupWarn reports whether a group row's cell in column c takes the
// warning colour: a maximum or the aggregate density at or above its
// threshold — the thresholds of the group's file (a directory node: of its
// path, #2960) — any function over threshold, any excess.
func (m *Model) groupWarn(g *complexity.Group, c Column) bool {
	t := m.th(g.File)
	switch c {
	case ColCCNMax:
		return g.CCNMax >= t.CCN
	case ColCogMax:
		return g.CogMax >= t.Cog
	case ColCmt:
		return t.Cmt > 0 && complexity.DensityPercent(g.CommentDensity) >= t.Cmt
	case ColOver:
		return g.OverThreshold > 0
	case ColExcess:
		return g.Excess > 0
	}
	return false
}

// groupCopyText is CopyText for a group row: "path: Class funcs=2 ccn=9
// (max 8) cog=12 (max 12)", the class left out of a file group.
func groupCopyText(r Row) string {
	g := r.Group
	head := r.Rel + ":"
	if g.Class != "" {
		head += " " + g.Class
	}
	return head + " funcs=" + itoa(g.Functions) + " ccn=" + itoa(g.CCNSum) + " (max " + itoa(g.CCNMax) + ")" +
		" cog=" + itoa(g.CogSum) + " (max " + itoa(g.CogMax) + ")"
}

// groupCSVHeader is the grouped export's first row, the CLI's JSON names.
var groupCSVHeader = []string{"file", "class", "functions", "nloc", "ccn_sum", "ccn_avg", "ccn_max",
	"cog_sum", "cog_avg", "cog_max", "comment_lines", "cmt_pct", "over_threshold", "excess"}

// groupCSV renders the visible group rows; the class column stays (empty
// for a file group) so both levels share one header. File groups inside a
// repository append commits and hot (#2957).
func (m *Model) groupCSV() string {
	var b strings.Builder
	hot := m.layoutIndex(ColHot) >= 0
	header := groupCSVHeader
	if hot {
		header = append(header[:len(header):len(header)], "commits", "hot")
	}
	writeCSVRow(&b, header)
	for _, r := range m.rows {
		g := r.Group
		fields := []string{r.Rel, g.Class, itoa(g.Functions), itoa(g.NLOC),
			itoa(g.CCNSum), fmt1(g.CCNAvg), itoa(g.CCNMax), itoa(g.CogSum), fmt1(g.CogAvg), itoa(g.CogMax),
			itoa(g.CommentLines), itoa(complexity.DensityPercent(g.CommentDensity)), itoa(g.OverThreshold), itoa(g.Excess)}
		if hot {
			fields = append(fields, itoa(r.Commits), itoa(r.Hot))
		}
		writeCSVRow(&b, fields)
	}
	return b.String()
}
