package complexitypanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
)

// changes_test.go covers the Changes tab (#2918): the sectioned rows with
// their before→after cells, the header's verdict line, the |Δ| sort and
// min:, the dirty mark, the removed-row open, the CSV export, the
// base-revision prompt and the empty states.

// sampleChanges diffs two files against HEAD: render grew, tiny shrank,
// fresh was added, gone was removed with its file, and list.go was renamed.
func sampleChanges() Changes {
	base := map[string][]complexity.Function{
		"/proj/internal/app/app.go": {fn("render", 0, 20, 10, 12), fn("tiny", 30, 32, 4, 3)},
		"/proj/internal/ui/old.go":  {fn("gone", 0, 9, 6, 2)},
	}
	head := map[string][]complexity.Function{
		"/proj/internal/app/app.go": {fn("render", 0, 24, 15, 20), fn("tiny", 34, 36, 3, 3), fn("fresh", 40, 44, 2, 1)},
	}
	return Changes{
		Rev:     "HEAD",
		Sha:     "abc1234",
		Diff:    complexity.DiffProject(base, head, complexity.Uniform(complexity.Thresholds{CCN: 15, Cog: 15, Cmt: 40})),
		Dirty:   map[string]bool{"/proj/internal/app/app.go": true},
		Renames: []string{"internal/ui/old.go → internal/ui/list.go"},
		Ready:   true,
	}
}

func changesPanel(t *testing.T) *Model {
	t.Helper()
	m := newPanel(t)
	m.SetChanges(sampleChanges())
	m.SetTab(TabChanges)
	return m
}

