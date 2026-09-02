package ghissues

// keycmds_test.go covers the bound pane chords of #2400: issues.copy (cmd+c)
// and the ctrl+up / ctrl+down selection walk.

import (
	"strconv"
	"testing"
)

// copyText runs the copy command and reads the resulting CopyMsg.
func copyText(t *testing.T, m *Model) (string, string) {
	t.Helper()
	cmd := m.CopyKeyCmd()
	if cmd == nil {
		return "", ""
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("copy produced %T, want CopyMsg", cmd())
	}
	return msg.Text, msg.What
}

// TestCopyKeyCopiesSelectedIssueURL: without a mouse selection the chord
// copies what the user is looking at — the selected issue's URL.
func TestCopyKeyCopiesSelectedIssueURL(t *testing.T) {
	m := filled(t)
	want := "https://e/" + strconv.Itoa(m.Selected().Number)

	text, what := copyText(t, m)
	if text != want {
		t.Fatalf("copied %q, want the selected issue's URL", text)
	}
	if what != "issue URL" {
		t.Fatalf("what = %q", what)
	}
}

// TestCopyKeyFallsBackToTitle: a forge listing without URLs still copies
// something useful — the number and title.
func TestCopyKeyFallsBackToTitle(t *testing.T) {
	m := filled(t)
	is := m.Selected()
	is.URL = ""
	want := "#" + strconv.Itoa(is.Number) + " " + is.Title

	text, what := copyText(t, m)
	if text != want {
		t.Fatalf("copied %q, want the number and title", text)
	}
	if what != "issue title" {
		t.Fatalf("what = %q", what)
	}
}

// TestCopyKeyPrefersSelection: a live mouse selection is what the pane's own
// "y" copies, and the chord must not overrule it.
func TestCopyKeyPrefersSelection(t *testing.T) {
	selClock(t)
	m := openDetail(t, "alpha beta", "gamma delta")
	if got := dragText(m, 0, 0, 5, 0); got == "" {
		t.Fatal("drag produced no selection")
	}

	text, what := copyText(t, m)
	if what != "selection" {
		t.Fatalf("what = %q, want the mouse selection", what)
	}
	if text == "" {
		t.Fatal("selection copy produced no text")
	}
}

// TestCopyKeyOnPRTab copies the pull request under the PR view's cursor.
func TestCopyKeyOnPRTab(t *testing.T) {
	m := filled(t)
	press(m, "tab")

	text, what := copyText(t, m)
	if text != "https://e/pr/9" || what != "pull request URL" {
		t.Fatalf("copied (%q, %q), want the selected PR's URL", text, what)
	}
}

// TestStepSelectionWalksTheList: ctrl+down / ctrl+up move the list cursor,
// wrapping like every other list step (#1666).
func TestStepSelectionWalksTheList(t *testing.T) {
	m := filled(t)
	start := m.Cursor()

	m.StepSelection(1)
	if m.Cursor() == start {
		t.Fatal("step down did not move the cursor")
	}
	m.StepSelection(-1)
	if m.Cursor() != start {
		t.Fatalf("cursor = %d after down+up, want %d", m.Cursor(), start)
	}
}

// TestStepSelectionWalksTheDetail: with a detail open the step walks the
// shown issue, exactly like the pane's ctrl+j / ctrl+k.
func TestStepSelectionWalksTheDetail(t *testing.T) {
	m := filled(t)
	press(m, "enter")
	if !m.DetailOpen() {
		t.Fatal("enter must open the issue detail")
	}
	before := m.Selected().Number

	m.StepSelection(1)

	if !m.DetailOpen() {
		t.Fatal("the detail must stay open while stepping")
	}
	if m.Selected().Number == before {
		t.Fatalf("shown issue stayed at #%d", before)
	}
}
