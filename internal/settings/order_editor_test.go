package settings

import (
	"reflect"
	"strings"
	"testing"

	"ike/internal/config"
	"ike/internal/fuzzy"
)

func matchOrderPages() []Page {
	return []Page{
		{Title: "Search Ranking", Entries: []Entry{
			{Key: "search.match_order", Type: Order, Title: "Match class order", Scope: config.UserScope,
				Options: fuzzy.ClassNames(), ValidateList: matchOrderValidate},
		}},
	}
}

// openOrderEditor opens the match-order entry's editor in the detail column.
func openOrderEditor(t *testing.T) (*Model, *orderEditor) {
	t.Helper()
	m := New(matchOrderPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*orderEditor)
	if !ok {
		t.Fatalf("editor = %T, want *orderEditor", m.editor)
	}
	return m, ed
}

// TestBasePagesExposeMatchOrder: the shipped schema carries the key as an
// Order entry over the six classes, with the loader's check attached.
func TestBasePagesExposeMatchOrder(t *testing.T) {
	for _, p := range BasePages([]string{"default"}, nil, nil) {
		for _, e := range p.Entries {
			if e.Key != "search.match_order" {
				continue
			}
			if e.Type != Order || !reflect.DeepEqual(e.Options, fuzzy.ClassNames()) || e.ValidateList == nil {
				t.Fatalf("search.match_order entry = %+v", e)
			}
			return
		}
	}
	t.Fatal("no settings entry for search.match_order")
}

func TestOrderEditorOpensOnLiveOrder(t *testing.T) {
	restoreConfig(t)
	_, ed := openOrderEditor(t)
	if !reflect.DeepEqual(ed.items, fuzzy.ClassNames()) {
		t.Fatalf("items = %v, want the default order", ed.items)
	}
	if ed.Dirty() {
		t.Fatal("a freshly opened editor must not read as dirty")
	}
}

// TestOrderEditorMovesAndPersists: shift+down / shift+up (and J / K) move the
// selected class, the settings column shows the staged order, and applying
// writes it to the live config.
func TestOrderEditorMovesAndPersists(t *testing.T) {
	restoreConfig(t)
	m, ed := openOrderEditor(t)
	m.Update(key("down"))
	m.Update(key("down")) // contains
	m.Update(key("shift+down"))
	want := []string{"exact", "prefix", "initials", "contains", "directory", "fuzzy"}
	if !reflect.DeepEqual(ed.items, want) || ed.idx != 3 {
		t.Fatalf("after shift+down: items = %v idx = %d, want %v at 3", ed.items, ed.idx, want)
	}
	m.Update(key("J"))
	m.Update(key("K"))
	m.Update(key("shift+up"))
	m.Update(key("shift+up"))
	want = []string{"exact", "contains", "prefix", "initials", "directory", "fuzzy"}
	if !reflect.DeepEqual(ed.items, want) || ed.idx != 1 {
		t.Fatalf("after moving up: items = %v idx = %d, want %v at 1", ed.items, ed.idx, want)
	}
	if !m.Dirty() {
		t.Fatal("a moved entry must stage a change")
	}
	if got := m.value("search.match_order"); got != strings.Join(want, ",") {
		t.Fatalf("staged value = %q", got)
	}
	m.SetSize(200, 20)
	if view := m.View(); !strings.Contains(view, "exact, contains, prefix") {
		t.Fatalf("the settings column must list the staged order:\n%s", view)
	}
	apply(t, m.applyChanges())
	if got := config.Get().Search.MatchOrder; !reflect.DeepEqual(got, want) {
		t.Fatalf("match_order = %v, want %v", got, want)
	}

	_, reopened := openOrderEditor(t)
	if !reflect.DeepEqual(reopened.items, want) || reopened.Dirty() {
		t.Fatalf("reopened items = %v (dirty %v), want %v", reopened.items, reopened.Dirty(), want)
	}
}

// TestOrderEditorStopsAtTheEnds: the first row does not move up, the last not
// down, and neither stages anything.
func TestOrderEditorStopsAtTheEnds(t *testing.T) {
	restoreConfig(t)
	m, ed := openOrderEditor(t)
	m.Update(key("shift+up"))
	ed.idx = len(ed.items) - 1
	m.Update(key("shift+down"))
	if !reflect.DeepEqual(ed.items, fuzzy.ClassNames()) || m.Dirty() {
		t.Fatalf("items = %v dirty = %v, want the order untouched", ed.items, m.Dirty())
	}
}

// TestOrderEditorNeverEditsMembers: the keys that add, edit and remove a List
// element do nothing here — the members are fixed.
func TestOrderEditorNeverEditsMembers(t *testing.T) {
	restoreConfig(t)
	m, ed := openOrderEditor(t)
	for _, k := range []string{"enter", "d", "x"} {
		m.Update(key(k))
	}
	if !reflect.DeepEqual(ed.items, fuzzy.ClassNames()) || ed.Capturing() || m.Dirty() {
		t.Fatalf("items = %v capturing = %v dirty = %v", ed.items, ed.Capturing(), m.Dirty())
	}
}

// TestOrderItemsRepairsAList: a value that is not a permutation of the
// options opens as one — unknown names and repeats dropped, missing options
// appended in schema order.
func TestOrderItemsRepairsAList(t *testing.T) {
	got := orderItems(fuzzy.ClassNames(), []string{"fuzzy", "scattered", "exact", "fuzzy"})
	want := []string{"fuzzy", "exact", "prefix", "contains", "initials", "directory"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orderItems = %v, want %v", got, want)
	}
}

// TestOrderEditorRejectsInvalidList: a list the loader would reject is
// refused with its message and nothing is staged.
func TestOrderEditorRejectsInvalidList(t *testing.T) {
	restoreConfig(t)
	m, ed := openOrderEditor(t)
	ed.items = []string{"exact", "prefix", "prefix", "initials", "directory", "fuzzy"}
	m.Update(key("shift+down"))
	if ed.err != `match class "prefix" is listed twice` {
		t.Fatalf("err = %q", ed.err)
	}
	if m.Dirty() {
		t.Fatal("a rejected order must not be staged")
	}
	if view := strings.Join(ed.View(80, 12), "\n"); !strings.Contains(view, "listed twice") {
		t.Fatalf("the rejection must be shown:\n%s", view)
	}
	for _, bad := range [][]string{
		{"exact", "prefix", "contains", "initials", "directory", "scattered"},
		{"exact", "prefix"},
	} {
		if matchOrderValidate(bad) == "" {
			t.Errorf("matchOrderValidate(%v) accepted", bad)
		}
	}
	if msg := matchOrderValidate(fuzzy.ClassNames()); msg != "" {
		t.Errorf("the default order was rejected: %s", msg)
	}
}

// TestOrderEditorPointer: a click selects a row, the wheel walks them.
func TestOrderEditorPointer(t *testing.T) {
	restoreConfig(t)
	_, ed := openOrderEditor(t)
	ed.Click(0, 3)
	if ed.idx != 3 {
		t.Fatalf("click: idx = %d, want 3", ed.idx)
	}
	ed.Wheel(10)
	if ed.idx != len(ed.items)-1 {
		t.Fatalf("wheel: idx = %d, want the last row", ed.idx)
	}
	ed.Click(0, 99)
	if ed.idx != len(ed.items)-1 {
		t.Fatalf("a click below the rows must not move the selection, idx = %d", ed.idx)
	}
}
