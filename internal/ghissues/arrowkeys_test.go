package ghissues

import "testing"

// arrowkeys_test.go covers the plain arrow keys of the issues window (#2537):
// up/down walk the selection, left/right walk the two tabs, and an open
// overlay keeps every arrow it owned before the bindings existed.

// TestStepSelectionArrowsFeedTheOverlay: the plain arrows are bound in the
// issues context, so the keymap layer resolves them before the pane sees
// them. With the filter overlay open the commands hand the key back to it —
// the overlay cursor moves and the list cursor stays put.
func TestStepSelectionArrowsFeedTheOverlay(t *testing.T) {
	m := filled(t)
	press(m, "f")
	if !m.PickerOpen() {
		t.Fatal("f must open the filter overlay")
	}
	row, at := m.ovCursor, m.Cursor()

	m.StepSelection(1)

	if m.ovCursor == row {
		t.Fatal("down must move the overlay cursor while the overlay is open")
	}
	if m.Cursor() != at {
		t.Fatalf("list cursor moved to %d, want it untouched at %d", m.Cursor(), at)
	}
}

// TestArrowsKeepTheMatchInput: on the match row the horizontal arrows belong
// to the text input, so the tab command must not walk the tabs while the
// overlay owns the keyboard.
func TestArrowsKeepTheMatchInput(t *testing.T) {
	m := filled(t)
	press(m, "f", "e", "x")
	if !m.Filtering() {
		t.Fatal("the match row must have the keyboard")
	}
	at := m.fInput.Cur

	m.SwitchTabCmd(-1)

	if m.ActiveTab() != TabIssues {
		t.Fatal("left must not change the tab while the match input is focused")
	}
	if m.fInput.Cur != at-1 {
		t.Fatalf("input cursor = %d, want %d (left moves inside the pattern)", m.fInput.Cur, at-1)
	}
	if m.Filter() != "ex" {
		t.Fatalf("pattern = %q, want %q untouched", m.Filter(), "ex")
	}
}

// TestStepSelectionArrowsWalkTheList: without an overlay the vertical arrows
// are the list walk they were bound for.
func TestStepSelectionArrowsWalkTheList(t *testing.T) {
	m := filled(t)
	at := m.Cursor()

	m.StepSelection(1)
	if m.Cursor() == at {
		t.Fatal("down must move the list cursor")
	}
	m.StepSelection(-1)
	if m.Cursor() != at {
		t.Fatalf("cursor = %d after down+up, want %d", m.Cursor(), at)
	}
}

// TestSwitchTabCmdWalksTheTabs: left/right walk the pane's two tabs from the
// list, wrapping like tab / shift+tab.
func TestSwitchTabCmdWalksTheTabs(t *testing.T) {
	m := filled(t)

	m.SwitchTabCmd(1)
	if m.ActiveTab() != TabPRs {
		t.Fatal("right must move to the PRs tab")
	}
	m.SwitchTabCmd(-1)
	if m.ActiveTab() != TabIssues {
		t.Fatal("left must move back to the issues tab")
	}
	m.SwitchTabCmd(-1)
	if m.ActiveTab() != TabPRs {
		t.Fatal("left must wrap to the PRs tab")
	}
}

// TestSwitchTabCmdFromTheDetail: the tab walk keeps one meaning everywhere —
// from an open detail view it leaves the detail and lands on the other tab's
// list, exactly like tab / shift+tab there.
func TestSwitchTabCmdFromTheDetail(t *testing.T) {
	m := filled(t)
	press(m, "enter")
	if !m.DetailOpen() {
		t.Fatal("enter must open the issue detail")
	}

	m.SwitchTabCmd(1)

	if m.DetailOpen() {
		t.Fatal("the detail must close when the tab changes")
	}
	if m.ActiveTab() != TabPRs {
		t.Fatal("right must move to the PRs tab from the detail view")
	}
}
