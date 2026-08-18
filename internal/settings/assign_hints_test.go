package settings

// assign_hints_test.go covers the Tool Layout value help (#1946): the hint
// candidates for tools.layout.assign (slot letters from the effective
// template, tool ids from the shared built-in list plus custom tools), the
// prefix narrowing, the commit validation messages, and the list editor
// wiring — rejection in place, hints rendered under the input.

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// withLayoutConfig installs a config carrying the issue template, a custom
// tool and the given assignments, restoring the previous config on cleanup.
func withLayoutConfig(t *testing.T, assign ...string) {
	t.Helper()
	prev := config.Get()
	c := *prev
	c.Tools.Custom = []config.ToolEntry{{Name: "lazygit", Command: "lazygit"}}
	c.Tools.Layout = config.ToolLayout{Template: []string{"XEEH", "XEEH", "TTZZ"}, Assign: assign}
	config.Set(&c)
	t.Cleanup(func() { config.Set(prev) })
}

func lookupLive(key string) string { return liveValue(key) }

func TestTemplateSlotsFollowLookup(t *testing.T) {
	slots := templateSlots(func(string) string { return "[XEEH XEEH TTZZ]" })
	if got := strings.Join(slots, " "); got != "H T X Z" {
		t.Fatalf("slots = %q, want %q (sorted, editor region excluded)", got, "H T X Z")
	}
	if templateSlots(func(string) string { return "" }) != nil {
		t.Fatal("an empty template must yield no slots")
	}
	if templateSlots(func(string) string { return "[XX XE]" }) != nil {
		t.Fatal("a structurally broken template must yield no slots")
	}
}

func TestAssignHintsBareTokenListsSlots(t *testing.T) {
	restoreConfig(t)
	withLayoutConfig(t)
	hints := assignHints(lookupLive, "")
	if got := strings.Join(hints, " "); got != "H= T= X= Z=" {
		t.Fatalf("bare-token hints = %q, want the template's slot letters", got)
	}
	if hints := assignHints(lookupLive, "T"); len(hints) != 1 || hints[0] != "T=" {
		t.Fatalf("hints for %q = %v, want the narrowed slot", "T", hints)
	}
}

func TestAssignHintsSlotPrefixListsTools(t *testing.T) {
	restoreConfig(t)
	withLayoutConfig(t)
	hints := assignHints(lookupLive, "X=")
	joined := strings.Join(hints, " ")
	for _, id := range append(config.BuiltinAssignTools(), "lazygit") {
		if !strings.Contains(joined, id) {
			t.Fatalf("hints %q must offer %q", joined, id)
		}
	}
	if hints := assignHints(lookupLive, "X=laz"); len(hints) != 1 || hints[0] != "lazygit" {
		t.Fatalf("hints for %q = %v, want the narrowed custom tool", "X=laz", hints)
	}
}

func TestAssignHintsWithoutTemplate(t *testing.T) {
	restoreConfig(t)
	hints := assignHints(lookupLive, "")
	if len(hints) != 1 || !strings.Contains(hints[0], "template") {
		t.Fatalf("hints = %v, want the set-a-template pointer", hints)
	}
}

func TestAssignValidateMessages(t *testing.T) {
	restoreConfig(t)
	withLayoutConfig(t)
	cases := []struct{ text, want string }{
		{"X=explorer", ""},
		{"T=lazygit", ""},
		{"X=terminal", ""},
		{"Z=run", ""},
		{"H=debug", ""},
		{"broken", `entry must be "SLOT=tool"`},
		{"E=vcs", `"E" is the editor region and takes no tool`},
		{"Q=explorer", `unknown slot "Q" (template defines H T X Z)`},
	}
	for _, c := range cases {
		if got := assignValidate(lookupLive, c.text); got != c.want {
			t.Fatalf("validate(%q) = %q, want %q", c.text, got, c.want)
		}
	}
	got := assignValidate(lookupLive, "X=typo")
	if !strings.Contains(got, `unknown tool "typo"`) || !strings.Contains(got, "terminal") || !strings.Contains(got, "lazygit") {
		t.Fatalf("unknown-tool message %q must name the valid ids, custom tools included", got)
	}
}

