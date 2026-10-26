package complexitypanel

// duplicates.go is the Duplicates tab of the Complexity window (Roadmap
// 0560, #3044): the project's clone groups — exact copies and copies with
// renamed identifiers or changed literals (complexity.Duplicates) — one row
// per group ranked by tokens × occurrences, expandable in place into one
// row per occurrence (`path:line–line`); enter on an occurrence opens it
// with its lines selected. The pane stays a consumer: the root model runs
// the detector off the Update goroutine on every settled index generation
// and hands the result in via SetDuplicates. Test-file hiding ('t') drops
// the occurrences in test files and the groups that fall below two; the
// filter row's file:, lang:, test:, min: / max: and bare words apply.
// Grouping, the breakdown, the baseline and the pins do not: a row is a
// clone group, not a function.

import (
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/filterexpr"
)

// Duplicates is the data behind the Duplicates tab, handed in whole by the
// root model.
type Duplicates struct {
	// Groups are the detected clone groups, best first.
	Groups []complexity.CloneGroup
	// MinTokens is the clone size they were found with
	// (complexity.duplicates_min_tokens).
	MinTokens int
	// Computing is true while the root model runs the detector; Ready once
	// a detection landed at least once.
	Computing bool
	Ready     bool
}

// DupRow is the per-group data of a Duplicates tab row: the group, its
// rank, the occurrences shown (the test-file hiding applied) and — on an
// occurrence row — the occurrence itself.
type DupRow struct {
	Group *complexity.CloneGroup
	// Index is the group's position in Duplicates.Groups, the tie-break.
	Index int
	// Occs are the group's visible occurrences, at least two.
	Occs []complexity.Occurrence
	// Occ is set on an occurrence row, nil on the group row.
	Occ *complexity.Occurrence
	// Open marks an expanded group row.
	Open bool
}

// occurrence reports an occurrence row.
func (r Row) occurrence() bool { return r.Dup != nil && r.Dup.Occ != nil }

// score is tokens × visible occurrences.
func (d *DupRow) score() int { return d.Group.Tokens * len(d.Occs) }

// lines is the longest visible occurrence's line count.
func (d *DupRow) lines() int {
	n := 0
	for _, o := range d.Occs {
		n = max(n, o.Lines())
	}
	return n
}

// files counts the distinct files of the visible occurrences.
func (d *DupRow) files() int {
	seen := map[string]bool{}
	for _, o := range d.Occs {
		seen[o.Path] = true
	}
	return len(seen)
}

// key identifies the group across detections.
func (d *DupRow) key() string {
	g := complexity.CloneGroup{Occurrences: d.Occs}
	return g.Key()
}

// SetDuplicates replaces the Duplicates tab's data and rebuilds its rows.
func (m *Model) SetDuplicates(d Duplicates) {
	m.dup = d
	if m.tab == TabDuplicates {
		m.Refresh()
	}
}

// DuplicatesData reports the tab's current data (tests, the root model).
func (m *Model) DuplicatesData() Duplicates { return m.dup }

// dupLayout is the Duplicates tab's column order.
var dupLayout = []Column{ColFile, ColScore, ColTokens, ColLines, ColOccur, ColDupFiles}

// The expansion marks of a group row.
const (
	dupClosedMark = "▸ "
	dupOpenMark   = "▾ "
	dupIndent     = "    "
)

