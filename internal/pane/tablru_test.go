package pane

import "testing"

// tablru_test.go covers the pane-level ingredients of the tab-limit LRU pick
// (#2640): the recency stamp the paths that do not switch tabs use, the
// restored recency order, the pinned-tab-free count the limit measures, and
// the deterministic tie-break.

func TestTouchTabStampsWithoutSwitching(t *testing.T) {
	i := editorInst(t)
	i.AddTab() // tab 1, active
	before := i.TabLastUsed(0)
	if !i.TouchTab(0) {
		t.Fatal("TouchTab must accept a valid index")
	}
	if i.ActiveTab() != 1 {
		t.Fatalf("TouchTab must not change the active tab, active = %d", i.ActiveTab())
	}
	if i.TabLastUsed(0) <= before || i.TabLastUsed(0) <= i.TabLastUsed(1) {
		t.Fatalf("the touched tab must carry the highest stamp, got %d vs %d",
			i.TabLastUsed(0), i.TabLastUsed(1))
	}
	if i.TouchTab(-1) || i.TouchTab(9) {
		t.Fatal("out-of-range indexes must be refused")
	}
}

func TestSetTabRecencyOrdersRestoredTabs(t *testing.T) {
	i := editorInst(t)
	i.AddTab()
	i.AddTab() // three tabs, tab 2 active
	// A restore's ranks: tab 1 most recently used, tab 0 least.
	i.SetTabRecency(1, 2)
	i.SetTabRecency(0, 1)
	if i.TabLastUsed(1) <= i.TabLastUsed(0) {
		t.Fatalf("restored ranks must keep their order, got %d vs %d",
			i.TabLastUsed(1), i.TabLastUsed(0))
	}
	// A later activation still wins over every restored rank.
	i.ActivateTab(0)
	if i.TabLastUsed(0) <= i.TabLastUsed(1) {
		t.Fatal("an activation after the restore must outrank the restored ranks")
	}
	if i.SetTabRecency(0, 0) || i.SetTabRecency(7, 3) {
		t.Fatal("a non-positive rank and an out-of-range index must be refused")
	}
}

func TestLimitTabCountExcludesPinnedTabs(t *testing.T) {
	i := editorInst(t)
	i.AddTab()
	i.AddTab()
	if got := i.LimitTabCount(); got != i.FileTabCount() {
		t.Fatalf("without pins both counts must agree, %d vs %d", got, i.FileTabCount())
	}
	i.SetTabPinned(0, true)
	i.SetTabPinned(1, true)
	if got, want := i.LimitTabCount(), i.FileTabCount()-2; got != want {
		t.Fatalf("LimitTabCount = %d, want %d (pinned tabs exempt)", got, want)
	}
}

// TestEvictableLRUTabTieBreaksByDistance: with no recency to go by — the tab
// list of a session saved before recency was persisted — the pick is the tab
// furthest from the active one, so it moves with the active tab instead of
// recycling one fixed slot.
func TestEvictableLRUTabTieBreaksByDistance(t *testing.T) {
	i := editorInst(t)
	for n := 0; n < 4; n++ {
		i.AddTab()
	}
	// Five file-backed tabs with no recency at all, active in the middle.
	for n := 0; n < i.TabCount(); n++ {
		i.tabs[n].deferred = &Deferred{Path: "f" + string(rune('a'+n)) + ".txt"}
		i.tabs[n].lastUsed = 0
	}
	i.active = 2
	idx, ok := i.EvictableLRUTab()
	if !ok || idx != 0 {
		t.Fatalf("equidistant tie must take the lower index, got %d (ok=%v)", idx, ok)
	}
	i.active = 1
	if idx, ok := i.EvictableLRUTab(); !ok || idx != 4 {
		t.Fatalf("the furthest tab from the active one must be picked, got %d (ok=%v)", idx, ok)
	}
	// A recency stamp beats the distance rule.
	i.tabs[4].lastUsed = 3
	i.tabs[3].lastUsed = 2
	i.tabs[2].lastUsed = 1
	i.tabs[0].lastUsed = 4
	if idx, ok := i.EvictableLRUTab(); !ok || idx != 2 {
		t.Fatalf("the least recently used tab must win over distance, got %d (ok=%v)", idx, ok)
	}
}

// TestEvictableLRUTabIncludesDeferredTabs (#2640): a tab restored but never
// activated names a file and holds no edits, so the limit may close it —
// skipping it was what made the eviction recycle the freshly opened slots.
func TestEvictableLRUTabIncludesDeferredTabs(t *testing.T) {
	i := editorInst(t)
	i.AddTab()
	i.tabs[0].deferred = &Deferred{Path: "restored.txt"}
	i.active = 1
	idx, ok := i.EvictableLRUTab()
	if !ok || idx != 0 {
		t.Fatalf("the deferred tab must be evictable, got %d (ok=%v)", idx, ok)
	}
	// A pin still protects it.
	i.SetTabPinned(0, true)
	if _, ok := i.EvictableLRUTab(); ok {
		t.Fatal("a pinned deferred tab must stay exempt")
	}
}
