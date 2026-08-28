package changefeed

import "testing"

// group_test.go covers the per-process grouping the change-feed panel renders
// its titled sections from (#2183).

// sourced is a Changed entry attributed to a process.
func sourced(path string, n int, source string) Entry {
	e := entry(path, n, "before")
	e.Source = source
	return e
}

// TestGroupsSplitByProcess: attributed entries group under their process,
// newest group first, and everything unattributed stays together at the end.
func TestGroupsSplitByProcess(t *testing.T) {
	f := New()
	f.Add(sourced("a.go", 1, "claude"))
	f.Add(entry("plain.go", 2, "x"))
	f.Add(sourced("b.go", 3, "gofmt"))
	f.Add(sourced("c.go", 4, "claude"))

	groups := f.Groups()
	if len(groups) != 3 {
		t.Fatalf("Groups = %d, want claude + gofmt + unknown; got %+v", len(groups), groups)
	}
	// Newest first overall: c.go (claude) leads, then b.go (gofmt).
	if groups[0].Source != "claude" || groups[1].Source != "gofmt" {
		t.Fatalf("group order = %q, %q; want claude then gofmt", groups[0].Source, groups[1].Source)
	}
	if len(groups[0].Entries) != 2 || groups[0].Entries[0].Path != "c.go" {
		t.Fatalf("claude group = %+v, want c.go then a.go", groups[0].Entries)
	}
	last := groups[len(groups)-1]
	if last.Attributed() || len(last.Entries) != 1 || last.Entries[0].Path != "plain.go" {
		t.Fatalf("unknown bucket = %+v, want the unattributed plain.go last", last)
	}
	if !f.Attributed() {
		t.Fatal("Attributed = false with attributed entries in the feed")
	}
}

// TestGroupsUngroupedWithoutAttribution: a feed nothing could be attributed to
// is one unknown bucket in plain feed order — the panel renders it flat.
func TestGroupsUngroupedWithoutAttribution(t *testing.T) {
	f := New()
	f.Add(entry("a.go", 1, "a"))
	f.Add(entry("b.go", 2, "b"))

	if f.Attributed() {
		t.Fatal("Attributed = true with no source anywhere")
	}
	groups := f.Groups()
	if len(groups) != 1 || groups[0].Attributed() {
		t.Fatalf("Groups = %+v, want one unknown bucket", groups)
	}
	if len(groups[0].Entries) != 2 || groups[0].Entries[0].Path != "b.go" {
		t.Fatalf("bucket = %+v, want newest-first feed order", groups[0].Entries)
	}
	var nilFeed *Feed
	if nilFeed.Groups() != nil || nilFeed.Attributed() {
		t.Fatal("a nil feed claimed groups")
	}
}

// TestCoalesceAdoptsLaterSource: a first event with nothing running to blame
// takes a later event's attribution, but an attribution once made is never
// rewritten — the row is about the process that first touched the file.
func TestCoalesceAdoptsLaterSource(t *testing.T) {
	f := New()
	f.Add(entry("a.go", 1, "a"))
	f.Add(sourced("a.go", 2, "claude"))
	if e, _ := f.Get("a.go"); e.Source != "claude" {
		t.Fatalf("Source = %q, want the later attribution adopted", e.Source)
	}
	f.Add(sourced("a.go", 3, "gofmt"))
	if e, _ := f.Get("a.go"); e.Source != "claude" {
		t.Fatalf("Source = %q, want the first attribution kept", e.Source)
	}
}