// dupRows lists the Duplicates tab's population: one row per group whose
// visible occurrences number at least two.
func (m *Model) dupRows() []Row {
	out := make([]Row, 0, len(m.dup.Groups))
	test := map[string]bool{}
	isTest := func(path string) bool {
		t, ok := test[path]
		if !ok {
			t = complexity.IsTestFile(m.root, path)
			test[path] = t
		}
		return t
	}
	for i := range m.dup.Groups {
		g := &m.dup.Groups[i]
		occs := g.Occurrences
		allTest := true
		if m.hideTests {
			occs = nil
			for _, o := range g.Occurrences {
				if !isTest(o.Path) {
					occs = append(occs, o)
				}
			}
		}
		if len(occs) < 2 {
			continue
		}
		for _, o := range occs {
			if !isTest(o.Path) {
				allTest = false
			}
		}
		d := &DupRow{Group: g, Index: i, Occs: occs}
		d.Open = m.dupOpen[d.key()]
		out = append(out, Row{Path: occs[0].Path, Rel: m.relPath(occs[0].Path), Lang: g.Lang, Test: allTest, Dup: d})
	}
	return out
}

// ToggleDuplicate expands the selected group row into its occurrences, or
// collapses it; on an occurrence row it collapses the parent. It reports
// false when the selection is no Duplicates row.
func (m *Model) ToggleDuplicate() bool {
	r, ok := m.Selected()
	if !ok || r.Dup == nil {
		return false
	}
	key := r.Dup.key()
	if r.occurrence() {
		if r.Parent >= 0 && r.Parent < len(m.rows) {
			m.cursor = r.Parent
		}
		delete(m.dupOpen, key)
		m.Refresh()
		return true
	}
	if m.dupOpen == nil {
		m.dupOpen = map[string]bool{}
	}
	if m.dupOpen[key] {
		delete(m.dupOpen, key)
	} else {
		m.dupOpen[key] = true
	}
	m.Refresh()
	return true
}

// DuplicateExpanded reports whether the selected group is expanded.
func (m *Model) DuplicateExpanded() bool {
	r, ok := m.Selected()
	return ok && r.Dup != nil && m.dupOpen[r.Dup.key()]
}

// spliceDuplicates inserts the occurrence rows of every expanded group
// right below it, after the filter and the sort placed the group rows.
func (m *Model) spliceDuplicates() {
	if m.tab != TabDuplicates || len(m.dupOpen) == 0 {
		return
	}
	rows := make([]Row, 0, len(m.rows))
	for _, r := range m.rows {
		rows = append(rows, r)
		if r.Dup == nil || !m.dupOpen[r.Dup.key()] {
			continue
		}
		parent := len(rows) - 1
		for i := range r.Dup.Occs {
			o := &r.Dup.Occs[i]
			rows = append(rows, Row{Path: o.Path, Rel: m.relPath(o.Path), Lang: r.Lang, Test: r.Test, Parent: parent,
				Dup: &DupRow{Group: r.Dup.Group, Index: r.Dup.Index, Occs: r.Dup.Occs, Occ: o}})
		}
	}
	m.rows = rows
}

// dupLocation renders an occurrence as "path:first–last" (1-based lines).
func dupLocation(rel string, o complexity.Occurrence) string {
	return rel + ":" + strconv.Itoa(o.Line+1) + "–" + strconv.Itoa(o.EndLine+1)
}

// dupCell renders the row's text for column c: the group row shows its
// first occurrence's location behind the expansion mark, the clone's
// score, tokens, lines, occurrence and file counts; an occurrence row its
// location indented and its own line count.
func (r Row) dupCell(c Column) string {
	d := r.Dup
	if o := d.Occ; o != nil {
		switch c {
		case ColFile:
			return dupIndent + dupLocation(r.Rel, *o)
		case ColLines:
			return strconv.Itoa(o.Lines())
		}
		return ""
	}
	switch c {
	case ColFile:
		mark := dupClosedMark
		if d.Open {
			mark = dupOpenMark
		}
		return mark + dupLocation(r.Rel, d.Occs[0])
	case ColScore:
		return strconv.Itoa(d.score())
	case ColTokens:
		return strconv.Itoa(d.Group.Tokens)
	case ColLines:
		return strconv.Itoa(d.lines())
	case ColOccur:
		return strconv.Itoa(len(d.Occs))
	case ColDupFiles:
		return strconv.Itoa(d.files())
	}
	return ""
}