func TestChangesTabSectionsAndCells(t *testing.T) {
	m := changesPanel(t)
	got := names(m.Rows())
	// Sections: changed (|ΔCCN| desc), added, removed.
	if strings.Join(got, ",") != "render,tiny,fresh,gone" {
		t.Fatalf("rows = %v", got)
	}
	rows := m.Rows()
	if rows[0].Change != complexity.Changed || rows[0].DeltaCCN() != 5 || rows[0].DeltaCognitive() != 8 || rows[0].Before.CCN != 10 {
		t.Errorf("render row = %+v", rows[0])
	}
	if rows[1].DeltaCCN() != -1 || rows[2].Change != complexity.Added || rows[2].DeltaCCN() != 2 || rows[3].Change != complexity.Removed || rows[3].DeltaCCN() != -6 {
		t.Errorf("deltas = %d %d %d", rows[1].DeltaCCN(), rows[2].DeltaCCN(), rows[3].DeltaCCN())
	}
	if rows[0].cell(ColCCN) != "10→15 (+5)" || rows[0].cell(ColCog) != "12→20 (+8)" || rows[0].cell(ColNLOC) != "21→25" {
		t.Errorf("render cells = %q %q %q", rows[0].cell(ColCCN), rows[0].cell(ColCog), rows[0].cell(ColNLOC))
	}
	if rows[2].cell(ColCCN) != "-→2 (+2)" || rows[3].cell(ColCCN) != "6→- (-6)" || rows[1].cell(ColCog) != "3→3 (0)" {
		t.Errorf("added/removed cells = %q %q %q", rows[2].cell(ColCCN), rows[3].cell(ColCCN), rows[1].cell(ColCog))
	}
	// The dirty buffer's rows carry the editor tabs' mark; the removed
	// file's row does not.
	if rows[0].cell(ColFunc) != "render*" || rows[3].cell(ColFunc) != "gone" {
		t.Errorf("dirty marks = %q %q", rows[0].cell(ColFunc), rows[3].cell(ColFunc))
	}
	// The head-side summary (removed units excluded).
	if s := m.Summary(); s.Functions != 3 || s.CCNSum != 20 {
		t.Errorf("summary = %+v", s)
	}
	m.SetSize(260, 12) // room for the whole header line
	v := m.View()
	for _, want := range []string{"base HEAD → working tree", "functions 3→3", "ΣCCN 20→20 (0)", "Σcog 17→24 (+7)", "excess 0→5 (+5)", "more complex",
		"1 renamed (removed + added): internal/ui/old.go → internal/ui/list.go", "change", "changed", "added", "removed", "R base", "4/4 rows"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
	// The section name is drawn once per section: "changed" heads render
	// and is blank on tiny's row.
	if strings.Count(v, "changed") != 1 {
		t.Errorf("section label must appear once:\n%s", v)
	}
	// The grid shows the Changes layout: no params / depth columns.
	// (The filter placeholder names the params: / depth: keys.)
	if hdr := strings.Join(m.headerLabels(), " "); strings.Contains(hdr, "params") || strings.Contains(hdr, "depth") {
		t.Errorf("Changes tab must not show params / depth:\n%s", v)
	}
}

func TestChangesSortAndFilterOnDelta(t *testing.T) {
	m := changesPanel(t)
	// Reverse: still sectioned, smallest |ΔCCN| first inside changed.
	m.Update(key("S"))
	if got := names(m.Rows()); strings.Join(got, ",") != "tiny,render,fresh,gone" {
		t.Fatalf("reversed rows = %v", got)
	}
	m.Update(key("S"))
	// min: reads |Δ| on the sort column.
	m.filter.SetText("min:3")
	m.Refresh()
	if got := names(m.Rows()); strings.Join(got, ",") != "render,gone" {
		t.Fatalf("min:3 rows = %v", got)
	}
	m.filter.SetText("metric:cog min:3")
	m.Refresh()
	if got := names(m.Rows()); strings.Join(got, ",") != "render" {
		t.Fatalf("metric:cog min:3 rows = %v", got)
	}
	// max: mirrors min: on |Δ|; |ΔCCN| render 5, tiny 1, fresh 2, gone 6.
	m.filter.SetText("metric:ccn max:2")
	m.Refresh()
	if got := names(m.Rows()); strings.Join(got, ",") != "tiny,fresh" {
		t.Fatalf("max:2 rows = %v", got)
	}
	m.filter.SetText("file:ui/")
	m.Refresh()
	if got := names(m.Rows()); strings.Join(got, ",") != "gone" {
		t.Fatalf("file: rows = %v", got)
	}
	m.filter.SetText("")
	m.Refresh()
	// 's' cycles through the Changes layout only.
	m.SetSort(ColCCN)
	seen := []Column{}
	for i := 0; i < len(changesLayout); i++ {
		m.Update(key("s"))
		col, _ := m.SortColumn()
		seen = append(seen, col)
	}
	if seen[0] != ColCog || seen[1] != ColNLOC || seen[2] != ColCmt || seen[3] != ColChange || seen[len(seen)-1] != ColCCN {
		t.Fatalf("sort cycle = %v", seen)
	}
}

func TestChangesOpenCopyExport(t *testing.T) {
	m := changesPanel(t)
	// Enter on a removed row opens the file at its first line.
	m.cursor = 3
	msg := m.Update(key("enter"))().(OpenLocationMsg)
	if msg.Path != "/proj/internal/ui/old.go" || msg.Line != 0 {
		t.Fatalf("removed open = %+v", msg)
	}
	m.cursor = 0
	msg = m.Update(key("enter"))().(OpenLocationMsg)
	if msg.Path != "/proj/internal/app/app.go" || msg.Line != 0 {
		t.Fatalf("changed open = %+v", msg)
	}
	if got := CopyText(m.Rows()[0]); got != "internal/app/app.go:1: render changed ccn=10→15 (+5) cog=12→20 (+8)" {
		t.Fatalf("copy = %q", got)
	}
	exp := m.Update(key("e"))().(ExportMsg)
	if exp.Label != "Changes" {
		t.Fatalf("label = %q", exp.Label)
	}
	lines := strings.Split(strings.TrimSpace(exp.CSV), "\n")
	if lines[0] != strings.Join(changesCSVHeader, ",") || len(lines) != 5 {
		t.Fatalf("csv = %q", exp.CSV)
	}
	if lines[1] != "changed,internal/app/app.go,render,function,1,10,15,5,12,20,8,21,25,0,0,0" {
		t.Errorf("csv row = %q", lines[1])
	}
	if lines[4] != "removed,internal/ui/old.go,gone,function,1,6,0,-6,2,0,-2,10,0,0,0,0" {
		t.Errorf("removed csv row = %q", lines[4])
	}
}

func TestChangesBasePrompt(t *testing.T) {
	m := changesPanel(t)
	if m.Update(key("R")) != nil || !m.Prompting() || !m.LineInputOpen() {
		t.Fatal("R must open the base prompt")
	}
	if v := m.View(); !strings.Contains(v, "base revision") || !strings.Contains(v, "HEAD") {
		t.Fatalf("prompt row missing:\n%s", v)
	}
	// The seeded text is selected: typing replaces it.
	for _, r := range "main" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if m.revField.Text != "main" {
		t.Fatalf("field = %q", m.revField.Text)
	}
	if !m.PasteText("~2") || m.revField.Text != "main~2" {
		t.Fatalf("paste into the prompt: %q", m.revField.Text)
	}
	cmd := m.Update(key("enter"))
	if cmd == nil || m.Prompting() {
		t.Fatal("enter must close the prompt and send SetBaseMsg")
	}
	if msg, ok := cmd().(SetBaseMsg); !ok || msg.Rev != "main~2" {
		t.Fatalf("msg = %#v", cmd())
	}
	// esc cancels without a message.
	m.Update(key("R"))
	if m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) != nil || m.Prompting() {
		t.Fatal("esc must cancel")
	}
	// A rejected base keeps the previous one and says why.
	c := m.Changes()
	c.Err = "git: unknown revision: nope"
	m.SetChanges(c)
	if v := m.View(); !strings.Contains(v, "base HEAD kept · git: unknown revision: nope") {
		t.Fatalf("error must show inline:\n%s", v)
	}
	// R does nothing on the other tabs or outside a repository.
	m.SetTab(TabProject)
	m.Update(key("R"))
	if m.Prompting() {
		t.Fatal("R on the Project tab must not prompt")
	}
	m.SetTab(TabChanges)
	m.SetChanges(Changes{NotGit: true})
	m.Update(key("R"))
	if m.Prompting() {
		t.Fatal("R outside a repository must not prompt")
	}
}

