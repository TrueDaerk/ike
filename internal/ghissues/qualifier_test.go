package ghissues

import (
	"reflect"
	"strings"
	"testing"
)

// qualifier_test.go covers the match input's structured qualifier layer
// (#2110): the parser, the round-trip into the filter model the overlay
// sections edit, the inline completion and the unknown-qualifier note.

// typeText feeds a string rune by rune into the model, like a user typing.
func typeText(m *Model, s string) {
	for _, r := range s {
		if r == ' ' {
			m.Update(key("space"))
			continue
		}
		m.Update(key(string(r)))
	}
}

func TestQualifierParser(t *testing.T) {
	cases := []struct {
		input string
		final bool
		rest  string
		quals []qualifier
	}{
		{"is:closed ", false, "", []qualifier{{"state", "closed"}}},
		{"state:all ", false, "", []qualifier{{"state", "all"}}},
		{"label:bug crash", false, "crash", []qualifier{{"label", "bug"}}},
		{"label:bug", false, "label:bug", nil},
		{"label:bug", true, "", []qualifier{{"label", "bug"}}},
		{`label:"help wanted" x`, false, "x", []qualifier{{"label", "help wanted"}}},
		{"sort:oldest is:all foo ", false, "foo ",
			[]qualifier{{"sort", "oldest"}, {"state", "all"}}},
		{"author:bo foo ", false, "author:bo foo ", nil},
		{"is:banana ", false, "is:banana ", nil},
		{"label: ", false, "label: ", nil},
		{"plain fuzzy text", false, "plain fuzzy text", nil},
	}
	for _, c := range cases {
		rest, quals := extractQualifiers(c.input, c.final)
		if rest != c.rest || !reflect.DeepEqual(quals, c.quals) {
			t.Errorf("extractQualifiers(%q, %v) = %q, %v; want %q, %v",
				c.input, c.final, rest, quals, c.rest, c.quals)
		}
	}
}