// dupValue is the row's sort key on column c: the location as text, the
// numbers otherwise. An occurrence row keeps its parent's keys so a sort
// never separates them (they are spliced after the sort anyway).
func (r Row) dupValue(c Column) (float64, string) {
	d := r.Dup
	switch c {
	case ColFile:
		return 0, r.Rel
	case ColScore:
		return float64(d.score()), ""
	case ColTokens:
		return float64(d.Group.Tokens), ""
	case ColLines:
		return float64(d.lines()), ""
	case ColOccur:
		return float64(len(d.Occs)), ""
	case ColDupFiles:
		return float64(d.files()), ""
	}
	return 0, ""
}

// dupMatches applies the filter row to a Duplicates row: file: matches any
// occurrence's path, lang: the group's language, test: the row's test flag
// (every occurrence in a test file), min: / max: bound the sort column, the
// bare words match the occurrences' locations; the function-level keys
// match nothing.
func (m *Model) dupMatches(q filterexpr.Query, r Row) bool {
	if q.Empty() {
		return true
	}
	bound := func(min bool) func(string) bool {
		return func(v string) bool {
			n, err := strconv.Atoi(v)
			if err != nil {
				return false
			}
			col := m.sortCol
			if !col.numeric() {
				col = ColScore
			}
			x, _ := r.dupValue(col)
			if min {
				return x >= float64(n)
			}
			return x <= float64(n)
		}
	}
	rels := r.dupRels()
	gates := []struct {
		field string
		match func(string) bool
	}{
		{"file", func(v string) bool {
			for _, rel := range rels {
				if filterexpr.MatchPath(v, rel) {
					return true
				}
			}
			return false
		}},
		{"lang", func(v string) bool { return strings.EqualFold(v, r.Lang) }},
		{"test", func(v string) bool { want, ok := testValue(v); return ok && r.Test == want }},
		{"min", bound(true)},
		{"max", bound(false)},
	}
	for _, g := range gates {
		if !q.KeepsAll(g.field, g.match) {
			return false
		}
	}
	for _, field := range []string{"fn", "kind", "class", "depth", "params", "nloc", "baseline", "pinned", "hidden"} {
		if !q.KeepsAll(field, func(string) bool { return false }) {
			return false
		}
	}
	_, ok := q.MatchFree(r.dupLabel())
	return ok
}

// dupRels lists the project-relative paths the row stands for: the
// occurrence's on an occurrence row, every occurrence's on a group row.
func (r Row) dupRels() []string {
	if r.occurrence() {
		return []string{r.Rel}
	}
	rels := make([]string, 0, len(r.Dup.Occs))
	for _, o := range r.Dup.Occs {
		rels = append(rels, relOf(r, o.Path))
	}
	return rels
}

// relOf renders path project-relative like the row's own Rel: the row's
// Rel is its first occurrence's, so the root prefix is what it dropped.
func relOf(r Row, path string) string {
	if root := strings.TrimSuffix(r.Path, r.Rel); root != "" && strings.HasPrefix(path, root) {
		return strings.TrimPrefix(path, root)
	}
	return path
}

// dupLabel is what the bare-word match reads: the locations.
func (r Row) dupLabel() string {
	if r.occurrence() {
		return dupLocation(r.Rel, *r.Dup.Occ)
	}
	parts := make([]string, 0, len(r.Dup.Occs))
	for _, o := range r.Dup.Occs {
		parts = append(parts, dupLocation(relOf(r, o.Path), o))
	}
	return strings.Join(parts, " ")
}

// dupKey identifies the row across refreshes.
func (r Row) dupKey() string {
	k := "d\x00" + r.Dup.key()
	if r.occurrence() {
		k += "\x00" + r.Path + ":" + strconv.Itoa(r.Dup.Occ.Line)
	}
	return k
}

