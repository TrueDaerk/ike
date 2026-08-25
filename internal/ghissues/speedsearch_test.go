package ghissues

// speedsearch_test.go covers the pickers' type-ahead (#2111): the label and
// assignee mutation pickers, the filter overlay's label section and the
// action menu all narrow while typing, esc peels the query before it closes
// the modal, and a hidden row is never written away.

import (
	"strings"
	"testing"

	"ike/internal/forge"
)

// manyLabels is the mutable pane with a label set big enough that arrow-
// walking is the problem the type-ahead solves.
func manyLabels(t *testing.T) (*Model, *[]forge.Mutation) {
	t.Helper()
	m, sent := mutable(t)
	m.SetRepoMeta(forge.RepoMetaMsg{
		Caps: forge.Capabilities{Triage: true, Push: true},
		Labels: []forge.Label{
			{Name: "area/editor"},
			{Name: "area/explorer"},
			{Name: "bug"},
			{Name: "documentation"},
			{Name: "enhancement"},
			{Name: "good first issue"},
		},
		Users: []string{"ada", "dev", "wheatley"},
	})
	return m, sent
}

func TestLabelPickerNarrowsOnTyping(t *testing.T) {
	m, _ := manyLabels(t)
	selectIssue(t, m, 1)
	press(m, "e")
	if !m.LabelEditorOpen() {
		t.Fatal("'e' must open the label picker")
	}
	if got := len(m.EditVisible()); got != 6 {
		t.Fatalf("the picker must start with every label, got %d", got)
	}
	press(m, "d", "o", "c")
	if got := m.EditVisible(); len(got) != 1 || got[0] != "documentation" {
		t.Fatalf("typing 'doc' must narrow to documentation alone, got %v", got)
	}
	if m.SpeedSearchQuery() != "doc" {
		t.Fatalf("query = %q, want doc", m.SpeedSearchQuery())
	}
	// The narrowed row is the one under the cursor, so space toggles it
	// without a single arrow press.
	press(m, "space")
	if got := m.EditSelection(); len(got) != 2 || got[1] != "documentation" {
		t.Fatalf("space must tick the narrowed row, got %v", got)
	}
}

func TestPickerTypeAheadIsVisibleInTheHeading(t *testing.T) {
	m, _ := manyLabels(t)
	selectIssue(t, m, 1)
	press(m, "e", "b", "u")
	if v := m.View(); !strings.Contains(v, "/bu") {
		t.Fatalf("the running query must be rendered:\n%s", v)
	}
}

func TestPickerBackspacePeelsTheQueryBeforeClearingTheSet(t *testing.T) {
	m, _ := manyLabels(t)
	selectIssue(t, m, 1) // #1 carries "bug"
	press(m, "e")
	before := len(m.EditSelection())
	if before == 0 {
		t.Fatal("the fixture issue must open with a ticked label")
	}
	press(m, "d", "o")
	press(m, "backspace")
	if m.SpeedSearchQuery() != "d" {
		t.Fatalf("backspace must peel one rune, got %q", m.SpeedSearchQuery())
	}
	if len(m.EditSelection()) != before {
		t.Fatal("peeling the query must not touch the selection")
	}
	press(m, "backspace") // query empty again
	press(m, "backspace") // now the picker's own "clear the set"
	if got := m.EditSelection(); len(got) != 0 {
		t.Fatalf("backspace on an idle search must clear the set, got %v", got)
	}
}

func TestPickerEscClearsTheQueryThenCloses(t *testing.T) {
	m, _ := manyLabels(t)
	selectIssue(t, m, 1)
	press(m, "e", "b", "u")
	press(m, "esc")
	if !m.LabelEditorOpen() {
		t.Fatal("the first esc must only clear the type-ahead")
	}
	if m.SpeedSearchQuery() != "" {
		t.Fatalf("the query must be gone, got %q", m.SpeedSearchQuery())
	}
	if got := len(m.EditVisible()); got != 6 {
		t.Fatalf("clearing the query must restore every row, got %d", got)
	}
	press(m, "esc")
	if m.LabelEditorOpen() {
		t.Fatal("the second esc must close the picker")
	}
}

