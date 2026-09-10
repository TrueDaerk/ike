package settings

// paneslot_hints_test.go covers the Reserved pane numbers entry (#2592): the
// commit validation (shape, range, known tool, uniqueness, the explorer's
// fixed 1), the candidates offered while an element is typed, and the list
// editor wiring — rejection in place, an edited row not conflicting with its
// own old value, and the write through to layout.pane_slots.

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// withPaneSlots installs a config carrying the given reserved-number table.
func withPaneSlots(t *testing.T, slots ...string) {
	t.Helper()
	prev := config.Get()
	c := *prev
	c.Layout.PaneSlots = slots
	config.Set(&c)
	t.Cleanup(func() { config.Set(prev) })
}

func TestPaneSlotValidateMessages(t *testing.T) {
	restoreConfig(t)
	withPaneSlots(t, "vcs=3", "terminal=2")
	cases := []struct{ text, want string }{
		{"problems=4", ""},
		{"usage=9", ""},
		{"broken", `not "tool=number"`},
		{"explorer=1", "the explorer is always pane 1 and takes no assignment"},
		{"vcs=x", `"x" is not a number`},
		{"vcs=1", "pane number 1 is outside 2…9 (1 is the explorer)"},
		{"problems=10", "pane number 10 is outside 2…9 (1 is the explorer)"},
		{"vcs=4", `tool "vcs" already has pane number 3`},
		{"problems=3", `pane number 3 is already reserved for "vcs"`},
	}
	for _, c := range cases {
		if got := paneSlotValidate(lookupLive, c.text); got != c.want {
			t.Errorf("validate(%q) = %q, want %q", c.text, got, c.want)
		}
	}
	got := paneSlotValidate(lookupLive, "typo=4")
	if !strings.Contains(got, `unknown tool "typo"`) || !strings.Contains(got, "structure") {
		t.Errorf("unknown-tool message %q must name the valid ids", got)
	}
}

func TestPaneSlotHintsOfferWhatIsFree(t *testing.T) {
	restoreConfig(t)
	withPaneSlots(t, "vcs=3")
	hints := strings.Join(paneSlotHints(lookupLive, ""), " ")
	if !strings.Contains(hints, "terminal=") || !strings.Contains(hints, "problems=") {
		t.Errorf("bare-token hints %q must offer the unassigned tools", hints)
	}
	if strings.Contains(hints, "vcs=") {
		t.Errorf("bare-token hints %q must drop the tool that already has a number", hints)
	}
	if strings.Contains(hints, "explorer") {
		t.Errorf("bare-token hints %q must not offer the explorer", hints)
	}
	if got := paneSlotHints(lookupLive, "prob"); len(got) != 1 || got[0] != "problems=" {
		t.Errorf("hints for %q = %v, want the narrowed tool", "prob", got)
	}
	nums := strings.Join(paneSlotHints(lookupLive, "terminal="), " ")
	if !strings.Contains(nums, "terminal=2") || strings.Contains(nums, "terminal=3") {
		t.Errorf("number hints %q must offer the free numbers and skip the taken 3", nums)
	}
	if strings.Contains(nums, "terminal=1") {
		t.Errorf("number hints %q must not offer the explorer's 1", nums)
	}
}

// paneSlotPages is the one-entry page the editor tests drive.
func paneSlotPages() []Page {
	for _, p := range BasePages([]string{"default"}, []string{"intellij-light"}, []string{"default"}) {
		for _, e := range p.Entries {
			if e.Key == "layout.pane_slots" {
				return []Page{{Title: "Pane numbers", Entries: []Entry{e}}}
			}
		}
	}
	return nil
}

// openPaneSlotEditor opens the Reserved pane numbers entry's list editor.
func openPaneSlotEditor(t *testing.T) (*Model, *listEditor) {
	t.Helper()
	pages := paneSlotPages()
	if len(pages) == 0 {
		t.Fatal("layout.pane_slots must be a schema entry (settings coverage, #2592)")
	}
	m := New(pages, testOpts(t))
	m.SetSize(90, 24)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*listEditor)
	if !ok {
		t.Fatalf("editor = %T, want *listEditor", m.editor)
	}
	if ed.e.Key != "layout.pane_slots" {
		t.Fatalf("editing %q, want layout.pane_slots", ed.e.Key)
	}
	return m, ed
}

// TestPaneSlotEditorRejectsAndPersists: a bad element is refused in place with
// its message, and a good one writes through to the config.
func TestPaneSlotEditorRejectsAndPersists(t *testing.T) {
	restoreConfig(t)
	withPaneSlots(t, "vcs=3")
	m, ed := openPaneSlotEditor(t)
	ed.idx = len(ed.items)
	m.Update(key("enter"))
	ed.tf.Set("problems=3")
	m.Update(key("enter"))
	if !ed.editing {
		t.Fatal("a rejected element must keep the row in edit mode")
	}
	if want := `pane number 3 is already reserved for "vcs"`; ed.err != want {
		t.Fatalf("err = %q, want %q", ed.err, want)
	}
	if m.Dirty() {
		t.Fatal("a rejected element must stage nothing")
	}
	ed.tf.Set("problems=4")
	m.Update(key("enter"))
	if ed.err != "" {
		t.Fatalf("err = %q, want it cleared", ed.err)
	}
	apply(t, m.applyChanges())
	got := strings.Join(config.Get().Layout.PaneSlots, ",")
	if got != "vcs=3,problems=4" {
		t.Fatalf("pane_slots = %q, want %q", got, "vcs=3,problems=4")
	}
}

// TestPaneSlotEditorRowDoesNotConflictWithItself: re-editing a row checks
// uniqueness against the *other* rows, so keeping the tool and moving its
// number is allowed.
func TestPaneSlotEditorRowDoesNotConflictWithItself(t *testing.T) {
	restoreConfig(t)
	withPaneSlots(t, "vcs=3", "problems=4")
	m, ed := openPaneSlotEditor(t)
	ed.idx = 0
	m.Update(key("enter"))
	ed.tf.Set("vcs=5")
	m.Update(key("enter"))
	if ed.err != "" {
		t.Fatalf("err = %q, want the row's own old value ignored", ed.err)
	}
	apply(t, m.applyChanges())
	if got := strings.Join(config.Get().Layout.PaneSlots, ","); got != "vcs=5,problems=4" {
		t.Fatalf("pane_slots = %q, want %q", got, "vcs=5,problems=4")
	}
}

// TestPaneSlotEditorShowsHints: the candidates are rendered under the input
// and narrowed by the text.
func TestPaneSlotEditorShowsHints(t *testing.T) {
	restoreConfig(t)
	withPaneSlots(t, "vcs=3")
	m, ed := openPaneSlotEditor(t)
	ed.idx = len(ed.items)
	m.Update(key("enter"))
	view := strings.Join(ed.View(80, 20), "\n")
	if !strings.Contains(view, "terminal=") {
		t.Fatalf("the editing view must hint the free tools:\n%s", view)
	}
	ed.tf.Set("structure=")
	view = strings.Join(ed.View(80, 20), "\n")
	if !strings.Contains(view, "structure=2") || strings.Contains(view, "structure=3") {
		t.Fatalf("the view must hint the free numbers and skip the taken 3:\n%s", view)
	}
}