// dupCopyText renders a row for 'y': "dup 120 tokens × 2: a.go:10–40,
// b.go:5–35" on a group row, "a.go:10–40 (dup 120 tokens × 2)" on an
// occurrence.
func dupCopyText(r Row) string {
	d := r.Dup
	size := "dup " + strconv.Itoa(d.Group.Tokens) + " tokens × " + strconv.Itoa(len(d.Occs))
	if r.occurrence() {
		return dupLocation(r.Rel, *d.Occ) + " (" + size + ")"
	}
	return size + ": " + strings.Join(strings.Fields(r.dupLabel()), ", ")
}

// dupCSVHeader is the Duplicates tab's export header.
var dupCSVHeader = []string{"group", "score", "tokens", "lines", "occurrences", "files", "path", "line", "end_line"}

// dupCSV renders the Duplicates tab: one row per occurrence of every
// visible group in the displayed order, expanded or not, numbered by group.
func (m *Model) dupCSV() string {
	var b strings.Builder
	writeCSVRow(&b, dupCSVHeader)
	n := 0
	for _, r := range m.rows {
		d := r.Dup
		if d == nil || r.occurrence() {
			continue
		}
		n++
		for _, o := range d.Occs {
			writeCSVRow(&b, []string{strconv.Itoa(n), strconv.Itoa(d.score()), strconv.Itoa(d.Group.Tokens), strconv.Itoa(d.lines()),
				strconv.Itoa(len(d.Occs)), strconv.Itoa(d.files()), relOf(r, o.Path), strconv.Itoa(o.Line + 1), strconv.Itoa(o.EndLine + 1)})
		}
	}
	return b.String()
}

// dupTotals renders the Duplicates tab's header: the group and occurrence
// counts of the visible population, the clone size, and the detector's
// state.
func (m *Model) dupTotals() string {
	d := m.dup
	var parts []string
	if d.Computing {
		parts = append(parts, "detecting…")
	}
	groups, occs := 0, 0
	for _, r := range m.rows {
		if r.Dup != nil && !r.occurrence() {
			groups++
			occs += len(r.Dup.Occs)
		}
	}
	if d.Ready {
		parts = append(parts, itoa(groups)+" groups", itoa(occs)+" occurrences")
	}
	if d.MinTokens > 0 {
		parts = append(parts, "≥ "+itoa(d.MinTokens)+" tokens")
	}
	return strings.Join(parts, " · ")
}

// dupEmpty explains an empty Duplicates list.
func (m *Model) dupEmpty() string {
	d := m.dup
	switch {
	case m.hasView && m.stats.Unavailable:
		return "(no syntax tree in this build — the index cannot measure)"
	case m.hasView && !m.stats.Enabled:
		return "(complexity.enabled is off)"
	case d.Computing && !d.Ready:
		return "(detecting duplicates…)"
	case !d.Ready:
		return "(no duplicates yet — the tab fills once the index settled)"
	case !m.filter.Empty():
		return "(no row matches the filter)"
	case m.hideTests && len(d.Groups) > 0:
		return "(every duplicate involves a test file — t shows them)"
	}
	return "(no duplicate of at least " + itoa(d.MinTokens) + " tokens)"
}

// dupKeyPress routes the keys the Duplicates tab handles itself: enter and
// space expand or collapse a group row, enter on an occurrence opens it
// with its lines selected; the function-level keys (grouping, the
// breakdown, the marks, the baseline, the pins, the function diff, the
// hidden set, the base prompt, the tree) do nothing here; `a` asks the
// agent to extract a same-file group (#3045).
func (m *Model) dupKeyPress(k string) (tea.Cmd, bool) {
	switch k {
	case "enter":
		r, ok := m.Selected()
		if !ok || r.Dup == nil {
			return nil, true
		}
		if r.occurrence() {
			return m.openOccurrence(r), true
		}
		m.ToggleDuplicate()
		return nil, true
	case "space":
		m.ToggleDuplicate()
		return nil, true
	case "a":
		return m.askDuplicateAgent(), true // #3045
	case "b", "v", "D", "m", "i", "p", "d", "R", "x", "X", "H":
		return nil, true
	}
	return nil, false
}

