package settings

// list_entry_test.go covers the List entry type (#1139): edited as a
// comma-separated text field, persisted as a TOML string array so the typed
// []string schema field decodes cleanly.

import (
	"testing"

	"ike/internal/config"
)

func listPages() []Page {
	return []Page{
		{Title: "Explorer", Entries: []Entry{
			{Key: "explorer.exclude", Type: List, Title: "Excluded entries", Scope: config.UserScope},
		}},
	}
}

func TestListEntryCommitsArray(t *testing.T) {
	restoreConfig(t)
	m := New(listPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	if !m.editing {
		t.Fatal("enter on a List entry must start a text edit")
	}
	// The field pre-fills with the Flat comma-joined value.
	if m.edit.text != config.Get().Flat()["explorer.exclude"] {
		t.Fatalf("edit prefill = %q, want the flat value", m.edit.text)
	}
	m.edit.text = " .git , *.pyc ,, node_modules "
	apply(t, m.Update(key("enter")))
	got := config.Get().Explorer.Exclude
	want := []string{".git", "*.pyc", "node_modules"}
	if len(got) != len(want) {
		t.Fatalf("exclude = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("exclude = %v, want %v", got, want)
		}
	}
}

// TestListEntryCommitsEmptyList: clearing the field writes an explicit empty
// array (no exclusions), distinct from resetting to the default.
func TestListEntryCommitsEmptyList(t *testing.T) {
	restoreConfig(t)
	m := New(listPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	m.edit.text = ""
	apply(t, m.Update(key("enter")))
	if got := config.Get().Explorer.Exclude; len(got) != 0 {
		t.Fatalf("exclude = %v, want empty", got)
	}
}

// TestExplorerPageInBaseCatalog: the Explorer page exists and carries the
// exclude entry (#1139).
func TestExplorerPageInBaseCatalog(t *testing.T) {
	for _, p := range BasePages([]string{"default"}) {
		if p.Title != "Explorer" {
			continue
		}
		for _, e := range p.Entries {
			if e.Key == "explorer.exclude" && e.Type == List {
				return
			}
		}
	}
	t.Fatal("Explorer page with a List-typed explorer.exclude entry missing")
}
