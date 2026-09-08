package project

import (
	"path/filepath"
	"testing"

	"ike/internal/palette"
)

// mru_test.go covers the MRU numbering behind project.switchMRU1…9 (#2489):
// the target list the chords resolve against. Since #2532 the numbering is
// invisible — no row renders its digit — so the picker half of the coverage
// is the guard that the rows carry *no* hint.

// TestMRUTargetsDropsCurrentProject is the resolution rule: history order,
// newest first, without the project one is standing in — so target 1 is the
// project one came from, project.switchLast's pick.
func TestMRUTargetsDropsCurrentProject(t *testing.T) {
	history := fixedHistory()
	got := MRUTargets(history, "/code/website", Group{})
	want := []string{"/code/ike", "/work/intra"}
	if len(got) != len(want) {
		t.Fatalf("targets = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("target %d = %q, want %q", i+1, got[i], w)
		}
	}
	// An unresolvable current project filters nothing out.
	if all := MRUTargets(history, "", Group{}); len(all) != len(history) {
		t.Errorf("no current project must keep every entry, got %v", all)
	}
	// The comparison is on cleaned paths, so a trailing-slash spelling of the
	// current root still drops its own row.
	if got := MRUTargets(history, filepath.Clean("/code/ike/"), Group{}); len(got) != 2 || got[0] != "/code/website" {
		t.Errorf("cleaned current root mismatch: %v", got)
	}
}

// TestPickerRowsCarryNoMRUDigit is the picker half of #2532: the chords are
// unchanged, but nothing in the list renders their number any more — the
// digits read as noise in front of the project names.
func TestPickerRowsCarryNoMRUDigit(t *testing.T) {
	items := pickerItems(t, "")
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %+v", items)
	}
	for _, it := range items {
		if it.Hint != "" {
			t.Errorf("row %q carries hint %q, want none", it.Title, it.Hint)
		}
	}
	for _, it := range pickerItems(t, "intra") {
		if it.Hint != "" {
			t.Errorf("filtered row %q carries hint %q, want none", it.Title, it.Hint)
		}
	}
}

// TestPickerListsHistoryNewestFirst is the ordering guard (#2532): an empty
// query lists the history verbatim, newest first, with the current project
// dropped (#2317).
func TestPickerListsHistoryNewestFirst(t *testing.T) {
	m, _ := newPicker(t, fixedHistory)
	items := m.Results("", palette.Context{Root: "/code/ike"})
	want := []string{"website", "intra"}
	if len(items) != len(want) {
		t.Fatalf("expected %d rows, got %+v", len(want), items)
	}
	for i, w := range want {
		if items[i].Title != w {
			t.Errorf("row %d = %q, want %q", i, items[i].Title, w)
		}
	}
}