// openOccurrence asks the root model to open the occurrence with its lines
// selected (OpenLocationMsg with EndLine, the candidate path of #2948).
func (m *Model) openOccurrence(r Row) tea.Cmd {
	o := *r.Dup.Occ
	msg := OpenLocationMsg{Path: r.Path, Line: o.Line, EndLine: o.EndLine}
	return func() tea.Msg { return msg }
}

// dupFooter is the Duplicates tab's hint line.
func (m *Model) dupFooter() string {
	if m.note != "" {
		return " · " + m.note // a refused `a` (#3045)
	}
	hints := " · tab view · s sort · S reverse · enter expand · e csv · E md · y copy · r rescan · / filter"
	if r, ok := m.Selected(); ok && r.occurrence() {
		hints = strings.Replace(hints, "enter expand", "enter open", 1)
	}
	if _, ok := m.AskAgent(); ok {
		hints += " · a extract" // #3045, #3046
		if m.selectedAgentRunning() {
			hints += " · esc cancel agent"
		}
	}
	if m.hideTests {
		hints += " · t tests: hidden"
	} else {
		hints += " · t tests"
	}
	if m.dup.MinTokens > 0 {
		hints += " · min " + itoa(m.dup.MinTokens) + " tokens"
	}
	return hints
}

// dupAgentRequest is the agent request of a Duplicates row (#3045): its
// group's occurrences — every one, the test-file hiding aside — to be
// extracted into one shared helper. Path is the first occurrence's file and
// Line / EndLine span that file's occurrences; a group spanning several
// files (#3046) names its first location and the file count.
func dupAgentRequest(r Row) (AskAgentMsg, string) {
	occs := r.Dup.Group.Occurrences
	if len(occs) < 2 {
		return AskAgentMsg{}, "a extracts a clone group with at least two occurrences"
	}
	path := occs[0].Path
	line, end := occs[0].Line, occs[0].EndLine
	files := map[string]bool{path: true}
	for _, o := range occs[1:] {
		files[o.Path] = true
		if o.Path == path {
			line, end = min(line, o.Line), max(end, o.EndLine)
		}
	}
	first := itoa(occs[0].Line+1) + "–" + itoa(occs[0].EndLine+1)
	name := itoa(len(occs)) + " duplicates of lines " + first
	if len(files) > 1 {
		name = itoa(len(occs)) + " duplicates of " + filepath.Base(path) + ":" + first + " across " + itoa(len(files)) + " files"
	}
	return AskAgentMsg{
		Path: path, Line: line, EndLine: end, Name: name, Lang: r.Dup.Group.Lang,
		Dup: append([]complexity.Occurrence(nil), occs...), Tokens: r.Dup.Group.Tokens,
	}, ""
}

// DuplicateAgentRefusal is why `a` refuses the selected Duplicates row —
// a group of fewer than two occurrences — and "" otherwise (#3045).
func (m *Model) DuplicateAgentRefusal() string {
	r, ok := m.Selected()
	if !ok || r.Dup == nil {
		return ""
	}
	_, why := dupAgentRequest(r)
	return why
}

// askDuplicateAgent is the Duplicates tab's `a`: a group (or one of its
// occurrences) asks the root model to have the agent extract it — within
// one file (#3045) or across several (#3046); a refused one says why in the
// note line.
func (m *Model) askDuplicateAgent() tea.Cmd {
	r, ok := m.Selected()
	if !ok || r.Dup == nil {
		return nil
	}
	req, why := dupAgentRequest(r)
	if why != "" {
		m.note = why
		return nil
	}
	return func() tea.Msg { return req }
}
