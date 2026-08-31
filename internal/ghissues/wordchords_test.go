package ghissues

// wordchords_test.go covers the word-edit chords inside the pane's pickers
// (#2360). Telemetry showed alt+backspace and ctrl+left leaking past the
// issues pane as unbound: the pickers' type-ahead (ui.SpeedSearch) was
// append-only and rejected every modifier chord, so a user deleting a word
// inside it hit the keymap instead of the input. Every type-ahead now edits
// through ui.EditKey, so the chords land where they were typed.

import (
	"strings"
	"testing"
)

// wordChordCases is the chord set the pane owes a text-entry surface, applied
// to the query "area/edi" with the caret at its end.
var wordChordCases = []struct {
	name  string
	keys  []string
	query string
	hint  string // the rendered query, "" when the case does not check it
}{
	{"alt+backspace deletes the word left", []string{"alt+backspace"}, "area/", "/area/▏"},
	{"ctrl+w deletes the word left", []string{"ctrl+w"}, "area/", "/area/▏"},
	{"super+backspace clears to the start", []string{"super+backspace"}, "", ""},
	{"alt+left then backspace deletes across the caret", []string{"alt+left", "backspace"}, "areaedi", "/area▏edi"},
	{"ctrl+left then backspace deletes across the caret", []string{"ctrl+left", "backspace"}, "areaedi", "/area▏edi"},
	{"alt+right walks the caret back", []string{"alt+left", "alt+left", "alt+right"}, "area/edi", "/area▏/edi"},
	{"ctrl+right walks the caret back", []string{"alt+left", "alt+left", "ctrl+right"}, "area/edi", "/area▏/edi"},
}

func TestLabelPickerTypeAheadTakesTheWordChords(t *testing.T) {
	for _, tc := range wordChordCases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := manyLabels(t)
			selectIssue(t, m, 1)
			press(m, "e", "enter") // the edit picker's first entry is Labels
			press(m, "a", "r", "e", "a", "/", "e", "d", "i")
			if m.SpeedSearchQuery() != "area/edi" {
				t.Fatalf("setup: query = %q", m.SpeedSearchQuery())
			}
			press(m, tc.keys...)
			if got := m.SpeedSearchQuery(); got != tc.query {
				t.Fatalf("query = %q, want %q", got, tc.query)
			}
			if tc.hint != "" {
				if v := m.View(); !strings.Contains(v, tc.hint) {
					t.Fatalf("the caret must render at %q:\n%s", tc.hint, v)
				}
			}
		})
	}
}

func TestAssigneePickerTypeAheadTakesTheWordChords(t *testing.T) {
	m, _ := manyLabels(t)
	selectIssue(t, m, 1)
	press(m, "e", "down", "enter") // Assignees…
	if !m.AssigneeEditorOpen() {
		t.Fatal("the edit picker's assignees entry must open the assignee picker")
	}
	press(m, "w", "h", "e", "a")
	press(m, "alt+backspace")
	if got := m.SpeedSearchQuery(); got != "" {
		t.Fatalf("alt+backspace must delete the word, got %q", got)
	}
}

func TestFilterLabelSectionTypeAheadTakesTheWordChords(t *testing.T) {
	for _, tc := range wordChordCases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := manyLabels(t)
			press(m, "l") // the filter overlay, opened on the label section
			press(m, "a", "r", "e", "a", "/", "e", "d", "i")
			if m.SpeedSearchQuery() != "area/edi" {
				t.Fatalf("setup: query = %q", m.SpeedSearchQuery())
			}
			press(m, tc.keys...)
			if got := m.SpeedSearchQuery(); got != tc.query {
				t.Fatalf("query = %q, want %q", got, tc.query)
			}
		})
	}
}

func TestActionMenuTypeAheadTakesTheWordChords(t *testing.T) {
	m, _ := manyLabels(t)
	press(m, "m")
	if !m.ActionMenuOpen() {
		t.Fatal("m must open the action menu")
	}
	press(m, "l", "a", "b", "e", "l", "s")
	press(m, "alt+backspace")
	if got := m.SpeedSearchQuery(); got != "" {
		t.Fatalf("alt+backspace must delete the word, got %q", got)
	}
}

// A caret is only worth having if typing follows it.
func TestPickerTypeAheadInsertsAtTheCaret(t *testing.T) {
	m, _ := manyLabels(t)
	selectIssue(t, m, 1)
	press(m, "e", "enter")
	press(m, "d", "c")
	press(m, "left", "o")
	if got := m.SpeedSearchQuery(); got != "doc" {
		t.Fatalf("query = %q, want doc", got)
	}
	if got := m.EditVisible(); len(got) != 1 || got[0] != "documentation" {
		t.Fatalf("the narrowing must follow the edit, got %v", got)
	}
}

// The keys the pickers keep for themselves stay theirs while a query runs:
// space toggles the row under the cursor and plain delete clears the working
// set, neither of them an edit of the query.
func TestPickerReservedKeysSurviveARunningQuery(t *testing.T) {
	m, _ := manyLabels(t)
	selectIssue(t, m, 1)
	press(m, "e", "enter")
	press(m, "d", "o", "c")
	press(m, "space")
	if got := m.EditSelection(); len(got) != 2 || got[1] != "documentation" {
		t.Fatalf("space must still toggle the narrowed row, got %v", got)
	}
	press(m, "delete")
	if got := m.EditSelection(); len(got) != 0 {
		t.Fatalf("delete must still clear the set, got %v", got)
	}
	if m.SpeedSearchQuery() != "doc" {
		t.Fatalf("delete must not touch the query, got %q", m.SpeedSearchQuery())
	}
}
