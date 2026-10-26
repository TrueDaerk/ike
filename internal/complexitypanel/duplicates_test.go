package complexitypanel

// duplicates_test.go covers the Duplicates tab (#3044): the group rows
// ranked by score, the expansion into occurrences, enter opening an
// occurrence with its range, test-file hiding, the filter row, the copy
// text, the CSV export, the state texts and the swallowed function keys.

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
)

func occ(path string, line, end, start, stop int) complexity.Occurrence {
	return complexity.Occurrence{Path: path, Line: line, EndLine: end, Start: start, End: stop}
}

// dupSample is three groups: a two-file Go clone, a Go clone inside one
// file whose second copy sits in a test file, and a PHP pair.
func dupSample() Duplicates {
	return Duplicates{MinTokens: 50, Ready: true, Groups: []complexity.CloneGroup{
		{Lang: "go", Tokens: 120, Occurrences: []complexity.Occurrence{
			occ("/proj/internal/app/app.go", 10, 40, 0, 120), occ("/proj/internal/ui/list.go", 5, 35, 7, 127)}},
		{Lang: "go", Tokens: 60, Occurrences: []complexity.Occurrence{
			occ("/proj/internal/app/app.go", 50, 58, 200, 260), occ("/proj/internal/app/app_test.go", 3, 11, 0, 60), occ("/proj/internal/app/app_test.go", 20, 28, 70, 130)}},
		{Lang: "php", Tokens: 55, Occurrences: []complexity.Occurrence{
			occ("/proj/web/index.php", 0, 9, 0, 55), occ("/proj/web/index.php", 12, 21, 60, 115)}},
	}}
}

func dupPanel(t *testing.T) *Model {
	t.Helper()
	m := newPanel(t)
	m.SetDuplicates(dupSample())
	m.SetTab(TabDuplicates)
	return m
}

// dupCells renders the Duplicates layout of row i.
func dupCells(m *Model, i int) []string { return m.Rows()[i].cells(dupLayout) }

