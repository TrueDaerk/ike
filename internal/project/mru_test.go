package project

import (
	"path/filepath"
	"testing"
	"time"

	"ike/internal/palette"
)

// mru_test.go covers the MRU numbering behind project.switchMRU1…9 (#2489):
// the target list the chords resolve against and the digit the picker rows
// carry, which must be the same numbering.

// TestMRUTargetsDropsCurrentProject is the resolution rule: history order,
// newest first, without the project one is standing in — so target 1 is the
// project one came from, project.switchLast's pick.
func TestMRUTargetsDropsCurrentProject(t *testing.T) {
	history := fixedHistory()
	got := MRUTargets(history, "/code/website")
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
	if all := MRUTargets(history, ""); len(all) != len(history) {
		t.Errorf("no current project must keep every entry, got %v", all)
	}
	// The comparison is on cleaned paths, so a trailing-slash spelling of the
	// current root still drops its own row.
	if got := MRUTargets(history, filepath.Clean("/code/ike/")); len(got) != 2 || got[0] != "/code/website" {
		t.Errorf("cleaned current root mismatch: %v", got)
	}
}

// TestMRUHintNumbersFirstNine guards the labels: one per digit key, nothing
// past the ninth entry — a hint no chord can reach would be a lie.
func TestMRUHintNumbersFirstNine(t *testing.T) {
	if got := MRUHint(0); got != "1" {
		t.Errorf("first hint = %q, want \"1\"", got)
	}
	if got := MRUHint(MaxMRU - 1); got != "9" {
		t.Errorf("last hint = %q, want \"9\"", got)
	}
	if got := MRUHint(MaxMRU); got != "" {
		t.Errorf("entry past the ninth = %q, want no hint", got)
	}
	if got := MRUHint(-1); got != "" {
		t.Errorf("negative index = %q, want no hint", got)
	}
}

// TestPickerRowsCarryMRUDigits is the picker half (#2489): the rows show the
// chord digit in front of the name, and the digit follows the *project*, not
// the row — a query that re-sorts the list must not renumber anything.
func TestPickerRowsCarryMRUDigits(t *testing.T) {
	items := pickerItems(t, "")
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %+v", items)
	}
	for i, want := range []string{"1", "2", "3"} {
		if items[i].Hint != want {
			t.Errorf("row %d (%s) hint = %q, want %q", i, items[i].Title, items[i].Hint, want)
		}
	}
	// "intra" is the third-most-recent project and keeps the digit 3 even
	// though the query makes it the only (hence first) row.
	filtered := pickerItems(t, "intra")
	if len(filtered) == 0 || filtered[0].Title != "intra" {
		t.Fatalf("query should list intra first, got %+v", filtered)
	}
	if filtered[0].Hint != "3" {
		t.Errorf("filtered row hint = %q, want the stable MRU digit \"3\"", filtered[0].Hint)
	}
	// The current project is dropped from the list *before* the numbering,
	// so the row below it moves up a digit.
	m, _ := newPicker(t, fixedHistory)
	fromIke := m.Results("", palette.Context{Root: "/code/ike"})
	if len(fromIke) != 2 || fromIke[0].Title != "website" || fromIke[0].Hint != "1" {
		t.Fatalf("from ike the top row must be website with digit 1, got %+v", fromIke)
	}
}

// TestPickerHintsStopAtNine guards the cap: a longer history numbers its
// first nine entries and leaves the rest unlabelled.
func TestPickerHintsStopAtNine(t *testing.T) {
	t0 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	long := func() []Entry {
		var out []Entry
		for i := 0; i < 12; i++ {
			out = append(out, Entry{
				Path:       filepath.Join("/code", string(rune('a'+i))),
				Name:       string(rune('a' + i)),
				LastOpened: t0,
			})
		}
		return out
	}
	m, _ := newPicker(t, long)
	items := m.Results("", palette.Context{})
	if len(items) != 12 {
		t.Fatalf("expected 12 rows, got %d", len(items))
	}
	if items[MaxMRU-1].Hint != "9" {
		t.Errorf("ninth row hint = %q, want \"9\"", items[MaxMRU-1].Hint)
	}
	for _, it := range items[MaxMRU:] {
		if it.Hint != "" {
			t.Errorf("row %q past the ninth carries hint %q", it.Title, it.Hint)
		}
	}
}
