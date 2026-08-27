package pane

import "testing"

// tabsmru_test.go covers the per-pane most-recently-used tab order (#2151),
// the ranking the tab picker lists a pane's tabs in.

// TestTabsByMRUFollowsActivation: the MRU order is the activation order
// reversed — the active tab leads, then the tabs by descending activation
// stamp — and it updates on every activation.
func TestTabsByMRUFollowsActivation(t *testing.T) {
	i := editorInst(t)
	i.AddTab() // tab 1
	i.AddTab() // tab 2
	i.AddTab() // tab 3, active (AddTab activates)
	i.ActivateTab(1)
	i.ActivateTab(0)
	i.ActivateTab(2)
	want := []int{2, 0, 1, 3}
	got := i.TabsByMRU()
	if len(got) != len(want) {
		t.Fatalf("TabsByMRU must cover every tab: got %v", got)
	}
	for n := range want {
		if got[n] != want[n] {
			t.Fatalf("MRU order %v, want %v", got, want)
		}
	}
	// Re-activating an older tab moves it to the front.
	i.ActivateTab(3)
	if got := i.TabsByMRU(); got[0] != 3 || got[1] != 2 {
		t.Fatalf("after activating tab 3 the order must lead 3,2; got %v", got)
	}
}

// TestTabsByMRUNeverActivatedLast: tabs that were never activated — the
// background tabs a layout restore installs — sort last in tab order, so a
// restored pane's picker still lists everything in a stable order.
func TestTabsByMRUNeverActivatedLast(t *testing.T) {
	i := editorInst(t)
	i.AddTab()
	i.ActivateTab(0)
	// Two tabs appended without activation, as a layout restore leaves them.
	i.tabs = append(i.tabs, newEditorTab(i.Editor()), newEditorTab(i.Editor()))
	want := []int{0, 1, 2, 3}
	got := i.TabsByMRU()
	for n := range want {
		if got[n] != want[n] {
			t.Fatalf("MRU order %v, want %v", got, want)
		}
	}
	if i.TabLastUsed(2) != 0 || i.TabLastUsed(9) != 0 {
		t.Fatal("never-activated and out-of-range tabs must report a zero stamp")
	}
	if i.TabLastUsed(0) <= i.TabLastUsed(1) {
		t.Fatal("the last activated tab must carry the highest stamp")
	}
}