func TestChangesEmptyStates(t *testing.T) {
	m := newPanel(t)
	m.SetTab(TabChanges)
	if v := m.View(); !strings.Contains(v, "(no diff yet") {
		t.Fatalf("fresh tab:\n%s", v)
	}
	m.SetChanges(Changes{Rev: "HEAD", Computing: true})
	if v := m.View(); !strings.Contains(v, "(measuring the base revision…)") || !strings.Contains(v, "computing…") {
		t.Fatalf("computing:\n%s", v)
	}
	m.SetChanges(Changes{Rev: "main", Ready: true})
	if v := m.View(); !strings.Contains(v, "(no function changed its metrics against main)") || !strings.Contains(v, "unchanged") {
		t.Fatalf("ready + empty:\n%s", v)
	}
	m.SetChanges(Changes{NotGit: true})
	if v := m.View(); !strings.Contains(v, "(not a git repository)") || strings.Contains(v, "ΣCCN") {
		t.Fatalf("not git:\n%s", v)
	}
	// Data set while another tab shows is picked up on the switch.
	m.SetTab(TabProject)
	m.SetChanges(sampleChanges())
	if m.Total() != 5 {
		t.Fatalf("Project total = %d", m.Total())
	}
	m.SetTab(TabChanges)
	if m.Total() != 4 {
		t.Fatalf("Changes total = %d", m.Total())
	}
}