func TestQualifierNote(t *testing.T) {
	cases := []struct{ input, want string }{
		{"author:bo ", `unknown qualifier "author:" — matched literally`},
		{"is:banana ", "is: wants open, closed or all"},
		{"sort:sideways x", "sort: wants newest, number, oldest, relevance, updated"},
		{"label: ", "label: wants a label name"},
		{"is:clo", ""}, // still being typed — no complaint yet
		{"plain text ", ""},
	}
	for _, c := range cases {
		if got := qualNote(c.input); got != c.want {
			t.Errorf("qualNote(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// TestQualifiersWriteFilterModel is the round-trip: one qualifier line writes
// the same filter model the overlay sections edit, and the chips reflect it.
func TestQualifiersWriteFilterModel(t *testing.T) {
	m := filled(t)
	press(m, "f")
	typeText(m, "sort:number label:bug crash ")
	if m.SortOrder() != SortNumber {
		t.Fatalf("sort = %v, want SortNumber", m.SortOrder())
	}
	if got := m.LabelFilter(); !reflect.DeepEqual(got, []string{"bug"}) {
		t.Fatalf("labels = %v, want [bug]", got)
	}
	if m.Filter() != "crash " {
		t.Fatalf("pattern = %q, want %q", m.Filter(), "crash ")
	}
	if m.Visible() != 1 {
		t.Fatalf("visible = %d, want 1 (bug ∧ crash)", m.Visible())
	}
	press(m, "enter")
	v := m.View()
	for _, chip := range []string{"bug", "sort: number", "match: crash"} {
		if !strings.Contains(v, chip) {
			t.Fatalf("chip %q missing from filter row:\n%s", chip, v)
		}
	}
}

// TestQualifiersEqualSections drives one model through qualifiers and another
// through the overlay's section rows; the filter models must come out equal.
func TestQualifiersEqualSections(t *testing.T) {
	a, b := filled(t), filled(t)
	press(a, "f")
	typeText(a, "is:all sort:updated label:feature x ")
	press(a, "enter")

	press(b, "f", "down", "space", "space") // state row: open → closed → all
	press(b, "down", "space", "space", "space")
	// sort row: relevance → newest → oldest → updated
	press(b, "down", "down", "down", "space") // grouping row, then label "feature"
	press(b, "enter")
	press(b, "f") // reopen on the match row for the pattern
	typeText(b, "x ")
	press(b, "enter")

	if a.StateFilter() != FilterAll || a.StateFilter() != b.StateFilter() {
		t.Fatalf("state: qualifiers %v, sections %v", a.StateFilter(), b.StateFilter())
	}
	if a.SortOrder() != SortUpdated || a.SortOrder() != b.SortOrder() {
		t.Fatalf("sort: qualifiers %v, sections %v", a.SortOrder(), b.SortOrder())
	}
	if !reflect.DeepEqual(a.LabelFilter(), []string{"feature"}) ||
		!reflect.DeepEqual(a.LabelFilter(), b.LabelFilter()) {
		t.Fatalf("labels: qualifiers %v, sections %v", a.LabelFilter(), b.LabelFilter())
	}
	if a.Filter() != "x " {
		t.Fatalf("pattern = %q, want %q", a.Filter(), "x ")
	}
}

// TestQualifierEnterTerminates: enter closes the overlay and still extracts a
// trailing qualifier the space never terminated.
func TestQualifierEnterTerminates(t *testing.T) {
	m := filled(t)
	press(m, "f")
	typeText(m, "is:closed")
	press(m, "enter")
	if m.PickerOpen() {
		t.Fatal("enter must close the overlay")
	}
	if m.StateFilter() != FilterClosed || m.Filter() != "" {
		t.Fatalf("state = %v filter = %q, want closed and empty",
			m.StateFilter(), m.Filter())
	}
}

func TestQualifierCompletion(t *testing.T) {
	m := filled(t)
	press(m, "f")
	typeText(m, "la")
	if got := m.matchCompletion(); got != "bel:" {
		t.Fatalf("ghost = %q, want %q", got, "bel:")
	}
	press(m, "tab")
	if m.Filter() != "label:" {
		t.Fatalf("tab must accept the name ghost, filter = %q", m.Filter())
	}
	typeText(m, "b")
	if got := m.matchCompletion(); got != "ug " {
		t.Fatalf("value ghost = %q, want %q", got, "ug ")
	}
	press(m, "tab")
	if m.Filter() != "" || !reflect.DeepEqual(m.LabelFilter(), []string{"bug"}) {
		t.Fatalf("accepted value must extract: filter %q labels %v",
			m.Filter(), m.LabelFilter())
	}
	// Without a ghost tab stays row navigation.
	press(m, "tab")
	if m.Filtering() {
		t.Fatal("tab without a ghost must leave the match row")
	}
}

func TestQualifierCompletionStateAndSort(t *testing.T) {
	m := filled(t)
	press(m, "f")
	typeText(m, "is:c")
	if got := m.matchCompletion(); got != "losed " {
		t.Fatalf("is: ghost = %q, want %q", got, "losed ")
	}
	press(m, "tab")
	if m.StateFilter() != FilterClosed || m.Filter() != "" {
		t.Fatalf("state = %v filter = %q after completion", m.StateFilter(), m.Filter())
	}
	typeText(m, "sort:nu")
	if got := m.matchCompletion(); got != "mber " {
		t.Fatalf("sort: ghost = %q, want %q", got, "mber ")
	}
}

// TestQualifierUnknownStaysLiteral: an unknown qualifier keeps matching as
// fuzzy text and the match row says why.
func TestQualifierUnknownStaysLiteral(t *testing.T) {
	m := filled(t)
	press(m, "f")
	typeText(m, "author:bo ")
	if m.Filter() != "author:bo " {
		t.Fatalf("unknown qualifier must stay in the pattern, filter = %q", m.Filter())
	}
	if v := m.View(); !strings.Contains(v, `unknown qualifier "author:"`) {
		t.Fatalf("the match row must explain the ignored token:\n%s", v)
	}
	if m.Visible() != 0 {
		t.Fatalf("visible = %d, want 0 — the token is fuzzy text", m.Visible())
	}
}

// TestQualifierEscReverts: esc restores the snapshot even after qualifiers
// wrote the model, exactly like section edits.
func TestQualifierEscReverts(t *testing.T) {
	m := filled(t)
	press(m, "f")
	typeText(m, "label:bug sort:number ")
	press(m, "esc")
	if len(m.LabelFilter()) != 0 || m.SortOrder() != SortRelevance || m.Filter() != "" {
		t.Fatalf("esc must revert qualifiers: labels %v sort %v filter %q",
			m.LabelFilter(), m.SortOrder(), m.Filter())
	}
}
