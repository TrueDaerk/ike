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

// --- [[tools.custom]] names (#2601) ---------------------------------------

// A configured custom tool is as assignable as a built-in window, and its
// entry takes part in the same uniqueness rules.
func TestParsePaneSlotsAcceptsCustomToolNames(t *testing.T) {
	slots, probs := ParsePaneSlots([]string{"lazygit=6", "vcs=3"}, "lazygit", "htop")
	if len(probs) != 0 {
		t.Fatalf("unexpected problems: %v", probs)
	}
	if len(slots) != 2 || slots[0].Tool != "lazygit" || slots[0].Number != 6 {
		t.Fatalf("slots = %v, want lazygit=6 first", slots)
	}
	if _, probs := ParsePaneSlots([]string{"lazygit=6", "lazygit=7"}, "lazygit"); len(probs) != 1 ||
		!strings.Contains(probs[0], "already has a pane number") {
		t.Errorf("a custom tool must take one number only, problems = %v", probs)
	}
	// Without the tool configured the same entry is the ordinary unknown-tool
	// drop — which is what a renamed or deleted [[tools.custom]] leaves behind.
	slots, probs = ParsePaneSlots([]string{"lazygit=6"})
	if len(slots) != 0 {
		t.Fatalf("an unconfigured tool was accepted as %v", slots)
	}
	if len(probs) != 1 {
		t.Fatalf("problems = %v, want one", probs)
	}
	for _, want := range []string{`unknown tool "lazygit"`, "structure", "[[tools.custom]]"} {
		if !strings.Contains(probs[0], want) {
			t.Errorf("message %q must name %q — both accepted sources", probs[0], want)
		}
	}
}

// ValidatePaneSlotEntry takes the same custom names, so the settings form and
// the loader agree on what may be typed.
func TestValidatePaneSlotEntryCustomToolName(t *testing.T) {
	if msg := ValidatePaneSlotEntry("lazygit=6", []string{"vcs=3"}, "lazygit"); msg != "" {
		t.Errorf("a configured custom tool was rejected: %s", msg)
	}
	if msg := ValidatePaneSlotEntry("lazygit=3", []string{"vcs=3"}, "lazygit"); !strings.Contains(msg, "already reserved") {
		t.Errorf("a taken number reported %q", msg)
	}
	if msg := ValidatePaneSlotEntry("lazygit=6", nil); !strings.Contains(msg, `unknown tool "lazygit"`) {
		t.Errorf("an unconfigured tool reported %q", msg)
	}
}

// A pane_slots entry naming a custom tool loads clean; one naming a tool that
// is gone is dropped with a layout.pane_slots diagnostic.
func TestValidatePaneSlotsWithCustomTools(t *testing.T) {
	c := defaults()
	c.Tools.Custom = []ToolEntry{{Name: "lazygit", Command: "lazygit"}}
	c.Layout.PaneSlots = []string{"lazygit=6", "gone=7"}
	diags := validate(c)
	if got := strings.Join(c.Layout.PaneSlots, ","); got != "lazygit=6" {
		t.Fatalf("kept %q, want only the configured custom tool", got)
	}
	ds := diagsFor(diags, "layout.pane_slots")
	if len(ds) != 1 || !strings.Contains(ds[0].Message, `unknown tool "gone"`) {
		t.Fatalf("diagnostics = %v, want one naming the vanished tool", ds)
	}
}

// A custom tool named like a built-in window loses the id to the window: the
// entry still works, but it reserves the window — said out loud, because a
// chord opening the wrong thing is worse than a rejected entry.
func TestPaneSlotBuiltinBeatsCustomToolOfTheSameName(t *testing.T) {
	c := defaults()
	c.Tools.Custom = []ToolEntry{{Name: "vcs", Command: "tig"}}
	c.Layout.PaneSlots = []string{"vcs=3"}
	diags := validate(c)
	if got := strings.Join(c.Layout.PaneSlots, ","); got != "vcs=3" {
		t.Fatalf("kept %q, want the entry kept for the built-in window", got)
	}
	ds := diagsFor(diags, "layout.pane_slots")
	if len(ds) != 1 || !strings.Contains(ds[0].Message, "built-in window") {
		t.Fatalf("diagnostics = %v, want one naming the shadowed custom tool", ds)
	}
}

// CustomNames is the shared reading of the configured tool names.
func TestToolsCustomNames(t *testing.T) {
	tools := Tools{Custom: []ToolEntry{{Name: "lazygit"}, {Name: ""}, {Name: "k9s"}}}
	if got := strings.Join(tools.CustomNames(), " "); got != "lazygit k9s" {
		t.Fatalf("CustomNames = %q, want the named entries in config order", got)
	}
}