func TestDuplicatesTabRanksGroups(t *testing.T) {
	m := dupPanel(t)
	if m.Tab() != TabDuplicates || m.Total() != 3 || len(m.Rows()) != 3 {
		t.Fatalf("tab=%v total=%d rows=%d", m.Tab(), m.Total(), len(m.Rows()))
	}
	if col, desc := m.SortColumn(); col != ColScore || !desc {
		t.Fatalf("sort = %v desc=%v, want score descending", col, desc)
	}
	// 120×2 = 240, 60×3 = 180, 55×2 = 110.
	if got := dupCells(m, 0); got[0] != "▸ internal/app/app.go:11–41" || got[1] != "240" || got[2] != "120" || got[3] != "31" || got[4] != "2" || got[5] != "2" {
		t.Fatalf("first row = %q", got)
	}
	if got := dupCells(m, 1); got[0] != "▸ internal/app/app.go:51–59" || got[1] != "180" || got[4] != "3" || got[5] != "2" {
		t.Fatalf("second row = %q", got)
	}
	if got := dupCells(m, 2); got[0] != "▸ web/index.php:1–10" || got[1] != "110" || got[5] != "1" {
		t.Fatalf("third row = %q", got)
	}
	if s := m.Summary(); s.Functions != 0 {
		t.Fatalf("a clone row is no function: %+v", s)
	}
	v := m.View()
	for _, want := range []string{"Duplicates", "3 groups", "7 occurrences", "≥ 50 tokens", "score ▼", "tokens", "occurrences", "3/3 rows", "enter expand", "min 50 tokens"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
	// Sorting by occurrences puts the triple first; the tie-break is rank.
	m.SetSort(ColOccur)
	if got := dupCells(m, 0); got[4] != "3" {
		t.Fatalf("occurrences sort: first row = %q", got)
	}
}

func TestDuplicatesExpandAndOpen(t *testing.T) {
	m := dupPanel(t)
	// enter on a group row expands it in place: one row per occurrence,
	// glued under it, the mark turned.
	if cmd := m.Update(key("enter")); cmd != nil {
		t.Fatal("expanding must not emit a message")
	}
	rows := m.Rows()
	if len(rows) != 5 || !rows[1].occurrence() || !rows[2].occurrence() || rows[3].occurrence() {
		t.Fatalf("rows after expand = %d (%v %v %v)", len(rows), rows[1].occurrence(), rows[2].occurrence(), rows[3].occurrence())
	}
	if got := dupCells(m, 0); got[0] != "▾ internal/app/app.go:11–41" {
		t.Fatalf("expanded mark: %q", got[0])
	}
	if got := dupCells(m, 1); got[0] != dupIndent+"internal/app/app.go:11–41" || got[3] != "31" || got[1] != "" {
		t.Fatalf("first occurrence = %q", got)
	}
	if got := dupCells(m, 2); got[0] != dupIndent+"internal/ui/list.go:6–36" || rows[2].Parent != 0 {
		t.Fatalf("second occurrence = %q parent=%d", got, rows[2].Parent)
	}
	if !m.DuplicateExpanded() || !strings.Contains(m.footer(), "enter expand") {
		t.Fatal("the group row stays selected and expanded")
	}
	// enter on an occurrence opens it with its lines selected.
	m.Update(key("down"))
	m.Update(key("down"))
	if !strings.Contains(m.footer(), "enter open") {
		t.Fatalf("footer on an occurrence: %s", m.footer())
	}
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter on an occurrence must open it")
	}
	msg, ok := cmd().(OpenLocationMsg)
	if !ok || msg.Path != "/proj/internal/ui/list.go" || msg.Line != 5 || msg.EndLine != 35 {
		t.Fatalf("open = %+v", msg)
	}
	// space on an occurrence collapses the parent and selects it; a
	// refresh keeps the expansion state of the others.
	m.Update(key("space"))
	if len(m.Rows()) != 3 || m.Cursor() != 0 || m.DuplicateExpanded() {
		t.Fatalf("collapse: rows=%d cursor=%d expanded=%v", len(m.Rows()), m.Cursor(), m.DuplicateExpanded())
	}
	m.Update(key("down"))
	m.Update(key("space"))
	if len(m.Rows()) != 6 {
		t.Fatalf("the triple expands into three: %d rows", len(m.Rows()))
	}
	m.SetDuplicates(dupSample())
	if len(m.Rows()) != 6 || m.Cursor() != 1 {
		t.Fatalf("a new detection keeps the expansion and the cursor: rows=%d cursor=%d", len(m.Rows()), m.Cursor())
	}
	// A double click on a group row expands it like enter.
	now := time.Unix(100, 0)
	m.SetNow(func() time.Time { return now })
	m.Click(3, m.headerRows()+5)
	now = now.Add(50 * time.Millisecond)
	m.Click(3, m.headerRows()+5)
	if len(m.Rows()) != 8 || m.Cursor() != 5 {
		t.Fatalf("double click on the PHP row: rows=%d cursor=%d", len(m.Rows()), m.Cursor())
	}
}

func TestDuplicatesHideTests(t *testing.T) {
	m := dupPanel(t)
	// 't' drops the test-file occurrences: the triple loses two and falls
	// below two occurrences, so it goes; the others stay.
	m.Update(key("t"))
	if len(m.Rows()) != 2 || m.Total() != 2 {
		t.Fatalf("hidden tests: %d rows (total %d)", len(m.Rows()), m.Total())
	}
	for _, r := range m.Rows() {
		if r.Dup.Group.Tokens == 60 {
			t.Fatal("the group left with one non-test occurrence must go")
		}
	}
	if !strings.Contains(m.View(), "2 groups · 4 occurrences") {
		t.Fatalf("totals must follow the visible population:\n%s", m.View())
	}
	m.Update(key("t"))
	if len(m.Rows()) != 3 {
		t.Fatalf("tests back: %d rows", len(m.Rows()))
	}
	// A group whose every occurrence is a test file is marked so; the
	// filter's test: reads the mark.
	d := Duplicates{Ready: true, MinTokens: 50, Groups: []complexity.CloneGroup{
		{Lang: "go", Tokens: 70, Occurrences: []complexity.Occurrence{occ("/proj/a_test.go", 0, 5, 0, 70), occ("/proj/b_test.go", 0, 5, 0, 70)}},
		{Lang: "go", Tokens: 60, Occurrences: []complexity.Occurrence{occ("/proj/a.go", 0, 5, 0, 60), occ("/proj/b_test.go", 10, 15, 80, 140)}},
	}}
	m.SetDuplicates(d)
	if rows := m.Rows(); !rows[0].Test || rows[1].Test {
		t.Fatalf("test marks = %v / %v", rows[0].Test, rows[1].Test)
	}
	m.filter.SetText("test:no")
	m.Refresh()
	if rows := m.Rows(); len(rows) != 1 || rows[0].Dup.Group.Tokens != 60 {
		t.Fatalf("test:no = %d rows", len(rows))
	}
	m.filter.SetText("")
	m.Update(key("t"))
	if len(m.Rows()) != 0 || !strings.Contains(m.View(), "every duplicate involves a test file") {
		t.Fatalf("all hidden:\n%s", m.View())
	}
}

