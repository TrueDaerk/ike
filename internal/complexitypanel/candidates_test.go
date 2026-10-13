package complexitypanel

import (
	"strings"
	"testing"

	"ike/internal/complexity"
)

// candidates_test.go covers the extraction-candidate rows (#2948): they
// follow the site rows of the expanded function, enter opens the file
// with the range, y copies the line, space / m / i treat them as children
// of the function, and the cursor stays on one across a refresh.

// candidatesView is sitesView with two candidates on render.
func candidatesView() complexity.View {
	v := sitesView()
	render := &v.Files["/proj/internal/app/app.go"][0]
	render.Candidates = []complexity.Candidate{
		{Line: 3, EndLine: 7, Kind: "switch", CogSaving: 4, NewCCN: 3, NewCog: 2, RestCCN: 5, RestCog: 3},
		{Line: 5, EndLine: 5, Kind: "if", CogSaving: 3, NewCCN: 2, NewCog: 1, RestCCN: 6, RestCog: 5},
	}
	return v
}

func candidatesPanel(t *testing.T) *Model {
	t.Helper()
	m := newPanel(t)
	m.Set(candidatesView(), complexity.Stats{Enabled: true})
	return m
}

func TestCandidateRowsFollowTheSites(t *testing.T) {
	m := candidatesPanel(t)
	m.Update(key(" "))
	want := "render,  if +3 (nesting 2),  switch +2 (nesting 1),  if +1,  && +1,  recursion +1,  case," +
		"  extract 4–8 (switch): −4 cog · new fn ccn 3 cog 2 · rest ccn 5 cog 3," +
		"  extract 6 (if): −3 cog · new fn ccn 2 cog 1 · rest ccn 6 cog 5," +
		"Page::show,ListNav,inner,tiny"
	if got := strings.Join(funcCells(m), ","); got != want {
		t.Fatalf("expanded rows =\n%s\nwant\n%s", got, want)
	}
	rows := m.Rows()
	c := rows[7]
	if c.Cand == nil || c.Parent != 0 || c.cell(ColLine) != "4" || c.cell(ColCCN) != "" || c.cell(ColCog) != "" || c.cell(ColFile) != "" {
		t.Errorf("candidate row = %+v cells %q %q %q", c, c.cell(ColLine), c.cell(ColCCN), c.cell(ColCog))
	}
	if v, _ := c.value(ColLine); v != 3 {
		t.Errorf("line value = %v", v)
	}
	// The totals and the CSV stay the functions'.
	if m.Total() != 5 || m.Summary().Functions != 5 {
		t.Errorf("total %d functions %d", m.Total(), m.Summary().Functions)
	}
	if csv := m.CSV(); strings.Contains(csv, "extract") || strings.Count(csv, "\n") != 6 {
		t.Errorf("csv:\n%s", csv)
	}
	if v := m.View(); !strings.Contains(v, "extract 4–8 (switch)") || !strings.Contains(v, "13/5 rows") {
		t.Errorf("view:\n%s", v)
	}
}

func TestCandidateRowOpensRangeAndCopies(t *testing.T) {
	m := candidatesPanel(t)
	m.Update(key(" "))
	for i := 0; i < 7; i++ {
		m.Update(key("j"))
	}
	if r, _ := m.Selected(); r.Cand == nil || r.Cand.Kind != "switch" {
		t.Fatalf("selected %+v", r)
	}
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter yields no command")
	}
	msg, ok := cmd().(OpenLocationMsg)
	if !ok || msg.Path != "/proj/internal/app/app.go" || msg.Line != 3 || msg.EndLine != 7 {
		t.Fatalf("open = %+v", msg)
	}
	cp, ok := m.Update(key("y"))().(CopyMsg)
	if !ok || cp.Text != "internal/app/app.go:4–8: extract (switch): −4 cog · new fn ccn 3 cog 2 · rest ccn 5 cog 3" {
		t.Fatalf("copy = %+v", cp)
	}
	// A one-line candidate opens without a range to select.
	m.Update(key("j"))
	msg = m.Update(key("enter"))().(OpenLocationMsg)
	if msg.Line != 5 || msg.EndLine != 5 {
		t.Errorf("one-line open = %+v", msg)
	}
	if cp := m.Update(key("y"))().(CopyMsg); !strings.HasPrefix(cp.Text, "internal/app/app.go:6: extract (if): −3 cog") {
		t.Errorf("one-line copy = %q", cp.Text)
	}
	// m marks the parent's sites; i toggles the parent's baseline entry;
	// space collapses the parent and selects it.
	if got, ok := m.ShowSites(); !ok || got.Line != m.Rows()[0].Fn.Line {
		t.Errorf("ShowSites on a candidate = %+v %v", got, ok)
	}
	if _, ok := m.ToggleBaseline(); !ok {
		t.Error("i on a candidate row must toggle the parent")
	}
	m.Update(key(" "))
	if r, _ := m.Selected(); r.Fn.Name != "render" || r.child() || len(m.Rows()) != 5 {
		t.Errorf("after collapse: %+v rows %d", r, len(m.Rows()))
	}
}

func TestCandidateCursorSurvivesRefresh(t *testing.T) {
	m := candidatesPanel(t)
	m.Update(key(" "))
	m.Update(key("G"))
	for i := 0; i < 4; i++ {
		m.Update(key("k"))
	}
	if r, _ := m.Selected(); r.Cand == nil || r.Cand.Kind != "if" {
		t.Fatalf("selected %+v", r)
	}
	v := candidatesView()
	v.Gen = 7
	m.Set(v, complexity.Stats{Enabled: true})
	if r, _ := m.Selected(); r.Cand == nil || r.Cand.Kind != "if" {
		t.Errorf("cursor moved to %+v", r)
	}
	// Candidates are no sites: a function with candidates but, somehow,
	// no sites does not expand; a grouped tab shows no candidate rows.
	m.Update(key("b"))
	for _, r := range m.Rows() {
		if r.Cand != nil {
			t.Fatalf("candidate row on a grouped tab: %+v", r)
		}
	}
}
