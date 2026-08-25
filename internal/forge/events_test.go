package forge

import "testing"

// TestEventKindConfigKeys: every kind maps onto a distinct forge.notify.* key
// and round-trips through ParseEventKind — the settings page and the config
// write-back both depend on that pairing.
func TestEventKindConfigKeys(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range EventKinds() {
		key := k.ConfigKey()
		if seen[key] {
			t.Fatalf("duplicate config key %q", key)
		}
		seen[key] = true
		back, ok := ParseEventKind(k.Name())
		if !ok || back != k {
			t.Fatalf("ParseEventKind(%q) = %v/%v want %v", k.Name(), back, ok, k)
		}
		if k.Label() == "forge event" {
			t.Fatalf("kind %v has no label", k)
		}
	}
	if len(seen) != 6 {
		t.Fatalf("kinds = %d want 6", len(seen))
	}
	if _, ok := ParseEventKind("nonsense"); ok {
		t.Fatal("an unknown name must not parse")
	}
}

func TestEventSummaryAndIssueGrouping(t *testing.T) {
	e := Event{Kind: IssueOpened, Number: 12, Title: "Fix the thing"}
	if got, want := e.Summary(), "new issue #12 — Fix the thing"; got != want {
		t.Fatalf("Summary() = %q want %q", got, want)
	}
	if got := (Event{Kind: PRMerged, Number: 3}).Summary(); got != "pull request merged #3" {
		t.Fatalf("titleless summary = %q", got)
	}
	if !IssueOpened.IsIssue() || PROpened.IsIssue() {
		t.Fatal("IsIssue must separate issues from pull requests")
	}
}