func TestDuplicatesFilter(t *testing.T) {
	m := dupPanel(t)
	set := func(q string) []Row {
		m.filter.SetText(q)
		m.Refresh()
		return m.Rows()
	}
	if rows := set("lang:php"); len(rows) != 1 || rows[0].Lang != "php" {
		t.Fatalf("lang:php = %d rows", len(rows))
	}
	// file: matches any occurrence, not only the first.
	if rows := set("file:list.go"); len(rows) != 1 || rows[0].Dup.Group.Tokens != 120 {
		t.Fatalf("file:list.go = %d rows", len(rows))
	}
	if rows := set("file:_test"); len(rows) != 1 || rows[0].Dup.Group.Tokens != 60 {
		t.Fatalf("file:_test = %d rows", len(rows))
	}
	if rows := set("-file:app.go"); len(rows) != 1 || rows[0].Lang != "php" {
		t.Fatalf("-file:app.go = %d rows", len(rows))
	}
	// min: / max: bound the sort column (score).
	if rows := set("min:150"); len(rows) != 2 {
		t.Fatalf("min:150 = %d rows", len(rows))
	}
	if rows := set("max:150"); len(rows) != 1 {
		t.Fatalf("max:150 = %d rows", len(rows))
	}
	// Bare words match the locations; the function-level keys match nothing.
	if rows := set("index.php"); len(rows) != 1 {
		t.Fatalf("free text = %d rows", len(rows))
	}
	if rows := set("fn:render"); len(rows) != 0 {
		t.Fatalf("fn: must match no clone row, got %d", len(rows))
	}
	if rows := set(""); len(rows) != 3 {
		t.Fatalf("cleared = %d rows", len(rows))
	}
	if !strings.Contains(m.emptyFor("kind:lambda"), "no row matches the filter") {
		t.Fatal("empty text under a filter")
	}
}

// emptyFor is the empty text after applying filter q.
func (m *Model) emptyFor(q string) string {
	m.filter.SetText(q)
	m.Refresh()
	return m.emptyText()
}

func TestDuplicatesCopyCSVAndMarkdown(t *testing.T) {
	m := dupPanel(t)
	cmd := m.Update(key("y"))
	if cmd == nil {
		t.Fatal("y must copy")
	}
	if msg := cmd().(CopyMsg); msg.Text != "dup 120 tokens × 2: internal/app/app.go:11–41, internal/ui/list.go:6–36" {
		t.Fatalf("copy = %q", msg.Text)
	}
	m.Update(key("enter"))
	m.Update(key("down"))
	if msg := m.Update(key("y"))().(CopyMsg); msg.Text != "internal/app/app.go:11–41 (dup 120 tokens × 2)" {
		t.Fatalf("occurrence copy = %q", msg.Text)
	}
	csv := m.CSV()
	lines := strings.Split(strings.TrimSpace(csv), "\n")
	if len(lines) != 8 || lines[0] != strings.Join(dupCSVHeader, ",") {
		t.Fatalf("csv = %d lines:\n%s", len(lines), csv)
	}
	if lines[1] != "1,240,120,31,2,2,internal/app/app.go,11,41" || lines[2] != "1,240,120,31,2,2,internal/ui/list.go,6,36" || !strings.HasPrefix(lines[3], "2,180,60,9,3,2,") {
		t.Fatalf("csv rows:\n%s", csv)
	}
	if ex := m.Update(key("e"))().(ExportMsg); ex.Label != "Duplicates" || ex.CSV != csv {
		t.Fatalf("export = %+v", ex)
	}
	md := m.Markdown()
	for _, want := range []string{"3 groups", "| score |", "internal/app/app.go:11–41", "240"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, dupIndent+"internal") {
		t.Fatalf("markdown must leave the occurrence rows out:\n%s", md)
	}
}