func layoutPages() []Page {
	for _, p := range BasePages([]string{"default"}, nil, nil) {
		if p.Title == "Tool Layout" {
			return []Page{p}
		}
	}
	return nil
}

// openAssignEditor opens the Slot assignments entry's editor.
func openAssignEditor(t *testing.T) (*Model, *listEditor) {
	t.Helper()
	m := New(layoutPages(), testOpts(t))
	m.SetSize(90, 24)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("down")) // template row first, assign second
	m.Update(key("enter"))
	ed, ok := m.editor.(*listEditor)
	if !ok {
		t.Fatalf("editor = %T, want *listEditor", m.editor)
	}
	if ed.e.Key != "tools.layout.assign" {
		t.Fatalf("editing %q, want tools.layout.assign", ed.e.Key)
	}
	return m, ed
}

// TestAssignEditorRejectsInvalidEntry: an invalid element is refused in
// place with the message naming the valid values; correcting it commits.
func TestAssignEditorRejectsInvalidEntry(t *testing.T) {
	restoreConfig(t)
	withLayoutConfig(t)
	m, ed := openAssignEditor(t)
	ed.idx = len(ed.items)
	m.Update(key("enter"))
	ed.tf.Set("Q=explorer")
	m.Update(key("enter"))
	if !ed.editing {
		t.Fatal("a rejected element must keep the row in edit mode")
	}
	if want := `unknown slot "Q" (template defines H T X Z)`; ed.err != want {
		t.Fatalf("err = %q, want %q", ed.err, want)
	}
	if len(ed.items) != 0 || m.Dirty() {
		t.Fatalf("nothing may be staged: items=%v dirty=%v", ed.items, m.Dirty())
	}
	ed.tf.Set("X=terminal")
	m.Update(key("enter"))
	if ed.err != "" {
		t.Fatalf("err = %q, want it cleared", ed.err)
	}
	apply(t, m.applyChanges())
	if got := config.Get().Tools.Layout.Assign; len(got) != 1 || got[0] != "X=terminal" {
		t.Fatalf("assign = %v, want [X=terminal]", got)
	}
}

// TestAssignEditorShowsHints: while an element is being typed, the editor
// view lists the valid values under the input and narrows them with the text.
func TestAssignEditorShowsHints(t *testing.T) {
	restoreConfig(t)
	withLayoutConfig(t)
	m, ed := openAssignEditor(t)
	ed.idx = len(ed.items)
	m.Update(key("enter"))
	view := strings.Join(ed.View(80, 20), "\n")
	for _, hint := range []string{"H=", "T=", "X=", "Z="} {
		if !strings.Contains(view, hint) {
			t.Fatalf("editing view must hint slot %q:\n%s", hint, view)
		}
	}
	ed.tf.Set("X=t")
	view = strings.Join(ed.View(80, 20), "\n")
	for _, id := range []string{"terminal", "tests"} {
		if !strings.Contains(view, id) {
			t.Fatalf("view for %q must hint %q:\n%s", "X=t", id, view)
		}
	}
	if strings.Contains(view, "explorer") {
		t.Fatalf("view for %q must narrow away non-matching ids:\n%s", "X=t", view)
	}
}

// TestAssignHintsFollowStagedTemplate: an uncommitted template edit in the
// same settings session already drives the slot hints.
func TestAssignHintsFollowStagedTemplate(t *testing.T) {
	restoreConfig(t)
	m := New(layoutPages(), testOpts(t))
	m.SetSize(90, 24)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter")) // template entry
	ed := m.editor.(*listEditor)
	for _, row := range []string{"AE", "BE"} {
		ed.idx = len(ed.items)
		m.Update(key("enter"))
		ed.tf.Set(row)
		m.Update(key("enter"))
	}
	if got := strings.Join(assignHints(m.value, ""), " "); got != "A= B=" {
		t.Fatalf("hints = %q, want the staged template's slots", got)
	}
}
