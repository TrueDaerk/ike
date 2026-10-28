package complexitypanel

import (
	"sort"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
)

// bulk_test.go covers the bulk forms of three one-row keys (#3082):
// shift+space expands / collapses the sites of every visible row, the
// grouping picker's choices and PickGroup, and BaselineMarked's one toggle
// over the filter result.

// expandedNames lists the expanded function rows, sorted.
func expandedNames(m *Model) []string {
	var out []string
	for _, r := range m.Rows() {
		if !r.child() && m.expanded[r.expandKey()] {
			out = append(out, r.Fn.Name)
		}
	}
	sort.Strings(out)
	return out
}

func shiftSpace() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModShift} }

func filterTo(m *Model, q string) {
	m.filter.SetText(q)
	m.Refresh()
}

func TestSitesAllTogglesVisibleRows(t *testing.T) {
	m := sitesPanel(t)
	m.Update(shiftSpace())
	if got := strings.Join(expandedNames(m), ","); got != "ListNav,render" {
		t.Fatalf("expand all: %s", got)
	}
	if len(m.Rows()) != 5+6+1 { // render's six sites, ListNav's one
		t.Fatalf("rows = %v", funcCells(m))
	}
	// Each child sits under its own parent.
	for _, r := range m.Rows() {
		if r.child() && m.Rows()[r.Parent].Fn.Name != r.Fn.Name {
			t.Fatalf("child of %s glued under %s", r.Fn.Name, m.Rows()[r.Parent].Fn.Name)
		}
	}
	// Any expanded → collapse all, the cursor on a site row lands on its
	// parent.
	m.cursor = 1
	if !m.ToggleAllSites() || len(expandedNames(m)) != 0 || len(m.Rows()) != 5 {
		t.Fatalf("collapse all: %v rows %d", expandedNames(m), len(m.Rows()))
	}
	if r, _ := m.Selected(); r.Fn.Name != "render" {
		t.Errorf("cursor on %s, want render", r.Fn.Name)
	}
	// Filtered-out rows are untouched: expand ListNav alone, then render
	// alone, then collapse ListNav alone.
	filterTo(m, "file:ui/")
	m.ToggleAllSites()
	filterTo(m, "")
	if got := strings.Join(expandedNames(m), ","); got != "ListNav" {
		t.Fatalf("filtered expand: %s", got)
	}
	filterTo(m, "file:app/")
	m.ToggleAllSites()
	filterTo(m, "")
	if got := strings.Join(expandedNames(m), ","); got != "ListNav,render" {
		t.Fatalf("second filtered expand: %s", got)
	}
	filterTo(m, "file:ui/")
	m.ToggleAllSites()
	filterTo(m, "")
	if got := strings.Join(expandedNames(m), ","); got != "render" {
		t.Fatalf("filtered collapse: %s", got)
	}
	// space after a bulk expansion joins it; on a single one it replaces.
	m.ToggleAllSites() // collapse render
	selectRow(t, m, "render")
	m.ToggleSites()
	selectRow(t, m, "ListNav")
	m.ToggleSites()
	if got := strings.Join(expandedNames(m), ","); got != "ListNav" {
		t.Fatalf("space replaces a single expansion: %s", got)
	}
	// A grouped tab has nothing to expand.
	m.CycleGroup()
	if m.ToggleAllSites() {
		t.Error("a grouped tab must not expand")
	}
}

func TestGroupChoicesAndPick(t *testing.T) {
	m := groupPanel(t)
	choiceLabels := func() string {
		var out []string
		for _, c := range m.GroupChoices() {
			l := c.Label
			if c.Current {
				l += "*"
			}
			out = append(out, l)
		}
		return strings.Join(out, ",")
	}
	if got := choiceLabels(); got != "none*,class,file,tree view" {
		t.Fatalf("project choices = %s", got)
	}
	if !m.PickGroup(GroupChoice{Level: complexity.ByFile}) || m.Grouping() != complexity.ByFile || m.Mode() != "project:file" {
		t.Fatalf("pick file: %v mode %q", m.Grouping(), m.Mode())
	}
	if !m.PickGroup(GroupChoice{Tree: true}) || !m.TreeView() || choiceLabels() != "none,class,file,tree view*" {
		t.Fatalf("pick tree: tree %v choices %s", m.TreeView(), choiceLabels())
	}
	if !m.PickGroup(GroupChoice{Level: complexity.ByClass}) || m.TreeView() || m.Grouping() != complexity.ByClass {
		t.Fatalf("pick class from the tree: tree %v level %v", m.TreeView(), m.Grouping())
	}
	if labels(m.Rows()) != "Client,Server" {
		t.Errorf("class rows = %s", labels(m.Rows()))
	}
	// The File tab offers none / class only and refuses the file level.
	m.SetTab(TabFile)
	if got := choiceLabels(); got != "none*,class" {
		t.Fatalf("file choices = %s", got)
	}
	if m.PickGroup(GroupChoice{Level: complexity.ByFile}) {
		t.Error("the File tab must refuse the file level")
	}
	m.SetTab(TabTrend)
	if m.GroupChoices() != nil {
		t.Error("the Trend tab does not group")
	}
}

func itemNames(msg BaselineToggleMsg) string {
	var out []string
	for _, it := range msg.Items {
		out = append(out, it.Fn.Name)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func TestBaselineMarkedFilterResult(t *testing.T) {
	m := newPanel(t)
	b := complexity.NewBaseline(15, 15)
	m.SetBaseline(b)
	msg, ok := m.BaselineMarked()
	if !ok || msg.Remove || itemNames(msg) != "ListNav,Page::show,render" {
		t.Fatalf("all over threshold: %+v ok=%v", msg, ok)
	}
	// Only the listed rows count.
	filterTo(m, "file:ui/")
	if msg, ok = m.BaselineMarked(); !ok || itemNames(msg) != "ListNav" {
		t.Fatalf("filtered: %+v ok=%v", msg, ok)
	}
	// A group row stands for its members.
	filterTo(m, "")
	m.CycleGroup()
	m.CycleGroup()
	if msg, ok = m.BaselineMarked(); !ok || itemNames(msg) != "ListNav,Page::show,render" {
		t.Fatalf("grouped: %+v ok=%v", msg, ok)
	}
	// Everything listed accepted → drop them all.
	for _, it := range msg.Items {
		b.Accept(it.Rel, it.Fn, it.Ordinal)
	}
	m.SetBaseline(b)
	if msg, ok = m.BaselineMarked(); !ok || !msg.Remove || len(msg.Items) != 3 {
		t.Fatalf("all accepted: %+v ok=%v", msg, ok)
	}
	// Nothing over threshold or accepted, and the Changes tab: nothing.
	filterTo(m, "file:nothing")
	if _, ok = m.BaselineMarked(); ok {
		t.Error("an empty list must yield nothing")
	}
	m.SetTab(TabChanges)
	if _, ok = m.BaselineMarked(); ok {
		t.Error("the Changes tab must yield nothing")
	}
}

func TestBaselineMarkedMixedPrefersPending(t *testing.T) {
	m, _ := baselinePanel(t) // render, Page::show accepted; ListNav worsened
	msg, ok := m.BaselineMarked()
	if !ok || msg.Remove || itemNames(msg) != "ListNav" {
		t.Fatalf("mixed: %+v ok=%v", msg, ok)
	}
}