func TestDuplicatesStateTexts(t *testing.T) {
	m := newPanel(t)
	m.SetTab(TabDuplicates)
	if !strings.Contains(m.View(), "no duplicates yet") {
		t.Fatalf("before any detection:\n%s", m.View())
	}
	m.SetDuplicates(Duplicates{Computing: true, MinTokens: 50})
	if v := m.View(); !strings.Contains(v, "detecting duplicates…") || !strings.Contains(v, "detecting…") {
		t.Fatalf("while detecting:\n%s", v)
	}
	m.SetDuplicates(Duplicates{Ready: true, MinTokens: 50})
	if v := m.View(); !strings.Contains(v, "no duplicate of at least 50 tokens") || !strings.Contains(v, "0 groups") {
		t.Fatalf("empty result:\n%s", v)
	}
	m.Set(sample(), complexity.Stats{Enabled: false})
	if !strings.Contains(m.View(), "complexity.enabled is off") {
		t.Fatalf("disabled index:\n%s", m.View())
	}
}

func TestDuplicatesSwallowsFunctionKeys(t *testing.T) {
	m := dupPanel(t)
	for _, k := range []string{"b", "v", "D", "m", "i", "p", "d", "R", "x", "X", "H"} {
		if cmd := m.Update(key(k)); cmd != nil {
			t.Fatalf("%q must do nothing on the Duplicates tab", k)
		}
	}
	if m.Grouping() != complexity.ByNone || m.treeActive() || m.histShown() || len(m.Rows()) != 3 {
		t.Fatal("the function-level keys must leave the tab alone")
	}
	// The other tabs are untouched by the Duplicates state.
	m.SetTab(TabProject)
	if len(m.Rows()) != 5 || m.Summary().Functions != 5 {
		t.Fatalf("Project tab after Duplicates: %d rows", len(m.Rows()))
	}
	var _ tea.Cmd
}

func TestDuplicatesAskAgent(t *testing.T) {
	m := dupPanel(t)
	// Row 0 spans two files (#3046): `a` names every occurrence and both
	// files, the span covering the first file's occurrences only.
	if why := m.DuplicateAgentRefusal(); why != "" {
		t.Fatalf("refusal = %q", why)
	}
	if !strings.Contains(m.dupFooter(), "a extract") {
		t.Errorf("footer = %q", m.dupFooter())
	}
	cmd := m.Update(key("a"))
	if cmd == nil {
		t.Fatal("a multi-file group must ask the agent")
	}
	req, ok := cmd().(AskAgentMsg)
	if !ok || req.Path != "/proj/internal/app/app.go" || req.Line != 10 || req.EndLine != 40 || req.Tokens != 120 || len(req.Dup) != 2 {
		t.Fatalf("request = %+v", req)
	}
	if req.Name != "2 duplicates of app.go:11–41 across 2 files" {
		t.Fatalf("name = %q", req.Name)
	}
	if p := req.Paths(); len(p) != 2 || p[0] != "/proj/internal/app/app.go" || p[1] != "/proj/internal/ui/list.go" {
		t.Fatalf("paths = %v", p)
	}
	// Row 2 (the PHP pair) lies in one file: `a` names every occurrence.
	m.Update(key("j"))
	m.Update(key("j"))
	if !strings.Contains(m.dupFooter(), "a extract") {
		t.Errorf("footer = %q", m.dupFooter())
	}
	cmd = m.Update(key("a"))
	if cmd == nil {
		t.Fatal("a same-file group must ask the agent")
	}
	req, ok = cmd().(AskAgentMsg)
	if !ok || req.Path != "/proj/web/index.php" || req.Line != 0 || req.EndLine != 21 || req.Lang != "php" || req.Tokens != 55 || len(req.Dup) != 2 {
		t.Fatalf("request = %+v", req)
	}
	if req.Name != "2 duplicates of lines 1–10" || req.Task() != "extract 2 duplicates of lines 1–10" {
		t.Fatalf("name = %q task = %q", req.Name, req.Task())
	}
	// An occurrence row stands for its group; the running group carries ⟳
	// and esc cancels it.
	m.ToggleDuplicate()
	m.Update(key("j"))
	if r, ok := m.AskAgent(); !ok || r.Key() != req.Key() {
		t.Fatalf("occurrence row request = %+v", r)
	}
	m.SetAgentRunning(map[string]bool{req.Key(): true})
	if !strings.Contains(m.View(), "web/index.php:1–10 ⟳") {
		t.Errorf("the running group must carry the mark:\n%s", m.View())
	}
	cmd = m.Update(key("esc"))
	if cmd == nil {
		t.Fatal("esc on a running group must cancel")
	}
	if c, ok := cmd().(CancelAgentMsg); !ok || c.Key() != req.Key() {
		t.Fatalf("cancel = %+v", c)
	}
}