// A narrowed picker hides rows; enter must still write the full diff, so a
// label the query hid is neither dropped from the issue nor re-added.
func TestNarrowedPickerWritesTheFullDiff(t *testing.T) {
	m, sent := manyLabels(t)
	selectIssue(t, m, 1)         // #1 carries "bug"
	press(m, "e", "e", "n", "h") // narrows to "enhancement"
	press(m, "space", "enter")
	if len(*sent) != 1 {
		t.Fatalf("the picker must send one mutation, got %v", *sent)
	}
	mut := (*sent)[0]
	if len(mut.AddLabels) != 1 || mut.AddLabels[0] != "enhancement" {
		t.Fatalf("the ticked row must be added, got %v", mut.AddLabels)
	}
	if len(mut.RemoveLabels) != 0 {
		t.Fatalf("a label the query merely hid must not be removed, got %v", mut.RemoveLabels)
	}
}

func TestAssigneePickerNarrowsOnTyping(t *testing.T) {
	m, sent := manyLabels(t)
	selectIssue(t, m, 2) // #2 has no assignee
	press(m, "u")
	if !m.AssigneeEditorOpen() {
		t.Fatal("'u' must open the assignee picker")
	}
	press(m, "w", "h")
	if got := m.EditVisible(); len(got) != 1 || got[0] != "wheatley" {
		t.Fatalf("typing 'wh' must narrow to wheatley alone, got %v", got)
	}
	press(m, "space", "enter")
	if len(*sent) != 1 || len((*sent)[0].Assignees) != 1 || (*sent)[0].Assignees[0] != "wheatley" {
		t.Fatalf("the narrowed pick must be written, got %v", *sent)
	}
}

func TestPickerTypeAheadWithNoMatchExplainsItself(t *testing.T) {
	m, _ := manyLabels(t)
	selectIssue(t, m, 1)
	press(m, "e", "z", "z", "z")
	if got := m.EditVisible(); len(got) != 0 {
		t.Fatalf("a fruitless query must narrow to nothing, got %v", got)
	}
	if v := m.View(); !strings.Contains(v, "nothing matches zzz") {
		t.Fatalf("the empty picker must say why:\n%s", v)
	}
	// The query is still editable — the modal is not a dead end.
	press(m, "backspace", "backspace", "backspace")
	if got := len(m.EditVisible()); got != 6 {
		t.Fatalf("editing the query back must restore the rows, got %d", got)
	}
}

func TestFilterLabelSectionNarrowsOnTyping(t *testing.T) {
	m, _ := manyLabels(t)
	press(m, "l") // opens the overlay on the label section
	if !m.PickerOpen() {
		t.Fatal("'l' must open the filter overlay")
	}
	if got := len(m.FilterVisibleLabels()); got < 6 {
		t.Fatalf("the section must start with every label, got %d", got)
	}
	press(m, "e", "n", "h")
	if got := m.FilterVisibleLabels(); len(got) != 1 || got[0] != "enhancement" {
		t.Fatalf("typing 'enh' must narrow the section to enhancement, got %v", got)
	}
	// Space still toggles, and the toggle lands on the narrowed row.
	press(m, "space")
	if got := m.LabelFilter(); len(got) != 1 || got[0] != "enhancement" {
		t.Fatalf("space must toggle the narrowed label, got %v", got)
	}
}

// The fixed rows above the label section keep their own keys: the type-ahead
// only starts once the cursor is inside the labels.
func TestFilterFixedRowsKeepTheirKeys(t *testing.T) {
	m, _ := manyLabels(t)
	press(m, "f")    // opens on the match row
	press(m, "down") // state row
	before := m.StateFilter()
	press(m, "space")
	if m.StateFilter() == before {
		t.Fatal("space on the state row must still cycle the gate")
	}
	if m.SpeedSearchQuery() != "" {
		t.Fatalf("the fixed rows must not start a type-ahead, got %q", m.SpeedSearchQuery())
	}
}

