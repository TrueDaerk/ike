package config

import (
	"strconv"
	"strings"
	"testing"
)

// paneslots_test.go covers layout.pane_slots (#2592): the parser the settings
// form and the app share, and the load-time validation that drops an entry
// the numbering could not honour — always with a diagnostic, because a
// silently ignored assignment reads as a broken chord.

func TestParsePaneSlots(t *testing.T) {
	slots, probs := ParsePaneSlots([]string{"terminal=2", " vcs = 3 ", "", "problems=4"})
	if len(probs) != 0 {
		t.Fatalf("unexpected problems: %v", probs)
	}
	want := "terminal=2 vcs=3 problems=4"
	var got []string
	for _, s := range slots {
		got = append(got, s.Tool+"="+strconv.Itoa(s.Number))
	}
	if strings.Join(got, " ") != want {
		t.Fatalf("slots = %q, want %q", strings.Join(got, " "), want)
	}
}

func TestParsePaneSlotsDropsWhatCannotWork(t *testing.T) {
	for _, tc := range []struct{ in, contains string }{
		{"broken", `not "tool=number"`},
		{"explorer=2", "the explorer is always pane 1"},
		{"typo=2", `unknown tool "typo"`},
		{"vcs=1", "outside 2…9"},
		{"vcs=10", "outside 2…9"},
		{"vcs=x", "is not a number"},
	} {
		slots, probs := ParsePaneSlots([]string{tc.in})
		if len(slots) != 0 {
			t.Errorf("%q was accepted as %v", tc.in, slots)
		}
		if len(probs) != 1 || !strings.Contains(probs[0], tc.contains) {
			t.Errorf("%q reported %v, want a message containing %q", tc.in, probs, tc.contains)
		}
	}
	// Duplicates: the first claim wins, the later one is reported.
	slots, probs := ParsePaneSlots([]string{"vcs=3", "problems=3", "vcs=4"})
	if len(slots) != 1 || slots[0].Tool != "vcs" || slots[0].Number != 3 {
		t.Fatalf("slots = %v, want only the first claim", slots)
	}
	if len(probs) != 2 {
		t.Fatalf("problems = %v, want one per dropped duplicate", probs)
	}
	if !strings.Contains(probs[0], "already reserved") || !strings.Contains(probs[1], "already has a pane number") {
		t.Errorf("duplicate messages must name the conflict: %v", probs)
	}
}

func TestValidatePaneSlots(t *testing.T) {
	c := defaults()
	c.Layout.PaneSlots = []string{"vcs=3", "nope=4", "problems=3"}
	diags := validate(c)
	if got := strings.Join(c.Layout.PaneSlots, ","); got != "vcs=3" {
		t.Fatalf("kept %q, want only the usable entry", got)
	}
	if n := len(diagsFor(diags, "layout.pane_slots")); n != 2 {
		t.Fatalf("%d diagnostics, want one per dropped entry (%v)", n, diags)
	}
}

// The shipped table keeps the four most-used tool windows on 2…5 and leaves
// the rest of the nine numbers to the document panes.
func TestDefaultPaneSlots(t *testing.T) {
	got := strings.Join(defaults().Layout.PaneSlots, ",")
	if want := "terminal=2,vcs=3,problems=4,structure=5"; got != want {
		t.Fatalf("default pane_slots = %q, want %q", got, want)
	}
	if _, probs := ParsePaneSlots(DefaultPaneSlots()); len(probs) != 0 {
		t.Fatalf("the shipped table must validate cleanly: %v", probs)
	}
	if got := Defaults()["layout.pane_slots"]; got != strings.Join(DefaultPaneSlots(), ",") {
		t.Fatalf("flat default = %q, want the joined table", got)
	}
}

// ValidatePaneSlotEntry answers for one element against its peers — the check
// the settings form runs before staging.
func TestValidatePaneSlotEntry(t *testing.T) {
	peers := []string{"vcs=3", "terminal=2"}
	if msg := ValidatePaneSlotEntry("problems=4", peers); msg != "" {
		t.Errorf("a free tool and number was rejected: %s", msg)
	}
	if msg := ValidatePaneSlotEntry("problems=3", peers); !strings.Contains(msg, "already reserved") {
		t.Errorf("a taken number reported %q", msg)
	}
	if msg := ValidatePaneSlotEntry("vcs=4", peers); !strings.Contains(msg, "already has pane number 3") {
		t.Errorf("a tool with a number reported %q", msg)
	}
	if msg := ValidatePaneSlotEntry("vcs=4", []string{"broken"}); msg != "" {
		t.Errorf("a broken peer must not block a good element: %s", msg)
	}
}
