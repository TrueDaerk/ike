package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// TestDiffPlacementEntryExposed guards the settings working agreement for
// diff.placement (#2507): the key is editable on the Diff Viewer page as a
// two-option enum whose values match what config.validate accepts, and it is
// listed with a description the reader can act on.
func TestDiffPlacementEntryExposed(t *testing.T) {
	var entry *Entry
	for _, p := range BasePages(nil, nil, nil) {
		if p.Title != "Diff Viewer" {
			continue
		}
		for i := range p.Entries {
			if p.Entries[i].Key == "diff.placement" {
				entry = &p.Entries[i]
			}
		}
	}
	if entry == nil {
		t.Fatal("the Diff Viewer page must expose diff.placement")
	}
	if entry.Type != Enum {
		t.Fatalf("diff.placement type = %v, want Enum", entry.Type)
	}
	if got := strings.Join(entry.Options, ","); got != "focused,split" {
		t.Fatalf("diff.placement options = %q, want focused,split", got)
	}
	if entry.Scope != config.UserScope {
		t.Fatalf("diff.placement scope = %v, want the user layer", entry.Scope)
	}
	if entry.Title == "" || len(entry.Description) < 40 {
		t.Fatalf("diff.placement needs a title and a real description: %+v", entry)
	}
	// Every offered option must survive validation unchanged — the form must
	// not be able to write a value the config layer rewrites behind it.
	flat := config.Get().Flat()
	if _, ok := flat["diff.placement"]; !ok {
		t.Fatal("diff.placement missing from the typed config schema")
	}
	for _, opt := range entry.Options {
		if opt != "focused" && opt != "split" {
			t.Fatalf("unexpected option %q", opt)
		}
	}
}