func TestFilterSectionEscClearsTheQueryThenReverts(t *testing.T) {
	m, _ := manyLabels(t)
	press(m, "l", "b", "u")
	if got := m.FilterVisibleLabels(); len(got) != 1 || got[0] != "bug" {
		t.Fatalf("typing 'bu' must narrow to bug, got %v", got)
	}
	press(m, "esc")
	if !m.PickerOpen() {
		t.Fatal("the first esc must keep the overlay open")
	}
	if m.SpeedSearchQuery() != "" {
		t.Fatalf("the first esc must clear the query, got %q", m.SpeedSearchQuery())
	}
	press(m, "esc")
	if m.PickerOpen() {
		t.Fatal("the second esc must close the overlay")
	}
}

// While a query runs the cursor stays inside the narrowed labels, so a label
// named like one of the rows above is still reachable.
func TestFilterSectionSearchKeepsTheCursorInTheLabels(t *testing.T) {
	m, _ := manyLabels(t)
	press(m, "l", "a", "r", "e", "a")
	if got := len(m.FilterVisibleLabels()); got != 2 {
		t.Fatalf("'area' must narrow to the two area labels, got %v", m.FilterVisibleLabels())
	}
	press(m, "down", "space")
	if got := m.LabelFilter(); len(got) != 1 || got[0] != "area/explorer" {
		t.Fatalf("down must walk the narrowed labels only, got %v", got)
	}
	press(m, "down") // wraps back onto the section's first row
	press(m, "space")
	if got := m.LabelFilter(); len(got) != 2 {
		t.Fatalf("the search must wrap inside the section, got %v", got)
	}
}

func TestActionMenuNarrowsOnTyping(t *testing.T) {
	m := filled(t)
	press(m, "m")
	full := len(m.viewActions())
	press(m, "b", "r", "o")
	acts := m.viewActions()
	if len(acts) == 0 || len(acts) >= full {
		t.Fatalf("typing 'bro' must narrow the menu, got %d of %d", len(acts), full)
	}
	for _, a := range acts {
		if !strings.Contains(a.label, "browser") {
			t.Fatalf("the menu must only keep the browser action, got %q", a.label)
		}
	}
	press(m, "esc")
	if !m.ActionMenuOpen() {
		t.Fatal("the first esc must only clear the type-ahead")
	}
	if len(m.viewActions()) != full {
		t.Fatal("clearing the query must restore every action")
	}
	press(m, "esc")
	if m.ActionMenuOpen() {
		t.Fatal("the second esc must close the menu")
	}
}

// The action menu matches key and label together, so the key a user half
// remembers finds its row too.
func TestActionMenuTypeAheadMatchesTheKey(t *testing.T) {
	m := filled(t)
	press(m, "m", "r")
	acts := m.viewActions()
	if len(acts) == 0 {
		t.Fatal("'r' must keep at least the refresh action")
	}
	found := false
	for _, a := range acts {
		if a.key == "r" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the action keyed 'r' must survive its own letter, got %v", acts)
	}
}

// A modal never inherits the previous one's query.
func TestOpeningAPickerResetsTheSearch(t *testing.T) {
	m, _ := manyLabels(t)
	selectIssue(t, m, 1)
	press(m, "e", "b", "u", "esc", "esc")
	press(m, "u")
	if m.SpeedSearchQuery() != "" {
		t.Fatalf("the assignee picker must open idle, got %q", m.SpeedSearchQuery())
	}
	press(m, "esc")
	press(m, "m")
	if m.SpeedSearchQuery() != "" {
		t.Fatalf("the action menu must open idle, got %q", m.SpeedSearchQuery())
	}
}
