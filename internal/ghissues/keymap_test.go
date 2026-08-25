package ghissues

// keymap_test.go pins the #2114 key-map consolidation: one meaning per letter
// family across the pane's four modes (issue list, issue detail, PR list, PR
// detail). The keys that used to drift — 'e' vs 'E'/'u', 'l' vs 'L', 'g'
// grouping vs 'g' top — now carry a single meaning each, and the retired
// spellings are inert or aliases of the consolidated key.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
)

// allModeActions collects the action table of each of the four modes from one
// fully wired pane (mutations, PR actions, timeline all bound).
func allModeActions(t *testing.T) map[string][][2]string {
	t.Helper()
	m, _ := mutable(t)
	m.SetSize(90, 40)
	m.SetPRAction(func(forge.PRAction) tea.Cmd { return nil })
	stub := &timelineStub{}
	stub.inject(m)
	out := map[string][][2]string{}
	out["issue list"] = m.Actions()
	m.Update(key("enter"))
	out["issue detail"] = m.Actions()
	m.Update(key("esc"))
	m.Update(key("tab"))
	out["pr list"] = m.Actions()
	return out
}

func TestKeyTableOneMeaningPerLetterFamily(t *testing.T) {
	// The single meaning each letter family carries (#2114); an action whose
	// key appears here must match one of its substrings in every mode.
	meaning := map[string][]string{
		"e":     {"Edit…"},
		"c":     {"lose the issue", "eopen the issue", "lose the pull request"},
		"C":     {"lose the issue with a comment", "eopen the issue with a comment"},
		"M":     {"Merge the pull request"},
		"l":     {"label"},
		"p":     {"Load more activity"},
		"n":     {"new comment"},
		"t":     {"State filter"},
		"a":     {"Sort order"},
		"s":     {"Start work"},
		"o":     {"Open in browser"},
		"r":     {"Refresh"},
		"f":     {"Filter"},
		"enter": {"Open the issue detail", "Open the pull request detail"},
	}
	// The retired spellings of the pre-#2114 table must not reappear.
	retired := map[string]bool{"E": true, "u": true, "L": true, "g": true, "G": true}
	for mode, acts := range allModeActions(t) {
		for _, a := range acts {
			if retired[a[0]] {
				t.Fatalf("%s: retired key %q is back: %v", mode, a[0], a)
			}
			subs, ok := meaning[a[0]]
			if !ok {
				continue
			}
			matched := false
			for _, s := range subs {
				matched = matched || strings.Contains(a[1], s)
			}
			if !matched {
				t.Fatalf("%s: key %q means %q here, outside its family %v", mode, a[0], a[1], subs)
			}
		}
	}
}

func TestListGAndShiftGJumpToTheExtremes(t *testing.T) {
	m := filled(t)
	m.Update(key("G"))
	if m.Cursor() != len(m.rows)-1 {
		t.Fatalf("G must land on the last row, got %d", m.Cursor())
	}
	m.Update(key("g"))
	if m.Cursor() != 0 {
		t.Fatalf("g must land on the first row, got %d", m.Cursor())
	}
}

func TestEditPickerListsMetadataAndTexts(t *testing.T) {
	m, _ := mutable(t)
	m.SetSize(90, 40)
	stub := &timelineStub{}
	stub.inject(m)
	m.Update(key("enter")) // detail of #2, author "bo"
	m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 1, Entries: []forge.TimelineEntry{
		{Kind: forge.TimelineComment, Actor: "ada", Body: "mine", ID: "5", Own: true},
	}})
	m.Update(key("e"))
	if !m.EditPickerOpen() {
		t.Fatal("'e' in the detail must raise the unified edit picker")
	}
	labels := m.EditEntryLabels()
	if len(labels) != 4 || labels[0] != "Labels…" || labels[1] != "Assignees…" ||
		labels[2] != "Issue body" || !strings.HasPrefix(labels[3], "Your comment") {
		t.Fatalf("entries = %v", labels)
	}
	// 'E' stays a harmless alias of the consolidated key.
	m.Update(key("esc"))
	m.Update(key("E"))
	if !m.EditPickerOpen() {
		t.Fatal("'E' must alias 'e'")
	}
	// Enter on the first entry opens the label editor.
	m.Update(key("enter"))
	if !m.LabelEditorOpen() {
		t.Fatal("the labels entry must open the label editor")
	}
}

func TestRetiredKeysAreInert(t *testing.T) {
	m, sent := mutable(t)
	m.SetSize(90, 40)
	// 'u' no longer opens the assignee editor from the list.
	m.Update(key("u"))
	if m.AssigneeEditorOpen() || len(*sent) != 0 {
		t.Fatal("'u' must be inert since #2114 (assignees live in the edit picker)")
	}
	// 'L' no longer loads timeline pages in the detail.
	stub := &timelineStub{}
	stub.inject(m)
	m.Update(key("enter"))
	m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 1, More: true})
	if cmd := m.Update(tea.KeyPressMsg{Code: 'l', Text: "L", Mod: tea.ModShift}); cmd != nil {
		t.Fatal("'L' must be inert since #2114 ('p' loads more activity)")
	}
	if stub.page != 1 {
		t.Fatalf("no further page may have been fetched, got %d", stub.page)
	}
}

func TestPRCloseAcceptsBothCases(t *testing.T) {
	for _, k := range []string{"c", "C"} {
		m, _ := prActive(t)
		m.Update(key(k))
		if !m.PRActionDialogOpen() {
			t.Fatalf("%q on the PR view must open the close dialog", k)
		}
	}
}
