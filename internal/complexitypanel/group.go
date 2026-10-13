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
// GroupMode / SetGroupMode carry both through a layout save.

// The group layouts, in display order: the File tab drops the file column.
var (
	classGroupLayout     = []Column{ColFile, ColClass, ColFuncs, ColNLOC, ColCCN, ColCCNAvg, ColCCNMax, ColCog, ColCogAvg, ColCogMax, ColCmt, ColOver, ColExcess}
	fileGroupLayout      = append([]Column{ColFile}, classGroupLayout[2:]...)
	fileTabClassLayout   = classGroupLayout[1:]
	groupSumLabels       = map[Column]string{ColCCN: "Σccn", ColCog: "Σcog"}
	groupCycleProjectTab = []complexity.Level{complexity.ByNone, complexity.ByClass, complexity.ByFile}
	groupCycleFileTab    = []complexity.Level{complexity.ByNone, complexity.ByClass}
)

// Grouping reports the selected tab's grouping level; the Changes tab never
// groups.
func (m *Model) Grouping() complexity.Level {
	switch m.tab {
	case TabProject:
		return m.projectGroup
	case TabFile:
		return m.fileGroup
	}
	return complexity.ByNone
}

// CycleGroup moves the selected tab to its next grouping level and reports
// whether the tab groups at all (false on the Changes tab).
func (m *Model) CycleGroup() bool {
	var cycle []complexity.Level
	var cur *complexity.Level
	switch m.tab {
	case TabProject:
		cycle, cur = groupCycleProjectTab, &m.projectGroup
	case TabFile:
		cycle, cur = groupCycleFileTab, &m.fileGroup
	default:
		return false
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

// fitSort resets a sort column the selected layout does not show to CCN
// descending — a group layout has no line / params / depth column, a
// function layout no group column.
func (m *Model) fitSort() {
	if m.layoutIndex(m.sortCol) < 0 {
		m.sortCol, m.desc = ColCCN, true
	}
}

// GroupMode renders both tabs' levels for the layout store: "project:class,
// file:class", the tabs left at none omitted, "" when neither groups.
func (m *Model) GroupMode() string {
	var parts []string
	if m.projectGroup != complexity.ByNone {
		parts = append(parts, "project:"+m.projectGroup.String())
	}
	if m.fileGroup != complexity.ByNone {
		parts = append(parts, "file:"+m.fileGroup.String())
	}
	return strings.Join(parts, ",")
}

// SetGroupMode restores GroupMode's form; an unknown tab or level (a newer
// build's) is ignored, and the File tab never takes the file level.
func (m *Model) SetGroupMode(mode string) {
	m.projectGroup, m.fileGroup = complexity.ByNone, complexity.ByNone
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
// GroupMode, then "tests:hidden" while the test files are hidden (#2950) —
// "project:file,file:class,tests:hidden".
func (m *Model) Mode() string {
	mode := m.GroupMode()
	if !m.hideTests {
		return mode
	}
	if mode == "" {
		return testsHidden
	}
	return mode + "," + testsHidden
}

// SetMode restores Mode's form; unknown tokens (a newer build's) are
// ignored.
func (m *Model) SetMode(mode string) {
	m.hideTests = false
	for _, part := range strings.Split(mode, ",") {
		if part == testsHidden {
			m.hideTests = true
		}
	}
	m.SetGroupMode(mode)
}

// groupLayout is the selected tab's group layout, nil when it does not
// group.
func (m *Model) groupLayout() []Column {
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
	groups := complexity.GroupFunctions(files, m.Grouping(), m.ccnWarn, m.cogWarn, m.cmtWarn)
	out := make([]Row, len(groups))
	for i := range groups {
		g := &groups[i]
		src := meta[g.File]
		out[i] = Row{Path: g.File, Rel: src.Rel, Lang: src.Lang, Test: src.Test, Group: g}
		if g.Class == "" {
			// A file group carries its file's hotspot numbers (#2957).
			out[i].Commits = m.churn[src.Rel]
			out[i].Hot = complexity.Hot(out[i].Commits, g.Summary)
		}
	}
	return out
}

// label is what fn: and the bare-word match read: the function name, a
// class group's class, a file group's path.
func (r Row) label() string {
	switch {
	case r.Group == nil:
		return r.Fn.Name
	case r.Group.Class != "":
		return r.Group.Class
	}
	return r.Rel
}

// key identifies a row across refreshes, so the cursor stays on it.
func (r Row) key() string {
	if r.Site != nil {
		return r.siteKey()
	}
	if r.Group != nil {
		return "g\x00" + r.Path + "\x00" + r.Group.Class
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
// threshold, any function over threshold, any excess.
func (m *Model) groupWarn(g *complexity.Group, c Column) bool {
	switch c {
	case ColCCNMax:
		return g.CCNMax >= m.ccnWarn
	case ColCogMax:
		return g.CogMax >= m.cogWarn
	case ColCmt:
		return m.cmtWarn > 0 && complexity.DensityPercent(g.CommentDensity) >= m.cmtWarn
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
